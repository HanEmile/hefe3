// Package db provides SQLite database operations for the link aggregator.
// It handles all database initialization, migrations, and low-level queries.
package db

import (
	"database/sql"
	"errors"
	"fmt"
	"log"
	"time"

	_ "modernc.org/sqlite" // Pure Go SQLite driver (no CGO)
)

// Common error types for database operations.
var (
	ErrPostNotFound       = errors.New("post not found")
	ErrCommentNotFound    = errors.New("comment not found")
	ErrTagNotFound        = errors.New("tag not found")
	ErrHatNotFound        = errors.New("hat not found")
	ErrCollectionNotFound = errors.New("collection not found")
)

// DB wraps the sql.DB connection with application-specific methods.
type DB struct {
	*sql.DB
}

// New creates a new database connection and initializes the schema.
func New(path string) (*DB, error) {
	db, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, fmt.Errorf("opening database: %w", err)
	}

	// Configure connection pool for optimal performance
	db.SetMaxOpenConns(25)              // Limit concurrent connections
	db.SetMaxIdleConns(5)               // Keep some connections alive
	db.SetConnMaxLifetime(5 * time.Minute) // Recycle stale connections

	// Enable foreign keys and WAL mode for better performance
	_, err = db.Exec(`
		PRAGMA foreign_keys = ON;
		PRAGMA journal_mode = WAL;
		PRAGMA busy_timeout = 5000;
	`)
	if err != nil {
		return nil, fmt.Errorf("setting pragmas: %w", err)
	}

	d := &DB{db}
	if err := d.migrate(); err != nil {
		return nil, fmt.Errorf("migrating: %w", err)
	}

	// Seed existing tags with root user as creator
	if err := d.SeedTagCreators(); err != nil {
		log.Printf("Warning: could not seed tag creators: %v", err)
	}

	// Seed default RSS feeds for tags
	if err := d.SeedDefaultFeeds(); err != nil {
		log.Printf("Warning: could not seed default feeds: %v", err)
	}

	return d, nil
}

// migrate runs all database migrations.
func (d *DB) migrate() error {
	schema := `
	-- Users table: stores all user accounts
	CREATE TABLE IF NOT EXISTS users (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		username TEXT UNIQUE NOT NULL,
		email TEXT UNIQUE NOT NULL,
		created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
		invited_by INTEGER REFERENCES users(id),
		is_admin BOOLEAN DEFAULT FALSE,
		is_banned BOOLEAN DEFAULT FALSE,
		about TEXT DEFAULT '',
		avatar BLOB,           -- Profile picture stored as binary
		avatar_type TEXT,      -- MIME type of avatar (image/png, image/jpeg, etc.)
		ban_reason TEXT DEFAULT '',
		ban_expires_at DATETIME,
		banned_by INTEGER REFERENCES users(id)
	);

	-- Login tokens: email-based passwordless authentication
	-- Tokens are single-use and expire after 15 minutes
	CREATE TABLE IF NOT EXISTS login_tokens (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		email TEXT NOT NULL,
		token TEXT UNIQUE NOT NULL,
		created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
		expires_at DATETIME NOT NULL,
		used BOOLEAN DEFAULT FALSE
	);

	-- Email change tokens: for changing user email with verification
	CREATE TABLE IF NOT EXISTS email_change_tokens (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		user_id INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
		new_email TEXT NOT NULL,
		token TEXT UNIQUE NOT NULL,
		created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
		expires_at DATETIME NOT NULL,
		used BOOLEAN DEFAULT FALSE
	);

	-- Sessions: tracks active user sessions
	CREATE TABLE IF NOT EXISTS sessions (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		user_id INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
		token TEXT UNIQUE NOT NULL,
		created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
		expires_at DATETIME NOT NULL
	);

	-- Invites: invitation codes for new users
	CREATE TABLE IF NOT EXISTS invites (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		code TEXT UNIQUE NOT NULL,
		created_by INTEGER NOT NULL REFERENCES users(id),
		created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
		used_by INTEGER REFERENCES users(id),
		used_at DATETIME,
		note TEXT DEFAULT ''
	);

	-- Tags: hierarchical categorization for posts
	-- Uses :: as namespace separator (e.g., "computer::security::linux")
	CREATE TABLE IF NOT EXISTS tags (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		name TEXT UNIQUE NOT NULL,
		description TEXT DEFAULT '',
		creator_id INTEGER REFERENCES users(id),
		created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
		is_active BOOLEAN DEFAULT TRUE
	);

	-- Tag admins: users who can moderate a specific tag
	CREATE TABLE IF NOT EXISTS tag_admins (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		tag_id INTEGER NOT NULL REFERENCES tags(id) ON DELETE CASCADE,
		user_id INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
		granted_by INTEGER REFERENCES users(id),
		granted_at DATETIME DEFAULT CURRENT_TIMESTAMP,
		UNIQUE(tag_id, user_id)
	);

	-- Moderation log: public record of moderation actions
	CREATE TABLE IF NOT EXISTS moderation_log (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		moderator_id INTEGER NOT NULL REFERENCES users(id),
		action TEXT NOT NULL,           -- 'ban', 'unban', 'delete_post', 'delete_comment', 'edit_post', 'edit_comment'
		target_user_id INTEGER REFERENCES users(id),
		target_post_id INTEGER REFERENCES posts(id),
		target_comment_id INTEGER REFERENCES comments(id),
		reason TEXT,
		created_at DATETIME DEFAULT CURRENT_TIMESTAMP
	);

	-- Posts: the main content - links or text posts
	CREATE TABLE IF NOT EXISTS posts (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		user_id INTEGER NOT NULL REFERENCES users(id),
		title TEXT NOT NULL,
		url TEXT,                    -- NULL for text-only posts
		body TEXT,                   -- Text content or description
		created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
		updated_at DATETIME DEFAULT CURRENT_TIMESTAMP,
		score INTEGER DEFAULT 0,
		is_deleted BOOLEAN DEFAULT FALSE,
		source_type TEXT DEFAULT 'user',      -- 'user', 'rss', 'bot', 'api'
		source_id INTEGER,                     -- References rss_feeds.id, bot_id, etc.
		hat_id INTEGER REFERENCES hats(id)     -- Optional hat worn when posting
	);

	-- Post tags: many-to-many relationship between posts and tags
	CREATE TABLE IF NOT EXISTS post_tags (
		post_id INTEGER NOT NULL REFERENCES posts(id) ON DELETE CASCADE,
		tag_id INTEGER NOT NULL REFERENCES tags(id) ON DELETE CASCADE,
		PRIMARY KEY (post_id, tag_id)
	);

	-- Comments: threaded comments on posts
	CREATE TABLE IF NOT EXISTS comments (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		post_id INTEGER NOT NULL REFERENCES posts(id) ON DELETE CASCADE,
		user_id INTEGER NOT NULL REFERENCES users(id),
		parent_id INTEGER REFERENCES comments(id),  -- NULL for top-level comments
		body TEXT NOT NULL,
		created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
		updated_at DATETIME DEFAULT CURRENT_TIMESTAMP,
		score INTEGER DEFAULT 0,
		is_deleted BOOLEAN DEFAULT FALSE,
		is_blurred BOOLEAN DEFAULT FALSE,  -- Blurred comments appear at bottom
		hat_id INTEGER REFERENCES hats(id)  -- Optional hat worn when commenting
	);

	-- Hats: verified identities for speaking on behalf of organizations
	CREATE TABLE IF NOT EXISTS hats (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		user_id INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
		name TEXT NOT NULL,           -- e.g., "Go Team Member"
		organization TEXT NOT NULL,   -- e.g., "Google"
		link TEXT,                    -- Verification link
		granted_by INTEGER REFERENCES users(id),
		granted_at DATETIME DEFAULT CURRENT_TIMESTAMP,
		is_active BOOLEAN DEFAULT TRUE,
		UNIQUE(user_id, name, organization)
	);

	-- Votes: upvotes on posts and comments
	CREATE TABLE IF NOT EXISTS votes (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		user_id INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
		post_id INTEGER REFERENCES posts(id) ON DELETE CASCADE,
		comment_id INTEGER REFERENCES comments(id) ON DELETE CASCADE,
		value INTEGER NOT NULL DEFAULT 1,  -- 1 for upvote
		created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
		UNIQUE(user_id, post_id),
		UNIQUE(user_id, comment_id),
		CHECK ((post_id IS NOT NULL AND comment_id IS NULL) OR
		       (post_id IS NULL AND comment_id IS NOT NULL))
	);

	-- Action log: full audit trail of all actions
	CREATE TABLE IF NOT EXISTS action_log (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		user_id INTEGER REFERENCES users(id),
		action TEXT NOT NULL,         -- e.g., 'post_create', 'comment_create', 'vote'
		target_type TEXT,             -- 'post', 'comment', 'user', etc.
		target_id INTEGER,
		details TEXT,                 -- JSON or text with additional details
		ip_address TEXT,
		created_at DATETIME DEFAULT CURRENT_TIMESTAMP
	);

	-- Saved posts/comments for users
	CREATE TABLE IF NOT EXISTS saved_items (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		user_id INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
		post_id INTEGER REFERENCES posts(id) ON DELETE CASCADE,
		comment_id INTEGER REFERENCES comments(id) ON DELETE CASCADE,
		created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
		UNIQUE(user_id, post_id),
		UNIQUE(user_id, comment_id)
	);

	-- RSS feeds for automatic polling
	CREATE TABLE IF NOT EXISTS rss_feeds (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		url TEXT UNIQUE NOT NULL,
		title TEXT DEFAULT '',
		tag_id INTEGER NOT NULL REFERENCES tags(id),
		created_by INTEGER REFERENCES users(id),   -- User who added the feed
		interval_minutes INTEGER DEFAULT 60,
		last_polled DATETIME,
		last_error TEXT,
		is_active BOOLEAN DEFAULT TRUE,
		poll_on_view BOOLEAN DEFAULT FALSE,        -- Only poll when tag is viewed
		auto_approve BOOLEAN DEFAULT TRUE,         -- Auto-publish or require approval
		max_items_per_poll INTEGER DEFAULT 10,     -- Limit items per poll
		created_at DATETIME DEFAULT CURRENT_TIMESTAMP
	);

	-- Tracks which RSS items have been imported
	CREATE TABLE IF NOT EXISTS rss_items (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		feed_id INTEGER NOT NULL REFERENCES rss_feeds(id) ON DELETE CASCADE,
		guid TEXT NOT NULL,
		post_id INTEGER REFERENCES posts(id),
		imported_at DATETIME DEFAULT CURRENT_TIMESTAMP,
		UNIQUE(feed_id, guid)
	);

	-- Tracks RSS feed poll history for statistics
	CREATE TABLE IF NOT EXISTS rss_poll_log (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		feed_id INTEGER NOT NULL REFERENCES rss_feeds(id) ON DELETE CASCADE,
		polled_at DATETIME DEFAULT CURRENT_TIMESTAMP,
		success BOOLEAN NOT NULL,
		http_status INTEGER,
		items_found INTEGER DEFAULT 0,
		items_imported INTEGER DEFAULT 0,
		error_message TEXT,
		duration_ms INTEGER
	);
	CREATE INDEX IF NOT EXISTS idx_rss_poll_log_feed_time ON rss_poll_log(feed_id, polled_at DESC);

	-- Post revisions: tracks edit history for posts
	CREATE TABLE IF NOT EXISTS post_revisions (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		post_id INTEGER NOT NULL REFERENCES posts(id) ON DELETE CASCADE,
		user_id INTEGER NOT NULL REFERENCES users(id),
		title TEXT NOT NULL,
		body TEXT,
		created_at DATETIME DEFAULT CURRENT_TIMESTAMP
	);

	-- Collections: user-created groupings of tags for combined viewing
	CREATE TABLE IF NOT EXISTS collections (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		user_id INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
		name TEXT NOT NULL,
		description TEXT DEFAULT '',
		created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
		UNIQUE(user_id, name)
	);

	-- Collection tags: many-to-many relationship between collections and tags
	CREATE TABLE IF NOT EXISTS collection_tags (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		collection_id INTEGER NOT NULL REFERENCES collections(id) ON DELETE CASCADE,
		tag_id INTEGER NOT NULL REFERENCES tags(id) ON DELETE CASCADE,
		UNIQUE(collection_id, tag_id)
	);

	-- Indexes for performance
	CREATE INDEX IF NOT EXISTS idx_posts_created_at ON posts(created_at DESC);
	CREATE INDEX IF NOT EXISTS idx_posts_score ON posts(score DESC);
	CREATE INDEX IF NOT EXISTS idx_posts_user_id ON posts(user_id);
	CREATE INDEX IF NOT EXISTS idx_comments_post_id ON comments(post_id);
	CREATE INDEX IF NOT EXISTS idx_comments_user_id ON comments(user_id);
	CREATE INDEX IF NOT EXISTS idx_action_log_user_id ON action_log(user_id);
	CREATE INDEX IF NOT EXISTS idx_action_log_created_at ON action_log(created_at DESC);
	CREATE INDEX IF NOT EXISTS idx_sessions_token ON sessions(token);
	CREATE INDEX IF NOT EXISTS idx_login_tokens_token ON login_tokens(token);
	CREATE INDEX IF NOT EXISTS idx_email_change_tokens_token ON email_change_tokens(token);
	CREATE INDEX IF NOT EXISTS idx_invites_code ON invites(code);
	CREATE INDEX IF NOT EXISTS idx_moderation_log_created_at ON moderation_log(created_at DESC);
	CREATE INDEX IF NOT EXISTS idx_tag_admins_tag_id ON tag_admins(tag_id);
	CREATE INDEX IF NOT EXISTS idx_tag_admins_user_id ON tag_admins(user_id);
	CREATE INDEX IF NOT EXISTS idx_tags_creator_id ON tags(creator_id);
	CREATE INDEX IF NOT EXISTS idx_rss_feeds_tag_id ON rss_feeds(tag_id);
	CREATE INDEX IF NOT EXISTS idx_rss_feeds_last_polled ON rss_feeds(last_polled);
	CREATE INDEX IF NOT EXISTS idx_rss_items_feed_id ON rss_items(feed_id);

	-- Additional indexes for vote lookups and post filtering
	CREATE INDEX IF NOT EXISTS idx_votes_user_post ON votes(user_id, post_id);
	CREATE INDEX IF NOT EXISTS idx_votes_user_comment ON votes(user_id, comment_id);
	CREATE INDEX IF NOT EXISTS idx_post_tags_post_id ON post_tags(post_id);
	CREATE INDEX IF NOT EXISTS idx_post_tags_tag_id ON post_tags(tag_id);
	CREATE INDEX IF NOT EXISTS idx_comments_parent_id ON comments(parent_id);
	CREATE INDEX IF NOT EXISTS idx_users_invited_by ON users(invited_by);
	CREATE INDEX IF NOT EXISTS idx_post_revisions_post_id ON post_revisions(post_id);
	CREATE INDEX IF NOT EXISTS idx_collections_user_id ON collections(user_id);
	CREATE INDEX IF NOT EXISTS idx_collection_tags_collection_id ON collection_tags(collection_id);
	CREATE INDEX IF NOT EXISTS idx_collection_tags_tag_id ON collection_tags(tag_id);

	-- Performance indexes for common query patterns
	CREATE INDEX IF NOT EXISTS idx_posts_is_deleted ON posts(is_deleted);
	CREATE INDEX IF NOT EXISTS idx_posts_user_deleted_created ON posts(user_id, is_deleted, created_at DESC);
	CREATE INDEX IF NOT EXISTS idx_comments_post_deleted ON comments(post_id, is_deleted);
	CREATE INDEX IF NOT EXISTS idx_rss_feeds_is_active ON rss_feeds(is_active);
	CREATE INDEX IF NOT EXISTS idx_sessions_token_expires ON sessions(token, expires_at);
	CREATE INDEX IF NOT EXISTS idx_login_tokens_lookup ON login_tokens(token, used, expires_at);
	`

	_, err := d.Exec(schema)
	if err != nil {
		return fmt.Errorf("executing schema: %w", err)
	}

	// Run migrations for existing databases (add columns that may not exist in older schemas)
	d.runMigrations()

	// Setup FTS5 full-text search
	if err := d.setupFTS(); err != nil {
		log.Printf("Warning: could not setup FTS: %v", err)
	}

	// Insert default tags if none exist
	var count int
	err = d.QueryRow("SELECT COUNT(*) FROM tags").Scan(&count)
	if err != nil {
		return fmt.Errorf("counting tags: %w", err)
	}

	if count == 0 {
		// Tags use :: as namespace separator for hierarchy
		// e.g., "threat-intel::malware::ransomware" allows filtering by:
		// - "threat-intel" (all threat intel posts)
		// - "threat-intel::malware" (malware subset)
		// - "threat-intel::malware::ransomware" (ransomware only)
		defaultTags := []struct{ name, desc string }{
			// Threat Intelligence - security research and threat analysis
			{"threat-intel", "Threat Intelligence"},
			{"threat-intel::malware", "Malware analysis and research"},
			{"threat-intel::malware::ransomware", "Ransomware families and campaigns"},
			{"threat-intel::malware::apt", "Advanced Persistent Threats and nation-state actors"},
			{"threat-intel::malware::botnets", "Botnets and C2 infrastructure"},
			{"threat-intel::malware::infostealers", "Information stealers and credential theft"},
			{"threat-intel::malware::loaders", "Malware loaders and droppers"},
			{"threat-intel::vulnerabilities", "Vulnerability research and CVEs"},
			{"threat-intel::campaigns", "Attack campaigns and threat actor tracking"},
			{"threat-intel::iocs", "Indicators of Compromise and detection rules"},
			{"threat-intel::techniques", "TTPs and attack techniques (MITRE ATT&CK)"},

			// Research - security research and publications
			{"research", "Security Research"},
			{"research::papers", "Academic papers and publications"},
			{"research::writeups", "Technical writeups and deep dives"},
			{"research::reversing", "Reverse engineering and binary analysis"},
			{"research::forensics", "Digital forensics and incident response"},
			{"research::cryptography", "Cryptography and cryptanalysis"},

			// Tools - security tools and utilities
			{"tools", "Security Tools"},
			{"tools::detection", "Detection and monitoring tools"},
			{"tools::analysis", "Analysis and investigation tools"},
			{"tools::offensive", "Offensive security and red team tools"},
			{"tools::defensive", "Defensive and blue team tools"},
			{"tools::automation", "Automation and orchestration"},

			// Community - community content
			{"community", "Community"},
			{"community::news", "Industry news and announcements"},
			{"community::conferences", "Conferences and talks"},
			{"community::jobs", "Security job postings"},
			{"community::ask", "Questions and discussions"},

			// Learning - educational content
			{"learning", "Learning"},
			{"learning::tutorials", "Tutorials and how-tos"},
			{"learning::courses", "Courses and training"},
			{"learning::certifications", "Certifications and career development"},
			{"learning::labs", "Practice labs and CTF writeups"},
		}

		for _, t := range defaultTags {
			_, err = d.Exec("INSERT INTO tags (name, description) VALUES (?, ?)", t.name, t.desc)
			if err != nil {
				log.Printf("Warning: could not insert default tag %s: %v", t.name, err)
			}
		}
	}

	return nil
}

// runMigrations adds new columns to existing tables.
// SQLite doesn't support IF NOT EXISTS for columns, so we check manually.
// This is called AFTER the schema is created to add columns to existing databases.
func (d *DB) runMigrations() {
	// Migration: Add creator_id and created_at to tags table
	d.addColumnIfNotExists("tags", "creator_id", "INTEGER REFERENCES users(id)")
	// SQLite doesn't allow CURRENT_TIMESTAMP as default in ALTER TABLE, so we use NULL
	d.addColumnIfNotExists("tags", "created_at", "DATETIME")
	d.addColumnIfNotExists("tags", "is_active", "BOOLEAN DEFAULT TRUE")

	// Migration: Add is_blurred to comments table
	d.addColumnIfNotExists("comments", "is_blurred", "BOOLEAN DEFAULT FALSE")

	// Migration: Add post source tracking
	d.addColumnIfNotExists("posts", "source_type", "TEXT DEFAULT 'user'")
	d.addColumnIfNotExists("posts", "source_id", "INTEGER")

	// Migration: Create rss_feeds table if it doesn't exist (for databases created before RSS was added)
	d.createTableIfNotExists(`
		CREATE TABLE IF NOT EXISTS rss_feeds (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			url TEXT UNIQUE NOT NULL,
			title TEXT DEFAULT '',
			tag_id INTEGER NOT NULL REFERENCES tags(id),
			created_by INTEGER REFERENCES users(id),
			interval_minutes INTEGER DEFAULT 60,
			last_polled DATETIME,
			last_error TEXT,
			is_active BOOLEAN DEFAULT TRUE,
			poll_on_view BOOLEAN DEFAULT FALSE,
			auto_approve BOOLEAN DEFAULT TRUE,
			max_items_per_poll INTEGER DEFAULT 10,
			created_at DATETIME DEFAULT CURRENT_TIMESTAMP
		)
	`)

	// Migration: Create rss_items table if it doesn't exist
	d.createTableIfNotExists(`
		CREATE TABLE IF NOT EXISTS rss_items (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			feed_id INTEGER NOT NULL REFERENCES rss_feeds(id) ON DELETE CASCADE,
			guid TEXT NOT NULL,
			post_id INTEGER REFERENCES posts(id),
			imported_at DATETIME DEFAULT CURRENT_TIMESTAMP,
			UNIQUE(feed_id, guid)
		)
	`)

	// Migration: Create rss_poll_log table if it doesn't exist
	d.createTableIfNotExists(`
		CREATE TABLE IF NOT EXISTS rss_poll_log (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			feed_id INTEGER NOT NULL REFERENCES rss_feeds(id) ON DELETE CASCADE,
			polled_at DATETIME DEFAULT CURRENT_TIMESTAMP,
			success BOOLEAN NOT NULL,
			items_found INTEGER DEFAULT 0,
			items_imported INTEGER DEFAULT 0,
			error_message TEXT
		)
	`)

	// Migration: Add RSS feed configuration options (for older rss_feeds tables)
	d.addColumnIfNotExists("rss_feeds", "created_by", "INTEGER REFERENCES users(id)")
	d.addColumnIfNotExists("rss_feeds", "poll_on_view", "BOOLEAN DEFAULT FALSE")
	d.addColumnIfNotExists("rss_feeds", "auto_approve", "BOOLEAN DEFAULT TRUE")
	d.addColumnIfNotExists("rss_feeds", "max_items_per_poll", "INTEGER DEFAULT 10")

	// Migration: Add ban reason and expiration to users
	d.addColumnIfNotExists("users", "ban_reason", "TEXT DEFAULT ''")
	d.addColumnIfNotExists("users", "ban_expires_at", "DATETIME")
	d.addColumnIfNotExists("users", "banned_by", "INTEGER REFERENCES users(id)")

	// Migration: Add hat_id to posts table for wearing hats when posting
	d.addColumnIfNotExists("posts", "hat_id", "INTEGER REFERENCES hats(id)")

	// Migration: Create indexes for RSS tables
	d.Exec("CREATE INDEX IF NOT EXISTS idx_rss_feeds_tag_id ON rss_feeds(tag_id)")
	d.Exec("CREATE INDEX IF NOT EXISTS idx_rss_feeds_last_polled ON rss_feeds(last_polled)")
	d.Exec("CREATE INDEX IF NOT EXISTS idx_rss_feeds_is_active ON rss_feeds(is_active)")
	d.Exec("CREATE INDEX IF NOT EXISTS idx_rss_items_feed_id ON rss_items(feed_id)")
	d.Exec("CREATE INDEX IF NOT EXISTS idx_rss_poll_log_feed_time ON rss_poll_log(feed_id, polled_at DESC)")
}

// createTableIfNotExists runs a CREATE TABLE IF NOT EXISTS statement.
func (d *DB) createTableIfNotExists(stmt string) {
	_, err := d.Exec(stmt)
	if err != nil {
		log.Printf("Warning: could not create table: %v", err)
	}
}

// addColumnIfNotExists adds a column to a table if it doesn't already exist.
func (d *DB) addColumnIfNotExists(table, column, definition string) {
	// Check if column exists by querying table info
	rows, err := d.Query("PRAGMA table_info(" + table + ")")
	if err != nil {
		return
	}
	defer rows.Close()

	for rows.Next() {
		var cid int
		var name, ctype string
		var notnull, pk int
		var dfltValue interface{}
		if err := rows.Scan(&cid, &name, &ctype, &notnull, &dfltValue, &pk); err != nil {
			continue
		}
		if name == column {
			return // Column already exists
		}
	}

	// Add the column
	_, err = d.Exec("ALTER TABLE " + table + " ADD COLUMN " + column + " " + definition)
	if err != nil {
		log.Printf("Warning: could not add column %s.%s: %v", table, column, err)
	}
}

// CleanupExpired removes expired sessions, login tokens, email change tokens,
// and old log entries. Should be called periodically (e.g., every hour).
func (d *DB) CleanupExpired() error {
	now := time.Now()

	_, err := d.Exec("DELETE FROM sessions WHERE expires_at < ?", now)
	if err != nil {
		return fmt.Errorf("cleaning sessions: %w", err)
	}

	_, err = d.Exec("DELETE FROM login_tokens WHERE expires_at < ?", now)
	if err != nil {
		return fmt.Errorf("cleaning login tokens: %w", err)
	}

	_, err = d.Exec("DELETE FROM email_change_tokens WHERE expires_at < ?", now)
	if err != nil {
		return fmt.Errorf("cleaning email change tokens: %w", err)
	}

	// Cleanup old action_log entries (keep 90 days)
	cutoff := now.AddDate(0, 0, -90)
	_, err = d.Exec("DELETE FROM action_log WHERE created_at < ?", cutoff)
	if err != nil {
		return fmt.Errorf("cleaning action log: %w", err)
	}

	// Cleanup old moderation_log entries (keep 1 year)
	cutoff = now.AddDate(-1, 0, 0)
	_, err = d.Exec("DELETE FROM moderation_log WHERE created_at < ?", cutoff)
	if err != nil {
		return fmt.Errorf("cleaning moderation log: %w", err)
	}

	return nil
}
