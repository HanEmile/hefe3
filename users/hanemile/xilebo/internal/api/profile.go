package api

import (
	"database/sql"
	"errors"
	"net/http"

	"github.com/hanemile/xilebo/pkg/protocol"
)

type updateProfileRequest struct {
	DisplayName string   `json:"display_name"`
	AvatarURL   string   `json:"avatar_url"`
	Tags        []string `json:"tags"`
}

func (s *Server) handleGetProfile(w http.ResponseWriter, r *http.Request) {
	handle := r.PathValue("handle")

	user, err := s.store.GetUserByHandle(r.Context(), handle)
	if errors.Is(err, sql.ErrNoRows) {
		writeError(w, http.StatusNotFound, "not_found", "User not found")
		return
	}
	if err != nil {
		s.logger.Error("get user", "error", err)
		writeError(w, http.StatusInternalServerError, "internal", "Failed to get user")
		return
	}

	tags, err := s.store.GetUserTags(r.Context(), user.ID)
	if err != nil {
		tags = []string{}
	}

	writeJSON(w, http.StatusOK, protocol.Profile{
		Handle:      user.Handle,
		DisplayName: user.DisplayName,
		AvatarURL:   user.AvatarURL,
		IdentityKey: user.IdentityKey,
		Tags:        tags,
	})
}

func (s *Server) handleUpdateProfile(w http.ResponseWriter, r *http.Request) {
	userID := UserIDFromContext(r.Context())

	var req updateProfileRequest
	if err := readJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "bad_request", "Invalid JSON body")
		return
	}

	if err := s.store.UpdateProfile(r.Context(), userID, req.DisplayName, req.AvatarURL); err != nil {
		s.logger.Error("update profile", "error", err)
		writeError(w, http.StatusInternalServerError, "internal", "Failed to update profile")
		return
	}

	if req.Tags != nil {
		if err := s.store.SetUserTags(r.Context(), userID, req.Tags); err != nil {
			s.logger.Error("set user tags", "error", err)
		}
	}

	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}
