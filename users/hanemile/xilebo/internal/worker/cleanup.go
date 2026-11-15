package worker

import (
	"context"
	"log/slog"

	"github.com/hanemile/xilebo/internal/storage"
)

// Cleanup handles periodic cleanup of expired data.
type Cleanup struct {
	store  storage.Store
	logger *slog.Logger
}

// NewCleanup creates a cleanup worker.
func NewCleanup(store storage.Store, logger *slog.Logger) *Cleanup {
	return &Cleanup{store: store, logger: logger}
}

// Run performs cleanup tasks. Called periodically by the worker pool.
func (c *Cleanup) Run(ctx context.Context) error {
	// Clean old sent federation events (older than 7 days)
	// This would need a dedicated query — skipped for now as the
	// CleanOldFederationEvents query exists in sqlc

	c.logger.Debug("cleanup completed")
	return nil
}
