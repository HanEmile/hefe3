package sfu

import (
	"encoding/json"
	"fmt"
	"log/slog"

	"github.com/pion/webrtc/v4"
)

// SignalMessage is the WebRTC signaling message format.
type SignalMessage struct {
	Type   string          `json:"type"`   // "offer", "answer", "candidate", "join", "leave"
	CallID string          `json:"call_id"`
	RoomID string          `json:"room_id"`
	PeerID string          `json:"peer_id"`
	Data   json.RawMessage `json:"data,omitempty"`
}

// SignalHandler processes WebRTC signaling messages and manages SFU rooms.
type SignalHandler struct {
	sfu    *SFU
	logger *slog.Logger
}

// NewSignalHandler creates a new signaling handler.
func NewSignalHandler(sfu *SFU, logger *slog.Logger) *SignalHandler {
	return &SignalHandler{sfu: sfu, logger: logger}
}

// HandleJoin creates a new peer connection for the room and returns an offer.
func (h *SignalHandler) HandleJoin(roomID, peerID, userID string) (*webrtc.SessionDescription, error) {
	room := h.sfu.GetOrCreateRoom(roomID)

	_, pc, err := room.AddPeer(peerID, userID)
	if err != nil {
		return nil, fmt.Errorf("add peer: %w", err)
	}

	// Create offer for the client
	offer, err := pc.CreateOffer(nil)
	if err != nil {
		return nil, fmt.Errorf("create offer: %w", err)
	}

	if err := pc.SetLocalDescription(offer); err != nil {
		return nil, fmt.Errorf("set local description: %w", err)
	}

	return pc.LocalDescription(), nil
}

// HandleOffer processes an SDP offer from a client and returns an answer.
func (h *SignalHandler) HandleOffer(roomID, peerID, userID string, offer webrtc.SessionDescription) (*webrtc.SessionDescription, error) {
	room := h.sfu.GetOrCreateRoom(roomID)

	peer, ok := room.GetPeer(peerID)
	if !ok {
		// Create new peer if not exists
		var err error
		_, _, err = room.AddPeer(peerID, userID)
		if err != nil {
			return nil, fmt.Errorf("add peer: %w", err)
		}
		peer, _ = room.GetPeer(peerID)
	}

	pc := peer.Connection

	if err := pc.SetRemoteDescription(offer); err != nil {
		return nil, fmt.Errorf("set remote description: %w", err)
	}

	answer, err := pc.CreateAnswer(nil)
	if err != nil {
		return nil, fmt.Errorf("create answer: %w", err)
	}

	if err := pc.SetLocalDescription(answer); err != nil {
		return nil, fmt.Errorf("set local description: %w", err)
	}

	return pc.LocalDescription(), nil
}

// HandleAnswer processes an SDP answer from a client.
func (h *SignalHandler) HandleAnswer(roomID, peerID string, answer webrtc.SessionDescription) error {
	room := h.sfu.GetOrCreateRoom(roomID)

	peer, ok := room.GetPeer(peerID)
	if !ok {
		return fmt.Errorf("peer %s not found in room %s", peerID, roomID)
	}

	return peer.Connection.SetRemoteDescription(answer)
}

// HandleCandidate adds an ICE candidate from a client.
func (h *SignalHandler) HandleCandidate(roomID, peerID string, candidate webrtc.ICECandidateInit) error {
	room := h.sfu.GetOrCreateRoom(roomID)

	peer, ok := room.GetPeer(peerID)
	if !ok {
		return fmt.Errorf("peer %s not found in room %s", peerID, roomID)
	}

	return peer.Connection.AddICECandidate(candidate)
}

// HandleLeave removes a peer from a room.
func (h *SignalHandler) HandleLeave(roomID, peerID string) {
	room := h.sfu.GetOrCreateRoom(roomID)
	room.RemovePeer(peerID)
	h.sfu.RemoveRoom(roomID) // Clean up if empty
}

// ShouldUseSFU returns true if the room should use SFU mode (4+ participants).
func (h *SignalHandler) ShouldUseSFU(roomID string) bool {
	room := h.sfu.GetOrCreateRoom(roomID)
	return room.PeerCount() >= 3 // SFU mode from 4 participants (3 existing + 1 joining)
}
