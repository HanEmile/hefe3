package api

import (
	"net/http"
	"time"

	"github.com/hanemile/xilebo/internal/storage"
	"github.com/hanemile/xilebo/pkg/protocol"
)

type uploadKeysRequest struct {
	SignedPreKey *protocol.PublicPreKey  `json:"signed_pre_key,omitempty"`
	OneTimeKeys  []protocol.PublicPreKey `json:"one_time_keys,omitempty"`
}

type claimKeysRequest struct {
	UserID string `json:"user_id"`
}

func (s *Server) handleUploadKeys(w http.ResponseWriter, r *http.Request) {
	userID := UserIDFromContext(r.Context())

	var req uploadKeysRequest
	if err := readJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "bad_request", "Invalid JSON body")
		return
	}

	now := time.Now().UnixMilli()

	if req.SignedPreKey != nil {
		if err := s.store.StorePreKey(r.Context(), storage.StorePreKeyParams{
			UserID:    userID,
			KeyID:     int64(req.SignedPreKey.KeyID),
			PublicKey: req.SignedPreKey.PublicKey,
			IsSigned:  true,
			Signature: req.SignedPreKey.Signature,
			CreatedAt: now,
		}); err != nil {
			s.logger.Error("store signed pre-key", "error", err)
			writeError(w, http.StatusInternalServerError, "internal", "Failed to store signed pre-key")
			return
		}
	}

	for _, otk := range req.OneTimeKeys {
		if err := s.store.StorePreKey(r.Context(), storage.StorePreKeyParams{
			UserID:    userID,
			KeyID:     int64(otk.KeyID),
			PublicKey: otk.PublicKey,
			IsSigned:  false,
			CreatedAt: now,
		}); err != nil {
			s.logger.Error("store one-time key", "error", err)
			writeError(w, http.StatusInternalServerError, "internal", "Failed to store one-time key")
			return
		}
	}

	count, _ := s.store.CountUnclaimedPreKeys(r.Context(), userID)
	writeJSON(w, http.StatusOK, map[string]int64{"unclaimed_keys": count})
}

func (s *Server) handleClaimKeys(w http.ResponseWriter, r *http.Request) {
	var req claimKeysRequest
	if err := readJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "bad_request", "Invalid JSON body")
		return
	}

	// Get user's identity key
	user, err := s.store.GetUserByHandle(r.Context(), req.UserID)
	if err != nil {
		// Try by ID
		user, err = s.store.GetUserByID(r.Context(), req.UserID)
		if err != nil {
			writeError(w, http.StatusNotFound, "not_found", "User not found")
			return
		}
	}

	signedPK, err := s.store.GetSignedPreKey(r.Context(), user.ID)
	if err != nil {
		writeError(w, http.StatusNotFound, "no_keys", "No signed pre-key available")
		return
	}

	bundle := protocol.PreKeyBundle{
		UserID:      user.Handle,
		IdentityKey: user.IdentityKey,
		SignedPreKey: protocol.PublicPreKey{
			KeyID:     int(signedPK.KeyID),
			PublicKey: signedPK.PublicKey,
			Signature: signedPK.Signature,
		},
	}

	// Try to claim a one-time pre-key
	otk, err := s.store.ClaimPreKey(r.Context(), user.ID)
	if err == nil {
		bundle.OneTimePreKey = &protocol.PublicPreKey{
			KeyID:     int(otk.KeyID),
			PublicKey: otk.PublicKey,
		}
	}

	writeJSON(w, http.StatusOK, bundle)
}
