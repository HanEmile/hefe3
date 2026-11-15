package api

import (
	"crypto/ed25519"
	"crypto/rand"
	"database/sql"
	"errors"
	"net/http"
	"time"

	"github.com/oklog/ulid/v2"
	"golang.org/x/crypto/argon2"

	"github.com/hanemile/xilebo/internal/storage"
	"github.com/hanemile/xilebo/pkg/protocol"
)

type registerRequest struct {
	Username string `json:"username"`
	Password string `json:"password"`
}

type registerResponse struct {
	UserID string `json:"user_id"`
	Handle string `json:"handle"`
	Token  string `json:"token"`
}

type loginRequest struct {
	Handle   string `json:"handle"`
	Password string `json:"password"`
}

type loginResponse struct {
	UserID string `json:"user_id"`
	Handle string `json:"handle"`
	Token  string `json:"token"`
}

func (s *Server) handleRegister(w http.ResponseWriter, r *http.Request) {
	var req registerRequest
	if err := readJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "bad_request", "Invalid JSON body")
		return
	}

	if req.Username == "" || req.Password == "" {
		writeError(w, http.StatusBadRequest, "bad_request", "Username and password are required")
		return
	}

	handle := protocol.MakeHandle(req.Username, s.cfg.Server.Hostname)
	if !protocol.ValidateHandle(handle) {
		writeError(w, http.StatusBadRequest, "bad_request", "Invalid username format")
		return
	}

	// Check if handle already exists
	_, err := s.store.GetUserByHandle(r.Context(), handle)
	if err == nil {
		writeError(w, http.StatusConflict, "user_exists", "Username already taken")
		return
	}
	if !errors.Is(err, sql.ErrNoRows) {
		s.logger.Error("check user exists", "error", err)
		writeError(w, http.StatusInternalServerError, "internal", "Internal server error")
		return
	}

	// Generate Ed25519 identity keypair
	pub, _, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal", "Failed to generate identity key")
		return
	}

	// Hash password with argon2id
	salt := make([]byte, 16)
	if _, err := rand.Read(salt); err != nil {
		writeError(w, http.StatusInternalServerError, "internal", "Failed to generate salt")
		return
	}
	hash := hashPassword(req.Password, salt)

	now := time.Now().UnixMilli()
	id := ulid.Make().String()

	user, err := s.store.CreateUser(r.Context(), storage.CreateUserParams{
		ID:            id,
		Handle:        handle,
		IdentityKey:   pub,
		PasswordHash:  hash,
		InstancePerms: protocol.DefaultUserPerms,
		CreatedAt:     now,
		UpdatedAt:     now,
	})
	if err != nil {
		s.logger.Error("create user", "error", err)
		writeError(w, http.StatusInternalServerError, "internal", "Failed to create user")
		return
	}

	token, err := generateToken(user.ID, s.cfg.Server.Hostname)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal", "Failed to generate token")
		return
	}

	writeJSON(w, http.StatusCreated, registerResponse{
		UserID: user.ID,
		Handle: user.Handle,
		Token:  token,
	})
}

func (s *Server) handleLogin(w http.ResponseWriter, r *http.Request) {
	var req loginRequest
	if err := readJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "bad_request", "Invalid JSON body")
		return
	}

	if req.Handle == "" || req.Password == "" {
		writeError(w, http.StatusBadRequest, "bad_request", "Handle and password are required")
		return
	}

	user, err := s.store.GetUserByHandle(r.Context(), req.Handle)
	if errors.Is(err, sql.ErrNoRows) {
		writeError(w, http.StatusUnauthorized, "unauthorized", "Invalid handle or password")
		return
	}
	if err != nil {
		s.logger.Error("get user", "error", err)
		writeError(w, http.StatusInternalServerError, "internal", "Internal server error")
		return
	}

	if !verifyPassword(req.Password, user.PasswordHash) {
		writeError(w, http.StatusUnauthorized, "unauthorized", "Invalid handle or password")
		return
	}

	token, err := generateToken(user.ID, s.cfg.Server.Hostname)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal", "Failed to generate token")
		return
	}

	writeJSON(w, http.StatusOK, loginResponse{
		UserID: user.ID,
		Handle: user.Handle,
		Token:  token,
	})
}

// hashPassword uses argon2id and returns "salt$hash" as hex.
func hashPassword(password string, salt []byte) string {
	hash := argon2.IDKey([]byte(password), salt, 1, 64*1024, 4, 32)
	return encodeHex(salt) + "$" + encodeHex(hash)
}

// verifyPassword checks a password against a "salt$hash" string.
func verifyPassword(password, stored string) bool {
	parts := splitOnDollar(stored)
	if len(parts) != 2 {
		return false
	}
	salt, err := decodeHex(parts[0])
	if err != nil {
		return false
	}
	expectedHash, err := decodeHex(parts[1])
	if err != nil {
		return false
	}
	hash := argon2.IDKey([]byte(password), salt, 1, 64*1024, 4, 32)
	return constantTimeEqual(hash, expectedHash)
}

func splitOnDollar(s string) []string {
	for i := range s {
		if s[i] == '$' {
			return []string{s[:i], s[i+1:]}
		}
	}
	return []string{s}
}

func encodeHex(b []byte) string {
	const hex = "0123456789abcdef"
	out := make([]byte, len(b)*2)
	for i, v := range b {
		out[i*2] = hex[v>>4]
		out[i*2+1] = hex[v&0x0f]
	}
	return string(out)
}

func decodeHex(s string) ([]byte, error) {
	if len(s)%2 != 0 {
		return nil, errors.New("odd length hex string")
	}
	out := make([]byte, len(s)/2)
	for i := 0; i < len(s); i += 2 {
		hi := unhex(s[i])
		lo := unhex(s[i+1])
		if hi == 0xff || lo == 0xff {
			return nil, errors.New("invalid hex character")
		}
		out[i/2] = hi<<4 | lo
	}
	return out, nil
}

func unhex(c byte) byte {
	switch {
	case c >= '0' && c <= '9':
		return c - '0'
	case c >= 'a' && c <= 'f':
		return c - 'a' + 10
	case c >= 'A' && c <= 'F':
		return c - 'A' + 10
	default:
		return 0xff
	}
}

func constantTimeEqual(a, b []byte) bool {
	if len(a) != len(b) {
		return false
	}
	var v byte
	for i := range a {
		v |= a[i] ^ b[i]
	}
	return v == 0
}
