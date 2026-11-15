package crypto

import (
	"crypto/rand"
	"fmt"

	dr "github.com/status-im/doubleratchet"
)

const maxSkip = 1000

// RatchetSession wraps the Double Ratchet session.
type RatchetSession struct {
	session dr.Session
}

// dhPair implements dr.DHPair for the Double Ratchet library.
type dhPair struct {
	priv [32]byte
	pub  [32]byte
}

func (d dhPair) PrivateKey() dr.Key { return d.priv[:] }
func (d dhPair) PublicKey() dr.Key  { return d.pub[:] }

// sessionStore implements dr.SessionStorage in-memory.
// In production, this would be backed by the database.
type sessionStore struct {
	state *dr.State
}

func (s *sessionStore) Load(id []byte) (*dr.State, error) {
	if s.state == nil {
		return nil, fmt.Errorf("session not found")
	}
	return s.state, nil
}

func (s *sessionStore) Save(id []byte, state *dr.State) error {
	s.state = state
	return nil
}

// NewRatchetSessionInitiator creates a new Double Ratchet session for the initiator (Alice).
// sharedKey comes from X3DH. remoteKey is Bob's signed pre-key public key.
func NewRatchetSessionInitiator(sessionID []byte, sharedKey [32]byte, remoteKey [32]byte) (*RatchetSession, error) {
	store := &sessionStore{}

	sess, err := dr.NewWithRemoteKey(
		sessionID,
		sharedKey[:],
		remoteKey[:],
		store,
		dr.WithMaxSkip(maxSkip),
	)
	if err != nil {
		return nil, fmt.Errorf("create ratchet session: %w", err)
	}

	return &RatchetSession{session: sess}, nil
}

// NewRatchetSessionResponder creates a new Double Ratchet session for the responder (Bob).
// sharedKey comes from X3DH. keyPair is Bob's signed pre-key pair.
func NewRatchetSessionResponder(sessionID []byte, sharedKey [32]byte, keyPair *Curve25519KeyPair) (*RatchetSession, error) {
	store := &sessionStore{}

	pair := dhPair{priv: keyPair.PrivateKey, pub: keyPair.PublicKey}
	sess, err := dr.New(
		sessionID,
		sharedKey[:],
		pair,
		store,
		dr.WithMaxSkip(maxSkip),
	)
	if err != nil {
		return nil, fmt.Errorf("create ratchet session: %w", err)
	}

	return &RatchetSession{session: sess}, nil
}

// Encrypt encrypts a plaintext message using the Double Ratchet.
func (s *RatchetSession) Encrypt(plaintext []byte) (dr.Message, error) {
	return s.session.RatchetEncrypt(plaintext, nil)
}

// Decrypt decrypts a Double Ratchet message.
func (s *RatchetSession) Decrypt(msg dr.Message) ([]byte, error) {
	return s.session.RatchetDecrypt(msg, nil)
}

// GenerateSessionID creates a random session identifier.
func GenerateSessionID() ([]byte, error) {
	id := make([]byte, 32)
	if _, err := rand.Read(id); err != nil {
		return nil, err
	}
	return id, nil
}
