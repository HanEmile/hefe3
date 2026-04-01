// Package db provides search functionality using SQLite FTS5.
package db

import (
	"database/sql"
	"fmt"
	"log"
	"regexp"
	"strings"

	"makhor/pkg/models"
)

// setupFTS creates the FTS5 virtual table and triggers for full-text search.
// FTS5 provides fast, relevance-ranked search with support for:
// - Phrase matching: "exact phrase"
// - Prefix matching: word*
// - Boolean operators: word1 AND word2, word1 OR word2, word1 NOT word2
// - Column-specific search: title:word, body:word
func (d *DB) setupFTS() error {
	// Check if FTS table already exists
	var name string
	err := d.QueryRow(`SELECT name FROM sqlite_master WHERE type='table' AND name='posts_fts'`).Scan(&name)
	if err == nil {
		// FTS table exists, nothing to do
		return nil
	}

	log.Println("Setting up FTS5 full-text search...")

	// Create the FTS5 virtual table
	// content='' creates a "contentless" FTS table that doesn't store original text
	// (the original is in the posts table, we just need the index)
	// tokenize='porter unicode61' enables:
	// - unicode61: proper unicode handling
	// - porter: stemming (e.g., "running" matches "run")
	_, err = d.Exec(`
		CREATE VIRTUAL TABLE posts_fts USING fts5(
			title,
			body,
			content='posts',
			content_rowid='id',
			tokenize='porter unicode61'
		)
	`)
	if err != nil {
		return fmt.Errorf("creating FTS table: %w", err)
	}

	// Create triggers to keep FTS index in sync with posts table
	// These fire automatically on INSERT, UPDATE, DELETE

	// Trigger for INSERT
	_, err = d.Exec(`
		CREATE TRIGGER IF NOT EXISTS posts_fts_insert AFTER INSERT ON posts BEGIN
			INSERT INTO posts_fts(rowid, title, body) VALUES (new.id, new.title, COALESCE(new.body, ''));
		END
	`)
	if err != nil {
		return fmt.Errorf("creating insert trigger: %w", err)
	}

	// Trigger for DELETE
	_, err = d.Exec(`
		CREATE TRIGGER IF NOT EXISTS posts_fts_delete AFTER DELETE ON posts BEGIN
			INSERT INTO posts_fts(posts_fts, rowid, title, body) VALUES('delete', old.id, old.title, COALESCE(old.body, ''));
		END
	`)
	if err != nil {
		return fmt.Errorf("creating delete trigger: %w", err)
	}

	// Trigger for UPDATE
	_, err = d.Exec(`
		CREATE TRIGGER IF NOT EXISTS posts_fts_update AFTER UPDATE ON posts BEGIN
			INSERT INTO posts_fts(posts_fts, rowid, title, body) VALUES('delete', old.id, old.title, COALESCE(old.body, ''));
			INSERT INTO posts_fts(rowid, title, body) VALUES (new.id, new.title, COALESCE(new.body, ''));
		END
	`)
	if err != nil {
		return fmt.Errorf("creating update trigger: %w", err)
	}

	// Populate FTS index with existing posts
	_, err = d.Exec(`
		INSERT INTO posts_fts(rowid, title, body)
		SELECT id, title, COALESCE(body, '') FROM posts WHERE is_deleted = FALSE
	`)
	if err != nil {
		return fmt.Errorf("populating FTS index: %w", err)
	}

	log.Println("FTS5 search setup complete")
	return nil
}

// sanitizeFTSQuery converts a user query into a safe FTS5 query.
// It handles special characters and provides sensible defaults.
func sanitizeFTSQuery(query string) string {
	query = strings.TrimSpace(query)
	if query == "" {
		return ""
	}

	// If the query contains FTS5 operators, validate it
	// Otherwise, convert to a simple search
	hasOperators := strings.ContainsAny(query, `"*()`) ||
		strings.Contains(query, " AND ") ||
		strings.Contains(query, " OR ") ||
		strings.Contains(query, " NOT ") ||
		strings.Contains(query, "title:") ||
		strings.Contains(query, "body:")

	if hasOperators {
		// User is using advanced syntax, sanitize but preserve operators
		// Remove any dangerous characters
		query = strings.ReplaceAll(query, ";", "")
		query = strings.ReplaceAll(query, "--", "")
		return query
	}

	// Simple query: split into words and search all of them
	words := strings.Fields(query)
	if len(words) == 0 {
		return ""
	}

	// Escape special FTS characters in each word
	var escaped []string
	specialChars := regexp.MustCompile(`(["\*\(\)])`)
	for _, word := range words {
		word = specialChars.ReplaceAllString(word, "")
		if word != "" {
			// Add prefix matching for partial word search
			escaped = append(escaped, word+"*")
		}
	}

	if len(escaped) == 0 {
		return ""
	}

	// Join with implicit AND (all words must match)
	return strings.Join(escaped, " ")
}

// SearchPostsFTS performs full-text search using FTS5.
// Returns posts ranked by relevance using BM25 algorithm.
// Supports:
//   - Simple word search: "golang tutorial"
//   - Phrase search: "\"exact phrase\""
//   - Prefix search: "prog*" (automatic for simple queries)
//   - Boolean: "go AND tutorial", "go NOT java"
//   - Column-specific: "title:golang"
func (d *DB) SearchPostsFTS(query string, page, perPage int, currentUserID *int64) ([]*models.Post, int, error) {
	ftsQuery := sanitizeFTSQuery(query)
	if ftsQuery == "" {
		return nil, 0, nil
	}

	// Count total matches
	var total int
	err := d.QueryRow(`
		SELECT COUNT(*) FROM posts_fts
		JOIN posts p ON posts_fts.rowid = p.id
		WHERE posts_fts MATCH ? AND p.is_deleted = FALSE
	`, ftsQuery).Scan(&total)
	if err != nil {
		// If FTS query fails, fall back to LIKE search
		log.Printf("FTS search failed, falling back to LIKE: %v", err)
		return d.SearchPosts(query, page, perPage, currentUserID)
	}

	if total == 0 {
		return nil, 0, nil
	}

	// Search with BM25 ranking
	// bm25() returns a negative score (more negative = better match)
	// We negate it to get positive scores for sorting
	rows, err := d.Query(`
		SELECT p.id, p.user_id, p.title, p.url, p.body, p.created_at, p.updated_at,
		       p.score, p.is_deleted, u.username,
		       (SELECT COUNT(*) FROM comments WHERE post_id = p.id AND is_deleted = FALSE) as comment_count,
		       p.source_type, p.source_id,
		       -bm25(posts_fts, 10.0, 1.0) as rank
		FROM posts_fts
		JOIN posts p ON posts_fts.rowid = p.id
		JOIN users u ON p.user_id = u.id
		WHERE posts_fts MATCH ? AND p.is_deleted = FALSE
		ORDER BY rank DESC, p.score DESC, p.created_at DESC
		LIMIT ? OFFSET ?
	`, ftsQuery, perPage, (page-1)*perPage)
	if err != nil {
		// Fall back to LIKE search on FTS query error
		log.Printf("FTS query error, falling back to LIKE: %v", err)
		return d.SearchPosts(query, page, perPage, currentUserID)
	}
	defer rows.Close()

	return d.scanSearchResults(rows, currentUserID, total)
}

// scanSearchResults scans rows from a search query into Post models.
func (d *DB) scanSearchResults(rows *sql.Rows, currentUserID *int64, total int) ([]*models.Post, int, error) {
	var posts []*models.Post
	var postIDs []int64

	for rows.Next() {
		post := &models.Post{}
		var rank float64
		var sourceType *string
		var sourceID *int64

		if err := rows.Scan(
			&post.ID, &post.UserID, &post.Title, &post.URL, &post.Body,
			&post.CreatedAt, &post.UpdatedAt, &post.Score, &post.IsDeleted,
			&post.Username, &post.CommentCount, &sourceType, &sourceID, &rank,
		); err != nil {
			return nil, 0, fmt.Errorf("scanning search result: %w", err)
		}

		if sourceType != nil {
			post.SourceType = models.PostSource(*sourceType)
		}
		if sourceID != nil {
			post.SourceID = sourceID
		}

		// Extract domain from URL
		if post.URL != "" {
			post.Domain = extractDomain(post.URL)
		}

		posts = append(posts, post)
		postIDs = append(postIDs, post.ID)
	}

	if err := rows.Err(); err != nil {
		return nil, 0, fmt.Errorf("iterating search results: %w", err)
	}

	// Batch load tags and votes
	if len(postIDs) > 0 {
		tagMap, err := d.GetTagsForPosts(postIDs)
		if err != nil {
			log.Printf("Error loading tags for search results: %v", err)
		} else {
			for _, post := range posts {
				if tags, ok := tagMap[post.ID]; ok {
					post.Tags = tags
				}
			}
		}

		// Load user votes if logged in
		if currentUserID != nil {
			voteMap, err := d.GetUserVotedPosts(*currentUserID, postIDs)
			if err != nil {
				log.Printf("Error loading votes for search results: %v", err)
			} else {
				for _, post := range posts {
					post.UserVoted = voteMap[post.ID]
				}
			}
		}
	}

	return posts, total, nil
}

// RebuildFTSIndex rebuilds the FTS index from scratch.
// This is useful if the index gets corrupted or out of sync.
func (d *DB) RebuildFTSIndex() error {
	log.Println("Rebuilding FTS index...")

	// Drop and recreate FTS table and triggers
	_, err := d.Exec(`DROP TABLE IF EXISTS posts_fts`)
	if err != nil {
		return fmt.Errorf("dropping FTS table: %w", err)
	}

	_, err = d.Exec(`DROP TRIGGER IF EXISTS posts_fts_insert`)
	if err != nil {
		return fmt.Errorf("dropping insert trigger: %w", err)
	}

	_, err = d.Exec(`DROP TRIGGER IF EXISTS posts_fts_delete`)
	if err != nil {
		return fmt.Errorf("dropping delete trigger: %w", err)
	}

	_, err = d.Exec(`DROP TRIGGER IF EXISTS posts_fts_update`)
	if err != nil {
		return fmt.Errorf("dropping update trigger: %w", err)
	}

	// Recreate FTS
	return d.setupFTS()
}

// SearchSuggestions returns search suggestions based on a partial query.
// Useful for autocomplete functionality.
func (d *DB) SearchSuggestions(prefix string, limit int) ([]string, error) {
	if len(prefix) < 2 {
		return nil, nil
	}

	ftsQuery := sanitizeFTSQuery(prefix)
	if ftsQuery == "" {
		return nil, nil
	}

	rows, err := d.Query(`
		SELECT DISTINCT p.title
		FROM posts_fts
		JOIN posts p ON posts_fts.rowid = p.id
		WHERE posts_fts MATCH ? AND p.is_deleted = FALSE
		ORDER BY p.score DESC
		LIMIT ?
	`, ftsQuery, limit)
	if err != nil {
		return nil, fmt.Errorf("getting suggestions: %w", err)
	}
	defer rows.Close()

	var suggestions []string
	for rows.Next() {
		var title string
		if err := rows.Scan(&title); err != nil {
			continue
		}
		suggestions = append(suggestions, title)
	}

	return suggestions, nil
}

// FTSHealthCheck verifies the FTS index is working correctly.
func (d *DB) FTSHealthCheck() error {
	// Check if FTS table exists
	var name string
	err := d.QueryRow(`SELECT name FROM sqlite_master WHERE type='table' AND name='posts_fts'`).Scan(&name)
	if err != nil {
		return fmt.Errorf("FTS table not found: %w", err)
	}

	// Check if triggers exist
	triggers := []string{"posts_fts_insert", "posts_fts_delete", "posts_fts_update"}
	for _, trigger := range triggers {
		err := d.QueryRow(`SELECT name FROM sqlite_master WHERE type='trigger' AND name=?`, trigger).Scan(&name)
		if err != nil {
			return fmt.Errorf("trigger %s not found: %w", trigger, err)
		}
	}

	// Check row counts match
	var postCount, ftsCount int
	d.QueryRow(`SELECT COUNT(*) FROM posts WHERE is_deleted = FALSE`).Scan(&postCount)
	d.QueryRow(`SELECT COUNT(*) FROM posts_fts`).Scan(&ftsCount)

	if postCount != ftsCount {
		return fmt.Errorf("row count mismatch: posts=%d, fts=%d", postCount, ftsCount)
	}

	return nil
}
