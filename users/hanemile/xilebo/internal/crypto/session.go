package crypto

import (
	"encoding/json"
	"fmt"
	"sync"
)

// SessionManager manages encryption sessions for a user.
type SessionManager struct {
	mu              sync.RWMutex
	identityKey     *Curve25519KeyPair
	pairwiseSessions map[string]*RatchetSession // remote user -> session
	senderKeys       map[string]*SenderKeyState  // room -> our sender key
	receivedKeys     map[string]map[string]*SenderKeyState // room -> sender -> their key
}

// NewSessionManager creates a new session manager.
func NewSessionManager(identityKey *Curve25519KeyPair) *SessionManager {
	return &SessionManager{
		identityKey:      identityKey,
		pairwiseSessions: make(map[string]*RatchetSession),
		senderKeys:       make(map[string]*SenderKeyState),
		receivedKeys:     make(map[string]map[string]*SenderKeyState),
	}
}

// GetOrCreatePairwiseSession returns an existing session or creates one via X3DH.
func (sm *SessionManager) GetOrCreatePairwiseSession(
	remoteUserID string,
	bundle *PreKeyBundle,
) (*RatchetSession, *X3DHResult, error) {
	sm.mu.Lock()
	defer sm.mu.Unlock()

	if sess, ok := sm.pairwiseSessions[remoteUserID]; ok {
		return sess, nil, nil
	}

	// Perform X3DH
	x3dhResult, err := X3DHInitiate(sm.identityKey, bundle)
	if err != nil {
		return nil, nil, fmt.Errorf("x3dh initiate: %w", err)
	}

	sessionID, err := GenerateSessionID()
	if err != nil {
		return nil, nil, err
	}

	sess, err := NewRatchetSessionInitiator(sessionID, x3dhResult.SharedKey, bundle.SignedPreKey.PublicKey)
	if err != nil {
		return nil, nil, fmt.Errorf("create ratchet: %w", err)
	}

	sm.pairwiseSessions[remoteUserID] = sess
	return sess, x3dhResult, nil
}

// SetPairwiseSession sets a session directly (for the responder side).
func (sm *SessionManager) SetPairwiseSession(remoteUserID string, sess *RatchetSession) {
	sm.mu.Lock()
	defer sm.mu.Unlock()
	sm.pairwiseSessions[remoteUserID] = sess
}

// GetSenderKey returns our sender key for a room, creating one if needed.
func (sm *SessionManager) GetSenderKey(roomID string) (*SenderKeyState, bool, error) {
	sm.mu.Lock()
	defer sm.mu.Unlock()

	if sk, ok := sm.senderKeys[roomID]; ok {
		return sk, false, nil
	}

	sk, err := NewSenderKey()
	if err != nil {
		return nil, false, err
	}
	sm.senderKeys[roomID] = sk
	return sk, true, nil // true = newly created, needs distribution
}

// SetReceivedSenderKey stores a sender key received from another user.
func (sm *SessionManager) SetReceivedSenderKey(roomID, senderUserID string, chainKey [32]byte, iteration uint32) {
	sm.mu.Lock()
	defer sm.mu.Unlock()

	if sm.receivedKeys[roomID] == nil {
		sm.receivedKeys[roomID] = make(map[string]*SenderKeyState)
	}
	sm.receivedKeys[roomID][senderUserID] = SenderKeyFromBytes(chainKey, iteration)
}

// GetReceivedSenderKey returns a received sender key for decryption.
func (sm *SessionManager) GetReceivedSenderKey(roomID, senderUserID string) (*SenderKeyState, bool) {
	sm.mu.RLock()
	defer sm.mu.RUnlock()

	if room, ok := sm.receivedKeys[roomID]; ok {
		if sk, ok := room[senderUserID]; ok {
			return sk, true
		}
	}
	return nil, false
}

// RotateSenderKey creates a new sender key for a room (e.g., after member removal).
func (sm *SessionManager) RotateSenderKey(roomID string) (*SenderKeyState, error) {
	sm.mu.Lock()
	defer sm.mu.Unlock()

	sk, err := NewSenderKey()
	if err != nil {
		return nil, err
	}
	sm.senderKeys[roomID] = sk
	return sk, nil
}

// SenderKeyDistribution is the payload sent to group members via to-device messages.
type SenderKeyDistribution struct {
	RoomID    string `json:"room_id"`
	SenderID  string `json:"sender_id"`
	ChainKey  []byte `json:"chain_key"`
	Iteration uint32 `json:"iteration"`
}

// MarshalDistribution creates a distribution message for the current sender key.
func (sm *SessionManager) MarshalDistribution(roomID, senderID string) ([]byte, error) {
	sm.mu.RLock()
	sk, ok := sm.senderKeys[roomID]
	sm.mu.RUnlock()

	if !ok {
		return nil, fmt.Errorf("no sender key for room %s", roomID)
	}

	ck := sk.ChainKey()
	dist := SenderKeyDistribution{
		RoomID:    roomID,
		SenderID:  senderID,
		ChainKey:  ck[:],
		Iteration: sk.Iteration(),
	}
	return json.Marshal(dist)
}
