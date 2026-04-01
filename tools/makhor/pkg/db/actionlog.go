// Action logging database operations.
// Provides full audit trail of all user actions.
package db

import (
	"database/sql"
	"fmt"
	"makhor/pkg/models"
)

// LogAction records an action in the audit log.
func (d *DB) LogAction(userID *int64, action, targetType string, targetID int64, details, ipAddress string) error {
	_, err := d.Exec(
		`INSERT INTO action_log (user_id, action, target_type, target_id, details, ip_address)
		 VALUES (?, ?, ?, ?, ?, ?)`,
		userID, action, targetType, targetID, details, ipAddress,
	)
	if err != nil {
		return fmt.Errorf("logging action: %w", err)
	}
	return nil
}

// ActionLogFilter specifies filters for action log queries.
type ActionLogFilter struct {
	Category   string // "post", "comment", "admin", "all"
	Username   string // filter by username
	TargetType string // filter by target type
	Action     string // filter by specific action
}

// ActionCategories maps category names to action prefixes.
var ActionCategories = map[string][]string{
	"post":    {"post_create", "post_update", "post_delete", "vote_post", "remove_post_tag"},
	"comment": {"comment_create", "comment_update", "comment_delete", "vote_comment", "comment_blur", "comment_unblur", "comment_tree_delete"},
	"admin":   {"mod_ban", "mod_unban", "hat_grant", "hat_revoke", "tag_create", "tag_admin_add", "tag_admin_remove", "feed_add", "feed_update", "feed_delete"},
	"user":    {"user_create", "user_login", "user_update", "invite_create", "invite_use"},
}

// GetActionLog retrieves the action log with pagination.
func (d *DB) GetActionLog(page, perPage int) ([]*models.ActionLog, int, error) {
	return d.GetActionLogFiltered(page, perPage, ActionLogFilter{})
}

// GetActionLogFiltered retrieves the action log with pagination and filters.
func (d *DB) GetActionLogFiltered(page, perPage int, filter ActionLogFilter) ([]*models.ActionLog, int, error) {
	// Build WHERE clause
	whereClause := "WHERE 1=1"
	var args []interface{}

	if filter.Category != "" && filter.Category != "all" {
		if actions, ok := ActionCategories[filter.Category]; ok {
			placeholders := ""
			for i, action := range actions {
				if i > 0 {
					placeholders += ", "
				}
				placeholders += "?"
				args = append(args, action)
			}
			whereClause += " AND a.action IN (" + placeholders + ")"
		}
	}

	if filter.Username != "" {
		whereClause += " AND u.username = ?"
		args = append(args, filter.Username)
	}

	if filter.TargetType != "" {
		whereClause += " AND a.target_type = ?"
		args = append(args, filter.TargetType)
	}

	if filter.Action != "" {
		whereClause += " AND a.action = ?"
		args = append(args, filter.Action)
	}

	// Count query
	var total int
	countQuery := `SELECT COUNT(*) FROM action_log a LEFT JOIN users u ON a.user_id = u.id ` + whereClause
	err := d.QueryRow(countQuery, args...).Scan(&total)
	if err != nil {
		return nil, 0, fmt.Errorf("counting action log: %w", err)
	}

	// Main query
	selectQuery := `
		SELECT a.id, a.user_id, a.action, a.target_type, a.target_id, a.details, a.ip_address, a.created_at,
		       COALESCE(u.username, 'anonymous') as username
		FROM action_log a
		LEFT JOIN users u ON a.user_id = u.id
		` + whereClause + `
		ORDER BY a.created_at DESC
		LIMIT ? OFFSET ?
	`
	args = append(args, perPage, (page-1)*perPage)

	rows, err := d.Query(selectQuery, args...)
	if err != nil {
		return nil, 0, fmt.Errorf("querying action log: %w", err)
	}
	defer rows.Close()

	var logs []*models.ActionLog
	for rows.Next() {
		log := &models.ActionLog{}
		var userID sql.NullInt64
		var details, ipAddress sql.NullString

		err := rows.Scan(
			&log.ID, &userID, &log.Action, &log.TargetType, &log.TargetID,
			&details, &ipAddress, &log.CreatedAt, &log.Username,
		)
		if err != nil {
			return nil, 0, fmt.Errorf("scanning action log: %w", err)
		}

		if userID.Valid {
			log.UserID = &userID.Int64
		}
		if details.Valid {
			log.Details = details.String
		}
		if ipAddress.Valid {
			log.IPAddress = ipAddress.String
		}

		// Add human-readable target info
		log.TargetInfo = d.getTargetInfo(log.TargetType, log.TargetID)

		logs = append(logs, log)
	}

	return logs, total, nil
}

// GetUserActionLog retrieves actions by a specific user.
func (d *DB) GetUserActionLog(userID int64, page, perPage int) ([]*models.ActionLog, int, error) {
	var total int
	err := d.QueryRow(`SELECT COUNT(*) FROM action_log WHERE user_id = ?`, userID).Scan(&total)
	if err != nil {
		return nil, 0, fmt.Errorf("counting user action log: %w", err)
	}

	rows, err := d.Query(`
		SELECT a.id, a.user_id, a.action, a.target_type, a.target_id, a.details, a.ip_address, a.created_at,
		       COALESCE(u.username, 'anonymous') as username
		FROM action_log a
		LEFT JOIN users u ON a.user_id = u.id
		WHERE a.user_id = ?
		ORDER BY a.created_at DESC
		LIMIT ? OFFSET ?
	`, userID, perPage, (page-1)*perPage)
	if err != nil {
		return nil, 0, fmt.Errorf("querying user action log: %w", err)
	}
	defer rows.Close()

	var logs []*models.ActionLog
	for rows.Next() {
		log := &models.ActionLog{}
		var uID sql.NullInt64
		var details, ipAddress sql.NullString

		err := rows.Scan(
			&log.ID, &uID, &log.Action, &log.TargetType, &log.TargetID,
			&details, &ipAddress, &log.CreatedAt, &log.Username,
		)
		if err != nil {
			return nil, 0, fmt.Errorf("scanning action log: %w", err)
		}

		if uID.Valid {
			log.UserID = &uID.Int64
		}
		if details.Valid {
			log.Details = details.String
		}
		if ipAddress.Valid {
			log.IPAddress = ipAddress.String
		}

		log.TargetInfo = d.getTargetInfo(log.TargetType, log.TargetID)

		logs = append(logs, log)
	}

	return logs, total, nil
}

// getTargetInfo returns a human-readable description of an action target.
func (d *DB) getTargetInfo(targetType string, targetID int64) string {
	switch targetType {
	case "post":
		var title string
		d.QueryRow("SELECT title FROM posts WHERE id = ?", targetID).Scan(&title)
		if title != "" {
			if len(title) > 50 {
				return title[:50] + "..."
			}
			return title
		}
		return fmt.Sprintf("post #%d", targetID)

	case "comment":
		var body string
		d.QueryRow("SELECT body FROM comments WHERE id = ?", targetID).Scan(&body)
		if body != "" {
			if len(body) > 50 {
				return body[:50] + "..."
			}
			return body
		}
		return fmt.Sprintf("comment #%d", targetID)

	case "user":
		var username string
		d.QueryRow("SELECT username FROM users WHERE id = ?", targetID).Scan(&username)
		if username != "" {
			return username
		}
		return fmt.Sprintf("user #%d", targetID)

	case "tag":
		var name string
		d.QueryRow("SELECT name FROM tags WHERE id = ?", targetID).Scan(&name)
		if name != "" {
			return name
		}
		return fmt.Sprintf("tag #%d", targetID)

	case "hat":
		var name, org string
		d.QueryRow("SELECT name, organization FROM hats WHERE id = ?", targetID).Scan(&name, &org)
		if name != "" && org != "" {
			return fmt.Sprintf("%s @ %s", name, org)
		} else if name != "" {
			return name
		}
		return fmt.Sprintf("hat #%d", targetID)

	case "feed":
		var title string
		d.QueryRow("SELECT title FROM rss_feeds WHERE id = ?", targetID).Scan(&title)
		if title != "" {
			return title
		}
		return fmt.Sprintf("feed #%d", targetID)

	default:
		return fmt.Sprintf("%s #%d", targetType, targetID)
	}
}

// GetRecentActions retrieves the most recent actions.
func (d *DB) GetRecentActions(limit int) ([]*models.ActionLog, error) {
	rows, err := d.Query(`
		SELECT a.id, a.user_id, a.action, a.target_type, a.target_id, a.details, a.ip_address, a.created_at,
		       COALESCE(u.username, 'anonymous') as username
		FROM action_log a
		LEFT JOIN users u ON a.user_id = u.id
		ORDER BY a.created_at DESC
		LIMIT ?
	`, limit)
	if err != nil {
		return nil, fmt.Errorf("querying recent actions: %w", err)
	}
	defer rows.Close()

	var logs []*models.ActionLog
	for rows.Next() {
		log := &models.ActionLog{}
		var userID sql.NullInt64
		var details, ipAddress sql.NullString

		err := rows.Scan(
			&log.ID, &userID, &log.Action, &log.TargetType, &log.TargetID,
			&details, &ipAddress, &log.CreatedAt, &log.Username,
		)
		if err != nil {
			return nil, fmt.Errorf("scanning action log: %w", err)
		}

		if userID.Valid {
			log.UserID = &userID.Int64
		}
		if details.Valid {
			log.Details = details.String
		}
		if ipAddress.Valid {
			log.IPAddress = ipAddress.String
		}

		log.TargetInfo = d.getTargetInfo(log.TargetType, log.TargetID)

		logs = append(logs, log)
	}

	return logs, nil
}
