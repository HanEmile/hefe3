package main

import (
	"bufio"
	"bytes"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha1"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/json"
	"encoding/pem"
	"flag"
	"fmt"
	"io"
	"log"
	"math/big"
	"net"
	"net/http"
	"net/http/httputil"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

var FlowCounter atomic.Uint64

type Flow struct {
	ID           uint64
	Request      *http.Request
	Response     *http.Response
	RequestBody  []byte
	ResponseBody []byte
	ResponseTime time.Time
	RequestTime  time.Time
}

func (f *Flow) JSON(body bool) string {
	dumpReq, _ := httputil.DumpRequest(f.Request, true)

	var dumpRes []byte
	var status int
	if f.Response != nil {
		dumpRes, _ = httputil.DumpResponse(f.Response, true)
		status = f.Response.StatusCode
	}
	payload := map[string]any{
		"id":           f.ID,
		"request":      string(dumpReq),
		"response":     string(dumpRes),
		"requestTime":  f.RequestTime.Format("2006-01-02 15:04:05 -07:00"),
		"responseTime": f.ResponseTime.Format("2006-01-02 15:04:05 -07:00"),
		"status":       status,
		"method":       f.Request.Method,
		"url":          f.Request.URL.String(),
	}

	if body {
		payload["requestBody"] = string(f.RequestBody)
		payload["responseBody"] = string(f.ResponseBody)
	}
	jsonBytes, _ := json.Marshal(payload)
	return string(jsonBytes)
}

type Listener struct {
	Address *url.URL

	Intercept        bool
	Intercepts       map[uint64]chan bool
	InterceptPayload map[uint64]*http.Request
	InterceptMutex   sync.Mutex

	Flows     map[uint64]Flow
	FlowMutex sync.Mutex

	Hitm *HITM
}

func (l *Listener) NewProxyHTTP() {
	proxyHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodConnect {
			l.HandleConnect(w, r)
		} else {
			handleHTTP(w, r)
		}
	})

	log.Printf("=== HTTP Proxy listening on %s", l.Address.String())
	if err := http.ListenAndServe(l.Address.Host, proxyHandler); err != nil {
		log.Fatalf("HTTP Proxy server %s failed: %v", l.Address.String(), err)
	}

	fmt.Println("Proxy done")
	l.Hitm.DoneChannel <- true
}

func (l *Listener) NewProxyHTTPS() {
	proxyHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodConnect {
			l.HandleConnect(w, r)
		} else {
			handleHTTP(w, r)
		}
	})

	server := &http.Server{
		Addr:           l.Address.Host,
		Handler:        proxyHandler,
		ReadTimeout:    5 * time.Second,
		WriteTimeout:   5 * time.Second,
		IdleTimeout:    120 * time.Second,
		MaxHeaderBytes: 1 << 20,
	}

	if _, err := l.Hitm.EnsureCert(l.Address, "httpsProxy", false); err != nil {
		log.Fatalf("HTTPS Proxy: failed to ensure cert for %s: %v", l.Address.String(), err)
	}

	certPath := l.Hitm.CertFilePath(l.Address, "httpsProxy", false)
	keyPath := l.Hitm.KeyFilePath(l.Address, "httpsProxy", false)

	log.Printf("=== HTTPS Proxy listening on %s with cert %s and key %s", l.Address.String(), certPath, keyPath)
	err := server.ListenAndServeTLS(certPath, keyPath)
	if err != nil {
		log.Fatalf("HTTPS Proxy server %s failed: %v", l.Address.String(), err)
	}

	fmt.Println("Proxy done")
	l.Hitm.DoneChannel <- true
}

func (l *Listener) HandleConnect(w http.ResponseWriter, r *http.Request) {
	targetConn, err := net.Dial("tcp", r.URL.Host)
	if err != nil {
		log.Printf("Failed to dial target %s: %v", r.URL.Host, err)
		http.Error(w, err.Error(), http.StatusServiceUnavailable)
		return
	}
	defer targetConn.Close()

	hijacker, ok := w.(http.Hijacker)
	if !ok {
		log.Print("Hijacking not supported")
		http.Error(w, "Hijacking not supported", http.StatusInternalServerError)
		return
	}

	clientConn, _, err := hijacker.Hijack()
	if err != nil {
		log.Printf("Failed to hijack connection: %v", err)
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	defer clientConn.Close()

	if _, err := clientConn.Write([]byte("HTTP/1.1 200 Connection established\r\n\r\n")); err != nil {
		log.Printf("Error writing CONNECT response to client: %v", err)
		return
	}

	targetHost := r.URL.Host

	tlsConfig := &tls.Config{
		NextProtos: []string{"http/1.1"},
		GetCertificate: func(info *tls.ClientHelloInfo) (*tls.Certificate, error) {
			hostname := info.ServerName
			if hostname == "" {
				h, _, err := net.SplitHostPort(targetHost)
				if err != nil {
					log.Printf("err splitting: %v", err)
					hostname = targetHost
				} else {
					hostname = h
				}
			}

			log.Println("Using name:", hostname)

			url, err := url.Parse(hostname)
			if err != nil {
				log.Printf("Failed to parse server name: %v", err)
				return nil, err
			}

			return l.Hitm.EnsureCert(url, url.String(), false)
		},
	}

	log.Println("TLS Handshake with the client")
	tlsClientConn := tls.Server(clientConn, tlsConfig)
	if err := tlsClientConn.Handshake(); err != nil {
		log.Printf("TLS Handshake failed: %v", err)
		return
	}
	defer tlsClientConn.Close()

	l.ProcessDecryptedTraffic(tlsClientConn, r.URL.Host)

	log.Printf("CONNECT tunnel to %s closed", r.URL.Host)
}

func (l *Listener) ProcessDecryptedTraffic(conn net.Conn, originalHost string) {
	reader := bufio.NewReader(conn)
	req, err := http.ReadRequest(reader)
	if err != nil {
		if err != io.EOF {
			log.Printf("Error reading decrypted request: %v", err)
		}
		return
	}
	logRequest(req, "[MITM-DECRYPTED-CLIENT-REQ]")

	req.URL.Scheme = "https"
	req.URL.Host = originalHost

	reqBodyBytes, _ := io.ReadAll(req.Body)
	req.Body.Close()
	req.Body = io.NopCloser(bytes.NewBuffer(reqBodyBytes))

	id := FlowCounter.Add(1)
	fl := Flow{
		ID:          id,
		Request:     req,
		RequestBody: reqBodyBytes,
		RequestTime: time.Now(),
	}
	l.FlowMutex.Lock()
	l.Flows[id] = fl
	l.FlowMutex.Unlock()
	l.Hitm.OutFlow <- fl
	log.Println("Sent flow to frontend")

	if l.Intercept {
		waitCh := make(chan bool)

		l.InterceptMutex.Lock()
		if l.Intercepts == nil {
			l.Intercepts = make(map[uint64]chan bool)
		}
		l.Intercepts[id] = waitCh
		l.InterceptMutex.Unlock()

		log.Printf("Request %d intercepted", id)

		select {
		case proceed, ok := <-waitCh:
			if !proceed || !ok {
				log.Printf("Request %d dropped by user", id)
				return
			}

			l.InterceptMutex.Lock()
			if updated := l.InterceptPayload[id]; updated != nil {
				updated.URL.Scheme = "https"
				updated.URL.Host = originalHost
				updated.RequestURI = ""
				req = updated
			}
			delete(l.Intercepts, id)
			delete(l.InterceptPayload, id)
			l.InterceptMutex.Unlock()

		case <-time.After(2 * time.Minute):
			log.Printf("Intercept timed out for ID %d", id)
			l.InterceptMutex.Lock()
			delete(l.Intercepts, id)
			l.InterceptMutex.Unlock()
			return
		}
	}

	caCertPool, err := x509.SystemCertPool()
	if err != nil {
		log.Println("Failed to get system cert pool")
		caCertPool = x509.NewCertPool()
	}
	caCertPool.AddCert(l.Hitm.CACert)

	var mitmTransport = &http.Transport{
		TLSClientConfig: &tls.Config{
			RootCAs:            caCertPool,
			InsecureSkipVerify: false,
		},
	}

	req.RequestURI = ""

	resp, err := mitmTransport.RoundTrip(req)
	if err != nil {
		log.Printf("Error forwarding request: %v", err)
		errMsg := "HTTP/1.1 502 Bad Gateway\r\nContent-Type: text/plain\r\nConnection: close\r\n\r\n" + err.Error()
		conn.Write([]byte(errMsg))
		return
	}

	respBodyBytes, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	resp.Body = io.NopCloser(bytes.NewBuffer(respBodyBytes))

	fl.Response = resp
	fl.ResponseBody = respBodyBytes
	fl.ResponseTime = time.Now()

	l.FlowMutex.Lock()
	l.Flows[id] = fl
	l.FlowMutex.Unlock()

	l.Hitm.OutFlow <- fl

	logRequest(req, "[MITM-REQ]")
	logRequest(resp, "[MITM-RES]")

	resp.Write(conn)
	resp.Body.Close()
}

type HITM struct {
	DoneChannel chan bool

	CertDir string

	CACert *x509.Certificate
	CAKey  *ecdsa.PrivateKey

	OutFlow chan Flow

	BackendAddress url.URL

	mux *http.ServeMux

	Listeners   map[string]*Listener
	ListenMutex sync.RWMutex

	Connections map[string]http.ResponseWriter
	ConnMutex   sync.Mutex
}

func (s *HITM) getListener(name string) *Listener {
	s.ListenMutex.RLock()
	defer s.ListenMutex.RUnlock()

	if name != "" {
		if l, ok := s.Listeners[name]; ok {
			return l
		}
		if !strings.HasPrefix(name, "http") {
			if l, ok := s.Listeners["http://"+name]; ok {
				return l
			}
		}
	}
	// Fallback mechanism to find primary proxy listener
	for _, l := range s.Listeners {
		if l.Address.Host != "127.0.0.1:8080" && l.Address.Host != "0.0.0.0:8080" {
			return l
		}
	}
	return nil
}

func (h *HITM) KeyFilePath(url *url.URL, commonName string, isCA bool) string {
	if isCA {
		return fmt.Sprintf("%s/%s-ca-key.pem", h.CertDir, clean(commonName))
	}
	return fmt.Sprintf("%s/%s-%s-key.pem", h.CertDir, clean(commonName), clean(url.Hostname()))
}

func (h *HITM) CertFilePath(url *url.URL, commonName string, isCA bool) string {
	if isCA {
		return fmt.Sprintf("%s/%s-ca-cert.pem", h.CertDir, clean(commonName))
	}
	return fmt.Sprintf("%s/%s-%s-cert.pem", h.CertDir, clean(commonName), clean(url.Hostname()))
}

func (h *HITM) GenOrReadPrivKey(url *url.URL, commonName string, isCA bool) (*ecdsa.PrivateKey, error) {
	keyfilePath := h.KeyFilePath(url, commonName, isCA)

	var privateKey *ecdsa.PrivateKey
	if keyFile, err := os.ReadFile(keyfilePath); err != nil {
		var err error
		privateKey, err = ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
		if err != nil {
			panic(err)
		}

		keyOut, err := os.OpenFile(keyfilePath, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0600)
		if err != nil {
			return nil, fmt.Errorf("failed to open the %s file for writing: %v", keyfilePath, err)
		}
		defer keyOut.Close()

		privKeyBytes, err := x509.MarshalECPrivateKey(privateKey)
		if err != nil {
			return nil, fmt.Errorf("failed to marshal EC private key: %v", err)
		}
		pem.Encode(keyOut, &pem.Block{Type: "EC PRIVATE KEY", Bytes: privKeyBytes})

		log.Printf("%s wrote new private key to %s", url.String(), keyfilePath)
	} else {
		block, _ := pem.Decode(keyFile)
		if block == nil {
			return nil, fmt.Errorf("failed to parse PEM block containing the key")
		}

		privateKey, err = x509.ParseECPrivateKey(block.Bytes)
		if err != nil {
			return nil, fmt.Errorf("failed to parse private key: %v", err)
		}
		log.Printf("%s Using existing private key: %s", url.String(), keyfilePath)
	}

	return privateKey, nil
}

func (h *HITM) EnsureCert(u *url.URL, commonName string, isCA bool) (*tls.Certificate, error) {
	log.Printf("EnsureCert: hostname=%s, commonName=%s, isCA=%t", u.Hostname(), commonName, isCA)

	for !isCA && h.CACert == nil {
		log.Println("Waiting for root CA to be initialized...")
		time.Sleep(1 * time.Second)
	}

	privKey, err := h.GenOrReadPrivKey(u, commonName, isCA)
	if err != nil {
		return nil, fmt.Errorf("failed to generate or read private key: %v", err)
	}

	certPath := h.CertFilePath(u, commonName, isCA)
	var derBytes []byte

	if cachedBytes, err := os.ReadFile(certPath); err == nil {
		block, _ := pem.Decode(cachedBytes)
		if block != nil {
			derBytes = block.Bytes
			parsed, err := x509.ParseCertificate(derBytes)
			if err != nil || derBytes == nil {
				return nil, fmt.Errorf("failed to parse certificate: %v", err)
			}
			if isCA {
				h.CACert = parsed
				h.CAKey = privKey
			}
		}
	}

	hostname := u.Hostname()
	if hostname == "" {
		hostname = u.Host
	}
	if h, _, err := net.SplitHostPort(hostname); err == nil {
		hostname = h
	}
	if hostname == "" {
		hostname = commonName
	}

	if derBytes == nil {
		serialNumber, err := rand.Int(rand.Reader, new(big.Int).Lsh(big.NewInt(1), 128))
		if err != nil {
			return nil, fmt.Errorf("failed to generate serial number: %v", err)
		}

		template := x509.Certificate{
			SerialNumber: serialNumber,
			Subject: pkix.Name{
				Organization: []string{"HITM Proxy"},
				CommonName:   hostname,
			},
			NotBefore:             time.Now().Add(-1 * time.Hour),
			NotAfter:              time.Now().AddDate(1, 0, 0),
			BasicConstraintsValid: true,
			IsCA:                  isCA,
		}

		var parent *x509.Certificate
		var signerKey *ecdsa.PrivateKey

		if isCA {
			template.KeyUsage = x509.KeyUsageCertSign | x509.KeyUsageDigitalSignature
			ski := hashKeyId(&privKey.PublicKey)
			if ski != nil {
				template.SubjectKeyId = ski
				template.AuthorityKeyId = ski
			}
			template.Subject.CommonName = commonName
			parent = &template
			signerKey = privKey
		} else {
			if h.CACert == nil {
				return nil, fmt.Errorf("root CA not initialized")
			}
			template.Subject.CommonName = hostname
			template.KeyUsage = x509.KeyUsageDigitalSignature
			template.ExtKeyUsage = []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth, x509.ExtKeyUsageClientAuth}
			template.DNSNames = []string{hostname}
			if ip := net.ParseIP(hostname); ip != nil {
				template.IPAddresses = []net.IP{ip}
			}
			parent = h.CACert
			signerKey = h.CAKey
		}

		derBytes, err = x509.CreateCertificate(rand.Reader, &template, parent, &privKey.PublicKey, signerKey)
		if err != nil {
			return nil, fmt.Errorf("failed to create certificate: %v", err)
		}

		f, err := os.Create(certPath)
		if err != nil {
			return nil, fmt.Errorf("failed to create certificate file: %v", err)
		}
		pem.Encode(f, &pem.Block{Type: "CERTIFICATE", Bytes: derBytes})
		f.Close()

		parsed, err := x509.ParseCertificate(derBytes)
		if err != nil {
			return nil, fmt.Errorf("failed to parse certificate: %v", err)
		}
		if isCA {
			h.CACert = parsed
			h.CAKey = privKey
		}
	}

	return &tls.Certificate{
		Certificate: [][]byte{derBytes},
		PrivateKey:  privKey,
	}, nil
}

func logRequest(r any, prefix string) {
	switch v := r.(type) {
	case *http.Request:
		if v == nil {
			log.Printf("%s Request: <nil>", prefix)
			return
		}
		log.Printf("%s Request: %s %s %s", prefix, v.Method, v.URL.String(), v.Proto)
		log.Printf("%s Host: %s", prefix, v.Host)
		for name, headers := range v.Header {
			for _, h := range headers {
				log.Printf("%s Header: %q: %q", prefix, name, h)
			}
		}
	case http.Request:
		log.Printf("%s Request: %s %s %s", prefix, v.Method, v.URL.String(), v.Proto)
		log.Printf("%s Host: %s", prefix, v.Host)
		for name, headers := range v.Header {
			for _, h := range headers {
				log.Printf("%s Header: %q: %q", prefix, name, h)
			}
		}
	case *http.Response:
		if v == nil {
			log.Printf("%s Response: <nil>", prefix)
			return
		}
		log.Printf("%s Response Status: %s", prefix, v.Status)
		for name, headers := range v.Header {
			for _, h := range headers {
				log.Printf("%s Header: %q: %q", prefix, name, h)
			}
		}
	case http.Response:
		log.Printf("%s Response Status: %s", prefix, v.Status)
		for name, headers := range v.Header {
			for _, h := range headers {
				log.Printf("%s Header: %q: %q", prefix, name, h)
			}
		}
	}
}

func hashKeyId(pub any) []byte {
	var publicBytes []byte
	switch k := pub.(type) {
	case *ecdsa.PublicKey:
		publicBytes = elliptic.Marshal(k.Curve, k.X, k.Y)
	default:
		return nil
	}
	h := sha1.New()
	h.Write(publicBytes)
	return h.Sum(nil)
}

func clean(input string) string {
	regexPattern := "[^a-zA-Z0-9-]+"
	reg := regexp.MustCompile(regexPattern)
	return reg.ReplaceAllString(input, "-")
}

func (h *HITM) NewListener(u string, name string, isProxy bool) *Listener {
	if !strings.HasPrefix(u, "http") {
		u = "http://" + u
	}
	parsedURL, err := url.Parse(u)
	if err != nil {
		log.Printf("NewListener: error parsing url: %+v", err)
	}

	l := Listener{
		Address: parsedURL,
		Hitm:    h,
	}

	if isProxy {
		l.Intercepts = make(map[uint64]chan bool)
		l.InterceptPayload = make(map[uint64]*http.Request)
		l.Flows = make(map[uint64]Flow)
	}

	h.ListenMutex.Lock()
	h.Listeners[parsedURL.String()] = &l
	h.ListenMutex.Unlock()
	return &l
}

func (s *HITM) Backend() {
	_, err := s.EnsureCert(&s.BackendAddress, "Backend", false)
	if err != nil {
		log.Fatalf("Failed to generate Cert: %v", err)
		return
	}

	// FIX: Centralized Goroutine drains outFlow and flushes to all listeners securely
	go func() {
		for f := range s.OutFlow {
			s.ConnMutex.Lock()
			payload := f.JSON(true)
			for _, conn := range s.Connections {
				fmt.Fprintf(conn, "data: %s\n\n", payload)
				if flusher, ok := conn.(http.Flusher); ok {
					flusher.Flush()
				}
			}
			s.ConnMutex.Unlock()
		}
	}()

	mux := http.NewServeMux()
	s.mux = mux

	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		http.ServeFile(w, r, "./static/index.html")
	})

	mux.Handle("/static/", http.StripPrefix("/static/", http.FileServer(http.Dir("./static"))))

	mux.HandleFunc("/repeat", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
			return
		}

		raw, _ := io.ReadAll(r.Body)
		req, err := http.ReadRequest(bufio.NewReader(strings.NewReader(string(raw))))
		if err != nil {
			fmt.Fprintf(w, "Error parsing request: %v", err)
			return
		}

		req.RequestURI = ""
		if req.URL.Scheme == "" {
			req.URL.Scheme = "https"
		}
		if req.URL.Host == "" {
			req.URL.Host = req.Host
		}

		// FIX: Load the host system's root certificate pool instead of a blank one
		caCertPool, err := x509.SystemCertPool()
		if err != nil {
			log.Println("Failed to get system cert pool, falling back to empty pool")
			caCertPool = x509.NewCertPool()
		}
		caCertPool.AddCert(s.CACert)

		transport := &http.Transport{
			TLSClientConfig: &tls.Config{
				RootCAs:            caCertPool,
				InsecureSkipVerify: false,
			},
			DisableCompression: true,
		}

		client := &http.Client{
			Transport: transport,
		}

		resp, err := client.Do(req)
		if err != nil {
			fmt.Fprintf(w, "Error sending: %v", err)
			return
		}
		defer resp.Body.Close()

		dump, err := httputil.DumpResponse(resp, true)
		if err != nil {
			log.Printf("[/Repeat] Failed to dump response: %v", err)
			return
		}
		w.Write(dump)
	})

	mux.HandleFunc("/flow", func(w http.ResponseWriter, r *http.Request) {
		idstr := r.URL.Query().Get("id")
		listener := r.URL.Query().Get("l")

		id, err := strconv.ParseUint(idstr, 10, 64)
		if err != nil {
			fmt.Fprintf(w, "Error parsing request: %v", err)
			return
		}

		l := s.getListener(listener)
		if l == nil {
			http.Error(w, "Listener not found", http.StatusNotFound)
			return
		}

		l.FlowMutex.Lock()
		f, ok := l.Flows[id]
		l.FlowMutex.Unlock()
		if ok {
			w.Header().Set("Content-Type", "application/json")
			fmt.Fprintf(w, "%s", f.JSON(true))
			return
		}
		http.Error(w, "Flow not found", 404)
	})

	mux.HandleFunc("/events", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.Header().Set("Cache-Control", "no-cache")
		w.Header().Set("Connection", "keep-alive")

		log.Printf("Client connected, adding %s to Connections (%d active connections)", r.RemoteAddr, len(s.Connections)+1)
		s.ConnMutex.Lock()
		s.Connections[r.RemoteAddr] = w
		s.ConnMutex.Unlock()

		<-r.Context().Done()

		log.Printf("Client disconnected, removing %s", r.RemoteAddr)
		s.ConnMutex.Lock()
		delete(s.Connections, r.RemoteAddr)
		s.ConnMutex.Unlock()
	})

	mux.HandleFunc("/history", func(w http.ResponseWriter, r *http.Request) {
		listenerName := r.URL.Query().Get("l")

		l := s.getListener(listenerName)
		if l == nil {
			http.Error(w, "Listener not found", http.StatusNotFound)
			return
		}

		var history []map[string]any
		l.FlowMutex.Lock()
		for _, f := range l.Flows {
			var item map[string]any
			if err := json.Unmarshal([]byte(f.JSON(false)), &item); err == nil {
				history = append(history, item)
			}
		}
		l.FlowMutex.Unlock()

		w.Header().Set("Content-Type", "application/json")
		if len(history) == 0 {
			w.Write([]byte("[]"))
			return
		}
		json.NewEncoder(w).Encode(history)
	})

	mux.HandleFunc("/intercept", func(w http.ResponseWriter, r *http.Request) {
		listener := r.URL.Query().Get("l")

		if r.Method == http.MethodPost {
			enable := r.URL.Query().Get("enable")
			l := s.getListener(listener)
			if l == nil {
				http.Error(w, "Listener not found", http.StatusNotFound)
				return
			}
			switch enable {
			case "true":
				l.Intercept = true
				log.Println("Intercept enabled")
			case "false":
				l.Intercept = false
				log.Println("Intercept disabled")
			}
		}
	})

	mux.HandleFunc("/forward", func(w http.ResponseWriter, r *http.Request) {
		listener := r.URL.Query().Get("l")

		if r.Method != http.MethodPost {
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
			return
		}

		id, _ := strconv.ParseUint(r.URL.Query().Get("id"), 10, 64)

		raw, _ := io.ReadAll(r.Body)
		updatedReq, _ := http.ReadRequest(bufio.NewReader(bytes.NewReader(raw)))

		l := s.getListener(listener)
		if l == nil {
			http.Error(w, "Listener not found", http.StatusNotFound)
			return
		}

		l.InterceptMutex.Lock()
		ch, ok := l.Intercepts[id]
		if ok {
			if l.InterceptPayload == nil {
				l.InterceptPayload = make(map[uint64]*http.Request)
			}
			l.InterceptPayload[id] = updatedReq
			l.InterceptMutex.Unlock()
			ch <- true
			fmt.Fprint(w, "Forwarded")
		} else {
			l.InterceptMutex.Unlock()
			http.Error(w, "Flow not found or not intercepted", 404)
		}
	})

	mux.HandleFunc("/drop", func(w http.ResponseWriter, r *http.Request) {
		listener := r.URL.Query().Get("l")

		id, _ := strconv.ParseUint(r.URL.Query().Get("id"), 10, 64)

		l := s.getListener(listener)
		if l == nil {
			http.Error(w, "Listener not found", http.StatusNotFound)
			return
		}

		l.InterceptMutex.Lock()
		if ch, ok := l.Intercepts[id]; ok {
			ch <- false
		}
		l.InterceptMutex.Unlock()
	})

	mux.HandleFunc("/ca/list", func(w http.ResponseWriter, r *http.Request) {
		files, err := os.ReadDir(s.CertDir)
		if err != nil {
			http.Error(w, "Could not read certs directory", 500)
			return
		}

		var certList []string
		for _, f := range files {
			if !f.IsDir() {
				certList = append(certList, f.Name())
			}
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(certList)
	})

	mux.HandleFunc("/ca/download", func(w http.ResponseWriter, r *http.Request) {
		name := r.URL.Query().Get("name")
		safeName := filepath.Base(name)
		path := filepath.Join(s.CertDir, safeName)

		w.Header().Set("Content-Disposition", "attachment; filename="+safeName)
		http.ServeFile(w, r, path)
	})

	mux.HandleFunc("/newListener", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}

		listenAddress := r.URL.Query().Get("addr")

		switch r.URL.Query().Get("type") {
		case "http":
			l := s.NewListener(listenAddress, "http-proxy", true)
			go l.NewProxyHTTP()
		case "https":
			l := s.NewListener(listenAddress, "https-proxy", true)
			go l.NewProxyHTTPS()
		}

		w.WriteHeader(http.StatusOK)
	})

	server := http.Server{
		Addr:    s.BackendAddress.Host,
		Handler: s.mux,
	}

	certPath := s.CertFilePath(&s.BackendAddress, "backend", false)
	keyPath := s.KeyFilePath(&s.BackendAddress, "backend", false)

	log.Printf("=== Backend listening on %s with cert %s and key %s", s.BackendAddress.String(), certPath, keyPath)
	err = server.ListenAndServeTLS(certPath, keyPath)
	if err != nil {
		log.Fatalf("Backend server failed: %v", err)
	}

	s.DoneChannel <- true
}

func handleHTTP(w http.ResponseWriter, r *http.Request) {
	targetURL := r.URL
	log.Println("handleHTTP", r.URL)
	if !targetURL.IsAbs() {
		targetURL = &url.URL{
			Scheme:   "http",
			Host:     r.Host,
			Path:     r.URL.Path,
			RawQuery: r.URL.RawQuery,
		}
		log.Printf("Constructed target URL for non-absolute request: %s", targetURL.String())
	}
	proxy := &httputil.ReverseProxy{
		Director: func(req *http.Request) {
			req.URL.Scheme = targetURL.Scheme
			req.URL.Host = targetURL.Host
			req.Host = targetURL.Host
			req.Header.Set("X-Forwarded-For", r.RemoteAddr)
			logRequest(req, "Proxy -> Target")
		},
		ModifyResponse: func(resp *http.Response) error {
			logRequest(resp, "Target -> Proxy")
			return nil
		},
		ErrorHandler: func(rw http.ResponseWriter, req *http.Request, err error) {
			log.Printf("HTTP Proxy error for request %s %s: %v", req.Method, req.URL.String(), err)
			http.Error(rw, "Bad Gateway", http.StatusBadGateway)
		},
	}

	proxy.ServeHTTP(w, r)
}

type Service struct {
	Name        string
	Entrypoint  func()
	DoneChannel chan bool
}

func main() {
	h := &HITM{
		DoneChannel: make(chan bool),
		CertDir:     "./certs",
		Listeners:   make(map[string]*Listener),
		Connections: make(map[string]http.ResponseWriter),
		OutFlow:     make(chan Flow, 100),
	}

	var BackendAddressRaw string
	flag.StringVar(&BackendAddressRaw, "l", "0.0.0.0:8080", "Listen address for the backend")
	flag.StringVar(&h.CertDir, "c", "./certs", "Directory containing TLS certificates")
	flag.Parse()

	BackendURL, err := url.Parse("https://" + BackendAddressRaw)
	if err != nil {
		log.Printf("error parsing backend address: %v", err)
	}
	h.BackendAddress = *BackendURL

	_, err = h.EnsureCert(BackendURL, "HITM Proxy CA", true)
	if err != nil {
		log.Printf("Failed to generate CA: %v", err)
	}

	h.NewListener("127.0.0.1:8080", "backend", false)

	httpProxy := h.NewListener("127.0.0.1:9001", "http-proxy", true)
	go httpProxy.NewProxyHTTP()

	httpsProxy := h.NewListener("127.0.0.1:9002", "https-proxy", true)
	go httpsProxy.NewProxyHTTPS()

	services := []Service{
		{
			Name:        "frontend",
			Entrypoint:  h.Backend,
			DoneChannel: h.DoneChannel,
		},
	}

	log.Println("Starting all services")
	for i, service := range services {
		log.Printf("%d/%d: Starting %s", i+1, len(services), service.Name)
		go service.Entrypoint()
	}

	for i := 0; i < len(services); i++ {
		<-h.DoneChannel
	}
}
