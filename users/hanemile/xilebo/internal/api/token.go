package api

import (
	"crypto/ed25519"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"time"
)

// tokenPayload is the JWT-like token payload.
type tokenPayload struct {
	UserID    string `json:"sub"`
	Hostname  string `json:"iss"`
	ExpiresAt int64  `json:"exp"`
	IssuedAt  int64  `json:"iat"`
}

// serverSigningKey is generated at startup.
// In production, this would be persisted or derived from config.
var serverSigningKey ed25519.PrivateKey

func init() {
	_, key, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		panic("failed to generate signing key: " + err.Error())
	}
	serverSigningKey = key
}

// generateToken creates a signed token for the given user.
func generateToken(userID, hostname string) (string, error) {
	payload := tokenPayload{
		UserID:    userID,
		Hostname:  hostname,
		ExpiresAt: time.Now().Add(24 * time.Hour).Unix(),
		IssuedAt:  time.Now().Unix(),
	}

	data, err := json.Marshal(payload)
	if err != nil {
		return "", fmt.Errorf("marshal token: %w", err)
	}

	sig := ed25519.Sign(serverSigningKey, data)

	// Token format: base64(payload).base64(signature)
	token := base64.RawURLEncoding.EncodeToString(data) + "." +
		base64.RawURLEncoding.EncodeToString(sig)

	return token, nil
}

// validateToken verifies a token and returns the user ID.
func validateToken(token, hostname string) (string, error) {
	parts := splitToken(token)
	if len(parts) != 2 {
		return "", errors.New("malformed token")
	}

	data, err := base64.RawURLEncoding.DecodeString(parts[0])
	if err != nil {
		return "", errors.New("invalid token encoding")
	}

	sig, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return "", errors.New("invalid signature encoding")
	}

	pubKey := serverSigningKey.Public().(ed25519.PublicKey)
	if !ed25519.Verify(pubKey, data, sig) {
		return "", errors.New("invalid signature")
	}

	var payload tokenPayload
	if err := json.Unmarshal(data, &payload); err != nil {
		return "", errors.New("invalid token payload")
	}

	if payload.ExpiresAt < time.Now().Unix() {
		return "", errors.New("token expired")
	}

	if payload.Hostname != hostname {
		return "", errors.New("token hostname mismatch")
	}

	return payload.UserID, nil
}

func splitToken(token string) []string {
	for i := len(token) - 1; i >= 0; i-- {
		if token[i] == '.' {
			return []string{token[:i], token[i+1:]}
		}
	}
	return []string{token}
}
