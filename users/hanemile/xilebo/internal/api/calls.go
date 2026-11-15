package api

import (
	"encoding/json"
	"net/http"

	"github.com/pion/webrtc/v4"
)

type callOfferRequest struct {
	RoomID string                    `json:"room_id"`
	PeerID string                    `json:"peer_id"`
	Offer  webrtc.SessionDescription `json:"offer"`
}

type callCandidateRequest struct {
	RoomID    string                   `json:"room_id"`
	PeerID    string                   `json:"peer_id"`
	Candidate webrtc.ICECandidateInit  `json:"candidate"`
}

type callAnswerRequest struct {
	RoomID string                    `json:"room_id"`
	PeerID string                    `json:"peer_id"`
	Answer webrtc.SessionDescription `json:"answer"`
}

func (s *Server) handleCallOffer(w http.ResponseWriter, r *http.Request) {
	if s.signalHandler == nil {
		writeError(w, http.StatusServiceUnavailable, "sfu_disabled", "Video calls are not enabled")
		return
	}

	userID := UserIDFromContext(r.Context())

	var req callOfferRequest
	if err := readJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "bad_request", "Invalid JSON body")
		return
	}

	if req.PeerID == "" {
		req.PeerID = userID
	}

	answer, err := s.signalHandler.HandleOffer(req.RoomID, req.PeerID, userID, req.Offer)
	if err != nil {
		s.logger.Error("handle offer", "error", err)
		writeError(w, http.StatusInternalServerError, "internal", "Failed to process offer")
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"answer":   answer,
		"use_sfu":  s.signalHandler.ShouldUseSFU(req.RoomID),
	})
}

func (s *Server) handleCallAnswer(w http.ResponseWriter, r *http.Request) {
	if s.signalHandler == nil {
		writeError(w, http.StatusServiceUnavailable, "sfu_disabled", "Video calls are not enabled")
		return
	}

	var req callAnswerRequest
	if err := readJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "bad_request", "Invalid JSON body")
		return
	}

	if err := s.signalHandler.HandleAnswer(req.RoomID, req.PeerID, req.Answer); err != nil {
		s.logger.Error("handle answer", "error", err)
		writeError(w, http.StatusInternalServerError, "internal", "Failed to process answer")
		return
	}

	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func (s *Server) handleCallCandidate(w http.ResponseWriter, r *http.Request) {
	if s.signalHandler == nil {
		writeError(w, http.StatusServiceUnavailable, "sfu_disabled", "Video calls are not enabled")
		return
	}

	var req callCandidateRequest
	if err := readJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "bad_request", "Invalid JSON body")
		return
	}

	if err := s.signalHandler.HandleCandidate(req.RoomID, req.PeerID, req.Candidate); err != nil {
		s.logger.Error("handle candidate", "error", err)
		writeError(w, http.StatusInternalServerError, "internal", "Failed to process candidate")
		return
	}

	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func (s *Server) handleCallLeave(w http.ResponseWriter, r *http.Request) {
	if s.signalHandler == nil {
		writeError(w, http.StatusServiceUnavailable, "sfu_disabled", "Video calls are not enabled")
		return
	}

	userID := UserIDFromContext(r.Context())

	var body struct {
		RoomID string `json:"room_id"`
		PeerID string `json:"peer_id"`
	}
	if err := readJSON(r, &body); err != nil {
		writeError(w, http.StatusBadRequest, "bad_request", "Invalid JSON body")
		return
	}
	if body.PeerID == "" {
		body.PeerID = userID
	}

	s.signalHandler.HandleLeave(body.RoomID, body.PeerID)

	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

// handleCallICEServers returns the ICE server configuration for P2P calls.
func (s *Server) handleCallICEServers(w http.ResponseWriter, r *http.Request) {
	servers := s.cfg.SFU.ICEServers
	result := make([]map[string]any, len(servers))
	for i, url := range servers {
		result[i] = map[string]any{"urls": []string{url}}
	}
	writeJSON(w, http.StatusOK, map[string]any{"ice_servers": result})
}

// Suppress unused import warning — json is used for type definitions
var _ = json.Marshal
