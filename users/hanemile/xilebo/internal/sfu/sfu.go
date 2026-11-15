package sfu

import (
	"log/slog"
	"sync"

	"github.com/pion/webrtc/v4"
)

// SFU manages media rooms and peer connections.
type SFU struct {
	mu     sync.RWMutex
	rooms  map[string]*Room
	config webrtc.Configuration
	logger *slog.Logger
}

// New creates a new SFU instance.
func New(iceServers []string, logger *slog.Logger) *SFU {
	urls := make([]webrtc.ICEServer, len(iceServers))
	for i, s := range iceServers {
		urls[i] = webrtc.ICEServer{URLs: []string{s}}
	}

	return &SFU{
		rooms: make(map[string]*Room),
		config: webrtc.Configuration{
			ICEServers: urls,
		},
		logger: logger,
	}
}

// GetOrCreateRoom returns an existing room or creates one.
func (s *SFU) GetOrCreateRoom(roomID string) *Room {
	s.mu.Lock()
	defer s.mu.Unlock()

	if room, ok := s.rooms[roomID]; ok {
		return room
	}

	room := newRoom(roomID, s.config, s.logger)
	s.rooms[roomID] = room
	return room
}

// RemoveRoom deletes a room if it's empty.
func (s *SFU) RemoveRoom(roomID string) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if room, ok := s.rooms[roomID]; ok {
		if room.PeerCount() == 0 {
			delete(s.rooms, roomID)
		}
	}
}

// RoomCount returns the number of active rooms.
func (s *SFU) RoomCount() int {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return len(s.rooms)
}

// Config returns the WebRTC configuration (for P2P fallback signaling).
func (s *SFU) Config() webrtc.Configuration {
	return s.config
}
