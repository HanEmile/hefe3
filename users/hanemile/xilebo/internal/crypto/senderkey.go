package crypto

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"sync"
)

// SenderKeyState holds the state for a sender in a group.
type SenderKeyState struct {
	mu        sync.Mutex
	chainKey  [32]byte
	iteration uint32
}

// SenderKeyMessage is an encrypted group message.
type SenderKeyMessage struct {
	SenderKeyID string `json:"sender_key_id"`
	Iteration   uint32 `json:"iteration"`
	Ciphertext  []byte `json:"ciphertext"`
	IV          []byte `json:"iv"`
	Signature   []byte `json:"signature"`
}

// NewSenderKey generates a new random sender key chain.
func NewSenderKey() (*SenderKeyState, error) {
	var chainKey [32]byte
	if _, err := rand.Read(chainKey[:]); err != nil {
		return nil, fmt.Errorf("generate sender key: %w", err)
	}
	return &SenderKeyState{
		chainKey:  chainKey,
		iteration: 0,
	}, nil
}

// SenderKeyFromBytes recreates a sender key from its chain key bytes.
func SenderKeyFromBytes(chainKey [32]byte, iteration uint32) *SenderKeyState {
	return &SenderKeyState{
		chainKey:  chainKey,
		iteration: iteration,
	}
}

// ChainKey returns the current chain key (for distribution to group members).
func (sk *SenderKeyState) ChainKey() [32]byte {
	sk.mu.Lock()
	defer sk.mu.Unlock()
	return sk.chainKey
}

// Iteration returns the current chain iteration.
func (sk *SenderKeyState) Iteration() uint32 {
	sk.mu.Lock()
	defer sk.mu.Unlock()
	return sk.iteration
}

// Encrypt encrypts a message using the sender key chain.
// Each call advances the chain (forward secrecy within the group).
func (sk *SenderKeyState) Encrypt(plaintext []byte) (*SenderKeyMessage, error) {
	sk.mu.Lock()
	defer sk.mu.Unlock()

	// Derive message key from chain key
	messageKey := deriveMessageKey(sk.chainKey)

	// Advance chain key (hash ratchet)
	sk.chainKey = advanceChainKey(sk.chainKey)
	iteration := sk.iteration
	sk.iteration++

	// Encrypt with AES-256-GCM
	block, err := aes.NewCipher(messageKey[:])
	if err != nil {
		return nil, fmt.Errorf("aes: %w", err)
	}

	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, fmt.Errorf("gcm: %w", err)
	}

	iv := make([]byte, gcm.NonceSize())
	if _, err := io.ReadFull(rand.Reader, iv); err != nil {
		return nil, fmt.Errorf("iv: %w", err)
	}

	ciphertext := gcm.Seal(nil, iv, plaintext, nil)

	// Compute HMAC signature for authentication
	sig := computeSignature(messageKey[:], ciphertext, iteration)

	return &SenderKeyMessage{
		Iteration:  iteration,
		Ciphertext: ciphertext,
		IV:         iv,
		Signature:  sig,
	}, nil
}

// Decrypt decrypts a message using the sender key chain.
// The receiver must advance their chain to match the sender's iteration.
func (sk *SenderKeyState) Decrypt(msg *SenderKeyMessage) ([]byte, error) {
	sk.mu.Lock()
	defer sk.mu.Unlock()

	// Advance chain to the correct iteration
	if msg.Iteration < sk.iteration {
		return nil, errors.New("senderkey: message from past iteration (replay?)")
	}

	// Skip forward if needed
	chainKey := sk.chainKey
	currentIter := sk.iteration
	for currentIter < msg.Iteration {
		chainKey = advanceChainKey(chainKey)
		currentIter++
	}

	// Derive message key
	messageKey := deriveMessageKey(chainKey)

	// Verify signature
	expectedSig := computeSignature(messageKey[:], msg.Ciphertext, msg.Iteration)
	if !hmac.Equal(msg.Signature, expectedSig) {
		return nil, errors.New("senderkey: signature verification failed")
	}

	// Decrypt
	block, err := aes.NewCipher(messageKey[:])
	if err != nil {
		return nil, fmt.Errorf("aes: %w", err)
	}

	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, fmt.Errorf("gcm: %w", err)
	}

	plaintext, err := gcm.Open(nil, msg.IV, msg.Ciphertext, nil)
	if err != nil {
		return nil, fmt.Errorf("decrypt: %w", err)
	}

	// Update state to next iteration
	sk.chainKey = advanceChainKey(chainKey)
	sk.iteration = msg.Iteration + 1

	return plaintext, nil
}

// deriveMessageKey derives a message encryption key from the chain key.
func deriveMessageKey(chainKey [32]byte) [32]byte {
	h := hmac.New(sha256.New, chainKey[:])
	h.Write([]byte{0x01}) // Message key derivation constant
	var key [32]byte
	copy(key[:], h.Sum(nil))
	return key
}

// advanceChainKey derives the next chain key (hash ratchet).
func advanceChainKey(chainKey [32]byte) [32]byte {
	h := hmac.New(sha256.New, chainKey[:])
	h.Write([]byte{0x02}) // Chain key derivation constant
	var next [32]byte
	copy(next[:], h.Sum(nil))
	return next
}

// computeSignature computes HMAC-SHA256 over ciphertext and iteration.
func computeSignature(key, ciphertext []byte, iteration uint32) []byte {
	h := hmac.New(sha256.New, key)
	h.Write(ciphertext)
	var iterBytes [4]byte
	binary.BigEndian.PutUint32(iterBytes[:], iteration)
	h.Write(iterBytes[:])
	return h.Sum(nil)
}
