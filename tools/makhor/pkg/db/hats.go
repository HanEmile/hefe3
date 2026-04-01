// Hat-related database operations.
// Hats allow users to speak officially for organizations.
package db

import (
	"database/sql"
	"fmt"
	"makhor/pkg/models"
)

// CreateHat grants a new hat to a user.
func (d *DB) CreateHat(userID int64, name, organization, link string, grantedBy *int64) (*models.Hat, error) {
	result, err := d.Exec(
		`INSERT INTO hats (user_id, name, organization, link, granted_by) VALUES (?, ?, ?, ?, ?)`,
		userID, name, organization, link, grantedBy,
	)
	if err != nil {
		return nil, fmt.Errorf("inserting hat: %w", err)
	}

	id, err := result.LastInsertId()
	if err != nil {
		return nil, fmt.Errorf("getting hat id: %w", err)
	}

	return d.GetHatByID(id)
}

// GetHatByID retrieves a hat by ID.
func (d *DB) GetHatByID(id int64) (*models.Hat, error) {
	hat := &models.Hat{}
	var grantedBy sql.NullInt64
	var link sql.NullString

	err := d.QueryRow(`
		SELECT id, user_id, name, organization, link, granted_by, granted_at, is_active
		FROM hats WHERE id = ?
	`, id).Scan(
		&hat.ID, &hat.UserID, &hat.Name, &hat.Organization, &link,
		&grantedBy, &hat.GrantedAt, &hat.IsActive,
	)

	if err == sql.ErrNoRows {
		return nil, ErrHatNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("querying hat: %w", err)
	}

	if grantedBy.Valid {
		hat.GrantedBy = &grantedBy.Int64
	}
	if link.Valid {
		hat.Link = link.String
	}

	return hat, nil
}

// GetUserHats retrieves all active hats for a user.
func (d *DB) GetUserHats(userID int64) ([]*models.Hat, error) {
	rows, err := d.Query(`
		SELECT id, user_id, name, organization, link, granted_by, granted_at, is_active
		FROM hats WHERE user_id = ? AND is_active = TRUE
		ORDER BY granted_at DESC
	`, userID)
	if err != nil {
		return nil, fmt.Errorf("querying user hats: %w", err)
	}
	defer rows.Close()

	var hats []*models.Hat
	for rows.Next() {
		hat := &models.Hat{}
		var grantedBy sql.NullInt64
		var link sql.NullString

		err := rows.Scan(
			&hat.ID, &hat.UserID, &hat.Name, &hat.Organization, &link,
			&grantedBy, &hat.GrantedAt, &hat.IsActive,
		)
		if err != nil {
			return nil, fmt.Errorf("scanning hat: %w", err)
		}

		if grantedBy.Valid {
			hat.GrantedBy = &grantedBy.Int64
		}
		if link.Valid {
			hat.Link = link.String
		}

		hats = append(hats, hat)
	}

	return hats, nil
}

// UserOwnsHat checks if a user owns a specific active hat.
func (d *DB) UserOwnsHat(userID, hatID int64) bool {
	var exists bool
	err := d.QueryRow(`
		SELECT EXISTS(SELECT 1 FROM hats WHERE id = ? AND user_id = ? AND is_active = TRUE)
	`, hatID, userID).Scan(&exists)
	return err == nil && exists
}

// RevokeHat deactivates a hat.
func (d *DB) RevokeHat(id int64) error {
	_, err := d.Exec(`UPDATE hats SET is_active = FALSE WHERE id = ?`, id)
	return err
}

// UpdateHat updates a hat's name, organization, and link.
func (d *DB) UpdateHat(id int64, name, organization, link string) error {
	_, err := d.Exec(
		`UPDATE hats SET name = ?, organization = ?, link = ? WHERE id = ?`,
		name, organization, link, id,
	)
	return err
}

// ReactivateHat reactivates a revoked hat.
func (d *DB) ReactivateHat(id int64) error {
	_, err := d.Exec(`UPDATE hats SET is_active = TRUE WHERE id = ?`, id)
	return err
}

// GetAllHats retrieves all hats (for admin view).
func (d *DB) GetAllHats() ([]*models.Hat, error) {
	rows, err := d.Query(`
		SELECT h.id, h.user_id, h.name, h.organization, h.link, h.granted_by, h.granted_at, h.is_active, u.username
		FROM hats h
		JOIN users u ON h.user_id = u.id
		ORDER BY h.granted_at DESC
	`)
	if err != nil {
		return nil, fmt.Errorf("querying all hats: %w", err)
	}
	defer rows.Close()

	var hats []*models.Hat
	for rows.Next() {
		hat := &models.Hat{}
		var grantedBy sql.NullInt64
		var link sql.NullString

		err := rows.Scan(
			&hat.ID, &hat.UserID, &hat.Name, &hat.Organization, &link,
			&grantedBy, &hat.GrantedAt, &hat.IsActive, &hat.Username,
		)
		if err != nil {
			return nil, fmt.Errorf("scanning hat: %w", err)
		}

		if grantedBy.Valid {
			hat.GrantedBy = &grantedBy.Int64
		}
		if link.Valid {
			hat.Link = link.String
		}

		hats = append(hats, hat)
	}

	return hats, nil
}
