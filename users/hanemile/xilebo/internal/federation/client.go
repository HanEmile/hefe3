package federation

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
)

// Client handles outbound federation requests to remote servers.
type Client struct {
	origin    string // our server hostname
	privKey   ed25519.PrivateKey
	discovery *DiscoveryCache
	httpClient *http.Client
	logger    *slog.Logger
}

// NewClient creates a federation client.
func NewClient(origin string, privKey ed25519.PrivateKey, httpClient *http.Client, logger *slog.Logger) *Client {
	return &Client{
		origin:     origin,
		privKey:    privKey,
		discovery:  NewDiscoveryCache(),
		httpClient: httpClient,
		logger:     logger,
	}
}

// FederationEvent is a PDU (Persistent Data Unit) sent between servers.
type FederationEvent struct {
	ID        string          `json:"id"`
	Type      string          `json:"type"`
	RoomID    string          `json:"room_id,omitempty"`
	Sender    string          `json:"sender"`
	Timestamp int64           `json:"timestamp"`
	Content   json.RawMessage `json:"content"`
	Origin    string          `json:"origin"`
}

// SendEvents sends a batch of events to a remote server.
func (c *Client) SendEvents(ctx context.Context, targetServer string, events []FederationEvent) error {
	wk, err := c.discovery.Resolve(ctx, targetServer)
	if err != nil {
		return fmt.Errorf("resolve %s: %w", targetServer, err)
	}

	body, err := json.Marshal(map[string]any{
		"origin": c.origin,
		"pdus":   events,
	})
	if err != nil {
		return fmt.Errorf("marshal events: %w", err)
	}

	url := fmt.Sprintf("https://%s/_xb/federation/v1/send", wk.Server)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	SignRequest(req, c.origin, c.privKey, body)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("send to %s: %w", targetServer, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		respBody, _ := io.ReadAll(io.LimitReader(resp.Body, 1024))
		return fmt.Errorf("federation send to %s returned %d: %s", targetServer, resp.StatusCode, string(respBody))
	}

	return nil
}

// QueryKeys fetches a user's pre-key bundle from their home server.
func (c *Client) QueryKeys(ctx context.Context, targetServer, userHandle string) (json.RawMessage, error) {
	wk, err := c.discovery.Resolve(ctx, targetServer)
	if err != nil {
		return nil, fmt.Errorf("resolve %s: %w", targetServer, err)
	}

	url := fmt.Sprintf("https://%s/_xb/federation/v1/keys/%s", wk.Server, userHandle)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	SignRequest(req, c.origin, c.privKey, nil)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("query keys from %s: %w", targetServer, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("query keys returned %d", resp.StatusCode)
	}

	body, err := io.ReadAll(io.LimitReader(resp.Body, 64*1024))
	if err != nil {
		return nil, err
	}
	return json.RawMessage(body), nil
}

// QueryProfile fetches a user's profile from their home server.
func (c *Client) QueryProfile(ctx context.Context, targetServer, userHandle string) (json.RawMessage, error) {
	wk, err := c.discovery.Resolve(ctx, targetServer)
	if err != nil {
		return nil, fmt.Errorf("resolve %s: %w", targetServer, err)
	}

	url := fmt.Sprintf("https://%s/_xb/federation/v1/user/%s", wk.Server, userHandle)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	SignRequest(req, c.origin, c.privKey, nil)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("query profile from %s: %w", targetServer, err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(io.LimitReader(resp.Body, 64*1024))
	if err != nil {
		return nil, err
	}
	return json.RawMessage(body), nil
}

// InviteUser sends a room invitation to a remote user's server.
func (c *Client) InviteUser(ctx context.Context, targetServer string, invite map[string]any) error {
	body, err := json.Marshal(invite)
	if err != nil {
		return err
	}

	wk, err := c.discovery.Resolve(ctx, targetServer)
	if err != nil {
		return fmt.Errorf("resolve %s: %w", targetServer, err)
	}

	url := fmt.Sprintf("https://%s/_xb/federation/v1/invite", wk.Server)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	SignRequest(req, c.origin, c.privKey, body)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("invite to %s: %w", targetServer, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("invite returned %d", resp.StatusCode)
	}

	return nil
}
