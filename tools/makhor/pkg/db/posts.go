// Post-related database operations.
package db

import (
	"database/sql"
	"fmt"
	"makhor/pkg/models"
	"net/url"
	"strings"
	"time"
)

// CreatePost creates a new post/link submission.
func (d *DB) CreatePost(userID int64, title, postURL, body string, tagIDs []int64) (*models.Post, error) {
	return d.CreatePostWithSourceAndHat(userID, title, postURL, body, tagIDs, models.SourceUser, nil, nil)
}

// CreatePostWithHat creates a new post with an optional hat.
func (d *DB) CreatePostWithHat(userID int64, title, postURL, body string, tagIDs []int64, hatID *int64) (*models.Post, error) {
	return d.CreatePostWithSourceAndHat(userID, title, postURL, body, tagIDs, models.SourceUser, nil, hatID)
}

// CreatePostWithSource creates a new post with source tracking.
func (d *DB) CreatePostWithSource(userID int64, title, postURL, body string, tagIDs []int64, sourceType models.PostSource, sourceID *int64) (*models.Post, error) {
	return d.createPostFull(userID, title, postURL, body, tagIDs, sourceType, sourceID, nil, nil)
}

// CreatePostWithSourceAndTime creates a new post with source tracking and a specific creation time.
func (d *DB) CreatePostWithSourceAndTime(userID int64, title, postURL, body string, tagIDs []int64, sourceType models.PostSource, sourceID *int64, createdAt *time.Time) (*models.Post, error) {
	return d.createPostFull(userID, title, postURL, body, tagIDs, sourceType, sourceID, nil, createdAt)
}

// CreatePostWithSourceAndHat creates a new post with source tracking and optional hat.
func (d *DB) CreatePostWithSourceAndHat(userID int64, title, postURL, body string, tagIDs []int64, sourceType models.PostSource, sourceID *int64, hatID *int64) (*models.Post, error) {
	return d.createPostFull(userID, title, postURL, body, tagIDs, sourceType, sourceID, hatID, nil)
}

// createPostFull is the internal function that creates a post with all options.
func (d *DB) createPostFull(userID int64, title, postURL, body string, tagIDs []int64, sourceType models.PostSource, sourceID *int64, hatID *int64, createdAt *time.Time) (*models.Post, error) {
	tx, err := d.Begin()
	if err != nil {
		return nil, fmt.Errorf("beginning transaction: %w", err)
	}
	defer tx.Rollback()

	var result sql.Result
	if createdAt != nil {
		// Format time as SQLite datetime string for compatibility
		timeStr := createdAt.UTC().Format("2006-01-02 15:04:05")
		result, err = tx.Exec(
			`INSERT INTO posts (user_id, title, url, body, score, source_type, source_id, hat_id, created_at, updated_at) VALUES (?, ?, ?, ?, 0, ?, ?, ?, ?, ?)`,
			userID, title, postURL, body, string(sourceType), sourceID, hatID, timeStr, timeStr,
		)
	} else {
		result, err = tx.Exec(
			`INSERT INTO posts (user_id, title, url, body, score, source_type, source_id, hat_id) VALUES (?, ?, ?, ?, 0, ?, ?, ?)`,
			userID, title, postURL, body, string(sourceType), sourceID, hatID,
		)
	}
	if err != nil {
		return nil, fmt.Errorf("inserting post: %w", err)
	}

	postID, err := result.LastInsertId()
	if err != nil {
		return nil, fmt.Errorf("getting post id: %w", err)
	}

	// Add tags
	for _, tagID := range tagIDs {
		_, err = tx.Exec(
			`INSERT INTO post_tags (post_id, tag_id) VALUES (?, ?)`,
			postID, tagID,
		)
		if err != nil {
			return nil, fmt.Errorf("inserting post tag: %w", err)
		}
	}

	if err := tx.Commit(); err != nil {
		return nil, fmt.Errorf("committing transaction: %w", err)
	}

	return d.GetPostByID(postID, nil)
}

// GetPostByID retrieves a post by ID, optionally checking if user voted.
func (d *DB) GetPostByID(id int64, currentUserID *int64) (*models.Post, error) {
	post := &models.Post{}
	var postURL, body, sourceType sql.NullString
	var sourceID, hatID sql.NullInt64

	err := d.QueryRow(`
		SELECT p.id, p.user_id, p.title, p.url, p.body, p.created_at, p.updated_at,
		       p.score, p.is_deleted, u.username,
		       (SELECT COUNT(*) FROM comments WHERE post_id = p.id AND is_deleted = FALSE) as comment_count,
		       p.source_type, p.source_id, p.hat_id
		FROM posts p
		JOIN users u ON p.user_id = u.id
		WHERE p.id = ?
	`, id).Scan(
		&post.ID, &post.UserID, &post.Title, &postURL, &body,
		&post.CreatedAt, &post.UpdatedAt, &post.Score, &post.IsDeleted,
		&post.Username, &post.CommentCount, &sourceType, &sourceID, &hatID,
	)

	if err == sql.ErrNoRows {
		return nil, ErrPostNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("querying post: %w", err)
	}

	if postURL.Valid {
		post.URL = postURL.String
		post.Domain = extractDomain(post.URL)
	}
	if body.Valid {
		post.Body = body.String
	}
	if sourceType.Valid {
		post.SourceType = models.PostSource(sourceType.String)
	}
	if sourceID.Valid {
		post.SourceID = &sourceID.Int64
	}
	if hatID.Valid {
		post.HatID = &hatID.Int64
		// Fetch the hat details
		post.Hat, _ = d.GetHatByID(hatID.Int64)
	}

	// Get tags
	post.Tags, err = d.GetPostTags(id)
	if err != nil {
		return nil, fmt.Errorf("getting post tags: %w", err)
	}

	// Check if current user voted
	if currentUserID != nil {
		var count int
		d.QueryRow(
			`SELECT COUNT(*) FROM votes WHERE user_id = ? AND post_id = ?`,
			*currentUserID, id,
		).Scan(&count)
		post.UserVoted = count > 0
	}

	return post, nil
}

// GetPosts retrieves posts with pagination and optional filters.
// tagName supports hierarchical namespacing with :: separator.
// e.g., "programming" matches "programming", "programming::go", "programming::rust"
// Posts without tags are hidden unless the user is the author or an admin.
func (d *DB) GetPosts(page, perPage int, tagName string, currentUserID *int64, isAdmin bool) ([]*models.Post, int, error) {
	// Build query with optional tag filter
	countQuery := `SELECT COUNT(DISTINCT p.id) FROM posts p`
	selectQuery := `
		SELECT DISTINCT p.id, p.user_id, p.title, p.url, p.body, p.created_at, p.updated_at,
		       p.score, p.is_deleted, u.username,
		       (SELECT COUNT(*) FROM comments WHERE post_id = p.id AND is_deleted = FALSE) as comment_count,
		       COALESCE(p.source_type, 'user') as source_type, p.source_id,
		       COALESCE(rf.title, rf.url) as source_name
		FROM posts p
		JOIN users u ON p.user_id = u.id
		LEFT JOIN rss_feeds rf ON p.source_type = 'rss' AND p.source_id = rf.id
	`

	var args []interface{}
	var whereClause string

	if tagName != "" {
		joinClause := `
			JOIN post_tags pt ON p.id = pt.post_id
			JOIN tags t ON pt.tag_id = t.id
		`
		countQuery += joinClause
		selectQuery += joinClause
		// Match exact tag OR tags that start with "tagName::" (hierarchical children)
		whereClause = ` WHERE p.is_deleted = FALSE AND (t.name = ? OR t.name LIKE ?)`
		args = append(args, tagName, tagName+"::%")
	} else {
		whereClause = ` WHERE p.is_deleted = FALSE`
		// Hide posts without tags unless user is author or admin
		if !isAdmin {
			if currentUserID != nil {
				whereClause += ` AND (EXISTS (SELECT 1 FROM post_tags WHERE post_id = p.id) OR p.user_id = ?)`
				args = append(args, *currentUserID)
			} else {
				whereClause += ` AND EXISTS (SELECT 1 FROM post_tags WHERE post_id = p.id)`
			}
		}
	}

	countQuery += whereClause
	selectQuery += whereClause

	// Get total count
	var total int
	err := d.QueryRow(countQuery, args...).Scan(&total)
	if err != nil {
		return nil, 0, fmt.Errorf("counting posts: %w", err)
	}

	// Get posts with pagination, sorted by score and recency
	selectQuery += ` ORDER BY p.score DESC, p.created_at DESC LIMIT ? OFFSET ?`
	args = append(args, perPage, (page-1)*perPage)

	rows, err := d.Query(selectQuery, args...)
	if err != nil {
		return nil, 0, fmt.Errorf("querying posts: %w", err)
	}
	defer rows.Close()

	posts := make([]*models.Post, 0, perPage)
	postIDs := make([]int64, 0, perPage)

	for rows.Next() {
		post := &models.Post{}
		var postURL, body, sourceType, sourceName sql.NullString
		var sourceID sql.NullInt64

		err := rows.Scan(
			&post.ID, &post.UserID, &post.Title, &postURL, &body,
			&post.CreatedAt, &post.UpdatedAt, &post.Score, &post.IsDeleted,
			&post.Username, &post.CommentCount, &sourceType, &sourceID, &sourceName,
		)
		if err != nil {
			return nil, 0, fmt.Errorf("scanning post: %w", err)
		}

		if postURL.Valid {
			post.URL = postURL.String
			post.Domain = extractDomain(post.URL)
		}
		if body.Valid {
			post.Body = body.String
		}
		if sourceType.Valid {
			post.SourceType = models.PostSource(sourceType.String)
		}
		if sourceID.Valid {
			post.SourceID = &sourceID.Int64
		}
		if sourceName.Valid {
			post.SourceName = sourceName.String
		}

		posts = append(posts, post)
		postIDs = append(postIDs, post.ID)
	}

	// Batch load tags for all posts (fixes N+1)
	if len(postIDs) > 0 {
		tagsByPost, _ := d.GetTagsForPosts(postIDs)
		for _, post := range posts {
			post.Tags = tagsByPost[post.ID]
		}
	}

	// Batch load votes for current user (fixes N+1)
	if currentUserID != nil && len(postIDs) > 0 {
		votedPosts, _ := d.GetUserVotedPosts(*currentUserID, postIDs)
		for _, post := range posts {
			post.UserVoted = votedPosts[post.ID]
		}
	}

	// Batch load hats for posts (fixes N+1)
	if len(postIDs) > 0 {
		hatsByPost, _ := d.GetHatsForPosts(postIDs)
		for _, post := range posts {
			post.Hat = hatsByPost[post.ID]
		}
	}

	return posts, total, nil
}

// GetPostsByDomain retrieves all posts from a specific domain.
func (d *DB) GetPostsByDomain(domain string, page, perPage int, currentUserID *int64, isAdmin bool) ([]*models.Post, int, error) {
	// Build the domain pattern to match - we extract domain from URL so need to match against it
	// URLs are stored with their full form, so we need to check if the domain matches
	domainPattern := "%://" + domain + "/%"
	domainPatternNoPath := "%://" + domain
	wwwPattern := "%://www." + domain + "/%"
	wwwPatternNoPath := "%://www." + domain

	countQuery := `SELECT COUNT(*) FROM posts p WHERE p.is_deleted = FALSE AND p.url != '' AND (p.url LIKE ? OR p.url LIKE ? OR p.url LIKE ? OR p.url LIKE ?)`
	selectQuery := `
		SELECT p.id, p.user_id, p.title, p.url, p.body, p.created_at, p.updated_at,
		       p.score, p.is_deleted, u.username,
		       (SELECT COUNT(*) FROM comments WHERE post_id = p.id AND is_deleted = FALSE) as comment_count,
		       COALESCE(p.source_type, 'user') as source_type, p.source_id,
		       COALESCE(rf.title, rf.url) as source_name
		FROM posts p
		JOIN users u ON p.user_id = u.id
		LEFT JOIN rss_feeds rf ON p.source_type = 'rss' AND p.source_id = rf.id
		WHERE p.is_deleted = FALSE AND p.url != '' AND (p.url LIKE ? OR p.url LIKE ? OR p.url LIKE ? OR p.url LIKE ?)
		ORDER BY p.created_at DESC
		LIMIT ? OFFSET ?
	`

	// Get total count
	var total int
	err := d.QueryRow(countQuery, domainPattern, domainPatternNoPath, wwwPattern, wwwPatternNoPath).Scan(&total)
	if err != nil {
		return nil, 0, fmt.Errorf("counting posts by domain: %w", err)
	}

	// Get posts
	rows, err := d.Query(selectQuery, domainPattern, domainPatternNoPath, wwwPattern, wwwPatternNoPath, perPage, (page-1)*perPage)
	if err != nil {
		return nil, 0, fmt.Errorf("querying posts by domain: %w", err)
	}
	defer rows.Close()

	posts := make([]*models.Post, 0, perPage)
	postIDs := make([]int64, 0, perPage)

	for rows.Next() {
		post := &models.Post{}
		var postURL, body, sourceType, sourceName sql.NullString
		var sourceID sql.NullInt64

		err := rows.Scan(
			&post.ID, &post.UserID, &post.Title, &postURL, &body,
			&post.CreatedAt, &post.UpdatedAt, &post.Score, &post.IsDeleted,
			&post.Username, &post.CommentCount, &sourceType, &sourceID, &sourceName,
		)
		if err != nil {
			return nil, 0, fmt.Errorf("scanning post: %w", err)
		}

		if postURL.Valid {
			post.URL = postURL.String
			post.Domain = extractDomain(post.URL)
		}
		if body.Valid {
			post.Body = body.String
		}
		if sourceType.Valid {
			post.SourceType = models.PostSource(sourceType.String)
		}
		if sourceID.Valid {
			post.SourceID = &sourceID.Int64
		}
		if sourceName.Valid {
			post.SourceName = sourceName.String
		}

		posts = append(posts, post)
		postIDs = append(postIDs, post.ID)
	}

	// Batch load tags for all posts
	if len(postIDs) > 0 {
		tagsByPost, _ := d.GetTagsForPosts(postIDs)
		for _, post := range posts {
			post.Tags = tagsByPost[post.ID]
		}
	}

	// Batch load votes for current user
	if currentUserID != nil && len(postIDs) > 0 {
		votedPosts, _ := d.GetUserVotedPosts(*currentUserID, postIDs)
		for _, post := range posts {
			post.UserVoted = votedPosts[post.ID]
		}
	}

	// Batch load hats for posts
	if len(postIDs) > 0 {
		hatsByPost, _ := d.GetHatsForPosts(postIDs)
		for _, post := range posts {
			post.Hat = hatsByPost[post.ID]
		}
	}

	return posts, total, nil
}

// GetPostsByTagNewest retrieves posts for a tag sorted by creation time (newest first).
func (d *DB) GetPostsByTagNewest(page, perPage int, tagName string, currentUserID *int64) ([]*models.Post, int, error) {
	return d.getPostsByTag(page, perPage, tagName, currentUserID, "p.created_at DESC", 0)
}

// GetPostsByTagTop retrieves posts for a tag sorted by score with optional time filter.
func (d *DB) GetPostsByTagTop(page, perPage int, tagName string, hoursAgo int, currentUserID *int64) ([]*models.Post, int, error) {
	return d.getPostsByTag(page, perPage, tagName, currentUserID, "p.score DESC, p.created_at DESC", hoursAgo)
}

// validOrderByClauses is a whitelist of allowed ORDER BY clauses to prevent SQL injection.
var validOrderByClauses = map[string]bool{
	"p.created_at DESC":              true,
	"p.score DESC, p.created_at DESC": true,
	"p.score DESC":                   true,
}

// getPostsByTag is a helper that retrieves posts for a tag with custom sorting.
func (d *DB) getPostsByTag(page, perPage int, tagName string, currentUserID *int64, orderBy string, hoursAgo int) ([]*models.Post, int, error) {
	// Validate orderBy against whitelist to prevent SQL injection
	if !validOrderByClauses[orderBy] {
		orderBy = "p.created_at DESC" // Safe default
	}
	countQuery := `SELECT COUNT(DISTINCT p.id) FROM posts p
		JOIN post_tags pt ON p.id = pt.post_id
		JOIN tags t ON pt.tag_id = t.id`
	selectQuery := `
		SELECT DISTINCT p.id, p.user_id, p.title, p.url, p.body, p.created_at, p.updated_at,
		       p.score, p.is_deleted, u.username,
		       (SELECT COUNT(*) FROM comments WHERE post_id = p.id AND is_deleted = FALSE) as comment_count,
		       COALESCE(p.source_type, 'user') as source_type, p.source_id,
		       COALESCE(rf.title, rf.url) as source_name
		FROM posts p
		JOIN users u ON p.user_id = u.id
		LEFT JOIN rss_feeds rf ON p.source_type = 'rss' AND p.source_id = rf.id
		JOIN post_tags pt ON p.id = pt.post_id
		JOIN tags t ON pt.tag_id = t.id
	`

	var args []interface{}
	whereClause := ` WHERE p.is_deleted = FALSE AND (t.name = ? OR t.name LIKE ?)`
	args = append(args, tagName, tagName+"::%")

	if hoursAgo > 0 {
		whereClause += ` AND p.created_at >= datetime('now', ?)`
		args = append(args, fmt.Sprintf("-%d hours", hoursAgo))
	}

	countQuery += whereClause
	selectQuery += whereClause

	// Get total count
	var total int
	err := d.QueryRow(countQuery, args...).Scan(&total)
	if err != nil {
		return nil, 0, fmt.Errorf("counting posts: %w", err)
	}

	// Get posts with pagination
	selectQuery += ` ORDER BY ` + orderBy + ` LIMIT ? OFFSET ?`
	args = append(args, perPage, (page-1)*perPage)

	rows, err := d.Query(selectQuery, args...)
	if err != nil {
		return nil, 0, fmt.Errorf("querying posts: %w", err)
	}
	defer rows.Close()

	posts := make([]*models.Post, 0, perPage)
	postIDs := make([]int64, 0, perPage)

	for rows.Next() {
		post := &models.Post{}
		var postURL, body, sourceType, sourceName sql.NullString
		var sourceID sql.NullInt64

		err := rows.Scan(
			&post.ID, &post.UserID, &post.Title, &postURL, &body,
			&post.CreatedAt, &post.UpdatedAt, &post.Score, &post.IsDeleted,
			&post.Username, &post.CommentCount, &sourceType, &sourceID, &sourceName,
		)
		if err != nil {
			return nil, 0, fmt.Errorf("scanning post: %w", err)
		}

		if postURL.Valid {
			post.URL = postURL.String
			post.Domain = extractDomain(post.URL)
		}
		if body.Valid {
			post.Body = body.String
		}
		if sourceType.Valid {
			post.SourceType = models.PostSource(sourceType.String)
		}
		if sourceID.Valid {
			post.SourceID = &sourceID.Int64
		}
		if sourceName.Valid {
			post.SourceName = sourceName.String
		}

		posts = append(posts, post)
		postIDs = append(postIDs, post.ID)
	}

	// Batch load tags for all posts (fixes N+1)
	if len(postIDs) > 0 {
		tagsByPost, _ := d.GetTagsForPosts(postIDs)
		for _, post := range posts {
			post.Tags = tagsByPost[post.ID]
		}
	}

	// Batch load votes for current user (fixes N+1)
	if currentUserID != nil && len(postIDs) > 0 {
		votedPosts, _ := d.GetUserVotedPosts(*currentUserID, postIDs)
		for _, post := range posts {
			post.UserVoted = votedPosts[post.ID]
		}
	}

	// Batch load hats for posts
	if len(postIDs) > 0 {
		hatsByPost, _ := d.GetHatsForPosts(postIDs)
		for _, post := range posts {
			post.Hat = hatsByPost[post.ID]
		}
	}

	return posts, total, nil
}

// GetNewestPosts retrieves posts sorted by creation time.
// Posts without tags are hidden unless the user is the author or an admin.
func (d *DB) GetNewestPosts(page, perPage int, currentUserID *int64, isAdmin bool) ([]*models.Post, int, error) {
	// Build tag filter clause
	tagFilterClause := ""
	var countArgs, selectArgs []interface{}
	if !isAdmin {
		if currentUserID != nil {
			tagFilterClause = ` AND (EXISTS (SELECT 1 FROM post_tags WHERE post_id = p.id) OR p.user_id = ?)`
			countArgs = append(countArgs, *currentUserID)
			selectArgs = append(selectArgs, *currentUserID)
		} else {
			tagFilterClause = ` AND EXISTS (SELECT 1 FROM post_tags WHERE post_id = p.id)`
		}
	}

	var total int
	err := d.QueryRow(`SELECT COUNT(*) FROM posts p WHERE is_deleted = FALSE`+tagFilterClause, countArgs...).Scan(&total)
	if err != nil {
		return nil, 0, fmt.Errorf("counting posts: %w", err)
	}

	selectArgs = append(selectArgs, perPage, (page-1)*perPage)
	rows, err := d.Query(`
		SELECT p.id, p.user_id, p.title, p.url, p.body, p.created_at, p.updated_at,
		       p.score, p.is_deleted, u.username,
		       (SELECT COUNT(*) FROM comments WHERE post_id = p.id AND is_deleted = FALSE) as comment_count,
		       COALESCE(p.source_type, 'user') as source_type, p.source_id,
		       COALESCE(rf.title, rf.url) as source_name
		FROM posts p
		JOIN users u ON p.user_id = u.id
		LEFT JOIN rss_feeds rf ON p.source_type = 'rss' AND p.source_id = rf.id
		WHERE p.is_deleted = FALSE`+tagFilterClause+`
		ORDER BY p.created_at DESC
		LIMIT ? OFFSET ?
	`, selectArgs...)
	if err != nil {
		return nil, 0, fmt.Errorf("querying posts: %w", err)
	}
	defer rows.Close()

	return d.scanPostsWithSource(rows, currentUserID, total)
}

// GetUserPosts retrieves posts by a specific user.
func (d *DB) GetUserPosts(userID int64, page, perPage int) ([]*models.Post, int, error) {
	var total int
	err := d.QueryRow(
		`SELECT COUNT(*) FROM posts WHERE user_id = ? AND is_deleted = FALSE`,
		userID,
	).Scan(&total)
	if err != nil {
		return nil, 0, fmt.Errorf("counting user posts: %w", err)
	}

	rows, err := d.Query(`
		SELECT p.id, p.user_id, p.title, p.url, p.body, p.created_at, p.updated_at,
		       p.score, p.is_deleted, u.username,
		       (SELECT COUNT(*) FROM comments WHERE post_id = p.id AND is_deleted = FALSE) as comment_count
		FROM posts p
		JOIN users u ON p.user_id = u.id
		WHERE p.user_id = ? AND p.is_deleted = FALSE
		ORDER BY p.created_at DESC
		LIMIT ? OFFSET ?
	`, userID, perPage, (page-1)*perPage)
	if err != nil {
		return nil, 0, fmt.Errorf("querying user posts: %w", err)
	}
	defer rows.Close()

	return d.scanPosts(rows, nil, total)
}

// scanPosts is a helper to scan multiple posts from rows.
func (d *DB) scanPosts(rows *sql.Rows, currentUserID *int64, total int) ([]*models.Post, int, error) {
	posts := make([]*models.Post, 0, 30) // Default page size
	postIDs := make([]int64, 0, 30)

	for rows.Next() {
		post := &models.Post{}
		var postURL, body sql.NullString

		err := rows.Scan(
			&post.ID, &post.UserID, &post.Title, &postURL, &body,
			&post.CreatedAt, &post.UpdatedAt, &post.Score, &post.IsDeleted,
			&post.Username, &post.CommentCount,
		)
		if err != nil {
			return nil, 0, fmt.Errorf("scanning post: %w", err)
		}

		if postURL.Valid {
			post.URL = postURL.String
			post.Domain = extractDomain(post.URL)
		}
		if body.Valid {
			post.Body = body.String
		}

		posts = append(posts, post)
		postIDs = append(postIDs, post.ID)
	}

	// Batch load tags for all posts (fixes N+1)
	if len(postIDs) > 0 {
		tagsByPost, _ := d.GetTagsForPosts(postIDs)
		for _, post := range posts {
			post.Tags = tagsByPost[post.ID]
		}
	}

	// Batch load votes for current user (fixes N+1)
	if currentUserID != nil && len(postIDs) > 0 {
		votedPosts, _ := d.GetUserVotedPosts(*currentUserID, postIDs)
		for _, post := range posts {
			post.UserVoted = votedPosts[post.ID]
		}
	}

	// Batch load hats for posts
	if len(postIDs) > 0 {
		hatsByPost, _ := d.GetHatsForPosts(postIDs)
		for _, post := range posts {
			post.Hat = hatsByPost[post.ID]
		}
	}

	return posts, total, nil
}

// scanPostsWithSource is a helper to scan multiple posts with source info from rows.
func (d *DB) scanPostsWithSource(rows *sql.Rows, currentUserID *int64, total int) ([]*models.Post, int, error) {
	posts := make([]*models.Post, 0, 30) // Default page size
	postIDs := make([]int64, 0, 30)

	for rows.Next() {
		post := &models.Post{}
		var postURL, body, sourceType, sourceName sql.NullString
		var sourceID sql.NullInt64

		err := rows.Scan(
			&post.ID, &post.UserID, &post.Title, &postURL, &body,
			&post.CreatedAt, &post.UpdatedAt, &post.Score, &post.IsDeleted,
			&post.Username, &post.CommentCount, &sourceType, &sourceID, &sourceName,
		)
		if err != nil {
			return nil, 0, fmt.Errorf("scanning post: %w", err)
		}

		if postURL.Valid {
			post.URL = postURL.String
			post.Domain = extractDomain(post.URL)
		}
		if body.Valid {
			post.Body = body.String
		}
		if sourceType.Valid {
			post.SourceType = models.PostSource(sourceType.String)
		}
		if sourceID.Valid {
			post.SourceID = &sourceID.Int64
		}
		if sourceName.Valid {
			post.SourceName = sourceName.String
		}

		posts = append(posts, post)
		postIDs = append(postIDs, post.ID)
	}

	// Batch load tags for all posts (fixes N+1)
	if len(postIDs) > 0 {
		tagsByPost, _ := d.GetTagsForPosts(postIDs)
		for _, post := range posts {
			post.Tags = tagsByPost[post.ID]
		}
	}

	// Batch load votes for current user (fixes N+1)
	if currentUserID != nil && len(postIDs) > 0 {
		votedPosts, _ := d.GetUserVotedPosts(*currentUserID, postIDs)
		for _, post := range posts {
			post.UserVoted = votedPosts[post.ID]
		}
	}

	// Batch load hats for posts
	if len(postIDs) > 0 {
		hatsByPost, _ := d.GetHatsForPosts(postIDs)
		for _, post := range posts {
			post.Hat = hatsByPost[post.ID]
		}
	}

	return posts, total, nil
}

// UpdatePost updates an existing post and saves a revision.
func (d *DB) UpdatePost(id int64, title, body string) error {
	_, err := d.Exec(
		`UPDATE posts SET title = ?, body = ?, updated_at = CURRENT_TIMESTAMP WHERE id = ?`,
		title, body, id,
	)
	return err
}

// UpdatePostWithRevision updates a post and saves the old content as a revision.
func (d *DB) UpdatePostWithRevision(postID, editorID int64, oldTitle, oldBody, newTitle, newBody string) error {
	tx, err := d.Begin()
	if err != nil {
		return fmt.Errorf("beginning transaction: %w", err)
	}
	defer tx.Rollback()

	// Save old content as a revision
	_, err = tx.Exec(
		`INSERT INTO post_revisions (post_id, user_id, title, body) VALUES (?, ?, ?, ?)`,
		postID, editorID, oldTitle, oldBody,
	)
	if err != nil {
		return fmt.Errorf("saving revision: %w", err)
	}

	// Update the post
	_, err = tx.Exec(
		`UPDATE posts SET title = ?, body = ?, updated_at = CURRENT_TIMESTAMP WHERE id = ?`,
		newTitle, newBody, postID,
	)
	if err != nil {
		return fmt.Errorf("updating post: %w", err)
	}

	return tx.Commit()
}

// GetPostRevisions retrieves all revisions for a post.
func (d *DB) GetPostRevisions(postID int64) ([]models.PostRevision, error) {
	rows, err := d.Query(`
		SELECT r.id, r.post_id, r.user_id, r.title, r.body, r.created_at, u.username
		FROM post_revisions r
		JOIN users u ON r.user_id = u.id
		WHERE r.post_id = ?
		ORDER BY r.created_at DESC
	`, postID)
	if err != nil {
		return nil, fmt.Errorf("querying revisions: %w", err)
	}
	defer rows.Close()

	var revisions []models.PostRevision
	for rows.Next() {
		var r models.PostRevision
		var body sql.NullString
		if err := rows.Scan(&r.ID, &r.PostID, &r.UserID, &r.Title, &body, &r.CreatedAt, &r.Username); err != nil {
			return nil, fmt.Errorf("scanning revision: %w", err)
		}
		if body.Valid {
			r.Body = body.String
		}
		revisions = append(revisions, r)
	}

	return revisions, nil
}

// GetPostRevisionCount returns the number of revisions for a post.
func (d *DB) GetPostRevisionCount(postID int64) int {
	var count int
	d.QueryRow(`SELECT COUNT(*) FROM post_revisions WHERE post_id = ?`, postID).Scan(&count)
	return count
}

// DeletePost soft-deletes a post.
func (d *DB) DeletePost(id int64) error {
	_, err := d.Exec(`UPDATE posts SET is_deleted = TRUE WHERE id = ?`, id)
	return err
}

// GetTagsForPosts batch loads tags for multiple posts.
// Returns a map of postID -> tags. This eliminates N+1 queries.
func (d *DB) GetTagsForPosts(postIDs []int64) (map[int64][]models.Tag, error) {
	if len(postIDs) == 0 {
		return nil, nil
	}

	// Build placeholders for IN clause
	placeholders := make([]string, len(postIDs))
	args := make([]interface{}, len(postIDs))
	for i, id := range postIDs {
		placeholders[i] = "?"
		args[i] = id
	}

	query := fmt.Sprintf(`
		SELECT pt.post_id, t.id, t.name, t.description, t.creator_id, t.created_at, t.is_active
		FROM tags t
		JOIN post_tags pt ON t.id = pt.tag_id
		WHERE pt.post_id IN (%s)
		ORDER BY t.name
	`, strings.Join(placeholders, ","))

	rows, err := d.Query(query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	result := make(map[int64][]models.Tag)
	for rows.Next() {
		var postID int64
		var tag models.Tag
		var creatorID sql.NullInt64
		var createdAt sql.NullTime
		if err := rows.Scan(&postID, &tag.ID, &tag.Name, &tag.Description, &creatorID, &createdAt, &tag.IsActive); err != nil {
			return nil, err
		}
		if creatorID.Valid {
			tag.CreatorID = &creatorID.Int64
		}
		if createdAt.Valid {
			tag.CreatedAt = createdAt.Time
		}
		result[postID] = append(result[postID], tag)
	}

	return result, nil
}

// GetUserVotedPosts batch checks which posts a user has voted on.
// Returns a map of postID -> voted. This eliminates N+1 queries.
func (d *DB) GetUserVotedPosts(userID int64, postIDs []int64) (map[int64]bool, error) {
	if len(postIDs) == 0 {
		return nil, nil
	}

	// Build placeholders for IN clause
	placeholders := make([]string, len(postIDs))
	args := make([]interface{}, len(postIDs)+1)
	args[0] = userID
	for i, id := range postIDs {
		placeholders[i] = "?"
		args[i+1] = id
	}

	query := fmt.Sprintf(`
		SELECT post_id FROM votes
		WHERE user_id = ? AND post_id IN (%s)
	`, strings.Join(placeholders, ","))

	rows, err := d.Query(query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	result := make(map[int64]bool)
	for rows.Next() {
		var postID int64
		if err := rows.Scan(&postID); err != nil {
			return nil, err
		}
		result[postID] = true
	}

	return result, nil
}

// GetHatsForPosts batch loads hats for posts that have them.
// Returns a map of postID -> Hat. This eliminates N+1 queries.
func (d *DB) GetHatsForPosts(postIDs []int64) (map[int64]*models.Hat, error) {
	if len(postIDs) == 0 {
		return nil, nil
	}

	// Build placeholders for IN clause
	placeholders := make([]string, len(postIDs))
	args := make([]interface{}, len(postIDs))
	for i, id := range postIDs {
		placeholders[i] = "?"
		args[i] = id
	}

	query := fmt.Sprintf(`
		SELECT p.id, h.id, h.user_id, h.name, h.organization, h.link, h.granted_by, h.granted_at, h.is_active
		FROM posts p
		JOIN hats h ON p.hat_id = h.id
		WHERE p.id IN (%s) AND p.hat_id IS NOT NULL
	`, strings.Join(placeholders, ","))

	rows, err := d.Query(query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	result := make(map[int64]*models.Hat)
	for rows.Next() {
		var postID int64
		hat := &models.Hat{}
		var grantedBy sql.NullInt64
		var link sql.NullString

		if err := rows.Scan(&postID, &hat.ID, &hat.UserID, &hat.Name, &hat.Organization, &link, &grantedBy, &hat.GrantedAt, &hat.IsActive); err != nil {
			return nil, err
		}
		if grantedBy.Valid {
			hat.GrantedBy = &grantedBy.Int64
		}
		if link.Valid {
			hat.Link = link.String
		}
		result[postID] = hat
	}

	return result, nil
}

// GetPostTags retrieves all tags for a post.
func (d *DB) GetPostTags(postID int64) ([]models.Tag, error) {
	rows, err := d.Query(`
		SELECT t.id, t.name, t.description, t.creator_id, t.created_at, t.is_active
		FROM tags t
		JOIN post_tags pt ON t.id = pt.tag_id
		WHERE pt.post_id = ?
	`, postID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var tags []models.Tag
	for rows.Next() {
		var tag models.Tag
		var creatorID sql.NullInt64
		var createdAt sql.NullTime
		if err := rows.Scan(&tag.ID, &tag.Name, &tag.Description, &creatorID, &createdAt, &tag.IsActive); err != nil {
			return nil, err
		}
		if creatorID.Valid {
			tag.CreatorID = &creatorID.Int64
		}
		if createdAt.Valid {
			tag.CreatedAt = createdAt.Time
		}
		tags = append(tags, tag)
	}

	return tags, nil
}

// GetAllTags retrieves all active tags.
func (d *DB) GetAllTags() ([]models.Tag, error) {
	rows, err := d.Query(`
		SELECT id, name, description, creator_id, created_at, is_active
		FROM tags WHERE is_active = TRUE ORDER BY name
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var tags []models.Tag
	for rows.Next() {
		var tag models.Tag
		var creatorID sql.NullInt64
		var createdAt sql.NullTime
		if err := rows.Scan(&tag.ID, &tag.Name, &tag.Description, &creatorID, &createdAt, &tag.IsActive); err != nil {
			return nil, err
		}
		if creatorID.Valid {
			tag.CreatorID = &creatorID.Int64
		}
		if createdAt.Valid {
			tag.CreatedAt = createdAt.Time
		}
		tags = append(tags, tag)
	}

	return tags, nil
}

// TagWithCount holds a tag and its post count (including hierarchical children).
type TagWithCount struct {
	models.Tag
	PostCount       int
	SubtagCount     int
	CreatorUsername string
}

// GetAllTagsWithCounts retrieves all active tags with their post counts.
// PostCount includes posts from hierarchical sub-tags (e.g., "programming" includes "programming::go").
func (d *DB) GetAllTagsWithCounts() ([]TagWithCount, error) {
	// First, get all post counts per tag in a single query
	postCounts := make(map[string]int)
	countRows, err := d.Query(`
		SELECT t.name, COUNT(DISTINCT pt.post_id) as post_count
		FROM tags t
		JOIN post_tags pt ON pt.tag_id = t.id
		JOIN posts p ON pt.post_id = p.id
		WHERE p.is_deleted = FALSE AND t.is_active = TRUE
		GROUP BY t.name
	`)
	if err != nil {
		return nil, err
	}
	defer countRows.Close()

	for countRows.Next() {
		var name string
		var count int
		if err := countRows.Scan(&name, &count); err != nil {
			return nil, err
		}
		postCounts[name] = count
	}

	// Now get all tags
	rows, err := d.Query(`
		SELECT t.id, t.name, t.description, t.creator_id, t.created_at, t.is_active,
		       COALESCE(u.username, '') as creator_username
		FROM tags t
		LEFT JOIN users u ON t.creator_id = u.id
		WHERE t.is_active = TRUE
		ORDER BY t.name
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var tags []TagWithCount
	for rows.Next() {
		var t TagWithCount
		var creatorID sql.NullInt64
		var createdAt sql.NullTime
		if err := rows.Scan(&t.ID, &t.Name, &t.Description, &creatorID, &createdAt, &t.IsActive, &t.CreatorUsername); err != nil {
			return nil, err
		}
		if creatorID.Valid {
			t.CreatorID = &creatorID.Int64
		}
		if createdAt.Valid {
			t.CreatedAt = createdAt.Time
		}
		tags = append(tags, t)
	}

	// Calculate hierarchical counts: for each tag, sum counts of itself and all subtags
	for i := range tags {
		count := postCounts[tags[i].Name]
		prefix := tags[i].Name + "::"
		for name, c := range postCounts {
			if strings.HasPrefix(name, prefix) {
				count += c
			}
		}
		tags[i].PostCount = count
	}

	return tags, nil
}

// GetTagByName retrieves a tag by its name.
func (d *DB) GetTagByName(name string) (*models.Tag, error) {
	var tag models.Tag
	var creatorID sql.NullInt64
	var createdAt sql.NullTime
	err := d.QueryRow(`
		SELECT id, name, description, creator_id, created_at, is_active
		FROM tags WHERE name = ? AND is_active = TRUE
	`, name).Scan(&tag.ID, &tag.Name, &tag.Description, &creatorID, &createdAt, &tag.IsActive)
	if err != nil {
		return nil, err
	}
	if creatorID.Valid {
		tag.CreatorID = &creatorID.Int64
	}
	if createdAt.Valid {
		tag.CreatedAt = createdAt.Time
	}
	return &tag, nil
}

// GetChildTags retrieves direct child tags of a parent tag.
// e.g., for "programming", returns "programming::languages", "programming::web", etc.
func (d *DB) GetChildTags(parentName string) ([]TagWithCount, error) {
	// Pattern matches "parent::child" but not "parent::child::grandchild"
	pattern := parentName + "::%"

	rows, err := d.Query(`
		SELECT t.id, t.name, t.description, t.creator_id, t.created_at, t.is_active,
		       COALESCE(u.username, '') as creator_username,
		       (SELECT COUNT(DISTINCT pt.post_id)
		        FROM post_tags pt
		        JOIN tags t2 ON pt.tag_id = t2.id
		        JOIN posts p ON pt.post_id = p.id
		        WHERE p.is_deleted = FALSE
		          AND (t2.name = t.name OR t2.name LIKE t.name || '::%')
		       ) as post_count
		FROM tags t
		LEFT JOIN users u ON t.creator_id = u.id
		WHERE t.is_active = TRUE
		  AND t.name LIKE ?
		  AND t.name NOT LIKE ?
		ORDER BY t.name
	`, pattern, parentName+"::_%::%")
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var tags []TagWithCount
	for rows.Next() {
		var t TagWithCount
		var creatorID sql.NullInt64
		var createdAt sql.NullTime
		if err := rows.Scan(&t.ID, &t.Name, &t.Description, &creatorID, &createdAt, &t.IsActive, &t.CreatorUsername, &t.PostCount); err != nil {
			return nil, err
		}
		if creatorID.Valid {
			t.CreatorID = &creatorID.Int64
		}
		if createdAt.Valid {
			t.CreatedAt = createdAt.Time
		}
		tags = append(tags, t)
	}

	return tags, nil
}

// GetRootTags retrieves only top-level tags (no :: in name).
func (d *DB) GetRootTags() ([]TagWithCount, error) {
	rows, err := d.Query(`
		SELECT t.id, t.name, t.description, t.creator_id, t.created_at, t.is_active,
		       COALESCE(u.username, '') as creator_username,
		       (SELECT COUNT(DISTINCT pt.post_id)
		        FROM post_tags pt
		        JOIN tags t2 ON pt.tag_id = t2.id
		        JOIN posts p ON pt.post_id = p.id
		        WHERE p.is_deleted = FALSE
		          AND (t2.name = t.name OR t2.name LIKE t.name || '::%')
		       ) as post_count,
		       (SELECT COUNT(*)
		        FROM tags t3
		        WHERE t3.is_active = TRUE
		          AND t3.name LIKE t.name || '::%'
		          AND t3.name NOT LIKE t.name || '::_%::%'
		       ) as subtag_count
		FROM tags t
		LEFT JOIN users u ON t.creator_id = u.id
		WHERE t.is_active = TRUE
		  AND t.name NOT LIKE '%::%'
		ORDER BY t.name
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var tags []TagWithCount
	for rows.Next() {
		var t TagWithCount
		var creatorID sql.NullInt64
		var createdAt sql.NullTime
		if err := rows.Scan(&t.ID, &t.Name, &t.Description, &creatorID, &createdAt, &t.IsActive, &t.CreatorUsername, &t.PostCount, &t.SubtagCount); err != nil {
			return nil, err
		}
		if creatorID.Valid {
			t.CreatorID = &creatorID.Int64
		}
		if createdAt.Valid {
			t.CreatedAt = createdAt.Time
		}
		tags = append(tags, t)
	}

	return tags, nil
}

// extractDomain extracts the domain from a URL for display.
func extractDomain(rawURL string) string {
	if rawURL == "" {
		return ""
	}

	parsed, err := url.Parse(rawURL)
	if err != nil {
		return ""
	}

	host := parsed.Host
	// Remove www. prefix
	host = strings.TrimPrefix(host, "www.")
	return host
}

// GetTopPosts retrieves posts by score within a time period.
// hoursAgo of 0 means all time.
func (d *DB) GetTopPosts(page, perPage int, hoursAgo int, currentUserID *int64, isAdmin bool) ([]*models.Post, int, error) {
	var whereClause string
	var args []interface{}

	if hoursAgo > 0 {
		whereClause = "WHERE p.is_deleted = FALSE AND p.created_at >= datetime('now', ?)"
		args = append(args, fmt.Sprintf("-%d hours", hoursAgo))
	} else {
		whereClause = "WHERE p.is_deleted = FALSE"
	}

	// Hide posts without tags unless user is author or admin
	if !isAdmin {
		if currentUserID != nil {
			whereClause += ` AND (EXISTS (SELECT 1 FROM post_tags WHERE post_id = p.id) OR p.user_id = ?)`
			args = append(args, *currentUserID)
		} else {
			whereClause += ` AND EXISTS (SELECT 1 FROM post_tags WHERE post_id = p.id)`
		}
	}

	var total int
	countQuery := fmt.Sprintf(`SELECT COUNT(*) FROM posts p %s`, whereClause)
	err := d.QueryRow(countQuery, args...).Scan(&total)
	if err != nil {
		return nil, 0, fmt.Errorf("counting top posts: %w", err)
	}

	selectQuery := fmt.Sprintf(`
		SELECT p.id, p.user_id, p.title, p.url, p.body, p.created_at, p.updated_at,
		       p.score, p.is_deleted, u.username,
		       (SELECT COUNT(*) FROM comments WHERE post_id = p.id AND is_deleted = FALSE) as comment_count,
		       COALESCE(p.source_type, 'user') as source_type, p.source_id,
		       COALESCE(rf.title, rf.url) as source_name
		FROM posts p
		JOIN users u ON p.user_id = u.id
		LEFT JOIN rss_feeds rf ON p.source_type = 'rss' AND p.source_id = rf.id
		%s
		ORDER BY p.score DESC, p.created_at DESC
		LIMIT ? OFFSET ?
	`, whereClause)

	args = append(args, perPage, (page-1)*perPage)

	rows, err := d.Query(selectQuery, args...)
	if err != nil {
		return nil, 0, fmt.Errorf("querying top posts: %w", err)
	}
	defer rows.Close()

	return d.scanPostsWithSource(rows, currentUserID, total)
}

// GetHotPosts retrieves a mix of high-scoring recent posts and newest posts.
// Returns posts from last 48h sorted by score, interspersed with some newest posts.
// Posts without tags are hidden unless the user is the author or an admin.
func (d *DB) GetHotPosts(page, perPage int, currentUserID *int64, isAdmin bool) ([]*models.Post, int, error) {
	// Build tag filter clause
	tagFilterClause := ""
	var countArgs, selectArgs []interface{}
	if !isAdmin {
		if currentUserID != nil {
			tagFilterClause = ` AND (EXISTS (SELECT 1 FROM post_tags WHERE post_id = p.id) OR p.user_id = ?)`
			countArgs = append(countArgs, *currentUserID)
			selectArgs = append(selectArgs, *currentUserID)
		} else {
			tagFilterClause = ` AND EXISTS (SELECT 1 FROM post_tags WHERE post_id = p.id)`
		}
	}

	// Get high-scoring posts from last 48 hours
	var total int
	err := d.QueryRow(`
		SELECT COUNT(*) FROM posts p
		WHERE is_deleted = FALSE AND created_at >= datetime('now', '-48 hours')`+tagFilterClause, countArgs...).Scan(&total)
	if err != nil {
		return nil, 0, fmt.Errorf("counting hot posts: %w", err)
	}

	// Ranking: score * time_decay where time_decay favors recent posts
	// Using a formula: score / (hours_old + 2)^1.5
	selectArgs = append(selectArgs, perPage, (page-1)*perPage)
	rows, err := d.Query(`
		SELECT p.id, p.user_id, p.title, p.url, p.body, p.created_at, p.updated_at,
		       p.score, p.is_deleted, u.username,
		       (SELECT COUNT(*) FROM comments WHERE post_id = p.id AND is_deleted = FALSE) as comment_count,
		       COALESCE(p.source_type, 'user') as source_type, p.source_id,
		       COALESCE(rf.title, rf.url) as source_name
		FROM posts p
		JOIN users u ON p.user_id = u.id
		LEFT JOIN rss_feeds rf ON p.source_type = 'rss' AND p.source_id = rf.id
		WHERE p.is_deleted = FALSE AND p.created_at >= datetime('now', '-48 hours')`+tagFilterClause+`
		ORDER BY
			(p.score * 1.0) / POWER((CAST((julianday('now') - julianday(p.created_at)) * 24 AS REAL) + 2), 1.5) DESC,
			p.created_at DESC
		LIMIT ? OFFSET ?
	`, selectArgs...)
	if err != nil {
		return nil, 0, fmt.Errorf("querying hot posts: %w", err)
	}
	defer rows.Close()

	return d.scanPostsWithSource(rows, currentUserID, total)
}

// SearchPosts searches posts by title.
func (d *DB) SearchPosts(query string, page, perPage int, currentUserID *int64) ([]*models.Post, int, error) {
	searchPattern := "%" + query + "%"

	var total int
	err := d.QueryRow(
		`SELECT COUNT(*) FROM posts WHERE is_deleted = FALSE AND title LIKE ?`,
		searchPattern,
	).Scan(&total)
	if err != nil {
		return nil, 0, fmt.Errorf("counting search results: %w", err)
	}

	rows, err := d.Query(`
		SELECT p.id, p.user_id, p.title, p.url, p.body, p.created_at, p.updated_at,
		       p.score, p.is_deleted, u.username,
		       (SELECT COUNT(*) FROM comments WHERE post_id = p.id AND is_deleted = FALSE) as comment_count
		FROM posts p
		JOIN users u ON p.user_id = u.id
		WHERE p.is_deleted = FALSE AND p.title LIKE ?
		ORDER BY p.score DESC, p.created_at DESC
		LIMIT ? OFFSET ?
	`, searchPattern, perPage, (page-1)*perPage)
	if err != nil {
		return nil, 0, fmt.Errorf("searching posts: %w", err)
	}
	defer rows.Close()

	return d.scanPosts(rows, currentUserID, total)
}

// GetPostsByTags retrieves posts that have any of the given tags.
// Used for multi-tag RSS feeds.
func (d *DB) GetPostsByTags(tagNames map[string]bool, page, perPage int) ([]*models.Post, error) {
	if len(tagNames) == 0 {
		return nil, nil
	}

	// Build placeholders for tag names
	placeholders := make([]string, 0, len(tagNames))
	args := make([]interface{}, 0, len(tagNames))
	for name := range tagNames {
		placeholders = append(placeholders, "?")
		args = append(args, name)
	}
	tagList := strings.Join(placeholders, ", ")

	// Query for posts with any of the given tags
	query := fmt.Sprintf(`
		SELECT DISTINCT p.id, p.user_id, p.title, p.url, p.body, p.created_at, p.updated_at,
		       p.score, p.is_deleted, u.username,
		       (SELECT COUNT(*) FROM comments WHERE post_id = p.id AND is_deleted = FALSE) as comment_count
		FROM posts p
		JOIN users u ON p.user_id = u.id
		JOIN post_tags pt ON p.id = pt.post_id
		JOIN tags t ON pt.tag_id = t.id
		WHERE p.is_deleted = FALSE
		  AND t.name IN (%s)
		ORDER BY p.created_at DESC
		LIMIT ? OFFSET ?
	`, tagList)

	args = append(args, perPage, (page-1)*perPage)

	rows, err := d.Query(query, args...)
	if err != nil {
		return nil, fmt.Errorf("querying posts by tags: %w", err)
	}
	defer rows.Close()

	posts, _, err := d.scanPosts(rows, nil, 0)
	return posts, err
}
