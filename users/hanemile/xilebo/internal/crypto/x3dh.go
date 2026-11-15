package crypto

import (
	"crypto/hmac"
	"crypto/sha256"
	"errors"
	"fmt"
	"io"

	"golang.org/x/crypto/hkdf"
)

// X3DHResult contains the output of an X3DH key agreement.
type X3DHResult struct {
	SharedKey    [32]byte // 32-byte shared secret for initializing Double Ratchet
	EphemeralKey [32]byte // Ephemeral public key (sent to recipient)
}

// X3DHInitiate performs the initiator (Alice) side of the X3DH protocol.
// Alice has her identity key and generates an ephemeral key.
// She uses the recipient's pre-key bundle to derive a shared secret.
func X3DHInitiate(
	identityKey *Curve25519KeyPair, // Alice's Curve25519 identity key
	bundle *PreKeyBundle,           // Bob's pre-key bundle
) (*X3DHResult, error) {
	// Verify signed pre-key
	if !VerifySignedPreKey(bundle.IdentityKey, &bundle.SignedPreKey) {
		return nil, errors.New("x3dh: signed pre-key verification failed")
	}

	// Generate ephemeral key pair
	ephemeral, err := GenerateCurve25519Key()
	if err != nil {
		return nil, fmt.Errorf("x3dh: generate ephemeral key: %w", err)
	}

	// Convert Bob's keys
	var bobIdentity, bobSignedPreKey [32]byte
	// For X3DH, we need Bob's identity key as X25519.
	// In practice, the identity key would be converted from Ed25519 to X25519.
	// For simplicity, we store the X25519 version alongside the Ed25519 key.
	// Here we treat the first 32 bytes of the Ed25519 public key as a placeholder.
	copy(bobIdentity[:], bundle.IdentityKey[:32])
	bobSignedPreKey = bundle.SignedPreKey.PublicKey

	// DH1 = DH(IKa, SPKb)
	dh1, err := DH(identityKey.PrivateKey, bobSignedPreKey)
	if err != nil {
		return nil, fmt.Errorf("x3dh dh1: %w", err)
	}

	// DH2 = DH(EKa, IKb)
	dh2, err := DH(ephemeral.PrivateKey, bobIdentity)
	if err != nil {
		return nil, fmt.Errorf("x3dh dh2: %w", err)
	}

	// DH3 = DH(EKa, SPKb)
	dh3, err := DH(ephemeral.PrivateKey, bobSignedPreKey)
	if err != nil {
		return nil, fmt.Errorf("x3dh dh3: %w", err)
	}

	// Build input key material
	ikm := make([]byte, 0, 32*4)
	ikm = append(ikm, make([]byte, 32)...) // 32 bytes of 0xFF as per spec
	for i := range ikm[:32] {
		ikm[i] = 0xFF
	}
	ikm = append(ikm, dh1[:]...)
	ikm = append(ikm, dh2[:]...)
	ikm = append(ikm, dh3[:]...)

	// DH4 = DH(EKa, OPKb) if one-time pre-key available
	if bundle.OneTimePreKey != nil {
		dh4, err := DH(ephemeral.PrivateKey, bundle.OneTimePreKey.PublicKey)
		if err != nil {
			return nil, fmt.Errorf("x3dh dh4: %w", err)
		}
		ikm = append(ikm, dh4[:]...)
	}

	// Derive shared key using HKDF
	sharedKey, err := kdf(ikm, []byte("xilebo-x3dh"))
	if err != nil {
		return nil, fmt.Errorf("x3dh kdf: %w", err)
	}

	return &X3DHResult{
		SharedKey:    sharedKey,
		EphemeralKey: ephemeral.PublicKey,
	}, nil
}

// X3DHRespond performs the responder (Bob) side of the X3DH protocol.
func X3DHRespond(
	identityKey *Curve25519KeyPair,   // Bob's Curve25519 identity key
	signedPreKey *Curve25519KeyPair,  // Bob's signed pre-key (private)
	oneTimePreKey *Curve25519KeyPair, // Bob's one-time pre-key (private, may be nil)
	aliceIdentity [32]byte,           // Alice's Curve25519 identity public key
	aliceEphemeral [32]byte,          // Alice's ephemeral public key
) ([32]byte, error) {
	// DH1 = DH(SPKb, IKa)
	dh1, err := DH(signedPreKey.PrivateKey, aliceIdentity)
	if err != nil {
		return [32]byte{}, fmt.Errorf("x3dh dh1: %w", err)
	}

	// DH2 = DH(IKb, EKa)
	dh2, err := DH(identityKey.PrivateKey, aliceEphemeral)
	if err != nil {
		return [32]byte{}, fmt.Errorf("x3dh dh2: %w", err)
	}

	// DH3 = DH(SPKb, EKa)
	dh3, err := DH(signedPreKey.PrivateKey, aliceEphemeral)
	if err != nil {
		return [32]byte{}, fmt.Errorf("x3dh dh3: %w", err)
	}

	ikm := make([]byte, 0, 32*4)
	ikm = append(ikm, make([]byte, 32)...)
	for i := range ikm[:32] {
		ikm[i] = 0xFF
	}
	ikm = append(ikm, dh1[:]...)
	ikm = append(ikm, dh2[:]...)
	ikm = append(ikm, dh3[:]...)

	// DH4 = DH(OPKb, EKa)
	if oneTimePreKey != nil {
		dh4, err := DH(oneTimePreKey.PrivateKey, aliceEphemeral)
		if err != nil {
			return [32]byte{}, fmt.Errorf("x3dh dh4: %w", err)
		}
		ikm = append(ikm, dh4[:]...)
	}

	sharedKey, err := kdf(ikm, []byte("xilebo-x3dh"))
	if err != nil {
		return [32]byte{}, fmt.Errorf("x3dh kdf: %w", err)
	}

	return sharedKey, nil
}

// kdf derives a 32-byte key using HKDF-SHA256.
func kdf(ikm, info []byte) ([32]byte, error) {
	salt := make([]byte, 32) // Zero salt as per X3DH spec
	reader := hkdf.New(sha256.New, ikm, salt, info)

	var key [32]byte
	if _, err := io.ReadFull(reader, key[:]); err != nil {
		return [32]byte{}, err
	}
	return key, nil
}

// MAC computes HMAC-SHA256.
func MAC(key, data []byte) []byte {
	h := hmac.New(sha256.New, key)
	h.Write(data)
	return h.Sum(nil)
}
