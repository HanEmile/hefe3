package store

import (
	"database/sql"
	"errors"
	"fmt"
	"time"
)

// CreateMagicLink stores a login token for userID, valid until expiresAt.
// tokenHash is the hex-sha256 of the raw token (see internal/token) - the
// raw token itself is never persisted.
func (s *Store) CreateMagicLink(userID int, tokenHash string, expiresAt time.Time) error {
	_, err := s.db.Exec(
		"INSERT INTO magic_links (token_hash, user_id, created_at, expires_at) VALUES (?, ?, ?, ?)",
		tokenHash, userID, time.Now(), expiresAt,
	)
	if err != nil {
		return fmt.Errorf("store: creating magic link: %w", err)
	}
	return nil
}

// ConsumeMagicLink atomically looks up and deletes an unexpired magic link
// by token hash, returning the user it belonged to. Deleting on lookup
// (rather than a separate check-then-delete) makes every link single-use
// even under concurrent requests for the same token.
func (s *Store) ConsumeMagicLink(tokenHash string) (userID int, err error) {
	err = s.db.QueryRow(
		"DELETE FROM magic_links WHERE token_hash=? AND expires_at > ? RETURNING user_id",
		tokenHash, time.Now(),
	).Scan(&userID)
	if errors.Is(err, sql.ErrNoRows) {
		return 0, ErrNotFound
	}
	if err != nil {
		return 0, fmt.Errorf("store: consuming magic link: %w", err)
	}
	return userID, nil
}

// PruneExpiredMagicLinks deletes expired, never-consumed tokens so the table
// doesn't grow unbounded. Safe to call periodically; consumed links are
// already gone (ConsumeMagicLink deletes them), so this only ever cleans up
// link requests nobody clicked.
func (s *Store) PruneExpiredMagicLinks() error {
	_, err := s.db.Exec("DELETE FROM magic_links WHERE expires_at <= ?", time.Now())
	if err != nil {
		return fmt.Errorf("store: pruning expired magic links: %w", err)
	}
	return nil
}
