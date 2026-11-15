package main

import (
	"bufio"
	"bytes"
	"context"
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
	"os/signal"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"time"
)

var FlowCounter atomic.Uint64
var JobCounter atomic.Uint64
var cleanRegex = regexp.MustCompile("[^a-zA-Z0-9-]+")

const MaxFlows = 10000

type Job struct {
	ID        uint64             `json:"id"`
	Type      string             `json:"type"`
	Status    string             `json:"status"`
	CreatedAt time.Time          `json:"createdAt"`
	Total     int                `json:"total"`
	Done      atomic.Int64       `json:"-"`
	Results   []FuzzResult       `json:"-"`
	ResultsMu sync.Mutex         `json:"-"`
	Config    any                `json:"config"` // FuzzConfig for fuzz jobs, SpiderConfig for spider jobs
	Cancel    context.CancelFunc `json:"-"`
}

func (j *Job) MarshalSummary() map[string]any {
	j.ResultsMu.Lock()
	resultCount := len(j.Results)
	j.ResultsMu.Unlock()
	return map[string]any{
		"id":        j.ID,
		"type":      j.Type,
		"status":    j.Status,
		"createdAt": j.CreatedAt.Format("2006-01-02 15:04:05"),
		"total":     j.Total,
		"done":      j.Done.Load(),
		"results":   resultCount,
	}
}

// FuzzConfig holds the full ffuf-style configuration for a fuzzing job.
type FuzzConfig struct {
	// Core
	RawRequest string   `json:"rawRequest"`
	Marker     string   `json:"marker"`
	Payloads   []string `json:"payloads"`
	Host       string   `json:"host"`
	Scheme     string   `json:"scheme"`
	Listener   string   `json:"listener"`

	// HTTP options
	Headers     []string `json:"headers"`     // -H "Name: Value"
	Method      string   `json:"method"`      // -X
	Cookies     string   `json:"cookies"`     // -b
	Data        string   `json:"data"`        // -d POST data
	HTTP2       bool     `json:"http2"`       // -http2
	IgnoreBody  bool     `json:"ignoreBody"`  // -ignore-body
	FollowRedir bool     `json:"followRedir"` // -r
	SNI         string   `json:"sni"`         // -sni
	TimeoutSec  int      `json:"timeoutSec"`  // -timeout (default 10)
	URL         string   `json:"url"`         // -u (used when no raw request)
	ProxyURL    string   `json:"proxyURL"`    // -x

	// General options
	Threads    int     `json:"threads"`    // -t (default 40)
	Rate       int     `json:"rate"`       // -rate (req/s, 0 = unlimited)
	DelayMin   float64 `json:"delayMin"`   // -p min delay seconds
	DelayMax   float64 `json:"delayMax"`   // -p max delay seconds
	MaxTimeSec int     `json:"maxTimeSec"` // -maxtime (0 = unlimited)
	StopOn403  bool    `json:"stopOn403"`  // -sf
	StopOnErr  bool    `json:"stopOnErr"`  // -se

	// Matcher options (a result "matches" if it passes matchers and isn't filtered)
	MatchCodes string `json:"matchCodes"` // -mc (default 200-299,301,302,307,401,403,405,500)
	MatchLines string `json:"matchLines"` // -ml
	MatchWords string `json:"matchWords"` // -mw
	MatchSize  string `json:"matchSize"`  // -ms
	MatchRegex string `json:"matchRegex"` // -mr
	MatchMode  string `json:"matchMode"`  // -mmode and|or

	// Filter options
	FilterCodes string `json:"filterCodes"` // -fc
	FilterLines string `json:"filterLines"` // -fl
	FilterWords string `json:"filterWords"` // -fw
	FilterSize  string `json:"filterSize"`  // -fs
	FilterRegex string `json:"filterRegex"` // -fr
	FilterMode  string `json:"filterMode"`  // -fmode and|or

	// Input options
	Extensions string `json:"extensions"` // -e .php,.html
	Mode       string `json:"mode"`       // -mode clusterbomb|pitchfork|sniper
}

// FuzzResult is one row of fuzzing output.
type FuzzResult struct {
	Payload    string `json:"payload"`
	StatusCode int    `json:"statusCode"`
	Length     int    `json:"length"`
	Words      int    `json:"words"`
	Lines      int    `json:"lines"`
	Duration   int64  `json:"durationMs"`
	Matched    bool   `json:"matched"`
	Error      string `json:"error,omitempty"`

	// bodyForRegex holds the response body during matcher evaluation; not persisted/serialized.
	bodyForRegex string `json:"-"`
}

type Flow struct {
	ID           uint64
	Source       string
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
		"source":       f.Source,
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

// persist renders a Flow into its on-disk PersistedFlow form.
func (f *Flow) persist() *PersistedFlow {
	dumpReq, _ := httputil.DumpRequest(f.Request, true)

	p := &PersistedFlow{
		id:          f.ID,
		source:      f.Source,
		method:      f.Request.Method,
		url:         f.Request.URL.String(),
		requestDump: dumpReq,
		requestBody: f.RequestBody,
		requestTime: f.RequestTime.Format("2006-01-02 15:04:05 -07:00"),
	}
	if f.Response != nil {
		dumpRes, _ := httputil.DumpResponse(f.Response, true)
		p.responseDump = dumpRes
		p.responseBody = f.ResponseBody
		p.status = f.Response.StatusCode
		p.responseTime = f.ResponseTime.Format("2006-01-02 15:04:05 -07:00")
		p.hasResponse = 1
	}
	return p
}

type Listener struct {
	Address *url.URL

	Intercept        atomic.Bool
	Intercepts       map[uint64]chan bool
	InterceptPayload map[uint64]*http.Request
	InterceptMutex   sync.Mutex

	Flows          map[uint64]*Flow
	PersistedFlows map[uint64]*PersistedFlow // restored from disk at startup
	FlowMutex      sync.Mutex

	// nextEvictID is the cursor for O(1) eviction: flow ids are monotonic
	// (FlowCounter), so the oldest live flow is at or after this id. We advance
	// past already-deleted ids and delete the first one we find. Guarded by
	// FlowMutex.
	nextEvictID uint64

	Hitm *HITM
}

// evictOldFlows removes the oldest flows when the map exceeds MaxFlows. Caller must hold FlowMutex.
func (l *Listener) evictOldFlows() {
	if len(l.Flows) <= MaxFlows {
		return
	}
	// Flow ids are monotonically increasing (FlowCounter), so the oldest live
	// flow is the smallest id present. Advance the cursor past ids that were
	// already evicted/never inserted, then delete the first one we hit. This is
	// amortized O(1) instead of an O(n) min-scan per insert.
	for {
		if _, ok := l.Flows[l.nextEvictID]; ok {
			delete(l.Flows, l.nextEvictID)
			l.nextEvictID++
			return
		}
		l.nextEvictID++
		// Safety: if the cursor somehow ran past everything (shouldn't happen, but
		// guards against an infinite loop on inconsistent state), fall back to a
		// single min-scan to re-anchor it.
		if l.nextEvictID > FlowCounter.Load() {
			var minID uint64
			first := true
			for id := range l.Flows {
				if first || id < minID {
					minID, first = id, false
				}
			}
			if !first {
				delete(l.Flows, minID)
				l.nextEvictID = minID + 1
			}
			return
		}
	}
}

func (l *Listener) NewProxyHTTP() {
	proxyHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodConnect {
			l.HandleConnect(w, r)
		} else {
			l.handleHTTP(w, r)
		}
	})

	log.Printf("=== HTTP Proxy listening on %s", l.Address.String())
	if err := http.ListenAndServe(l.Address.Host, proxyHandler); err != nil {
		log.Fatalf("HTTP Proxy server %s failed: %v", l.Address.String(), err)
	}
}

func (l *Listener) NewProxyHTTPS() {
	proxyHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodConnect {
			l.HandleConnect(w, r)
		} else {
			l.handleHTTP(w, r)
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
	if err := server.ListenAndServeTLS(certPath, keyPath); err != nil {
		log.Fatalf("HTTPS Proxy server %s failed: %v", l.Address.String(), err)
	}
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

	caCertPool, err := x509.SystemCertPool()
	if err != nil {
		log.Println("Failed to get system cert pool")
		caCertPool = x509.NewCertPool()
	}
	caCertPool.AddCert(l.Hitm.CACert)

	// The proxy's own CA is added to the pool so upstream certs signed by it are trusted.
	// This is intentional for the MITM flow — not a bypass of certificate verification.
	mitmTransport := &http.Transport{
		TLSClientConfig: &tls.Config{
			RootCAs: caCertPool,
		},
	}

	for {
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
		fl := &Flow{
			ID:          id,
			Source:      "proxy",
			Request:     req,
			RequestBody: reqBodyBytes,
			RequestTime: time.Now(),
		}
		l.FlowMutex.Lock()
		l.Flows[id] = fl
		l.evictOldFlows()
		l.FlowMutex.Unlock()
		l.Hitm.OutFlow <- *fl
		log.Println("Sent flow to frontend")

		if l.Intercept.Load() {
			waitCh := make(chan bool)

			l.InterceptMutex.Lock()
			l.Intercepts[id] = waitCh
			l.InterceptMutex.Unlock()

			log.Printf("Request %d intercepted", id)

			select {
			case proceed, ok := <-waitCh:
				if !proceed || !ok {
					log.Printf("Request %d dropped by user", id)
					continue
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
				continue
			}
		}

		req.RequestURI = ""

		resp, err := mitmTransport.RoundTrip(req)
		if err != nil {
			log.Printf("Error forwarding request: %v", err)
			errMsg := "HTTP/1.1 502 Bad Gateway\r\nContent-Type: text/plain\r\nConnection: close\r\n\r\n" + err.Error()
			conn.Write([]byte(errMsg))
			continue
		}

		respBodyBytes, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		resp.Body = io.NopCloser(bytes.NewBuffer(respBodyBytes))

		fl.Response = resp
		fl.ResponseBody = respBodyBytes
		fl.ResponseTime = time.Now()

		l.Hitm.OutFlow <- *fl

		logRequest(req, "[MITM-REQ]")
		logRequest(resp, "[MITM-RES]")

		resp.Write(conn)
	}
}

type HITM struct {
	CertDir string

	Store *Store

	Jobs    map[uint64]*Job
	JobsMu  sync.RWMutex

	CACert  *x509.Certificate
	CAKey   *ecdsa.PrivateKey
	CAReady chan struct{}

	CertLocks sync.Map

	OutFlow chan Flow

	BackendAddress url.URL

	mux *http.ServeMux

	Listeners   map[string]*Listener
	ListenMutex sync.RWMutex

	Connections map[string]chan string
	ConnMutex   sync.Mutex

	// /map graph cache. mapGen is bumped by the OutFlow drain on every persisted
	// flow; the cache is keyed by source filter and only valid while its
	// generation matches mapGen. This makes repeated /map calls O(1).
	mapCacheMu  sync.RWMutex
	mapCache    map[string][]byte
	mapCacheGen uint64
	mapGen      atomic.Uint64

	// mapStore holds the immutable Map v2 snapshot (mapsnapshot.go),
	// rebuilt single-flight when mapGen advances.
	mapStore *MapStore
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
	lockKey := commonName + ":" + u.Hostname()
	val, _ := h.CertLocks.LoadOrStore(lockKey, &sync.Mutex{})
	mu := val.(*sync.Mutex)
	mu.Lock()
	defer mu.Unlock()

	log.Printf("EnsureCert: hostname=%s, commonName=%s, isCA=%t", u.Hostname(), commonName, isCA)

	if !isCA {
		<-h.CAReady
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
	if hostOnly, _, err := net.SplitHostPort(hostname); err == nil {
		hostname = hostOnly
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
	return cleanRegex.ReplaceAllString(input, "-")
}

// normalizeRawRequest fixes up a raw HTTP request typed in a textarea so that
// http.ReadRequest can parse it. Browsers/textareas use bare "\n" line endings
// and often omit the blank line terminating the headers, which makes
// http.ReadRequest fail with "unexpected EOF". We normalize all line endings to
// CRLF and guarantee a header-terminating blank line.
func normalizeRawRequest(raw string) string {
	// Normalize CRLF/CR/LF to a single \n first, then re-emit CRLF.
	raw = strings.ReplaceAll(raw, "\r\n", "\n")
	raw = strings.ReplaceAll(raw, "\r", "\n")

	// Split headers from body on the first blank line (if any).
	var head, body string
	if idx := strings.Index(raw, "\n\n"); idx != -1 {
		head = raw[:idx]
		body = raw[idx+2:]
	} else {
		head = strings.TrimRight(raw, "\n")
		body = ""
	}

	headLines := strings.Split(head, "\n")
	for i := range headLines {
		headLines[i] = strings.TrimRight(headLines[i], " \t")
	}
	rebuilt := strings.Join(headLines, "\r\n")

	// Always terminate the header block with a blank line.
	rebuilt += "\r\n\r\n"
	if body != "" {
		rebuilt += body
	}
	return rebuilt
}

func (h *HITM) NewListener(u string, name string, isProxy bool) *Listener {
	if !strings.HasPrefix(u, "http") {
		u = "http://" + u
	}
	parsedURL, err := url.Parse(u)
	if err != nil {
		log.Fatalf("NewListener: error parsing url %q: %v", u, err)
	}

	l := Listener{
		Address:        parsedURL,
		Hitm:           h,
		PersistedFlows: make(map[uint64]*PersistedFlow),
	}

	if isProxy {
		l.Intercepts = make(map[uint64]chan bool)
		l.InterceptPayload = make(map[uint64]*http.Request)
		l.Flows = make(map[uint64]*Flow)
	}

	key := parsedURL.String()
	h.ListenMutex.Lock()
	h.Listeners[key] = &l
	h.ListenMutex.Unlock()

	if isProxy {
		h.Store.SaveListener(key, parsedURL.String(), name, isProxy)
	}
	return &l
}

func (s *HITM) Backend() {
	_, err := s.EnsureCert(&s.BackendAddress, "backend", false)
	if err != nil {
		log.Fatalf("Failed to generate Cert: %v", err)
		return
	}

	// FIX: Centralized Goroutine drains outFlow, persists each flow, and flushes to all listeners securely
	go func() {
		for f := range s.OutFlow {
			fl := f
			s.Store.SaveFlow(&fl)
			// Invalidate the /map graph cache: a new/updated flow changes the graph.
			s.mapGen.Add(1)
			payload := f.JSON(true)
			s.ConnMutex.Lock()
			for addr, ch := range s.Connections {
				select {
				case ch <- payload:
				default:
					log.Printf("SSE client %s too slow, dropping event", addr)
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
		req, err := http.ReadRequest(bufio.NewReader(strings.NewReader(normalizeRawRequest(string(raw)))))
		if err != nil {
			_, _ = fmt.Fprintf(w, "Error parsing request: %v", err)
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

		// Proxy CA is in the pool so self-signed upstream certs are trusted (intentional for MITM).
		transport := &http.Transport{
			TLSClientConfig: &tls.Config{
				RootCAs: caCertPool,
			},
			DisableCompression: true,
		}

		client := &http.Client{
			Transport: transport,
		}

		resp, err := client.Do(req)
		if err != nil {
			_, _ = fmt.Fprintf(w, "Error sending: %v", err)
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
		pf, pok := l.PersistedFlows[id]
		l.FlowMutex.Unlock()
		if ok {
			w.Header().Set("Content-Type", "application/json")
			fmt.Fprintf(w, "%s", f.JSON(true))
			return
		}
		if pok {
			w.Header().Set("Content-Type", "application/json")
			fmt.Fprintf(w, "%s", pf.JSON(true))
			return
		}
		http.Error(w, "Flow not found", 404)
	})

	mux.HandleFunc("/events", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.Header().Set("Cache-Control", "no-cache")
		w.Header().Set("Connection", "keep-alive")

		flusher, ok := w.(http.Flusher)
		if !ok {
			http.Error(w, "Streaming not supported", http.StatusInternalServerError)
			return
		}

		ch := make(chan string, 64)
		log.Printf("Client connected, adding %s to Connections (%d active connections)", r.RemoteAddr, len(s.Connections)+1)
		s.ConnMutex.Lock()
		s.Connections[r.RemoteAddr] = ch
		s.ConnMutex.Unlock()

		defer func() {
			log.Printf("Client disconnected, removing %s", r.RemoteAddr)
			s.ConnMutex.Lock()
			delete(s.Connections, r.RemoteAddr)
			s.ConnMutex.Unlock()
		}()

		ctx := r.Context()
		for {
			select {
			case <-ctx.Done():
				return
			case payload := <-ch:
				fmt.Fprintf(w, "data: %s\n\n", payload)
				flusher.Flush()
			}
		}
	})

	mux.HandleFunc("/history", func(w http.ResponseWriter, r *http.Request) {
		listenerName := r.URL.Query().Get("l")

		l := s.getListener(listenerName)
		if l == nil {
			http.Error(w, "Listener not found", http.StatusNotFound)
			return
		}

		l.FlowMutex.Lock()
		history := make([]map[string]any, 0, len(l.Flows)+len(l.PersistedFlows))
		seen := make(map[uint64]bool, len(l.Flows))
		for _, f := range l.Flows {
			seen[f.ID] = true
			var status int
			if f.Response != nil {
				status = f.Response.StatusCode
			}
			history = append(history, map[string]any{
				"id":           f.ID,
				"source":       f.Source,
				"method":       f.Request.Method,
				"url":          f.Request.URL.String(),
				"status":       status,
				"requestTime":  f.RequestTime.Format("2006-01-02 15:04:05 -07:00"),
				"responseTime": f.ResponseTime.Format("2006-01-02 15:04:05 -07:00"),
			})
		}
		for id, pf := range l.PersistedFlows {
			if seen[id] {
				continue
			}
			history = append(history, pf.Summary())
		}
		l.FlowMutex.Unlock()

		w.Header().Set("Content-Type", "application/json")
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
				l.Intercept.Store(true)
				log.Println("Intercept enabled")
			case "false":
				l.Intercept.Store(false)
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

		id, err := strconv.ParseUint(r.URL.Query().Get("id"), 10, 64)
		if err != nil {
			http.Error(w, "Invalid flow ID", http.StatusBadRequest)
			return
		}

		raw, err := io.ReadAll(r.Body)
		if err != nil {
			http.Error(w, "Failed to read body", http.StatusBadRequest)
			return
		}
		updatedReq, err := http.ReadRequest(bufio.NewReader(bytes.NewReader(raw)))
		if err != nil {
			http.Error(w, "Invalid HTTP request in body", http.StatusBadRequest)
			return
		}

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

		id, err := strconv.ParseUint(r.URL.Query().Get("id"), 10, 64)
		if err != nil {
			http.Error(w, "Invalid flow ID", http.StatusBadRequest)
			return
		}

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
			if !f.IsDir() && strings.HasSuffix(f.Name(), "-cert.pem") {
				certList = append(certList, f.Name())
			}
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(certList)
	})

	mux.HandleFunc("/ca/download", func(w http.ResponseWriter, r *http.Request) {
		name := r.URL.Query().Get("name")
		safeName := filepath.Base(name)
		if strings.Contains(safeName, "key") {
			http.Error(w, "Forbidden", http.StatusForbidden)
			return
		}
		path := filepath.Join(s.CertDir, safeName)

		w.Header().Set("Content-Disposition", "attachment; filename="+safeName)
		http.ServeFile(w, r, path)
	})

	mux.HandleFunc("/newListener", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			w.WriteHeader(http.StatusMethodNotAllowed)
			log.Println("Could not create a new listener")
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

	// --- Listeners API ---
	mux.HandleFunc("/listeners", func(w http.ResponseWriter, r *http.Request) {
		s.ListenMutex.RLock()
		var list []map[string]string
		for key, l := range s.Listeners {
			list = append(list, map[string]string{
				"key":     key,
				"address": l.Address.String(),
				"host":    l.Address.Host,
				"scheme":  l.Address.Scheme,
			})
		}
		s.ListenMutex.RUnlock()
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(list)
	})

	// --- Jobs API ---
	mux.HandleFunc("/version", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(s.Store.Version())
	})

	mux.HandleFunc("/map", s.mapHandler)
	s.registerMapV2(mux)
	mux.HandleFunc("/mapcrawl", s.mapCrawlHandler)

	mux.HandleFunc("/jobs", func(w http.ResponseWriter, r *http.Request) {
		s.JobsMu.RLock()
		jobs := make([]map[string]any, 0, len(s.Jobs))
		for _, j := range s.Jobs {
			jobs = append(jobs, j.MarshalSummary())
		}
		s.JobsMu.RUnlock()
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(jobs)
	})

	mux.HandleFunc("/jobs/results", func(w http.ResponseWriter, r *http.Request) {
		idStr := r.URL.Query().Get("id")
		id, err := strconv.ParseUint(idStr, 10, 64)
		if err != nil {
			http.Error(w, "Invalid job ID", http.StatusBadRequest)
			return
		}
		s.JobsMu.RLock()
		j, ok := s.Jobs[id]
		s.JobsMu.RUnlock()
		if !ok {
			http.Error(w, "Job not found", http.StatusNotFound)
			return
		}
		// Pagination: ?offset=N&limit=M. With no params we return up to
		// defaultLimit results (so a 10k-result job doesn't dump everything by
		// accident); pass an explicit large limit to override.
		const defaultLimit = 1000
		q := r.URL.Query()
		_, hasOffset := q["offset"]
		_, hasLimit := q["limit"]
		offset := 0
		if v, err := strconv.Atoi(q.Get("offset")); err == nil && v > 0 {
			offset = v
		}
		limit := defaultLimit
		if v, err := strconv.Atoi(q.Get("limit")); err == nil && v >= 0 {
			limit = v
		}

		j.ResultsMu.Lock()
		total := len(j.Results)
		// When no pagination params are given and the set is small, return it all
		// to preserve the previous behavior for existing callers.
		if !hasOffset && !hasLimit && total <= defaultLimit {
			results := make([]FuzzResult, total)
			copy(results, j.Results)
			j.ResultsMu.Unlock()
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(results)
			return
		}
		start := offset
		if start > total {
			start = total
		}
		end := start + limit
		if end > total {
			end = total
		}
		window := make([]FuzzResult, end-start)
		copy(window, j.Results[start:end])
		j.ResultsMu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(window)
	})

	mux.HandleFunc("/jobs/cancel", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
			return
		}
		idStr := r.URL.Query().Get("id")
		id, err := strconv.ParseUint(idStr, 10, 64)
		if err != nil {
			http.Error(w, "Invalid job ID", http.StatusBadRequest)
			return
		}
		s.JobsMu.RLock()
		j, ok := s.Jobs[id]
		s.JobsMu.RUnlock()
		if !ok {
			http.Error(w, "Job not found", http.StatusNotFound)
			return
		}
		if j.Cancel != nil {
			j.Cancel()
		}
		j.Status = "cancelled"
		s.Store.SaveJob(j)
		fmt.Fprint(w, "Cancelled")
	})

	// --- Fuzzer API ---
	mux.HandleFunc("/fuzz/fetch-wordlist", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
			return
		}
		var req struct {
			URL string `json:"url"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.URL == "" {
			http.Error(w, "Provide a url field", http.StatusBadRequest)
			return
		}
		resp, err := http.Get(req.URL)
		if err != nil {
			http.Error(w, "Failed to fetch: "+err.Error(), http.StatusBadGateway)
			return
		}
		defer resp.Body.Close()
		body, err := io.ReadAll(io.LimitReader(resp.Body, 50*1024*1024))
		if err != nil {
			http.Error(w, "Failed to read: "+err.Error(), http.StatusBadGateway)
			return
		}
		lines := strings.Split(string(body), "\n")
		var payloads []string
		for _, line := range lines {
			line = strings.TrimSpace(line)
			if line != "" && !strings.HasPrefix(line, "#") {
				payloads = append(payloads, line)
			}
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{"count": len(payloads), "payloads": payloads})
	})

	mux.HandleFunc("/fuzz", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
			return
		}

		var cfg FuzzConfig
		if err := json.NewDecoder(r.Body).Decode(&cfg); err != nil {
			http.Error(w, "Invalid config: "+err.Error(), http.StatusBadRequest)
			return
		}
		applyFuzzDefaults(&cfg)
		if len(cfg.Payloads) == 0 {
			http.Error(w, "No payloads provided", http.StatusBadRequest)
			return
		}

		// Extension expansion (-e): each payload is also tried with each extension.
		payloads := expandPayloads(cfg.Payloads, cfg.Extensions)

		ctx, cancel := context.WithCancel(context.Background())
		id := JobCounter.Add(1)
		job := &Job{
			ID:        id,
			Type:      "fuzz",
			Status:    "running",
			CreatedAt: time.Now(),
			Total:     len(payloads),
			Config:    cfg,
			Cancel:    cancel,
		}
		s.JobsMu.Lock()
		s.Jobs[id] = job
		s.JobsMu.Unlock()
		s.Store.SaveJob(job)

		go s.runFuzzJob(ctx, cancel, job, cfg, payloads)

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{"jobId": id})
	})

	// --- Spider API ---
	mux.HandleFunc("/spider", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
			return
		}
		var cfg SpiderConfig
		if err := json.NewDecoder(r.Body).Decode(&cfg); err != nil {
			http.Error(w, "Invalid config: "+err.Error(), http.StatusBadRequest)
			return
		}
		applySpiderDefaults(&cfg)
		if len(cfg.Sites) == 0 {
			http.Error(w, "No sites provided", http.StatusBadRequest)
			return
		}

		ctx, cancel := context.WithCancel(context.Background())
		id := JobCounter.Add(1)
		job := &Job{
			ID:        id,
			Type:      "spider",
			Status:    "running",
			CreatedAt: time.Now(),
			Total:     0,
			Config:    cfg,
			Cancel:    cancel,
		}
		s.JobsMu.Lock()
		s.Jobs[id] = job
		s.JobsMu.Unlock()
		s.Store.SaveJob(job)

		go s.runSpiderJob(ctx, cancel, job, cfg)

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{"jobId": id})
	})

	server := http.Server{
		Addr:    s.BackendAddress.Host,
		Handler: s.mux,
	}

	certPath := s.CertFilePath(&s.BackendAddress, "backend", false)
	keyPath := s.KeyFilePath(&s.BackendAddress, "backend", false)

	stop := make(chan os.Signal, 1)
	signal.Notify(stop, os.Interrupt, syscall.SIGTERM)

	go func() {
		log.Printf("=== Backend listening on %s with cert %s and key %s", s.BackendAddress.String(), certPath, keyPath)
		log.Printf(">>> Frontend ready: open https://%s/ in your browser", s.BackendAddress.Host)
		if err = server.ListenAndServeTLS(certPath, keyPath); err != nil && err != http.ErrServerClosed {
			log.Fatalf("Backend server failed: %v", err)
		}
	}()

	<-stop
	log.Println("Shutting down gracefully...")
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := server.Shutdown(ctx); err != nil {
		log.Fatalf("Shutdown failed: %v", err)
	}
	log.Println("Server stopped")
}

func (l *Listener) handleHTTP(w http.ResponseWriter, r *http.Request) {
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

	reqBodyBytes, _ := io.ReadAll(r.Body)
	r.Body.Close()
	r.Body = io.NopCloser(bytes.NewBuffer(reqBodyBytes))

	id := FlowCounter.Add(1)
	fl := &Flow{
		ID:          id,
		Source:      "proxy",
		Request:     r,
		RequestBody: reqBodyBytes,
		RequestTime: time.Now(),
	}
	if l.Flows != nil {
		l.FlowMutex.Lock()
		l.Flows[id] = fl
		l.evictOldFlows()
		l.FlowMutex.Unlock()
		l.Hitm.OutFlow <- *fl
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

			if l.Flows != nil {
				respBody, _ := io.ReadAll(resp.Body)
				resp.Body.Close()
				resp.Body = io.NopCloser(bytes.NewBuffer(respBody))

				fl.Response = resp
				fl.ResponseBody = respBody
				fl.ResponseTime = time.Now()
				l.Hitm.OutFlow <- *fl
			}

			return nil
		},
		ErrorHandler: func(rw http.ResponseWriter, req *http.Request, err error) {
			log.Printf("HTTP Proxy error for request %s %s: %v", req.Method, req.URL.String(), err)
			http.Error(rw, "Bad Gateway", http.StatusBadGateway)
		},
	}

	proxy.ServeHTTP(w, r)
}

func main() {
	h := &HITM{
		CertDir:     "./certs",
		Jobs:        make(map[uint64]*Job),
		Listeners:   make(map[string]*Listener),
		Connections: make(map[string]chan string),
		OutFlow:     make(chan Flow, 100),
		CAReady:     make(chan struct{}),
		mapStore:    NewMapStore(),
	}

	var BackendAddressRaw string
	var dbPath string
	flag.StringVar(&BackendAddressRaw, "l", "127.0.0.1:8080", "Listen address for the backend")
	flag.StringVar(&h.CertDir, "c", "./certs", "Directory containing TLS certificates")
	flag.StringVar(&dbPath, "db", "./hitm.db", "Path to the SQLite database file")
	var mapDump bool
	flag.BoolVar(&mapDump, "mapdump", false, "Build the state map from the DB, print a summary, and exit")
	var mapV2Dump bool
	flag.BoolVar(&mapV2Dump, "mapv2dump", false, "Build the Map v2 snapshot, print a summary, and exit")
	flag.Parse()

	// persistence
	store, err := OpenStore(dbPath)
	if err != nil {
		log.Fatalf("Failed to open database: %v", err)
	}
	defer store.Close()
	h.Store = store

	if mapDump {
		dumpMap(store)
		return
	}

	if mapV2Dump {
		dumpMapV2(h)
		return
	}

	// backend
	BackendURL, err := url.Parse("https://" + BackendAddressRaw)
	if err != nil {
		log.Printf("error parsing backend address: %v", err)
	}
	h.BackendAddress = *BackendURL

	_, err = h.EnsureCert(BackendURL, "HITM Proxy CA", true)
	if err != nil {
		log.Fatalf("Failed to generate CA: %v", err)
	}
	close(h.CAReady)

	h.NewListener("127.0.0.1:8080", "backend", false)

	// http proxy
	httpProxy := h.NewListener("127.0.0.1:9001", "http-proxy", true)
	go httpProxy.NewProxyHTTP()

	// https proxy
	httpsProxy := h.NewListener("https://127.0.0.1:9002", "https-proxy", true)
	go httpsProxy.NewProxyHTTPS()

	// restore any user-created listeners from previous runs
	h.restoreListeners()

	// restore persisted state (jobs + flows). Counters resume past the highest seen id.
	h.restoreState()

	log.Println("Starting backend")
	h.Backend()
}

// restoreListeners recreates user-created proxy listeners persisted in the DB.
// The three bootstrap listeners are already up, so anything else is restored.
func (h *HITM) restoreListeners() {
	persisted, err := h.Store.LoadListeners()
	if err != nil {
		log.Printf("Failed to load listeners: %v", err)
		return
	}
	for _, pl := range persisted {
		h.ListenMutex.RLock()
		_, exists := h.Listeners[pl.Key]
		h.ListenMutex.RUnlock()
		if exists {
			continue
		}
		log.Printf("Restoring listener %s (%s)", pl.Key, pl.Name)
		l := h.NewListener(pl.Address, pl.Name, pl.IsProxy)
		if strings.HasPrefix(pl.Address, "https") {
			go l.NewProxyHTTPS()
		} else {
			go l.NewProxyHTTP()
		}
	}
}

// restoreState loads persisted jobs and flows back into memory and resumes the
// id counters so new ids don't collide with persisted ones.
func (h *HITM) restoreState() {
	log.Println("Restoring jobs from database...")
	jobs, maxJobID, err := h.Store.LoadJobs()
	if err != nil {
		log.Printf("Failed to load jobs: %v", err)
	} else {
		h.JobsMu.Lock()
		for _, j := range jobs {
			h.Jobs[j.ID] = j
		}
		h.JobsMu.Unlock()
		if maxJobID > JobCounter.Load() {
			JobCounter.Store(maxJobID)
		}
		log.Printf("Restored %d jobs from database (next job id: %d)", len(jobs), JobCounter.Load()+1)
	}

	log.Println("Restoring flows from database...")
	flows, maxFlowID, err := h.Store.LoadFlows()
	if err != nil {
		log.Printf("Failed to load flows: %v", err)
		return
	}
	if maxFlowID > FlowCounter.Load() {
		FlowCounter.Store(maxFlowID)
	}
	// Restore flows into the HTTPS proxy listener (the one the frontend queries by
	// default). Fall back to the primary proxy if it isn't present.
	target := h.getListener("https://127.0.0.1:9002")
	if target == nil {
		target = h.getListener("")
	}
	if target != nil {
		target.FlowMutex.Lock()
		for id, pf := range flows {
			target.PersistedFlows[id] = pf
		}
		target.FlowMutex.Unlock()
		log.Printf("Restored %d flows into listener %s (next flow id: %d)", len(flows), target.Address.String(), FlowCounter.Load()+1)
	} else {
		log.Printf("Restored %d flows but found no proxy listener to attach them to", len(flows))
	}
	log.Println("State restore complete")
}
