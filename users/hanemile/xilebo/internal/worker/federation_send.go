package worker

import (
	"context"
	"encoding/json"
	"log/slog"

	"github.com/hanemile/xilebo/internal/federation"
	"github.com/hanemile/xilebo/internal/storage"
)

// FederationSender processes the outbound federation queue.
type FederationSender struct {
	store    storage.Store
	client   *federation.Client
	queue    *federation.Queue
	relay    *federation.Relay // may be nil
	logger   *slog.Logger
}

// NewFederationSender creates a federation sender worker.
func NewFederationSender(
	store storage.Store,
	client *federation.Client,
	queue *federation.Queue,
	relay *federation.Relay,
	logger *slog.Logger,
) *FederationSender {
	return &FederationSender{
		store:  store,
		client: client,
		queue:  queue,
		relay:  relay,
		logger: logger,
	}
}

// Process sends pending federation events for all known servers.
func (fs *FederationSender) Process(ctx context.Context) error {
	// Get distinct target servers from the queue
	// For now, we process known federation servers
	servers, err := fs.getTargetServers(ctx)
	if err != nil {
		return err
	}

	for _, server := range servers {
		if err := fs.processServer(ctx, server); err != nil {
			fs.logger.Error("federation send failed", "server", server, "error", err)
		}
	}

	return nil
}

func (fs *FederationSender) processServer(ctx context.Context, targetServer string) error {
	pending, err := fs.queue.GetPending(ctx, targetServer, 50)
	if err != nil {
		return err
	}

	if len(pending) == 0 {
		return nil
	}

	// Parse events from queue
	events := make([]federation.FederationEvent, 0, len(pending))
	for _, item := range pending {
		var event federation.FederationEvent
		if err := json.Unmarshal([]byte(item.EventData), &event); err != nil {
			fs.logger.Error("parse queued event", "error", err, "id", item.ID)
			continue
		}
		events = append(events, event)
	}

	if len(events) == 0 {
		return nil
	}

	// Try direct federation first
	err = fs.client.SendEvents(ctx, targetServer, events)
	if err != nil {
		// Try relay fallback if configured
		if fs.relay != nil {
			fs.logger.Info("direct federation failed, trying relay", "server", targetServer)
			err = fs.relay.Send(ctx, targetServer, events)
		}
	}

	if err != nil {
		// Retry later with exponential backoff
		for _, item := range pending {
			fs.queue.RetryLater(ctx, item.ID, item.Attempts)
		}
		return err
	}

	// Mark all as sent
	for _, item := range pending {
		fs.queue.MarkSent(ctx, item.ID)
	}

	fs.logger.Info("federation sent", "server", targetServer, "count", len(events))
	return nil
}

func (fs *FederationSender) getTargetServers(ctx context.Context) ([]string, error) {
	// Get all pending distinct target servers from queue
	// This is a simplified version — in production you'd have a separate query
	// For now, get pending events with limit 1 per server
	// We check a few known patterns
	items, err := fs.store.GetPendingFederationEvents(ctx, "", 0, 100)
	if err != nil {
		// If the query fails (empty server string), return empty
		return nil, nil
	}

	seen := make(map[string]bool)
	var servers []string
	for _, item := range items {
		if !seen[item.TargetServer] {
			seen[item.TargetServer] = true
			servers = append(servers, item.TargetServer)
		}
	}
	return servers, nil
}
