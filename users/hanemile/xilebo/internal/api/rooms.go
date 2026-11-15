package api

import (
	"database/sql"
	"encoding/json"
	"errors"
	"net/http"
	"time"

	"github.com/oklog/ulid/v2"

	"github.com/hanemile/xilebo/internal/storage"
	"github.com/hanemile/xilebo/pkg/protocol"
)

type createRoomRequest struct {
	Name     string `json:"name"`
	Topic    string `json:"topic"`
	IsDirect bool   `json:"is_direct"`
	RoomType string `json:"room_type"`
}

type roomResponse struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	Topic     string `json:"topic"`
	AvatarURL string `json:"avatar_url"`
	IsDirect  bool   `json:"is_direct"`
	RoomType  string `json:"room_type"`
	CreatedBy string `json:"created_by"`
	CreatedAt int64  `json:"created_at"`
}

func (s *Server) handleCreateRoom(w http.ResponseWriter, r *http.Request) {
	userID := UserIDFromContext(r.Context())

	var req createRoomRequest
	if err := readJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "bad_request", "Invalid JSON body")
		return
	}

	if req.RoomType == "" {
		req.RoomType = protocol.RoomTypeChat
	}

	now := time.Now().UnixMilli()
	roomID := ulid.Make().String()

	room, err := s.store.CreateRoom(r.Context(), storage.CreateRoomParams{
		ID:        roomID,
		Name:      req.Name,
		Topic:     req.Topic,
		IsDirect:  req.IsDirect,
		RoomType:  req.RoomType,
		CreatedBy: userID,
		CreatedAt: now,
	})
	if err != nil {
		s.logger.Error("create room", "error", err)
		writeError(w, http.StatusInternalServerError, "internal", "Failed to create room")
		return
	}

	// Auto-join the creator with admin power level
	if err := s.store.AddMember(r.Context(), roomID, userID, protocol.PowerLevelAdmin, now); err != nil {
		s.logger.Error("add creator to room", "error", err)
	}

	writeJSON(w, http.StatusCreated, toRoomResponse(room))
}

func (s *Server) handleListRooms(w http.ResponseWriter, r *http.Request) {
	userID := UserIDFromContext(r.Context())

	rooms, err := s.store.ListUserRooms(r.Context(), userID)
	if err != nil {
		s.logger.Error("list rooms", "error", err)
		writeError(w, http.StatusInternalServerError, "internal", "Failed to list rooms")
		return
	}

	out := make([]roomResponse, len(rooms))
	for i, room := range rooms {
		out[i] = toRoomResponse(room)
	}
	writeJSON(w, http.StatusOK, out)
}

func (s *Server) handleGetRoom(w http.ResponseWriter, r *http.Request) {
	roomID := r.PathValue("roomID")

	room, err := s.store.GetRoom(r.Context(), roomID)
	if errors.Is(err, sql.ErrNoRows) {
		writeError(w, http.StatusNotFound, "not_found", "Room not found")
		return
	}
	if err != nil {
		s.logger.Error("get room", "error", err)
		writeError(w, http.StatusInternalServerError, "internal", "Failed to get room")
		return
	}

	writeJSON(w, http.StatusOK, toRoomResponse(room))
}

func (s *Server) handleJoinRoom(w http.ResponseWriter, r *http.Request) {
	userID := UserIDFromContext(r.Context())
	roomID := r.PathValue("roomID")

	// Verify room exists
	_, err := s.store.GetRoom(r.Context(), roomID)
	if errors.Is(err, sql.ErrNoRows) {
		writeError(w, http.StatusNotFound, "not_found", "Room not found")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal", "Failed to get room")
		return
	}

	now := time.Now().UnixMilli()
	if err := s.store.AddMember(r.Context(), roomID, userID, protocol.PowerLevelDefault, now); err != nil {
		s.logger.Error("join room", "error", err)
		writeError(w, http.StatusInternalServerError, "internal", "Failed to join room")
		return
	}

	writeJSON(w, http.StatusOK, map[string]string{"status": "joined"})
}

func (s *Server) handleLeaveRoom(w http.ResponseWriter, r *http.Request) {
	userID := UserIDFromContext(r.Context())
	roomID := r.PathValue("roomID")

	if err := s.store.RemoveMember(r.Context(), roomID, userID); err != nil {
		s.logger.Error("leave room", "error", err)
		writeError(w, http.StatusInternalServerError, "internal", "Failed to leave room")
		return
	}

	writeJSON(w, http.StatusOK, map[string]string{"status": "left"})
}

func (s *Server) handleGetMembers(w http.ResponseWriter, r *http.Request) {
	roomID := r.PathValue("roomID")

	members, err := s.store.GetMembers(r.Context(), roomID)
	if err != nil {
		s.logger.Error("get members", "error", err)
		writeError(w, http.StatusInternalServerError, "internal", "Failed to get members")
		return
	}

	writeJSON(w, http.StatusOK, members)
}

func (s *Server) handleSetRoomState(w http.ResponseWriter, r *http.Request) {
	userID := UserIDFromContext(r.Context())
	roomID := r.PathValue("roomID")
	eventType := r.PathValue("eventType")

	var content map[string]any
	if err := readJSON(r, &content); err != nil {
		writeError(w, http.StatusBadRequest, "bad_request", "Invalid JSON body")
		return
	}

	contentJSON, _ := json.Marshal(content)

	now := time.Now().UnixMilli()
	if err := s.store.SetRoomState(r.Context(), storage.SetRoomStateParams{
		RoomID:    roomID,
		EventType: eventType,
		Content:   string(contentJSON),
		Sender:    userID,
		Timestamp: now,
	}); err != nil {
		s.logger.Error("set room state", "error", err)
		writeError(w, http.StatusInternalServerError, "internal", "Failed to set room state")
		return
	}

	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func toRoomResponse(room storage.Room) roomResponse {
	return roomResponse{
		ID:        room.ID,
		Name:      room.Name,
		Topic:     room.Topic,
		AvatarURL: room.AvatarURL,
		IsDirect:  room.IsDirect,
		RoomType:  room.RoomType,
		CreatedBy: room.CreatedBy,
		CreatedAt: room.CreatedAt,
	}
}
