// Invite-related database operations.
package db

import (
	"database/sql"
	"fmt"
	"makhor/pkg/models"
	"time"
)

// CreateInvite generates a new invite code for a user.
func (d *DB) CreateInvite(userID int64, note string) (*models.Invite, error) {
	code, err := generateToken(16)
	if err != nil {
		return nil, fmt.Errorf("generating invite code: %w", err)
	}

	result, err := d.Exec(
		`INSERT INTO invites (code, created_by, note) VALUES (?, ?, ?)`,
		code, userID, note,
	)
	if err != nil {
		return nil, fmt.Errorf("inserting invite: %w", err)
	}

	id, err := result.LastInsertId()
	if err != nil {
		return nil, fmt.Errorf("getting invite id: %w", err)
	}

	return d.GetInviteByID(id)
}

// GetInviteByID retrieves an invite by ID.
func (d *DB) GetInviteByID(id int64) (*models.Invite, error) {
	invite := &models.Invite{}
	var usedBy sql.NullInt64
	var usedAt sql.NullTime

	err := d.QueryRow(`
		SELECT id, code, created_by, created_at, used_by, used_at, note
		FROM invites WHERE id = ?
	`, id).Scan(
		&invite.ID, &invite.Code, &invite.CreatedBy, &invite.CreatedAt,
		&usedBy, &usedAt, &invite.Note,
	)

	if err == sql.ErrNoRows {
		return nil, ErrInvalidInvite
	}
	if err != nil {
		return nil, fmt.Errorf("querying invite: %w", err)
	}

	if usedBy.Valid {
		invite.UsedBy = &usedBy.Int64
	}
	if usedAt.Valid {
		invite.UsedAt = &usedAt.Time
	}

	return invite, nil
}

// GetInviteByCode retrieves an invite by its code.
func (d *DB) GetInviteByCode(code string) (*models.Invite, error) {
	var id int64
	err := d.QueryRow("SELECT id FROM invites WHERE code = ?", code).Scan(&id)
	if err == sql.ErrNoRows {
		return nil, ErrInvalidInvite
	}
	if err != nil {
		return nil, fmt.Errorf("querying invite by code: %w", err)
	}
	return d.GetInviteByID(id)
}

// UseInvite marks an invite as used by a new user.
// Uses atomic update to prevent race conditions.
func (d *DB) UseInvite(code string, userID int64) error {
	now := time.Now()
	// Atomically update only if not already used
	result, err := d.Exec(
		`UPDATE invites SET used_by = ?, used_at = ? WHERE code = ? AND used_by IS NULL`,
		userID, now, code,
	)
	if err != nil {
		return fmt.Errorf("marking invite used: %w", err)
	}

	rowsAffected, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("checking rows affected: %w", err)
	}

	if rowsAffected == 0 {
		// Either invite doesn't exist or was already used
		return ErrInvalidInvite
	}

	return nil
}

// GetUserInvites retrieves all invites created by a user.
func (d *DB) GetUserInvites(userID int64) ([]*models.Invite, error) {
	rows, err := d.Query(`
		SELECT i.id, i.code, i.created_by, i.created_at, i.used_by, i.used_at, i.note, u.username
		FROM invites i
		LEFT JOIN users u ON i.used_by = u.id
		WHERE i.created_by = ? ORDER BY i.created_at DESC
	`, userID)
	if err != nil {
		return nil, fmt.Errorf("querying user invites: %w", err)
	}
	defer rows.Close()

	var invites []*models.Invite
	for rows.Next() {
		invite := &models.Invite{}
		var usedBy sql.NullInt64
		var usedAt sql.NullTime
		var usedByUsername sql.NullString

		err := rows.Scan(
			&invite.ID, &invite.Code, &invite.CreatedBy, &invite.CreatedAt,
			&usedBy, &usedAt, &invite.Note, &usedByUsername,
		)
		if err != nil {
			return nil, fmt.Errorf("scanning invite: %w", err)
		}

		if usedBy.Valid {
			invite.UsedBy = &usedBy.Int64
		}
		if usedAt.Valid {
			invite.UsedAt = &usedAt.Time
		}
		if usedByUsername.Valid {
			invite.UsedByUsername = usedByUsername.String
		}

		invites = append(invites, invite)
	}

	return invites, nil
}

// GetPendingInviteCount returns the number of unused invites for a user.
func (d *DB) GetPendingInviteCount(userID int64) (int, error) {
	var count int
	err := d.QueryRow(
		`SELECT COUNT(*) FROM invites WHERE created_by = ? AND used_by IS NULL`,
		userID,
	).Scan(&count)
	return count, err
}
