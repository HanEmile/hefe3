package crypto

import (
	"bytes"
	"testing"
)

func TestIdentityKeyGeneration(t *testing.T) {
	key, err := GenerateIdentityKey()
	if err != nil {
		t.Fatalf("GenerateIdentityKey: %v", err)
	}

	if len(key.Ed25519Public) != 32 {
		t.Errorf("public key length = %d, want 32", len(key.Ed25519Public))
	}

	// Test signing and verification
	data := []byte("test message")
	sig := key.Sign(data)

	if !Verify(key.Ed25519Public, data, sig) {
		t.Error("signature verification failed")
	}

	// Tampered data should fail
	if Verify(key.Ed25519Public, []byte("tampered"), sig) {
		t.Error("tampered data should fail verification")
	}
}

func TestCurve25519KeyGeneration(t *testing.T) {
	kp, err := GenerateCurve25519Key()
	if err != nil {
		t.Fatalf("GenerateCurve25519Key: %v", err)
	}

	// Keys should not be all zeros
	var zero [32]byte
	if kp.PublicKey == zero {
		t.Error("public key is all zeros")
	}
	if kp.PrivateKey == zero {
		t.Error("private key is all zeros")
	}
}

func TestDiffieHellman(t *testing.T) {
	alice, err := GenerateCurve25519Key()
	if err != nil {
		t.Fatalf("generate alice key: %v", err)
	}

	bob, err := GenerateCurve25519Key()
	if err != nil {
		t.Fatalf("generate bob key: %v", err)
	}

	// DH(alice_priv, bob_pub) should equal DH(bob_priv, alice_pub)
	sharedA, err := DH(alice.PrivateKey, bob.PublicKey)
	if err != nil {
		t.Fatalf("DH(alice, bob): %v", err)
	}

	sharedB, err := DH(bob.PrivateKey, alice.PublicKey)
	if err != nil {
		t.Fatalf("DH(bob, alice): %v", err)
	}

	if sharedA != sharedB {
		t.Error("DH shared secrets do not match")
	}
}

func TestSignedPreKey(t *testing.T) {
	identity, err := GenerateIdentityKey()
	if err != nil {
		t.Fatal(err)
	}

	spk, kp, err := GenerateSignedPreKey(1, identity)
	if err != nil {
		t.Fatal(err)
	}

	if spk.KeyID != 1 {
		t.Errorf("key ID = %d, want 1", spk.KeyID)
	}

	if kp.PublicKey != spk.PublicKey {
		t.Error("pre-key public key mismatch")
	}

	if !VerifySignedPreKey(identity.Ed25519Public, spk) {
		t.Error("signed pre-key verification failed")
	}
}

func TestOneTimePreKeys(t *testing.T) {
	otks, kps, err := GenerateOneTimePreKeys(10, 5)
	if err != nil {
		t.Fatal(err)
	}

	if len(otks) != 5 {
		t.Errorf("got %d keys, want 5", len(otks))
	}
	if len(kps) != 5 {
		t.Errorf("got %d keypairs, want 5", len(kps))
	}

	for i, otk := range otks {
		if otk.KeyID != int64(10+i) {
			t.Errorf("key[%d] ID = %d, want %d", i, otk.KeyID, 10+i)
		}
	}
}

func TestSenderKeyEncryptDecrypt(t *testing.T) {
	sk, err := NewSenderKey()
	if err != nil {
		t.Fatal(err)
	}

	// Create a receiver copy with the same chain key
	chainKey := sk.ChainKey()
	receiver := SenderKeyFromBytes(chainKey, 0)

	plaintext := []byte("hello group!")

	msg, err := sk.Encrypt(plaintext)
	if err != nil {
		t.Fatalf("encrypt: %v", err)
	}

	decrypted, err := receiver.Decrypt(msg)
	if err != nil {
		t.Fatalf("decrypt: %v", err)
	}

	if !bytes.Equal(plaintext, decrypted) {
		t.Errorf("decrypted = %q, want %q", decrypted, plaintext)
	}
}

func TestSenderKeyForwardSecrecy(t *testing.T) {
	sk, err := NewSenderKey()
	if err != nil {
		t.Fatal(err)
	}

	chainKey := sk.ChainKey()

	// Encrypt 3 messages
	msg1, _ := sk.Encrypt([]byte("msg1"))
	msg2, _ := sk.Encrypt([]byte("msg2"))
	msg3, _ := sk.Encrypt([]byte("msg3"))

	// A late joiner with the chain key at iteration 0
	// can decrypt all messages (they know the starting chain key)
	receiver := SenderKeyFromBytes(chainKey, 0)

	d1, err := receiver.Decrypt(msg1)
	if err != nil {
		t.Fatalf("decrypt msg1: %v", err)
	}
	if string(d1) != "msg1" {
		t.Errorf("msg1 = %q", d1)
	}

	d2, err := receiver.Decrypt(msg2)
	if err != nil {
		t.Fatalf("decrypt msg2: %v", err)
	}
	if string(d2) != "msg2" {
		t.Errorf("msg2 = %q", d2)
	}

	d3, err := receiver.Decrypt(msg3)
	if err != nil {
		t.Fatalf("decrypt msg3: %v", err)
	}
	if string(d3) != "msg3" {
		t.Errorf("msg3 = %q", d3)
	}
}

func TestSenderKeySkipForward(t *testing.T) {
	sk, err := NewSenderKey()
	if err != nil {
		t.Fatal(err)
	}

	chainKey := sk.ChainKey()

	// Encrypt 3 messages, but receiver only gets msg3
	sk.Encrypt([]byte("msg1"))
	sk.Encrypt([]byte("msg2"))
	msg3, _ := sk.Encrypt([]byte("msg3"))

	// Receiver starts at iteration 0 and should be able to skip to iteration 2
	receiver := SenderKeyFromBytes(chainKey, 0)

	d3, err := receiver.Decrypt(msg3)
	if err != nil {
		t.Fatalf("decrypt msg3 (skipping): %v", err)
	}
	if string(d3) != "msg3" {
		t.Errorf("msg3 = %q", d3)
	}
}

func TestDoubleRatchetSession(t *testing.T) {
	// Generate shared secret (simulating X3DH output)
	var sharedKey [32]byte
	copy(sharedKey[:], []byte("test-shared-secret-32-bytes-long"))

	bobKey, err := GenerateCurve25519Key()
	if err != nil {
		t.Fatal(err)
	}

	aliceID, err := GenerateSessionID()
	if err != nil {
		t.Fatal(err)
	}

	bobID, err := GenerateSessionID()
	if err != nil {
		t.Fatal(err)
	}

	// Alice initiates
	alice, err := NewRatchetSessionInitiator(aliceID, sharedKey, bobKey.PublicKey)
	if err != nil {
		t.Fatalf("alice session: %v", err)
	}

	// Bob responds
	bob, err := NewRatchetSessionResponder(bobID, sharedKey, bobKey)
	if err != nil {
		t.Fatalf("bob session: %v", err)
	}

	// Alice encrypts
	msg, err := alice.Encrypt([]byte("hello bob"))
	if err != nil {
		t.Fatalf("alice encrypt: %v", err)
	}

	// Bob decrypts
	plaintext, err := bob.Decrypt(msg)
	if err != nil {
		t.Fatalf("bob decrypt: %v", err)
	}

	if string(plaintext) != "hello bob" {
		t.Errorf("decrypted = %q, want %q", plaintext, "hello bob")
	}

	// Bob encrypts back
	msg2, err := bob.Encrypt([]byte("hello alice"))
	if err != nil {
		t.Fatalf("bob encrypt: %v", err)
	}

	plaintext2, err := alice.Decrypt(msg2)
	if err != nil {
		t.Fatalf("alice decrypt: %v", err)
	}

	if string(plaintext2) != "hello alice" {
		t.Errorf("decrypted = %q, want %q", plaintext2, "hello alice")
	}
}
