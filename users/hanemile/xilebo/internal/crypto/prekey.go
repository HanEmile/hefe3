package crypto

import (
	"crypto/ed25519"
	"fmt"
)

// PreKeyBundle contains the public key material needed to establish an encrypted session.
type PreKeyBundle struct {
	IdentityKey   ed25519.PublicKey // Long-term Ed25519 identity key
	SignedPreKey  SignedPreKey      // Periodically rotated, signed with identity key
	OneTimePreKey *OneTimePreKey   // Ephemeral, consumed on use (may be nil)
}

// SignedPreKey is a medium-term Curve25519 key signed by the identity key.
type SignedPreKey struct {
	KeyID     int64
	PublicKey [32]byte
	Signature []byte // Ed25519 signature over PublicKey
}

// OneTimePreKey is an ephemeral Curve25519 key used once and discarded.
type OneTimePreKey struct {
	KeyID     int64
	PublicKey [32]byte
}

// GenerateSignedPreKey generates a signed pre-key and signs it with the identity key.
func GenerateSignedPreKey(keyID int64, identityKey *IdentityKeyPair) (*SignedPreKey, *Curve25519KeyPair, error) {
	kp, err := GenerateCurve25519Key()
	if err != nil {
		return nil, nil, fmt.Errorf("generate signed pre-key: %w", err)
	}

	signature := identityKey.Sign(kp.PublicKey[:])

	return &SignedPreKey{
		KeyID:     keyID,
		PublicKey: kp.PublicKey,
		Signature: signature,
	}, kp, nil
}

// VerifySignedPreKey verifies that a signed pre-key was signed by the given identity key.
func VerifySignedPreKey(identityKey ed25519.PublicKey, spk *SignedPreKey) bool {
	return Verify(identityKey, spk.PublicKey[:], spk.Signature)
}

// GenerateOneTimePreKeys generates a batch of one-time pre-keys.
func GenerateOneTimePreKeys(startID int64, count int) ([]OneTimePreKey, []*Curve25519KeyPair, error) {
	otks := make([]OneTimePreKey, count)
	kps := make([]*Curve25519KeyPair, count)

	for i := 0; i < count; i++ {
		kp, err := GenerateCurve25519Key()
		if err != nil {
			return nil, nil, fmt.Errorf("generate one-time pre-key %d: %w", i, err)
		}
		otks[i] = OneTimePreKey{
			KeyID:     startID + int64(i),
			PublicKey: kp.PublicKey,
		}
		kps[i] = kp
	}

	return otks, kps, nil
}
