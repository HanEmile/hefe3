// User-related database operations.
package db

import (
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"makhor/pkg/models"
	"time"
)

// Token and session expiry durations.
const (
	LoginTokenExpiry      = 15 * time.Minute
	SessionExpiry         = 30 * 24 * time.Hour
	EmailChangeTokenExpiry = 24 * time.Hour
)

var (
	ErrUserNotFound  = errors.New("user not found")
	ErrUserExists    = errors.New("user already exists")
	ErrInvalidToken  = errors.New("invalid or expired token")
	ErrInvalidInvite = errors.New("invalid or used invite code")
)

// generateToken creates a cryptographically secure random token.
func generateToken(length int) (string, error) {
	bytes := make([]byte, length)
	if _, err := rand.Read(bytes); err != nil {
		return "", err
	}
	return hex.EncodeToString(bytes), nil
}

// CreateLoginToken generates a login token for the given email.
// The token expires in 15 minutes.
func (d *DB) CreateLoginToken(email string) (string, error) {
	token, err := generateToken(32)
	if err != nil {
		return "", fmt.Errorf("generating token: %w", err)
	}

	expiresAt := time.Now().Add(LoginTokenExpiry)

	_, err = d.Exec(
		`INSERT INTO login_tokens (email, token, expires_at) VALUES (?, ?, ?)`,
		email, token, expiresAt,
	)
	if err != nil {
		return "", fmt.Errorf("inserting login token: %w", err)
	}

	return token, nil
}

// ValidateLoginToken checks if a token is valid and returns the associated email.
// Marks the token as used atomically to prevent race conditions.
func (d *DB) ValidateLoginToken(token string) (string, error) {
	// Atomically mark as used and return email if valid
	// This prevents race conditions where two requests could use the same token
	var email string
	err := d.QueryRow(`
		UPDATE login_tokens
		SET used = TRUE
		WHERE token = ? AND used = FALSE AND expires_at > datetime('now')
		RETURNING email
	`, token).Scan(&email)

	if err == sql.ErrNoRows {
		return "", ErrInvalidToken
	}
	if err != nil {
		return "", fmt.Errorf("validating token: %w", err)
	}

	return email, nil
}

// CreateSession creates a new session for the user.
func (d *DB) CreateSession(userID int64) (string, error) {
	token, err := generateToken(32)
	if err != nil {
		return "", fmt.Errorf("generating session token: %w", err)
	}

	expiresAt := time.Now().Add(SessionExpiry)

	_, err = d.Exec(
		`INSERT INTO sessions (user_id, token, expires_at) VALUES (?, ?, ?)`,
		userID, token, expiresAt,
	)
	if err != nil {
		return "", fmt.Errorf("inserting session: %w", err)
	}

	return token, nil
}

// ValidateSession checks if a session token is valid and returns the user.
// Uses atomic query to prevent race conditions with expiry checking.
func (d *DB) ValidateSession(token string) (*models.User, error) {
	// Atomically check if session is valid (not expired)
	// This prevents race conditions where a session could be used after expiry
	var userID int64
	err := d.QueryRow(`
		SELECT user_id FROM sessions
		WHERE token = ? AND expires_at > datetime('now')
	`, token).Scan(&userID)

	if err == sql.ErrNoRows {
		// Clean up expired sessions for this token (if any exist)
		d.Exec("DELETE FROM sessions WHERE token = ? AND expires_at <= datetime('now')", token)
		return nil, ErrInvalidToken
	}
	if err != nil {
		return nil, fmt.Errorf("querying session: %w", err)
	}

	return d.GetUserByID(userID)
}

// DeleteSession removes a session (logout).
func (d *DB) DeleteSession(token string) error {
	_, err := d.Exec("DELETE FROM sessions WHERE token = ?", token)
	return err
}

// CreateUser creates a new user account.
func (d *DB) CreateUser(username, email string, invitedBy *int64) (*models.User, error) {
	// Check if username or email already exists
	var count int
	err := d.QueryRow(
		`SELECT COUNT(*) FROM users WHERE username = ? OR email = ?`,
		username, email,
	).Scan(&count)
	if err != nil {
		return nil, fmt.Errorf("checking existing user: %w", err)
	}
	if count > 0 {
		return nil, ErrUserExists
	}

	result, err := d.Exec(
		`INSERT INTO users (username, email, invited_by) VALUES (?, ?, ?)`,
		username, email, invitedBy,
	)
	if err != nil {
		return nil, fmt.Errorf("inserting user: %w", err)
	}

	id, err := result.LastInsertId()
	if err != nil {
		return nil, fmt.Errorf("getting user id: %w", err)
	}

	return d.GetUserByID(id)
}

// GetUserByID retrieves a user by their ID.
func (d *DB) GetUserByID(id int64) (*models.User, error) {
	user := &models.User{}
	var invitedBy sql.NullInt64
	var avatarType sql.NullString
	var banReason sql.NullString
	var banExpiresAt sql.NullTime
	var bannedBy sql.NullInt64

	err := d.QueryRow(`
		SELECT u.id, u.username, u.email, u.created_at, u.invited_by, u.is_admin, u.is_banned,
		       u.about, u.avatar, u.avatar_type, u.ban_reason, u.ban_expires_at, u.banned_by,
		       COALESCE(b.username, '') as banned_by_username
		FROM users u
		LEFT JOIN users b ON u.banned_by = b.id
		WHERE u.id = ?
	`, id).Scan(
		&user.ID, &user.Username, &user.Email, &user.CreatedAt,
		&invitedBy, &user.IsAdmin, &user.IsBanned, &user.About,
		&user.Avatar, &avatarType, &banReason, &banExpiresAt, &bannedBy,
		&user.BannedByUsername,
	)

	if err == sql.ErrNoRows {
		return nil, ErrUserNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("querying user: %w", err)
	}

	if invitedBy.Valid {
		user.InvitedBy = &invitedBy.Int64
	}
	if avatarType.Valid {
		user.AvatarType = avatarType.String
	}
	if banReason.Valid {
		user.BanReason = banReason.String
	}
	if banExpiresAt.Valid {
		user.BanExpiresAt = &banExpiresAt.Time
	}
	if bannedBy.Valid {
		user.BannedBy = &bannedBy.Int64
	}

	return user, nil
}

// GetUserByUsername retrieves a user by username.
func (d *DB) GetUserByUsername(username string) (*models.User, error) {
	var id int64
	err := d.QueryRow("SELECT id FROM users WHERE username = ?", username).Scan(&id)
	if err == sql.ErrNoRows {
		return nil, ErrUserNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("querying user by username: %w", err)
	}
	return d.GetUserByID(id)
}

// GetUserByEmail retrieves a user by email.
func (d *DB) GetUserByEmail(email string) (*models.User, error) {
	var id int64
	err := d.QueryRow("SELECT id FROM users WHERE email = ?", email).Scan(&id)
	if err == sql.ErrNoRows {
		return nil, ErrUserNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("querying user by email: %w", err)
	}
	return d.GetUserByID(id)
}

// UpdateUser updates user profile information.
func (d *DB) UpdateUser(id int64, about string) error {
	_, err := d.Exec("UPDATE users SET about = ? WHERE id = ?", about, id)
	return err
}

// UpdateUserAvatar updates the user's profile picture.
func (d *DB) UpdateUserAvatar(id int64, avatar []byte, avatarType string) error {
	_, err := d.Exec(
		"UPDATE users SET avatar = ?, avatar_type = ? WHERE id = ?",
		avatar, avatarType, id,
	)
	return err
}

// SetUserBanned sets the banned status of a user (legacy, for simple unbans).
func (d *DB) SetUserBanned(id int64, banned bool) error {
	if banned {
		_, err := d.Exec("UPDATE users SET is_banned = ? WHERE id = ?", banned, id)
		return err
	}
	// When unbanning, clear all ban fields
	_, err := d.Exec(`
		UPDATE users
		SET is_banned = FALSE, ban_reason = '', ban_expires_at = NULL, banned_by = NULL
		WHERE id = ?
	`, id)
	return err
}

// BanUser bans a user with reason, optional expiration, and tracks who banned them.
func (d *DB) BanUser(id int64, reason string, expiresAt *time.Time, bannedBy int64) error {
	_, err := d.Exec(`
		UPDATE users
		SET is_banned = TRUE, ban_reason = ?, ban_expires_at = ?, banned_by = ?
		WHERE id = ?
	`, reason, expiresAt, bannedBy, id)
	return err
}

// CheckAndClearExpiredBans checks if a user's ban has expired and clears it if so.
// Returns true if the ban was cleared.
func (d *DB) CheckAndClearExpiredBans(id int64) (bool, error) {
	result, err := d.Exec(`
		UPDATE users
		SET is_banned = FALSE, ban_reason = '', ban_expires_at = NULL, banned_by = NULL
		WHERE id = ? AND is_banned = TRUE AND ban_expires_at IS NOT NULL AND ban_expires_at < datetime('now')
	`, id)
	if err != nil {
		return false, err
	}
	rows, err := result.RowsAffected()
	if err != nil {
		return false, err
	}
	return rows > 0, nil
}

// CreateEmailChangeToken creates a token for email change verification.
func (d *DB) CreateEmailChangeToken(userID int64, newEmail, token string) error {
	expiresAt := time.Now().Add(EmailChangeTokenExpiry)
	_, err := d.Exec(
		`INSERT INTO email_change_tokens (user_id, new_email, token, expires_at) VALUES (?, ?, ?, ?)`,
		userID, newEmail, token, expiresAt,
	)
	return err
}

// GetEmailChangeToken retrieves and validates an email change token.
func (d *DB) GetEmailChangeToken(token string) (int64, string, error) {
	var userID int64
	var newEmail string
	var used bool
	var expiresAt time.Time

	err := d.QueryRow(
		`SELECT user_id, new_email, used, expires_at FROM email_change_tokens WHERE token = ?`,
		token,
	).Scan(&userID, &newEmail, &used, &expiresAt)

	if err == sql.ErrNoRows {
		return 0, "", fmt.Errorf("invalid token")
	}
	if err != nil {
		return 0, "", err
	}
	if used {
		return 0, "", fmt.Errorf("token already used")
	}
	if time.Now().After(expiresAt) {
		return 0, "", fmt.Errorf("token expired")
	}

	return userID, newEmail, nil
}

// UseEmailChangeToken marks the token as used and updates the user's email.
// Uses atomic operations to prevent race conditions.
func (d *DB) UseEmailChangeToken(token string) error {
	tx, err := d.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	// Atomically mark token as used and get the data if valid
	var userID int64
	var newEmail string
	err = tx.QueryRow(`
		UPDATE email_change_tokens
		SET used = TRUE
		WHERE token = ? AND used = FALSE AND expires_at > datetime('now')
		RETURNING user_id, new_email
	`, token).Scan(&userID, &newEmail)

	if err == sql.ErrNoRows {
		return fmt.Errorf("invalid or expired token")
	}
	if err != nil {
		return fmt.Errorf("validating token: %w", err)
	}

	// Update user email
	_, err = tx.Exec("UPDATE users SET email = ? WHERE id = ?", newEmail, userID)
	if err != nil {
		return fmt.Errorf("updating email: %w", err)
	}

	return tx.Commit()
}

// GetInviteTree returns all users invited by a given user (recursively).
func (d *DB) GetInviteTree(userID int64) (*models.InviteTreeNode, error) {
	user, err := d.GetUserByID(userID)
	if err != nil {
		return nil, err
	}

	node := &models.InviteTreeNode{User: user}

	// Get direct invitees
	rows, err := d.Query("SELECT id FROM users WHERE invited_by = ?", userID)
	if err != nil {
		return nil, fmt.Errorf("querying invitees: %w", err)
	}
	defer rows.Close()

	var childIDs []int64
	for rows.Next() {
		var childID int64
		if err := rows.Scan(&childID); err != nil {
			return nil, fmt.Errorf("scanning invitee: %w", err)
		}
		childIDs = append(childIDs, childID)
	}

	// Recursively build tree
	for _, childID := range childIDs {
		childNode, err := d.GetInviteTree(childID)
		if err != nil {
			return nil, err
		}
		node.Children = append(node.Children, childNode)
	}

	return node, nil
}

// GetUserInviter returns the user who invited the given user.
func (d *DB) GetUserInviter(userID int64) (*models.User, error) {
	var inviterID sql.NullInt64
	err := d.QueryRow("SELECT invited_by FROM users WHERE id = ?", userID).Scan(&inviterID)
	if err != nil {
		return nil, err
	}
	if !inviterID.Valid {
		return nil, nil // No inviter (root user)
	}
	return d.GetUserByID(inviterID.Int64)
}

// IsInvitedBy checks if targetUserID was invited by inviterID.
func (d *DB) IsInvitedBy(targetUserID, inviterID int64) bool {
	var invitedBy sql.NullInt64
	err := d.QueryRow("SELECT invited_by FROM users WHERE id = ?", targetUserID).Scan(&invitedBy)
	if err != nil || !invitedBy.Valid {
		return false
	}
	return invitedBy.Int64 == inviterID
}
