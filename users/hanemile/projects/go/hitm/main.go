package main

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha1"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"flag"
	"fmt"
	"log"
	"math/big"
	"net"
	"net/http"
	"net/url"
	"os"
	"regexp"
	"sync/atomic"
	"time"
)

var ListenerCounter atomic.Uint64

func clean(input string) string {
	regexPattern := "[^a-zA-Z0-9-]+"
	reg := regexp.MustCompile(regexPattern)
	return reg.ReplaceAllString(input, "-")
}

func hashKeyId(pub any) []byte {
	var publicBytes []byte
	switch k := pub.(type) {
	case *ecdsa.PublicKey:
		publicBytes = elliptic.Marshal(k.Curve, k.X, k.Y)
	default:
		// Fallback or handle RSA if you switch back
		return nil
	}
	h := sha1.New()
	h.Write(publicBytes)
	return h.Sum(nil)
}

type Listener struct {
	ListenerID   uint64
	AddressRaw   string
	Address      *url.URL
	Variant      string
	Hitm         *HITM
	Handler      *http.ServeMux
	TLS          bool
	CertFilePath string
	KeyFilePath  string
}

func (l *Listener) HttpBackendHandler() *http.ServeMux {
	log.Println(">> HttpBackendListener")

	mux := http.NewServeMux()

	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprintf(w, "index")
	})

	return mux
}

func (l *Listener) HttpProxyHandler() *http.ServeMux {
	log.Println(">> HttpProxyListener")
	mux := http.NewServeMux()

	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprintf(w, "index")
	})

	return mux
	// if r.Method == http.MethodConnect {
	// 	l.HandleConnect(w, r)
	// } else {
	// 	l.HandleHTTP(w, r)
	// }
}

func (l *Listener) HttpsProxyHandler() *http.ServeMux {
	log.Println(">> HttpsProxyListener")
	mux := http.NewServeMux()

	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprintf(w, "index")
	})

	return mux
	// if r.Method == http.MethodConnect {
	// l.HandleConnect(w, r)
	// } else {
	// l.HandleHTTP(w, r)
	// }
}

func (l *Listener) EnsureCert() {
	log.Printf(">>> l.EnsureCert")
	l.CertFilePath = l.Hitm.CertFilePath(l.Address, l.Address.Host, false)
	l.KeyFilePath = l.Hitm.KeyFilePath(l.Address, l.Address.Host, false)
	l.Hitm.EnsureCert(l.Address, l.Address.Host, false)
}

func (l *Listener) Run() {
	if l.TLS {
		server := http.Server{
			Addr:    l.Address.Host,
			Handler: l.Handler,
		}
		l.EnsureCert()
		log.Printf("> %s listening on %s", l.Variant, l.Address.String())
		go func() {
			err := server.ListenAndServeTLS(l.CertFilePath, l.KeyFilePath)
			if err != nil {
				log.Fatal(err)
			}
		}()
	} else {
		log.Printf("> %s listening on %s", l.Variant, l.Address.String())
		go func() {
			err := http.ListenAndServe(l.Address.Host, l.Handler)
			if err != nil {
				log.Fatal(err)
			}
		}()
	}
}

func (l *Listener) HandleConnect(w http.ResponseWriter, r *http.Request) {
	log.Printf("HandleConnect %s", r.Method)
}
func (l *Listener) HandleHTTP(w http.ResponseWriter, r *http.Request) {
	log.Printf("HandleHTTP %s", r.Method)
}

type HITM struct {
	CertDir            string
	CACert             *x509.Certificate
	CAKey              *ecdsa.PrivateKey
	PrivateKeys        map[string]*ecdsa.PrivateKey
	PublicCertificates map[string]*x509.Certificate
	Listeners          map[uint64]*Listener
	DoneChannel        chan bool
}

func (h *HITM) GenOrReadPrivKey(url *url.URL, commonName string, isCA bool) error {
	log.Printf(">>>>> GenOrReadPrivKey %s %s %t", url.String(), commonName, isCA)

	keyfilePath := h.KeyFilePath(url, commonName, isCA)

	var privateKey *ecdsa.PrivateKey
	if keyFile, err := os.ReadFile(keyfilePath); err != nil {
		var err error
		privateKey, err = ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
		if err != nil {
			return fmt.Errorf("failed to generate a key: %v", err)
		}

		keyOut, err := os.OpenFile(keyfilePath, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0600)
		if err != nil {
			return fmt.Errorf("failed to open the %s file for writing: %v", keyfilePath, err)
		}
		defer keyOut.Close()

		privKeyBytes, err := x509.MarshalECPrivateKey(privateKey)
		if err != nil {
			return fmt.Errorf("failed to marshal EC private key: %v", err)
		}
		pem.Encode(keyOut, &pem.Block{Type: "EC PRIVATE KEY", Bytes: privKeyBytes})

		log.Printf("===== %s wrote new private key to %s", url.String(), keyfilePath)
	} else {
		block, _ := pem.Decode(keyFile)
		if block == nil {
			return fmt.Errorf("failed to parse PEM block containing the key: %v", err)
		}

		privateKey, err = x509.ParseECPrivateKey(block.Bytes)
		if err != nil {
			return fmt.Errorf("failed to parse private key: %v", err)
		}
		log.Printf("===== Using existing private key: %s", keyfilePath)
	}

	if isCA {
		h.CAKey = privateKey
		log.Println("[CA] key set")
	}

	h.PrivateKeys[commonName] = privateKey

	return nil
}

func (h *HITM) GenOrReadPubCert(url *url.URL, commonName string, isCA bool) error {
	log.Printf(">>>>> GenOrReadPubCert %s %s %t", url.String(), commonName, isCA)

	certFilePath := h.CertFilePath(url, commonName, isCA)

	var derBytes []byte

	// try to read it from disk
	cachedBytes, err := os.ReadFile(certFilePath)
	if err == nil {
		block, _ := pem.Decode(cachedBytes)
		if block != nil {
			derBytes = block.Bytes
			parsed, err := x509.ParseCertificate(derBytes)
			if err == nil && derBytes != nil {
				log.Printf("===== Using existing public certificate: %s", certFilePath)
				if isCA {
					h.CACert = parsed
					log.Println("[CA] cert set")
				}
				h.PublicCertificates[commonName] = parsed
				return nil
			}
		}
	}

	// no cached certificate found, generate a new one

	serialNumber, err := rand.Int(rand.Reader, new(big.Int).Lsh(big.NewInt(1), 128))
	if err != nil {
		return fmt.Errorf("failed to generate serial number: %v", err)
	}

	log.Printf("Serial Number: %+x", serialNumber.Bytes())

	template := x509.Certificate{
		SerialNumber: serialNumber,
		Subject: pkix.Name{
			Organization: []string{"HITM Proxy"},
			CommonName:   commonName,
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
		privKey, ok := h.PrivateKeys[commonName]
		if ok {
			ski := hashKeyId(privKey.PublicKey)
			if ski != nil {
				template.SubjectKeyId = ski
				template.AuthorityKeyId = ski
			}
			template.Subject.CommonName = commonName
			parent = &template // Self-sign
			signerKey = privKey
		} else {
			return fmt.Errorf("private key not found for %s", commonName)
		}
	} else {
		if h.CACert == nil {
			return fmt.Errorf("root CA not initialized")
		}
		template.Subject.CommonName = commonName
		template.KeyUsage = x509.KeyUsageDigitalSignature
		template.ExtKeyUsage = []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth, x509.ExtKeyUsageClientAuth}
		template.DNSNames = []string{url.Hostname()}
		if ip := net.ParseIP(url.Hostname()); ip != nil {
			template.IPAddresses = []net.IP{ip}
		}
		parent = h.CACert
		signerKey = h.CAKey
	}

	derBytes, err = x509.CreateCertificate(rand.Reader, &template, parent, &h.PrivateKeys[commonName].PublicKey, signerKey)
	if err != nil {
		return fmt.Errorf("failed to create certificate: %v", err)
	}

	// Save to disk
	f, err := os.Create(certFilePath)
	if err != nil {
		return fmt.Errorf("failed to create certificate file: %v", err)
	}
	pem.Encode(f, &pem.Block{Type: "CERTIFICATE", Bytes: derBytes})
	f.Close()

	// Hydrate State
	parsed, err := x509.ParseCertificate(derBytes)
	if err != nil {
		return fmt.Errorf("failed to parse certificate: %v", err)
	}
	if isCA {
		h.CACert = parsed
		log.Println("[CA] cert set")
	}

	h.PublicCertificates[commonName] = parsed

	log.Printf("===== %s wrote new public certificate to %s", url.String(), certFilePath)
	return nil
}

func (h *HITM) EnsureCert(u *url.URL, commonName string, isCA bool) error {
	log.Printf(">>>> h.EnsureCert: hostname=%s, commonName=%s, isCA=%t", u.Hostname(), commonName, isCA)

	for !isCA && h.CACert == nil {
		log.Println("Waiting for root CA to be initialized...")
		time.Sleep(1 * time.Second)
	}

	err := h.GenOrReadPrivKey(u, commonName, isCA)
	if err != nil {
		return fmt.Errorf("failed to generate or read private key: %v", err)
	}

	err = h.GenOrReadPubCert(u, commonName, isCA)
	if err != nil {
		return fmt.Errorf("failed to generate or read public certificate: %v", err)
	}
	return nil
}

// Given a listener with only AddressRaw and Variant, this will fill in the rest
func (h *HITM) NewListener(l Listener) *Listener {
	log.Printf("> New Listener %s", l.AddressRaw)
	addr, err := url.Parse(l.AddressRaw)
	if err != nil {
		log.Printf("Could not parse the given listenAddress: %+v", err)
		return nil
	}

	l.ListenerID = ListenerCounter.Add(1)
	l.Address = addr
	l.Hitm = h
	l.TLS = addr.Scheme == "https"

	switch l.Variant {
	case "backend":
		l.Handler = l.HttpBackendHandler()
	case "http-proxy":
		// l.Handler = http.HandlerFunc(l.HttpProxyListener)
	case "https-proxy":
		// l.Handler = http.HandlerFunc(l.HttpsProxyListener)
	default:
		log.Println("Listener variant not defined")
	}

	h.Listeners[l.ListenerID] = &l

	return &l
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

func main() {
	log.Println("Hello World")

	var certDir string
	flag.StringVar(&certDir, "cert-dir", "./certs", "The directory to store the certs in")
	flag.Parse()

	log.Printf("Defining HITM")
	h := &HITM{
		CertDir:            certDir,
		Listeners:          make(map[uint64]*Listener),
		DoneChannel:        make(chan bool),
		PrivateKeys:        make(map[string]*ecdsa.PrivateKey),
		PublicCertificates: make(map[string]*x509.Certificate),
	}

	caURL, err := url.Parse("https://127.0.0.1")
	if err != nil {
		log.Fatalf("Failed to parse CA URL: %v", err)
	}
	err = h.EnsureCert(caURL, "HITM CA", true)
	if err != nil {
		log.Fatalf("Error creating CA Cert: %+v", err)
	}

	h.NewListener(Listener{AddressRaw: "https://127.0.0.1:8080", Variant: "backend"}).Run()
	// h.NewListener(Listener{AddressRaw: "http://127.0.0.1:9001", Variant: "http-proxy"}).Run()
	// h.NewListener(Listener{AddressRaw: "https://127.0.0.1:9002", Variant: "https-proxy"}).Run()

	log.Printf("All running")

	for i := 0; i < len(h.Listeners); i++ {
		<-h.DoneChannel
		log.Printf("Listener %d stopped", i)
	}
}
