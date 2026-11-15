package federation

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"fmt"
	"math/big"
	"net"
	"net/http"
	"os"
	"time"
)

// LoadOrGenerateTLS loads TLS certificates from disk or generates self-signed ones.
func LoadOrGenerateTLS(certPath, keyPath, hostname string) (*tls.Config, error) {
	// Try loading from disk first
	if certPath != "" && keyPath != "" {
		cert, err := tls.LoadX509KeyPair(certPath, keyPath)
		if err == nil {
			return &tls.Config{
				Certificates: []tls.Certificate{cert},
				ClientAuth:   tls.RequestClientCert,
				MinVersion:   tls.VersionTLS13,
			}, nil
		}
		// Fall through to generate if files don't exist
		if !os.IsNotExist(err) {
			return nil, fmt.Errorf("load tls: %w", err)
		}
	}

	// Generate self-signed certificate
	cert, err := generateSelfSignedCert(hostname)
	if err != nil {
		return nil, fmt.Errorf("generate self-signed cert: %w", err)
	}

	return &tls.Config{
		Certificates: []tls.Certificate{*cert},
		ClientAuth:   tls.RequestClientCert,
		MinVersion:   tls.VersionTLS13,
	}, nil
}

func generateSelfSignedCert(hostname string) (*tls.Certificate, error) {
	priv, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return nil, err
	}

	serial, err := rand.Int(rand.Reader, new(big.Int).Lsh(big.NewInt(1), 128))
	if err != nil {
		return nil, err
	}

	template := x509.Certificate{
		SerialNumber: serial,
		Subject: pkix.Name{
			Organization: []string{"xilebo"},
			CommonName:   hostname,
		},
		NotBefore:             time.Now(),
		NotAfter:              time.Now().Add(365 * 24 * time.Hour),
		KeyUsage:              x509.KeyUsageDigitalSignature | x509.KeyUsageKeyEncipherment,
		ExtKeyUsage:           []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth, x509.ExtKeyUsageClientAuth},
		BasicConstraintsValid: true,
	}

	// Add SANs
	if ip := net.ParseIP(hostname); ip != nil {
		template.IPAddresses = append(template.IPAddresses, ip)
	} else {
		template.DNSNames = append(template.DNSNames, hostname)
	}

	certDER, err := x509.CreateCertificate(rand.Reader, &template, &template, &priv.PublicKey, priv)
	if err != nil {
		return nil, err
	}

	certPEM := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: certDER})
	privDER, err := x509.MarshalECPrivateKey(priv)
	if err != nil {
		return nil, err
	}
	keyPEM := pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: privDER})

	cert, err := tls.X509KeyPair(certPEM, keyPEM)
	if err != nil {
		return nil, err
	}

	return &cert, nil
}

// MakeMTLSClient creates an HTTP client configured for mTLS federation.
func MakeMTLSClient(tlsConfig *tls.Config) *http.Client {
	transport := &http.Transport{
		TLSClientConfig: &tls.Config{
			Certificates:       tlsConfig.Certificates,
			InsecureSkipVerify: true, // Self-signed certs in federation
			MinVersion:         tls.VersionTLS13,
		},
	}
	return &http.Client{
		Transport: transport,
		Timeout:   30 * time.Second,
	}
}
