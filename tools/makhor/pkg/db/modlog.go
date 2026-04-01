package db

import (
	"makhor/pkg/models"
	"time"
)

// ModerationAction constants
const (
	ModActionBan           = "ban"
	ModActionUnban         = "unban"
	ModActionDeletePost    = "delete_post"
	ModActionDeleteComment = "delete_comment"
	ModActionEditPost      = "edit_post"
	ModActionEditComment   = "edit_comment"
	ModActionGrantHat      = "grant_hat"
	ModActionRevokeHat     = "revoke_hat"
)

// LogModeration records a moderation action in the public log.
func (d *DB) LogModeration(moderatorID int64, action string, targetUserID, targetPostID, targetCommentID *int64, reason string) error {
	_, err := d.Exec(`
		INSERT INTO moderation_log (moderator_id, action, target_user_id, target_post_id, target_comment_id, reason)
		VALUES (?, ?, ?, ?, ?, ?)
	`, moderatorID, action, targetUserID, targetPostID, targetCommentID, reason)
	return err
}

// ModerationLogEntry represents an entry in the moderation log with resolved names.
type ModerationLogEntry struct {
	ID               int64
	ModeratorID      int64
	ModeratorName    string
	Action           string
	TargetUserID     *int64
	TargetUsername   *string
	TargetPostID     *int64
	TargetPostTitle  *string
	TargetCommentID  *int64
	Reason           string
	CreatedAt        time.Time
}

// GetModerationLog retrieves moderation log entries with pagination.
func (d *DB) GetModerationLog(page, perPage int) ([]ModerationLogEntry, *models.Pagination, error) {
	// Count total entries
	var total int
	err := d.QueryRow("SELECT COUNT(*) FROM moderation_log").Scan(&total)
	if err != nil {
		return nil, nil, err
	}

	pagination := &models.Pagination{}
	*pagination = models.NewPagination(page, perPage, total)
	offset := (page - 1) * perPage

	rows, err := d.Query(`
		SELECT
			ml.id,
			ml.moderator_id,
			m.username,
			ml.action,
			ml.target_user_id,
			tu.username,
			ml.target_post_id,
			p.title,
			ml.target_comment_id,
			ml.reason,
			ml.created_at
		FROM moderation_log ml
		JOIN users m ON ml.moderator_id = m.id
		LEFT JOIN users tu ON ml.target_user_id = tu.id
		LEFT JOIN posts p ON ml.target_post_id = p.id
		ORDER BY ml.created_at DESC
		LIMIT ? OFFSET ?
	`, perPage, offset)
	if err != nil {
		return nil, nil, err
	}
	defer rows.Close()

	var entries []ModerationLogEntry
	for rows.Next() {
		var e ModerationLogEntry
		err := rows.Scan(
			&e.ID,
			&e.ModeratorID,
			&e.ModeratorName,
			&e.Action,
			&e.TargetUserID,
			&e.TargetUsername,
			&e.TargetPostID,
			&e.TargetPostTitle,
			&e.TargetCommentID,
			&e.Reason,
			&e.CreatedAt,
		)
		if err != nil {
			return nil, nil, err
		}
		entries = append(entries, e)
	}

	return entries, pagination, rows.Err()
}
