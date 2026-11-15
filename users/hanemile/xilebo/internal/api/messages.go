package api

import (
	"net/http"
	"strconv"
	"time"

	"github.com/oklog/ulid/v2"

	"github.com/hanemile/xilebo/internal/api/ws"
	"github.com/hanemile/xilebo/internal/storage"
	"github.com/hanemile/xilebo/pkg/protocol"
)

type sendMessageRequest struct {
	EventType string `json:"event_type"`
	Content   string `json:"content"`
}

type messageResponse struct {
	ID        string `json:"id"`
	RoomID    string `json:"room_id"`
	Sender    string `json:"sender"`
	EventType string `json:"event_type"`
	Content   string `json:"content"`
	OriginTS  int64  `json:"origin_ts"`
	ServerTS  int64  `json:"server_ts"`
}

func (s *Server) handleSendMessage(w http.ResponseWriter, r *http.Request) {
	userID := UserIDFromContext(r.Context())
	roomID := r.PathValue("roomID")

	var req sendMessageRequest
	if err := readJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "bad_request", "Invalid JSON body")
		return
	}

	if req.EventType == "" {
		req.EventType = protocol.EventRoomMessage
	}

	now := time.Now().UnixMilli()
	msgID := ulid.Make().String()

	// Get the sender's handle for the event
	user, err := s.store.GetUserByID(r.Context(), userID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal", "Failed to get user")
		return
	}

	if err := s.store.InsertMessage(r.Context(), storage.InsertMessageParams{
		ID:        msgID,
		RoomID:    roomID,
		Sender:    user.Handle,
		EventType: req.EventType,
		Content:   req.Content,
		OriginTS:  now,
		ServerTS:  now,
	}); err != nil {
		s.logger.Error("insert message", "error", err)
		writeError(w, http.StatusInternalServerError, "internal", "Failed to send message")
		return
	}

	// Broadcast to WebSocket clients in this room
	s.hub.BroadcastToRoom(roomID, ws.OutgoingMessage{
		Type:   req.EventType,
		RoomID: roomID,
		Sender: user.Handle,
		Content: map[string]any{
			"id":        msgID,
			"body":      req.Content,
			"origin_ts": now,
			"server_ts": now,
		},
	})

	writeJSON(w, http.StatusOK, messageResponse{
		ID:        msgID,
		RoomID:    roomID,
		Sender:    user.Handle,
		EventType: req.EventType,
		Content:   req.Content,
		OriginTS:  now,
		ServerTS:  now,
	})
}

func (s *Server) handleGetMessages(w http.ResponseWriter, r *http.Request) {
	roomID := r.PathValue("roomID")

	before := time.Now().UnixMilli()
	if v := r.URL.Query().Get("before"); v != "" {
		if parsed, err := strconv.ParseInt(v, 10, 64); err == nil {
			before = parsed
		}
	}

	limit := 50
	if v := r.URL.Query().Get("limit"); v != "" {
		if parsed, err := strconv.Atoi(v); err == nil && parsed > 0 && parsed <= 200 {
			limit = parsed
		}
	}

	msgs, err := s.store.GetMessages(r.Context(), roomID, before, limit)
	if err != nil {
		s.logger.Error("get messages", "error", err)
		writeError(w, http.StatusInternalServerError, "internal", "Failed to get messages")
		return
	}

	out := make([]messageResponse, len(msgs))
	for i, m := range msgs {
		out[i] = messageResponse{
			ID:        m.ID,
			RoomID:    m.RoomID,
			Sender:    m.Sender,
			EventType: m.EventType,
			Content:   m.Content,
			OriginTS:  m.OriginTS,
			ServerTS:  m.ServerTS,
		}
	}
	writeJSON(w, http.StatusOK, out)
}
