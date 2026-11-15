package federation

import (
	"crypto/ed25519"
	"crypto/rand"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestSignAndVerify(t *testing.T) {
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}

	body := []byte(`{"test":"data"}`)
	req := httptest.NewRequest(http.MethodPost, "/_xb/federation/v1/send", nil)

	SignRequest(req, "server-a.example.com", priv, body)

	// Verify the headers were set
	if req.Header.Get(headerOrigin) != "server-a.example.com" {
		t.Errorf("origin = %q", req.Header.Get(headerOrigin))
	}
	if req.Header.Get(headerTimestamp) == "" {
		t.Error("timestamp not set")
	}
	if req.Header.Get(headerSignature) == "" {
		t.Error("signature not set")
	}

	// Verify the signature
	if err := VerifyRequest(req, pub, body); err != nil {
		t.Errorf("VerifyRequest failed: %v", err)
	}
}

func TestVerifyTamperedBody(t *testing.T) {
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}

	body := []byte(`{"test":"data"}`)
	req := httptest.NewRequest(http.MethodPost, "/_xb/federation/v1/send", nil)

	SignRequest(req, "server-a.example.com", priv, body)

	// Verify with tampered body should fail
	tampered := []byte(`{"test":"tampered"}`)
	if err := VerifyRequest(req, pub, tampered); err == nil {
		t.Error("expected verification to fail with tampered body")
	}
}

func TestVerifyWrongKey(t *testing.T) {
	_, priv, _ := ed25519.GenerateKey(rand.Reader)
	otherPub, _, _ := ed25519.GenerateKey(rand.Reader)

	body := []byte(`{"test":"data"}`)
	req := httptest.NewRequest(http.MethodPost, "/_xb/federation/v1/send", nil)

	SignRequest(req, "server-a.example.com", priv, body)

	// Verify with wrong key should fail
	if err := VerifyRequest(req, otherPub, body); err == nil {
		t.Error("expected verification to fail with wrong key")
	}
}

func TestVerifyMissingHeaders(t *testing.T) {
	pub, _, _ := ed25519.GenerateKey(rand.Reader)

	req := httptest.NewRequest(http.MethodPost, "/_xb/federation/v1/send", nil)

	if err := VerifyRequest(req, pub, nil); err == nil {
		t.Error("expected error for missing headers")
	}
}
