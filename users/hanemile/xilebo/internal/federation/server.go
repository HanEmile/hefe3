package federation

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"time"

	"github.com/hanemile/xilebo/internal/storage"
)

// FedServer handles inbound federation requests from remote servers.
type FedServer struct {
	store    storage.Store
	hostname string
	logger   *slog.Logger
}

// NewFedServer creates a federation server handler.
func NewFedServer(store storage.Store, hostname string, logger *slog.Logger) *FedServer {
	return &FedServer{
		store:    store,
		hostname: hostname,
		logger:   logger,
	}
}

// Handler returns the federation API HTTP handler.
func (fs *FedServer) Handler() http.Handler {
	mux := http.NewServeMux()

	fed := "/_xb/federation/v1"

	// Well-known (public, no auth)
	mux.HandleFunc("GET /.well-known/xilebo-server", fs.handleWellKnown)

	// Federation endpoints (signed)
	mux.HandleFunc("POST "+fed+"/send", fs.handleReceiveEvents)
	mux.HandleFunc("GET "+fed+"/keys/{handle}", fs.handleQueryKeys)
	mux.HandleFunc("POST "+fed+"/keys/claim", fs.handleClaimKeys)
	mux.HandleFunc("GET "+fed+"/user/{handle}", fs.handleQueryUser)
	mux.HandleFunc("POST "+fed+"/invite", fs.handleInvite)
	mux.HandleFunc("POST "+fed+"/join", fs.handleJoin)
	mux.HandleFunc("POST "+fed+"/leave", fs.handleLeave)
	mux.HandleFunc("GET "+fed+"/rooms/{roomID}/state", fs.handleRoomState)
	mux.HandleFunc("POST "+fed+"/backfill", fs.handleBackfill)

	return mux
}

func (fs *FedServer) handleWellKnown(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(WellKnown{
		Server: fs.hostname + ":8448",
	})
}

func (fs *FedServer) handleReceiveEvents(w http.ResponseWriter, r *http.Request) {
	body, err := io.ReadAll(io.LimitReader(r.Body, 10*1024*1024)) // 10MB max
	if err != nil {
		http.Error(w, "read body", http.StatusBadRequest)
		return
	}

	// TODO: Verify signature against known server key
	origin := OriginFromRequest(r)
	fs.logger.Info("federation receive", "origin", origin, "size", len(body))

	var payload struct {
		Origin string            `json:"origin"`
		PDUs   []json.RawMessage `json:"pdus"`
	}
	if err := json.Unmarshal(body, &payload); err != nil {
		http.Error(w, "invalid json", http.StatusBadRequest)
		return
	}

	// Process each event
	for _, pdu := range payload.PDUs {
		var event FederationEvent
		if err := json.Unmarshal(pdu, &event); err != nil {
			fs.logger.Error("parse federation event", "error", err)
			continue
		}

		if err := fs.processEvent(r.Context(), &event); err != nil {
			fs.logger.Error("process federation event", "error", err, "event_id", event.ID)
		}
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
}

func (fs *FedServer) processEvent(ctx context.Context, event *FederationEvent) error {
	switch event.Type {
	case "m.room.message", "m.room.encrypted":
		return fs.store.InsertMessage(ctx, storage.InsertMessageParams{
			ID:        event.ID,
			RoomID:    event.RoomID,
			Sender:    event.Sender,
			EventType: event.Type,
			Content:   string(event.Content),
			OriginTS:  event.Timestamp,
			ServerTS:  event.Timestamp,
		})
	case "m.room.member":
		// Handle remote user joining/leaving
		var memberContent struct {
			Membership string `json:"membership"`
		}
		json.Unmarshal(event.Content, &memberContent)

		switch memberContent.Membership {
		case "join":
			return fs.store.AddMember(ctx, event.RoomID, event.Sender, 0, event.Timestamp)
		case "leave":
			return fs.store.RemoveMember(ctx, event.RoomID, event.Sender)
		}
	}
	return nil
}

func (fs *FedServer) handleQueryKeys(w http.ResponseWriter, r *http.Request) {
	handle := r.PathValue("handle")

	user, err := fs.store.GetUserByHandle(r.Context(), handle)
	if err != nil {
		http.Error(w, "user not found", http.StatusNotFound)
		return
	}

	signedPK, err := fs.store.GetSignedPreKey(r.Context(), user.ID)
	if err != nil {
		http.Error(w, "no keys", http.StatusNotFound)
		return
	}

	bundle := map[string]any{
		"user_id":      user.Handle,
		"identity_key": user.IdentityKey,
		"signed_pre_key": map[string]any{
			"key_id":    signedPK.KeyID,
			"public_key": signedPK.PublicKey,
			"signature":  signedPK.Signature,
		},
	}

	// Try to include a one-time key
	otk, err := fs.store.ClaimPreKey(r.Context(), user.ID)
	if err == nil {
		bundle["one_time_pre_key"] = map[string]any{
			"key_id":    otk.KeyID,
			"public_key": otk.PublicKey,
		}
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(bundle)
}

func (fs *FedServer) handleClaimKeys(w http.ResponseWriter, r *http.Request) {
	var req struct {
		UserID string `json:"user_id"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}

	// Same as handleQueryKeys but specifically for claiming
	fs.handleQueryKeys(w, r)
}

func (fs *FedServer) handleQueryUser(w http.ResponseWriter, r *http.Request) {
	handle := r.PathValue("handle")

	user, err := fs.store.GetUserByHandle(r.Context(), handle)
	if err != nil {
		http.Error(w, "user not found", http.StatusNotFound)
		return
	}

	tags, _ := fs.store.GetUserTags(r.Context(), user.ID)
	if tags == nil {
		tags = []string{}
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]any{
		"handle":       user.Handle,
		"display_name": user.DisplayName,
		"avatar_url":   user.AvatarURL,
		"identity_key": user.IdentityKey,
		"tags":         tags,
	})
}

func (fs *FedServer) handleInvite(w http.ResponseWriter, r *http.Request) {
	body, _ := io.ReadAll(io.LimitReader(r.Body, 64*1024))

	var invite struct {
		RoomID string `json:"room_id"`
		UserID string `json:"user_id"`
		Sender string `json:"sender"`
	}
	if err := json.Unmarshal(body, &invite); err != nil {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}

	fs.logger.Info("federation invite",
		"room", invite.RoomID,
		"user", invite.UserID,
		"from", invite.Sender,
	)

	// Store as a to-device message for the invited user
	user, err := fs.store.GetUserByHandle(r.Context(), invite.UserID)
	if err != nil {
		http.Error(w, "user not found", http.StatusNotFound)
		return
	}

	fs.store.StoreToDeviceMessage(r.Context(), storage.ToDeviceParams{
		Sender:    invite.Sender,
		Recipient: user.ID,
		EventType: "m.room.invite",
		Content:   string(body),
		CreatedAt: timeNowMillis(),
	})

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
}

func (fs *FedServer) handleJoin(w http.ResponseWriter, r *http.Request) {
	body, _ := io.ReadAll(io.LimitReader(r.Body, 64*1024))

	var req struct {
		RoomID string `json:"room_id"`
		UserID string `json:"user_id"`
	}
	if err := json.Unmarshal(body, &req); err != nil {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}

	now := timeNowMillis()
	if err := fs.store.AddMember(r.Context(), req.RoomID, req.UserID, 0, now); err != nil {
		fs.logger.Error("federation join", "error", err)
		http.Error(w, "failed to join", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{"status": "joined"})
}

func (fs *FedServer) handleLeave(w http.ResponseWriter, r *http.Request) {
	body, _ := io.ReadAll(io.LimitReader(r.Body, 64*1024))

	var req struct {
		RoomID string `json:"room_id"`
		UserID string `json:"user_id"`
	}
	if err := json.Unmarshal(body, &req); err != nil {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}

	if err := fs.store.RemoveMember(r.Context(), req.RoomID, req.UserID); err != nil {
		fs.logger.Error("federation leave", "error", err)
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{"status": "left"})
}

func (fs *FedServer) handleRoomState(w http.ResponseWriter, r *http.Request) {
	roomID := r.PathValue("roomID")

	state, err := fs.store.GetRoomState(r.Context(), roomID)
	if err != nil {
		http.Error(w, "room not found", http.StatusNotFound)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(state)
}

func (fs *FedServer) handleBackfill(w http.ResponseWriter, r *http.Request) {
	body, _ := io.ReadAll(io.LimitReader(r.Body, 64*1024))

	var req struct {
		RoomID string `json:"room_id"`
		Limit  int    `json:"limit"`
		Before int64  `json:"before"`
	}
	if err := json.Unmarshal(body, &req); err != nil {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}

	if req.Limit <= 0 || req.Limit > 100 {
		req.Limit = 50
	}

	msgs, err := fs.store.GetMessages(r.Context(), req.RoomID, req.Before, req.Limit)
	if err != nil {
		http.Error(w, "failed to get messages", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]any{
		"events": msgs,
	})
}

func timeNowMillis() int64 {
	return time.Now().UnixMilli()
}
