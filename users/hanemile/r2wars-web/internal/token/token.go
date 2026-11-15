// Package token generates single-use, high-entropy tokens for magic-link
// login: a random value goes out in the emailed URL, and only its hash is
// ever stored, so a stolen database dump doesn't hand out valid login links.
package token

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"fmt"
)

// rawBytes is the amount of entropy in each generated token. 32 bytes
// (256 bits) is comfortably more than enough to make guessing infeasible.
const rawBytes = 32

// New generates a fresh token. raw is the URL-safe string to email the user
// (put it in the link, never log or store it); hash is what the store
// layer persists and later looks up by.
func New() (raw string, hash string, err error) {
	b := make([]byte, rawBytes)
	if _, err := rand.Read(b); err != nil {
		return "", "", fmt.Errorf("token: generating random bytes: %w", err)
	}
	raw = base64.RawURLEncoding.EncodeToString(b)
	return raw, Hash(raw), nil
}

// Hash deterministically hashes a raw token for lookup/storage, hex-encoded
// for storing in a TEXT column. Unlike a password, a token is already
// high-entropy random data, so a fast cryptographic hash (not a slow KDF
// like argon2/bcrypt) is the right tool: there's nothing for an offline
// attacker to brute-force.
func Hash(raw string) string {
	sum := sha256.Sum256([]byte(raw))
	return hex.EncodeToString(sum[:])
}
