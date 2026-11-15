package main

import (
	"context"
	"flag"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/hanemile/xilebo/internal/api"
	"github.com/hanemile/xilebo/internal/config"
	"github.com/hanemile/xilebo/internal/federation"
	"github.com/hanemile/xilebo/internal/storage/sqlite"
	"github.com/hanemile/xilebo/internal/worker"
)

func main() {
	configPath := flag.String("config", "", "Path to config file (TOML)")
	flag.Parse()

	logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{
		Level: slog.LevelInfo,
	}))

	cfg, err := config.Load(*configPath)
	if err != nil {
		logger.Error("failed to load config", "error", err)
		os.Exit(1)
	}

	logger.Info("starting xilebo server",
		"address", cfg.Server.Address,
		"hostname", cfg.Server.Hostname,
		"database", cfg.Database.Driver,
	)

	// Initialize storage
	var store *sqlite.Store
	switch cfg.Database.Driver {
	case "sqlite":
		store, err = sqlite.New(cfg.Database.DSN)
		if err != nil {
			logger.Error("failed to open database", "error", err)
			os.Exit(1)
		}
	default:
		logger.Error("unsupported database driver", "driver", cfg.Database.Driver)
		os.Exit(1)
	}
	defer store.Close()

	// Run migrations
	if err := runMigrations(store); err != nil {
		logger.Error("failed to run migrations", "error", err)
		os.Exit(1)
	}

	// Create media upload directory
	if cfg.Media.UploadDir != "" {
		os.MkdirAll(cfg.Media.UploadDir, 0o755)
	}

	// Create API server
	srv := api.New(store, cfg, logger)
	httpServer := &http.Server{
		Addr:         cfg.Server.Address,
		Handler:      srv.Handler(),
		ReadTimeout:  30 * time.Second,
		WriteTimeout: 30 * time.Second,
		IdleTimeout:  120 * time.Second,
	}

	// Start background workers
	workerPool := worker.NewPool(logger)

	// Start federation if enabled
	if cfg.Federation.Enabled {
		logger.Info("federation enabled", "address", cfg.Federation.Address)

		fedServer := federation.NewFedServer(store, cfg.Server.Hostname, logger)
		fedQueue := federation.NewQueue(store, logger)
		_ = fedQueue // Used by federation sender

		// Start federation listener
		go func() {
			fedHTTP := &http.Server{
				Addr:    cfg.Federation.Address,
				Handler: fedServer.Handler(),
			}
			logger.Info("federation listening", "address", cfg.Federation.Address)
			if err := fedHTTP.ListenAndServe(); err != nil && err != http.ErrServerClosed {
				logger.Error("federation server error", "error", err)
			}
		}()
	}

	// Cleanup worker runs every 10 minutes
	cleanupWorker := worker.NewCleanup(store, logger)
	workerPool.Run("cleanup", 10*time.Minute, cleanupWorker.Run)

	// Graceful shutdown
	done := make(chan os.Signal, 1)
	signal.Notify(done, os.Interrupt, syscall.SIGTERM)

	go func() {
		logger.Info("listening", "address", cfg.Server.Address)
		if err := httpServer.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			logger.Error("server error", "error", err)
			os.Exit(1)
		}
	}()

	<-done
	logger.Info("shutting down...")
	workerPool.Stop()

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	if err := httpServer.Shutdown(ctx); err != nil {
		logger.Error("shutdown error", "error", err)
	}

	logger.Info("server stopped")
}

// runMigrations applies the schema directly for now.
// In production, use goose for proper migrations.
func runMigrations(store *sqlite.Store) error {
	schema := `
CREATE TABLE IF NOT EXISTS users (
    id              TEXT PRIMARY KEY,
    handle          TEXT NOT NULL UNIQUE,
    display_name    TEXT NOT NULL DEFAULT '',
    avatar_url      TEXT NOT NULL DEFAULT '',
    identity_key    BLOB NOT NULL,
    password_hash   TEXT NOT NULL,
    instance_perms  INTEGER NOT NULL DEFAULT 0,
    created_at      INTEGER NOT NULL,
    updated_at      INTEGER NOT NULL
);

CREATE TABLE IF NOT EXISTS user_tags (
    user_id TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    tag     TEXT NOT NULL,
    PRIMARY KEY (user_id, tag)
);

CREATE TABLE IF NOT EXISTS rooms (
    id          TEXT PRIMARY KEY,
    name        TEXT NOT NULL DEFAULT '',
    topic       TEXT NOT NULL DEFAULT '',
    avatar_url  TEXT NOT NULL DEFAULT '',
    is_direct   INTEGER NOT NULL DEFAULT 0,
    room_type   TEXT NOT NULL DEFAULT 'chat',
    created_by  TEXT NOT NULL,
    created_at  INTEGER NOT NULL
);

CREATE TABLE IF NOT EXISTS room_members (
    room_id     TEXT NOT NULL REFERENCES rooms(id) ON DELETE CASCADE,
    user_id     TEXT NOT NULL,
    power_level INTEGER NOT NULL DEFAULT 0,
    joined_at   INTEGER NOT NULL,
    PRIMARY KEY (room_id, user_id)
);

CREATE TABLE IF NOT EXISTS room_state (
    room_id     TEXT NOT NULL REFERENCES rooms(id) ON DELETE CASCADE,
    event_type  TEXT NOT NULL,
    state_key   TEXT NOT NULL DEFAULT '',
    content     TEXT NOT NULL,
    sender      TEXT NOT NULL,
    timestamp   INTEGER NOT NULL,
    PRIMARY KEY (room_id, event_type, state_key)
);

CREATE TABLE IF NOT EXISTS messages (
    id              TEXT PRIMARY KEY,
    room_id         TEXT NOT NULL REFERENCES rooms(id) ON DELETE CASCADE,
    sender          TEXT NOT NULL,
    event_type      TEXT NOT NULL,
    content         TEXT NOT NULL,
    origin_ts       INTEGER NOT NULL,
    server_ts       INTEGER NOT NULL,
    vector_clock    TEXT NOT NULL DEFAULT '{}'
);
CREATE INDEX IF NOT EXISTS idx_messages_room_ts ON messages(room_id, server_ts);
CREATE INDEX IF NOT EXISTS idx_messages_room_id ON messages(room_id, id);

CREATE TABLE IF NOT EXISTS prekeys (
    id          INTEGER PRIMARY KEY AUTOINCREMENT,
    user_id     TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    key_id      INTEGER NOT NULL,
    public_key  BLOB NOT NULL,
    is_signed   INTEGER NOT NULL DEFAULT 0,
    signature   BLOB,
    claimed     INTEGER NOT NULL DEFAULT 0,
    created_at  INTEGER NOT NULL
);
CREATE INDEX IF NOT EXISTS idx_prekeys_user_unclaimed ON prekeys(user_id, claimed) WHERE claimed = 0;

CREATE TABLE IF NOT EXISTS sessions (
    id              TEXT PRIMARY KEY,
    local_user_id   TEXT NOT NULL,
    remote_user_id  TEXT NOT NULL,
    session_data    BLOB NOT NULL,
    updated_at      INTEGER NOT NULL
);
CREATE INDEX IF NOT EXISTS idx_sessions_pair ON sessions(local_user_id, remote_user_id);

CREATE TABLE IF NOT EXISTS federation_queue (
    id              TEXT PRIMARY KEY,
    target_server   TEXT NOT NULL,
    event_data      TEXT NOT NULL,
    created_at      INTEGER NOT NULL,
    attempts        INTEGER NOT NULL DEFAULT 0,
    next_retry      INTEGER NOT NULL,
    sent_at         INTEGER
);
CREATE INDEX IF NOT EXISTS idx_fed_queue_pending ON federation_queue(target_server, next_retry) WHERE sent_at IS NULL;

CREATE TABLE IF NOT EXISTS federation_servers (
    hostname    TEXT PRIMARY KEY,
    public_key  BLOB NOT NULL,
    verified_at INTEGER NOT NULL,
    last_seen   INTEGER NOT NULL
);

CREATE TABLE IF NOT EXISTS media (
    id              TEXT PRIMARY KEY,
    uploader_id     TEXT NOT NULL,
    filename        TEXT NOT NULL,
    content_type    TEXT NOT NULL,
    size_bytes      INTEGER NOT NULL,
    storage_path    TEXT NOT NULL,
    created_at      INTEGER NOT NULL
);

CREATE TABLE IF NOT EXISTS to_device_messages (
    id          INTEGER PRIMARY KEY AUTOINCREMENT,
    sender      TEXT NOT NULL,
    recipient   TEXT NOT NULL,
    event_type  TEXT NOT NULL,
    content     TEXT NOT NULL,
    created_at  INTEGER NOT NULL
);
CREATE INDEX IF NOT EXISTS idx_to_device_recipient ON to_device_messages(recipient);
`

	_, err := store.DB().Exec(schema)
	if err != nil {
		return fmt.Errorf("apply schema: %w", err)
	}
	return nil
}
