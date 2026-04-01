// RSS feed database operations.
package db

import (
	"database/sql"
	"fmt"
	"makhor/pkg/models"
	"time"
)

// RSSFeed represents an RSS feed to poll.
type RSSFeed struct {
	ID              int64      `json:"id"`
	URL             string     `json:"url"`
	Title           string     `json:"title"`
	TagID           int64      `json:"tag_id"`
	TagName         string     `json:"tag_name,omitempty"`
	CreatedBy       *int64     `json:"created_by,omitempty"`
	CreatedByName   string     `json:"created_by_name,omitempty"`
	IntervalMinutes int        `json:"interval_minutes"`
	LastPolled      *time.Time `json:"last_polled,omitempty"`
	LastError       string     `json:"last_error,omitempty"`
	IsActive        bool       `json:"is_active"`
	PollOnView      bool       `json:"poll_on_view"`      // Only poll when tag is viewed
	AutoApprove     bool       `json:"auto_approve"`      // Auto-publish or require approval
	MaxItemsPerPoll int        `json:"max_items_per_poll"` // Limit items per poll
	CreatedAt       time.Time  `json:"created_at"`
	ItemCount       int        `json:"item_count,omitempty"` // Number of items imported
}

// RSSItem represents a tracked RSS item.
type RSSItem struct {
	ID         int64
	FeedID     int64
	GUID       string
	PostID     *int64
	ImportedAt time.Time
}

// CreateRSSFeedOptions contains options for creating an RSS feed.
type CreateRSSFeedOptions struct {
	URL             string
	Title           string
	TagID           int64
	CreatedBy       int64
	IntervalMinutes int
	PollOnView      bool
	AutoApprove     bool
	MaxItemsPerPoll int
}

// CreateRSSFeed creates a new RSS feed.
func (d *DB) CreateRSSFeed(url, title string, tagID int64, intervalMinutes int) (*RSSFeed, error) {
	return d.CreateRSSFeedWithOptions(CreateRSSFeedOptions{
		URL:             url,
		Title:           title,
		TagID:           tagID,
		IntervalMinutes: intervalMinutes,
		AutoApprove:     true,
		MaxItemsPerPoll: 10,
	})
}

// CreateRSSFeedWithOptions creates a new RSS feed with full options.
func (d *DB) CreateRSSFeedWithOptions(opts CreateRSSFeedOptions) (*RSSFeed, error) {
	// Verify tag exists
	var exists int
	err := d.QueryRow("SELECT COUNT(*) FROM tags WHERE id = ?", opts.TagID).Scan(&exists)
	if err != nil || exists == 0 {
		return nil, ErrTagNotFound
	}

	// Set defaults
	if opts.IntervalMinutes < 5 {
		opts.IntervalMinutes = 60
	}
	if opts.MaxItemsPerPoll < 1 {
		opts.MaxItemsPerPoll = 10
	}

	var createdBy interface{} = nil
	if opts.CreatedBy > 0 {
		createdBy = opts.CreatedBy
	}

	result, err := d.Exec(`
		INSERT INTO rss_feeds (url, title, tag_id, created_by, interval_minutes, poll_on_view, auto_approve, max_items_per_poll)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?)
	`, opts.URL, opts.Title, opts.TagID, createdBy, opts.IntervalMinutes, opts.PollOnView, opts.AutoApprove, opts.MaxItemsPerPoll)
	if err != nil {
		return nil, fmt.Errorf("inserting feed: %w", err)
	}

	id, err := result.LastInsertId()
	if err != nil {
		return nil, fmt.Errorf("getting last insert id: %w", err)
	}
	return d.GetRSSFeedByID(id)
}

// GetRSSFeedByID retrieves a feed by ID.
func (d *DB) GetRSSFeedByID(id int64) (*RSSFeed, error) {
	feed := &RSSFeed{}
	var lastPolled sql.NullTime
	var lastError sql.NullString
	var createdBy sql.NullInt64
	var createdByName sql.NullString

	err := d.QueryRow(`
		SELECT f.id, f.url, f.title, f.tag_id, t.name, f.created_by, u.username,
		       f.interval_minutes, f.last_polled, f.last_error, f.is_active,
		       COALESCE(f.poll_on_view, FALSE), COALESCE(f.auto_approve, TRUE),
		       COALESCE(f.max_items_per_poll, 10), f.created_at
		FROM rss_feeds f
		JOIN tags t ON f.tag_id = t.id
		LEFT JOIN users u ON f.created_by = u.id
		WHERE f.id = ?
	`, id).Scan(
		&feed.ID, &feed.URL, &feed.Title, &feed.TagID, &feed.TagName, &createdBy, &createdByName,
		&feed.IntervalMinutes, &lastPolled, &lastError, &feed.IsActive,
		&feed.PollOnView, &feed.AutoApprove, &feed.MaxItemsPerPoll, &feed.CreatedAt,
	)
	if err != nil {
		return nil, err
	}

	if lastPolled.Valid {
		feed.LastPolled = &lastPolled.Time
	}
	if lastError.Valid {
		feed.LastError = lastError.String
	}
	if createdBy.Valid {
		feed.CreatedBy = &createdBy.Int64
	}
	if createdByName.Valid {
		feed.CreatedByName = createdByName.String
	}

	feed.ItemCount = d.GetRSSItemCount(id)
	return feed, nil
}

// GetAllRSSFeeds retrieves all feeds.
func (d *DB) GetAllRSSFeeds() ([]*RSSFeed, error) {
	rows, err := d.Query(`
		SELECT f.id, f.url, f.title, f.tag_id, t.name, f.created_by, u.username,
		       f.interval_minutes, f.last_polled, f.last_error, f.is_active,
		       COALESCE(f.poll_on_view, FALSE), COALESCE(f.auto_approve, TRUE),
		       COALESCE(f.max_items_per_poll, 10), f.created_at,
		       (SELECT COUNT(*) FROM rss_items WHERE feed_id = f.id)
		FROM rss_feeds f
		JOIN tags t ON f.tag_id = t.id
		LEFT JOIN users u ON f.created_by = u.id
		ORDER BY f.created_at DESC
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	return d.scanFeeds(rows)
}

// GetRSSFeedsByTag retrieves all feeds for a specific tag.
func (d *DB) GetRSSFeedsByTag(tagID int64) ([]*RSSFeed, error) {
	rows, err := d.Query(`
		SELECT f.id, f.url, f.title, f.tag_id, t.name, f.created_by, u.username,
		       f.interval_minutes, f.last_polled, f.last_error, f.is_active,
		       COALESCE(f.poll_on_view, FALSE), COALESCE(f.auto_approve, TRUE),
		       COALESCE(f.max_items_per_poll, 10), f.created_at,
		       (SELECT COUNT(*) FROM rss_items WHERE feed_id = f.id)
		FROM rss_feeds f
		JOIN tags t ON f.tag_id = t.id
		LEFT JOIN users u ON f.created_by = u.id
		WHERE f.tag_id = ?
		ORDER BY f.created_at DESC
	`, tagID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	return d.scanFeeds(rows)
}

// GetFeedsDueForPolling returns feeds that need to be polled (excluding poll_on_view feeds).
func (d *DB) GetFeedsDueForPolling() ([]*RSSFeed, error) {
	rows, err := d.Query(`
		SELECT f.id, f.url, f.title, f.tag_id, t.name, f.created_by, u.username,
		       f.interval_minutes, f.last_polled, f.last_error, f.is_active,
		       COALESCE(f.poll_on_view, FALSE), COALESCE(f.auto_approve, TRUE),
		       COALESCE(f.max_items_per_poll, 10), f.created_at,
		       (SELECT COUNT(*) FROM rss_items WHERE feed_id = f.id)
		FROM rss_feeds f
		JOIN tags t ON f.tag_id = t.id
		LEFT JOIN users u ON f.created_by = u.id
		WHERE f.is_active = TRUE
		  AND COALESCE(f.poll_on_view, FALSE) = FALSE
		  AND (f.last_polled IS NULL
		       OR datetime(f.last_polled, '+' || f.interval_minutes || ' minutes') <= datetime('now'))
		ORDER BY f.last_polled ASC NULLS FIRST
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	return d.scanFeeds(rows)
}

// GetFeedsDueForViewPolling returns poll_on_view feeds for a tag that are due for polling.
func (d *DB) GetFeedsDueForViewPolling(tagID int64) ([]*RSSFeed, error) {
	rows, err := d.Query(`
		SELECT f.id, f.url, f.title, f.tag_id, t.name, f.created_by, u.username,
		       f.interval_minutes, f.last_polled, f.last_error, f.is_active,
		       COALESCE(f.poll_on_view, FALSE), COALESCE(f.auto_approve, TRUE),
		       COALESCE(f.max_items_per_poll, 10), f.created_at,
		       (SELECT COUNT(*) FROM rss_items WHERE feed_id = f.id)
		FROM rss_feeds f
		JOIN tags t ON f.tag_id = t.id
		LEFT JOIN users u ON f.created_by = u.id
		WHERE f.is_active = TRUE
		  AND f.tag_id = ?
		  AND COALESCE(f.poll_on_view, FALSE) = TRUE
		  AND (f.last_polled IS NULL
		       OR datetime(f.last_polled, '+' || f.interval_minutes || ' minutes') <= datetime('now'))
		ORDER BY f.last_polled ASC NULLS FIRST
	`, tagID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	return d.scanFeeds(rows)
}

// scanFeeds scans rows into RSSFeed structs.
func (d *DB) scanFeeds(rows *sql.Rows) ([]*RSSFeed, error) {
	var feeds []*RSSFeed
	for rows.Next() {
		feed := &RSSFeed{}
		var lastPolled sql.NullTime
		var lastError sql.NullString
		var createdBy sql.NullInt64
		var createdByName sql.NullString

		err := rows.Scan(
			&feed.ID, &feed.URL, &feed.Title, &feed.TagID, &feed.TagName, &createdBy, &createdByName,
			&feed.IntervalMinutes, &lastPolled, &lastError, &feed.IsActive,
			&feed.PollOnView, &feed.AutoApprove, &feed.MaxItemsPerPoll, &feed.CreatedAt,
			&feed.ItemCount,
		)
		if err != nil {
			return nil, err
		}

		if lastPolled.Valid {
			feed.LastPolled = &lastPolled.Time
		}
		if lastError.Valid {
			feed.LastError = lastError.String
		}
		if createdBy.Valid {
			feed.CreatedBy = &createdBy.Int64
		}
		if createdByName.Valid {
			feed.CreatedByName = createdByName.String
		}

		feeds = append(feeds, feed)
	}

	return feeds, nil
}

// UpdateRSSFeed updates feed settings.
func (d *DB) UpdateRSSFeed(id int64, intervalMinutes int, pollOnView, autoApprove bool, maxItems int, isActive bool) error {
	_, err := d.Exec(`
		UPDATE rss_feeds
		SET interval_minutes = ?, poll_on_view = ?, auto_approve = ?,
		    max_items_per_poll = ?, is_active = ?
		WHERE id = ?
	`, intervalMinutes, pollOnView, autoApprove, maxItems, isActive, id)
	return err
}

// UpdateFeedPolled updates the last polled time and clears error.
func (d *DB) UpdateFeedPolled(id int64) error {
	_, err := d.Exec(`
		UPDATE rss_feeds SET last_polled = CURRENT_TIMESTAMP, last_error = NULL
		WHERE id = ?
	`, id)
	return err
}

// UpdateFeedError sets an error message for a feed.
func (d *DB) UpdateFeedError(id int64, errMsg string) error {
	_, err := d.Exec(`
		UPDATE rss_feeds SET last_polled = CURRENT_TIMESTAMP, last_error = ?
		WHERE id = ?
	`, errMsg, id)
	return err
}

// DeleteRSSFeed deletes a feed.
func (d *DB) DeleteRSSFeed(id int64) error {
	_, err := d.Exec("DELETE FROM rss_feeds WHERE id = ?", id)
	return err
}

// HasRSSItem checks if an RSS item has already been imported.
func (d *DB) HasRSSItem(feedID int64, guid string) bool {
	var count int
	d.QueryRow("SELECT COUNT(*) FROM rss_items WHERE feed_id = ? AND guid = ?", feedID, guid).Scan(&count)
	return count > 0
}

// CreateRSSItem records an imported RSS item.
func (d *DB) CreateRSSItem(feedID int64, guid string, postID int64) error {
	_, err := d.Exec(`
		INSERT INTO rss_items (feed_id, guid, post_id)
		VALUES (?, ?, ?)
	`, feedID, guid, postID)
	return err
}

// GetRSSItemCount returns the number of items imported from a feed.
func (d *DB) GetRSSItemCount(feedID int64) int {
	var count int
	d.QueryRow("SELECT COUNT(*) FROM rss_items WHERE feed_id = ?", feedID).Scan(&count)
	return count
}

// GetPostsByFeed retrieves posts from a specific RSS feed with pagination.
func (d *DB) GetPostsByFeed(feedID int64, page, perPage int) ([]*models.Post, int, error) {
	// Get total count
	var total int
	err := d.QueryRow(`
		SELECT COUNT(*) FROM posts p
		WHERE p.source_type = 'rss' AND p.source_id = ? AND p.is_deleted = FALSE
	`, feedID).Scan(&total)
	if err != nil {
		return nil, 0, err
	}

	offset := (page - 1) * perPage

	rows, err := d.Query(`
		SELECT p.id, p.user_id, p.title, p.url, p.body, p.created_at, p.updated_at,
		       p.score, p.is_deleted, u.username,
		       (SELECT COUNT(*) FROM comments WHERE post_id = p.id AND is_deleted = FALSE) as comment_count
		FROM posts p
		JOIN users u ON p.user_id = u.id
		WHERE p.source_type = 'rss' AND p.source_id = ? AND p.is_deleted = FALSE
		ORDER BY p.created_at DESC
		LIMIT ? OFFSET ?
	`, feedID, perPage, offset)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()

	var posts []*models.Post
	for rows.Next() {
		post := &models.Post{}
		err := rows.Scan(
			&post.ID, &post.UserID, &post.Title, &post.URL, &post.Body,
			&post.CreatedAt, &post.UpdatedAt, &post.Score, &post.IsDeleted,
			&post.Username, &post.CommentCount,
		)
		if err != nil {
			return nil, 0, err
		}
		post.Domain = extractDomain(post.URL)
		post.SourceType = models.SourceRSS
		post.SourceID = &feedID
		posts = append(posts, post)
	}

	// Load tags for all posts
	for _, post := range posts {
		post.Tags, _ = d.GetPostTags(post.ID)
	}

	return posts, total, nil
}

// CanManageTagFeed checks if a user can manage feeds for a tag.
// Returns true if user is site admin, tag creator, or tag admin.
func (d *DB) CanManageTagFeed(userID, tagID int64) bool {
	// Check if site admin
	var isAdmin bool
	d.QueryRow("SELECT is_admin FROM users WHERE id = ?", userID).Scan(&isAdmin)
	if isAdmin {
		return true
	}

	// Check if tag creator
	var creatorID sql.NullInt64
	d.QueryRow("SELECT creator_id FROM tags WHERE id = ?", tagID).Scan(&creatorID)
	if creatorID.Valid && creatorID.Int64 == userID {
		return true
	}

	// Check if tag admin
	var count int
	d.QueryRow("SELECT COUNT(*) FROM tag_admins WHERE tag_id = ? AND user_id = ?", tagID, userID).Scan(&count)
	return count > 0
}

// CanEditFeed checks if a user can edit a specific RSS feed.
// Returns true if user is site admin, feed creator, or tag moderator for the feed's tag.
func (d *DB) CanEditFeed(userID, feedID int64) bool {
	// Check if site admin
	var isAdmin bool
	d.QueryRow("SELECT is_admin FROM users WHERE id = ?", userID).Scan(&isAdmin)
	if isAdmin {
		return true
	}

	// Get feed info
	var createdBy sql.NullInt64
	var tagID int64
	err := d.QueryRow("SELECT created_by, tag_id FROM rss_feeds WHERE id = ?", feedID).Scan(&createdBy, &tagID)
	if err != nil {
		return false
	}

	// Check if feed creator
	if createdBy.Valid && createdBy.Int64 == userID {
		return true
	}

	// Check if can manage tag feeds
	return d.CanManageTagFeed(userID, tagID)
}

// UpdateRSSFeedFull updates feed settings including title and URL.
func (d *DB) UpdateRSSFeedFull(id int64, url, title string, intervalMinutes int, pollOnView, autoApprove bool, maxItems int, isActive bool) error {
	_, err := d.Exec(`
		UPDATE rss_feeds
		SET url = ?, title = ?, interval_minutes = ?, poll_on_view = ?, auto_approve = ?,
		    max_items_per_poll = ?, is_active = ?
		WHERE id = ?
	`, url, title, intervalMinutes, pollOnView, autoApprove, maxItems, isActive, id)
	return err
}

// RSSPollLog represents a single poll attempt record.
type RSSPollLog struct {
	ID            int64
	FeedID        int64
	PolledAt      time.Time
	Success       bool
	HTTPStatus    int
	ItemsFound    int
	ItemsImported int
	ErrorMessage  string
	DurationMs    int
}

// LogRSSPoll records a poll attempt for a feed.
func (d *DB) LogRSSPoll(feedID int64, success bool, httpStatus, itemsFound, itemsImported int, errorMsg string, durationMs int) error {
	_, err := d.Exec(`
		INSERT INTO rss_poll_log (feed_id, success, http_status, items_found, items_imported, error_message, duration_ms)
		VALUES (?, ?, ?, ?, ?, ?, ?)
	`, feedID, success, httpStatus, itemsFound, itemsImported, errorMsg, durationMs)
	return err
}

// RSSFeedStats contains statistics for an RSS feed.
type RSSFeedStats struct {
	TotalPolls         int
	SuccessfulPolls    int
	FailedPolls        int
	TotalItemsFound    int
	TotalItemsImported int
	AvgItemsPerPoll    float64
	SuccessRate        float64
	FailRate24h        float64 // Failure rate in last 24 hours
	LastPolls          []RSSPollLog
	// HTTP status breakdown
	StatusCounts map[int]int
}

// GetRSSFeedStats retrieves statistics for a feed.
func (d *DB) GetRSSFeedStats(feedID int64) (*RSSFeedStats, error) {
	stats := &RSSFeedStats{
		StatusCounts: make(map[int]int),
	}

	// Overall stats
	err := d.QueryRow(`
		SELECT
			COUNT(*) as total_polls,
			SUM(CASE WHEN success THEN 1 ELSE 0 END) as successful_polls,
			SUM(CASE WHEN NOT success THEN 1 ELSE 0 END) as failed_polls,
			COALESCE(SUM(items_found), 0) as total_items_found,
			COALESCE(SUM(items_imported), 0) as total_items_imported
		FROM rss_poll_log
		WHERE feed_id = ?
	`, feedID).Scan(
		&stats.TotalPolls,
		&stats.SuccessfulPolls,
		&stats.FailedPolls,
		&stats.TotalItemsFound,
		&stats.TotalItemsImported,
	)
	if err != nil {
		return nil, err
	}

	// Calculate rates
	if stats.TotalPolls > 0 {
		stats.SuccessRate = float64(stats.SuccessfulPolls) / float64(stats.TotalPolls) * 100
		stats.AvgItemsPerPoll = float64(stats.TotalItemsImported) / float64(stats.TotalPolls)
	}

	// 24h failure rate
	var polls24h, failed24h int
	err = d.QueryRow(`
		SELECT
			COUNT(*) as polls,
			SUM(CASE WHEN NOT success THEN 1 ELSE 0 END) as failed
		FROM rss_poll_log
		WHERE feed_id = ? AND polled_at >= datetime('now', '-24 hours')
	`, feedID).Scan(&polls24h, &failed24h)
	if err == nil && polls24h > 0 {
		stats.FailRate24h = float64(failed24h) / float64(polls24h) * 100
	}

	// HTTP status breakdown
	rows, err := d.Query(`
		SELECT http_status, COUNT(*) as count
		FROM rss_poll_log
		WHERE feed_id = ? AND http_status > 0
		GROUP BY http_status
		ORDER BY count DESC
	`, feedID)
	if err == nil {
		defer rows.Close()
		for rows.Next() {
			var status, count int
			if rows.Scan(&status, &count) == nil {
				stats.StatusCounts[status] = count
			}
		}
	}

	// Last 20 polls
	rows, err = d.Query(`
		SELECT id, feed_id, polled_at, success, COALESCE(http_status, 0),
		       items_found, items_imported, COALESCE(error_message, ''), COALESCE(duration_ms, 0)
		FROM rss_poll_log
		WHERE feed_id = ?
		ORDER BY polled_at DESC
		LIMIT 20
	`, feedID)
	if err == nil {
		defer rows.Close()
		for rows.Next() {
			var log RSSPollLog
			if rows.Scan(&log.ID, &log.FeedID, &log.PolledAt, &log.Success, &log.HTTPStatus,
				&log.ItemsFound, &log.ItemsImported, &log.ErrorMessage, &log.DurationMs) == nil {
				stats.LastPolls = append(stats.LastPolls, log)
			}
		}
	}

	return stats, nil
}

// CleanOldPollLogs removes poll logs older than the specified duration.
func (d *DB) CleanOldPollLogs(maxAge time.Duration) (int64, error) {
	result, err := d.Exec(`
		DELETE FROM rss_poll_log
		WHERE polled_at < datetime('now', ?)
	`, fmt.Sprintf("-%d seconds", int(maxAge.Seconds())))
	if err != nil {
		return 0, err
	}
	return result.RowsAffected()
}
