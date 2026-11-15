package federation

import (
	"context"
	"encoding/json"
	"log/slog"
	"time"

	"github.com/oklog/ulid/v2"

	"github.com/hanemile/xilebo/internal/storage"
)

// Queue manages persistent outbound federation events.
type Queue struct {
	store  storage.Store
	logger *slog.Logger
}

// NewQueue creates a federation outbound queue.
func NewQueue(store storage.Store, logger *slog.Logger) *Queue {
	return &Queue{store: store, logger: logger}
}

// Enqueue adds an event to the outbound queue for a target server.
func (q *Queue) Enqueue(ctx context.Context, targetServer string, event FederationEvent) error {
	eventData, err := json.Marshal(event)
	if err != nil {
		return err
	}

	now := time.Now().UnixMilli()
	return q.store.EnqueueFederationEvent(ctx, storage.EnqueueFedEventParams{
		ID:           ulid.Make().String(),
		TargetServer: targetServer,
		EventData:    string(eventData),
		CreatedAt:    now,
		NextRetry:    now, // Ready immediately
	})
}

// GetPending retrieves pending events for a target server.
func (q *Queue) GetPending(ctx context.Context, targetServer string, limit int) ([]storage.FederationQueueItem, error) {
	now := time.Now().UnixMilli()
	return q.store.GetPendingFederationEvents(ctx, targetServer, now, limit)
}

// MarkSent marks an event as successfully sent.
func (q *Queue) MarkSent(ctx context.Context, eventID string) error {
	return q.store.MarkFederationEventSent(ctx, eventID, time.Now().UnixMilli())
}

// RetryLater increments the attempt counter and sets the next retry time.
func (q *Queue) RetryLater(ctx context.Context, eventID string, attempt int) error {
	// Exponential backoff: 1s, 2s, 4s, 8s, ... capped at 1 hour
	delay := time.Duration(1<<uint(attempt)) * time.Second
	if delay > time.Hour {
		delay = time.Hour
	}
	nextRetry := time.Now().Add(delay).UnixMilli()
	return q.store.IncrementFederationAttempt(ctx, eventID, nextRetry)
}
