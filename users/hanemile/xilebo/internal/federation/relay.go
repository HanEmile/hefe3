package federation

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
)

// Relay implements relay-assisted federation for servers behind NAT.
// Events are sent to a relay server which stores and forwards them.
type Relay struct {
	relayURL string
	origin   string
	client   *http.Client
	logger   *slog.Logger
}

// NewRelay creates a relay client for relay-assisted federation.
func NewRelay(relayURL, origin string, client *http.Client, logger *slog.Logger) *Relay {
	return &Relay{
		relayURL: relayURL,
		origin:   origin,
		client:   client,
		logger:   logger,
	}
}

// Send stores events at the relay for the target server to pick up.
func (r *Relay) Send(ctx context.Context, targetServer string, events []FederationEvent) error {
	body, err := json.Marshal(map[string]any{
		"origin": r.origin,
		"target": targetServer,
		"pdus":   events,
	})
	if err != nil {
		return err
	}

	url := fmt.Sprintf("%s/relay/send", r.relayURL)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := r.client.Do(req)
	if err != nil {
		return fmt.Errorf("relay send: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		respBody, _ := io.ReadAll(io.LimitReader(resp.Body, 1024))
		return fmt.Errorf("relay returned %d: %s", resp.StatusCode, string(respBody))
	}

	return nil
}

// Poll retrieves pending events from the relay for our server.
func (r *Relay) Poll(ctx context.Context) ([]FederationEvent, error) {
	url := fmt.Sprintf("%s/relay/poll?server=%s", r.relayURL, r.origin)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}

	resp, err := r.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("relay poll: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("relay poll returned %d", resp.StatusCode)
	}

	var result struct {
		Events []FederationEvent `json:"events"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, err
	}

	return result.Events, nil
}
