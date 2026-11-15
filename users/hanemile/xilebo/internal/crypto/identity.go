package crypto

import (
	"crypto/ed25519"
	"crypto/rand"
	"fmt"

	"golang.org/x/crypto/curve25519"
)

// IdentityKeyPair holds an Ed25519 signing key and a Curve25519 key exchange key.
// The Ed25519 key is used for signing pre-keys.
// The Curve25519 key is derived from Ed25519 for X3DH key agreement.
type IdentityKeyPair struct {
	Ed25519Public  ed25519.PublicKey
	Ed25519Private ed25519.PrivateKey
}

// GenerateIdentityKey creates a new Ed25519 identity key pair.
func GenerateIdentityKey() (*IdentityKeyPair, error) {
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		return nil, fmt.Errorf("generate ed25519 key: %w", err)
	}
	return &IdentityKeyPair{
		Ed25519Public:  pub,
		Ed25519Private: priv,
	}, nil
}

// Sign signs data with the identity key.
func (k *IdentityKeyPair) Sign(data []byte) []byte {
	return ed25519.Sign(k.Ed25519Private, data)
}

// Verify checks a signature against the public key.
func Verify(publicKey ed25519.PublicKey, data, signature []byte) bool {
	return ed25519.Verify(publicKey, data, signature)
}

// Curve25519KeyPair is an X25519 key pair for Diffie-Hellman key exchange.
type Curve25519KeyPair struct {
	PublicKey  [32]byte
	PrivateKey [32]byte
}

// GenerateCurve25519Key creates a new X25519 key pair.
func GenerateCurve25519Key() (*Curve25519KeyPair, error) {
	var priv [32]byte
	if _, err := rand.Read(priv[:]); err != nil {
		return nil, fmt.Errorf("generate x25519 key: %w", err)
	}

	// Clamp for Curve25519
	priv[0] &= 248
	priv[31] &= 127
	priv[31] |= 64

	pub, err := curve25519.X25519(priv[:], curve25519.Basepoint)
	if err != nil {
		return nil, fmt.Errorf("derive x25519 public key: %w", err)
	}

	var kp Curve25519KeyPair
	copy(kp.PrivateKey[:], priv[:])
	copy(kp.PublicKey[:], pub)
	return &kp, nil
}

// DH performs a Diffie-Hellman key exchange.
func DH(privateKey, publicKey [32]byte) ([32]byte, error) {
	shared, err := curve25519.X25519(privateKey[:], publicKey[:])
	if err != nil {
		return [32]byte{}, fmt.Errorf("dh: %w", err)
	}
	var result [32]byte
	copy(result[:], shared)
	return result, nil
}
