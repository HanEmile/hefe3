package db

import "time"

// Stats contains aggregate statistics about the application.
type Stats struct {
	Users          int64
	Posts          int64
	Comments       int64
	Tags           int64
	InvitesPending int64
	InvitesUsed    int64
	Sessions       int64
	PostsBySource  map[string]int64
	DatabaseSize   int64 // in bytes
	Uptime         time.Duration
}

var startTime = time.Now()

// GetStats returns aggregate statistics for the application.
func (d *DB) GetStats() (*Stats, error) {
	stats := &Stats{
		PostsBySource: make(map[string]int64),
		Uptime:        time.Since(startTime),
	}

	// Single query for most counts
	err := d.QueryRow(`
		SELECT
			(SELECT COUNT(*) FROM users),
			(SELECT COUNT(*) FROM posts),
			(SELECT COUNT(*) FROM comments),
			(SELECT COUNT(*) FROM tags),
			(SELECT COUNT(*) FROM invites WHERE used_by IS NULL),
			(SELECT COUNT(*) FROM invites WHERE used_by IS NOT NULL),
			(SELECT COUNT(*) FROM sessions WHERE expires_at > datetime('now'))
	`).Scan(
		&stats.Users,
		&stats.Posts,
		&stats.Comments,
		&stats.Tags,
		&stats.InvitesPending,
		&stats.InvitesUsed,
		&stats.Sessions,
	)
	if err != nil {
		return nil, err
	}

	// Posts by source
	rows, err := d.Query(`SELECT source, COUNT(*) FROM posts GROUP BY source`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	for rows.Next() {
		var source string
		var count int64
		if err := rows.Scan(&source, &count); err != nil {
			return nil, err
		}
		stats.PostsBySource[source] = count
	}

	// Database size
	err = d.QueryRow(`SELECT page_count * page_size FROM pragma_page_count(), pragma_page_size()`).Scan(&stats.DatabaseSize)
	if err != nil {
		// Non-fatal, some SQLite builds may not support this
		stats.DatabaseSize = 0
	}

	return stats, nil
}
