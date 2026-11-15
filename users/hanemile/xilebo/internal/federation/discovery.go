package federation

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"sync"
	"time"
)

// WellKnown is the response from /.well-known/xilebo-server.
type WellKnown struct {
	Server    string `json:"server"`     // host:port for federation
	PublicKey string `json:"public_key"` // base64-encoded Ed25519 public key
}

// DiscoveryCache caches .well-known lookups.
type DiscoveryCache struct {
	mu      sync.RWMutex
	entries map[string]*discoveryEntry
	client  *http.Client
}

type discoveryEntry struct {
	wk        WellKnown
	fetchedAt time.Time
	ttl       time.Duration
}

// NewDiscoveryCache creates a discovery cache with an HTTP client.
func NewDiscoveryCache() *DiscoveryCache {
	return &DiscoveryCache{
		entries: make(map[string]*discoveryEntry),
		client: &http.Client{
			Timeout: 10 * time.Second,
		},
	}
}

// Resolve fetches the federation endpoint for a server hostname.
// Results are cached for 1 hour.
func (dc *DiscoveryCache) Resolve(ctx context.Context, hostname string) (*WellKnown, error) {
	dc.mu.RLock()
	if entry, ok := dc.entries[hostname]; ok {
		if time.Since(entry.fetchedAt) < entry.ttl {
			dc.mu.RUnlock()
			return &entry.wk, nil
		}
	}
	dc.mu.RUnlock()

	wk, err := dc.fetch(ctx, hostname)
	if err != nil {
		return nil, err
	}

	dc.mu.Lock()
	dc.entries[hostname] = &discoveryEntry{
		wk:        *wk,
		fetchedAt: time.Now(),
		ttl:       1 * time.Hour,
	}
	dc.mu.Unlock()

	return wk, nil
}

// Invalidate removes a cached entry.
func (dc *DiscoveryCache) Invalidate(hostname string) {
	dc.mu.Lock()
	delete(dc.entries, hostname)
	dc.mu.Unlock()
}

func (dc *DiscoveryCache) fetch(ctx context.Context, hostname string) (*WellKnown, error) {
	url := fmt.Sprintf("https://%s/.well-known/xilebo-server", hostname)

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}

	resp, err := dc.client.Do(req)
	if err != nil {
		// Fall back to http for local development
		url = fmt.Sprintf("http://%s/.well-known/xilebo-server", hostname)
		req, _ = http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
		resp, err = dc.client.Do(req)
		if err != nil {
			return nil, fmt.Errorf("fetch well-known: %w", err)
		}
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("well-known returned %d", resp.StatusCode)
	}

	var wk WellKnown
	if err := json.NewDecoder(resp.Body).Decode(&wk); err != nil {
		return nil, fmt.Errorf("decode well-known: %w", err)
	}

	if wk.Server == "" {
		wk.Server = hostname + ":8448"
	}

	return &wk, nil
}
