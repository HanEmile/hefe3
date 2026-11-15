package ws

import (
	"log/slog"
	"sync"
)

// OutgoingMessage is the message format broadcast to WebSocket clients.
type OutgoingMessage struct {
	Type    string `json:"type"`
	RoomID  string `json:"room_id,omitempty"`
	Sender  string `json:"sender,omitempty"`
	Content any    `json:"content"`
}

// Hub maintains active WebSocket connections and broadcasts messages.
type Hub struct {
	mu     sync.RWMutex
	conns  map[*Conn]bool
	rooms  map[string]map[*Conn]bool // roomID -> connections
	logger *slog.Logger
}

// NewHub creates a new Hub.
func NewHub(logger *slog.Logger) *Hub {
	return &Hub{
		conns:  make(map[*Conn]bool),
		rooms:  make(map[string]map[*Conn]bool),
		logger: logger,
	}
}

// Run starts the hub. Currently a no-op since we use mutex-based dispatch.
func (h *Hub) Run() {}

// Register adds a connection to the hub.
func (h *Hub) Register(c *Conn) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.conns[c] = true
}

// Unregister removes a connection from the hub and all rooms.
func (h *Hub) Unregister(c *Conn) {
	h.mu.Lock()
	defer h.mu.Unlock()
	delete(h.conns, c)
	for roomID, members := range h.rooms {
		delete(members, c)
		if len(members) == 0 {
			delete(h.rooms, roomID)
		}
	}
}

// JoinRoom subscribes a connection to a room's broadcasts.
func (h *Hub) JoinRoom(roomID string, c *Conn) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.rooms[roomID] == nil {
		h.rooms[roomID] = make(map[*Conn]bool)
	}
	h.rooms[roomID][c] = true
}

// LeaveRoom unsubscribes a connection from a room.
func (h *Hub) LeaveRoom(roomID string, c *Conn) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if members, ok := h.rooms[roomID]; ok {
		delete(members, c)
		if len(members) == 0 {
			delete(h.rooms, roomID)
		}
	}
}

// BroadcastToRoom sends a message to all connections in a room.
func (h *Hub) BroadcastToRoom(roomID string, msg OutgoingMessage) {
	h.mu.RLock()
	members := h.rooms[roomID]
	// Copy the slice under read lock
	targets := make([]*Conn, 0, len(members))
	for c := range members {
		targets = append(targets, c)
	}
	h.mu.RUnlock()

	for _, c := range targets {
		if err := c.Send(msg); err != nil {
			h.logger.Debug("ws send failed", "error", err)
		}
	}
}

// BroadcastToUser sends a message to all connections for a specific user.
func (h *Hub) BroadcastToUser(userID string, msg OutgoingMessage) {
	h.mu.RLock()
	targets := make([]*Conn, 0)
	for c := range h.conns {
		if c.UserID() == userID {
			targets = append(targets, c)
		}
	}
	h.mu.RUnlock()

	for _, c := range targets {
		if err := c.Send(msg); err != nil {
			h.logger.Debug("ws send failed", "error", err)
		}
	}
}
