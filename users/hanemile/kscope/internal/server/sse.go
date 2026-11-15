// internal/server/sse.go — Server-Sent Events hub and version registry.
// The SSE hub fans out every protocol.Msg to all connected browser clients.
// The version registry maps label → VersionStore with thread-safe access.
//
// Part of github.com/hanemile/kscope
package server

import (
	"encoding/json"
	"fmt"
	"net/http"
	"path/filepath"
	"sync"
	"time"

	"github.com/hanemile/kscope/internal/protocol"
	"github.com/hanemile/kscope/internal/store"
)

// ── SSE Hub ───────────────────────────────────────────────────────────────────

type sseHub struct {
	mu      sync.Mutex
	clients map[chan []byte]struct{}
	quit    chan struct{}
}

func newSSEHub() *sseHub {
	return &sseHub{clients: make(map[chan []byte]struct{}), quit: make(chan struct{})}
}

func (h *sseHub) Start() {} // nothing to start; broadcast is synchronous

func (h *sseHub) Stop() { close(h.quit) }

func (h *sseHub) add(ch chan []byte) {
	h.mu.Lock(); h.clients[ch] = struct{}{}; h.mu.Unlock()
}

func (h *sseHub) remove(ch chan []byte) {
	h.mu.Lock(); delete(h.clients, ch); h.mu.Unlock()
}

func (h *sseHub) broadcast(m *protocol.Msg) {
	b, err := json.Marshal(m)
	if err != nil { return }
	line := []byte(fmt.Sprintf("event: %s\ndata: %s\n\n", m.Kind, b))
	h.mu.Lock()
	for ch := range h.clients {
		select { case ch <- line: default: }
	}
	h.mu.Unlock()
}

func (h *sseHub) broadcastRaw(event string, data interface{}) {
	b, err := json.Marshal(data)
	if err != nil { return }
	line := []byte(fmt.Sprintf("event: %s\ndata: %s\n\n", event, b))
	h.mu.Lock()
	for ch := range h.clients { select { case ch <- line: default: } }
	h.mu.Unlock()
}

// handleSSE is the HTTP handler for /api/events.
func (s *Server) handleSSE(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type",  "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection",    "keep-alive")
	w.Header().Set("Access-Control-Allow-Origin", "*")

	ch := make(chan []byte, 64)
	s.sse.add(ch)
	defer s.sse.remove(ch)

	// Replay current indexing state so newly-connected browsers get immediate feedback.
	// phase "walk" = indexing in progress; phase "done" = complete.
	s.vs.Each(func(vs *store.VersionStore) {
		phase := "walk"
		pct   := 0.0
		if vs.Indexed { phase = "done"; pct = 100 }
		idx := vs.Read()
		m := protocol.MustNew(protocol.KindIndexProgress, protocol.IndexProgressPayload{
			Label:   vs.Label,
			Phase:   phase,
			Pct:     pct,
			Symbols: int64(idx.SymbolCount()),
			Files:   int64(idx.FileCount()),
		})
		b, _ := json.Marshal(m)
		fmt.Fprintf(w, "event: %s\ndata: %s\n\n", m.Kind, b)
	})
	// Send current job list
	jobs := s.pool.Jobs()
	if len(jobs) > 0 {
		b, _ := json.Marshal(jobs)
		fmt.Fprintf(w, "event: jobs\ndata: %s\n\n", b)
	}
	if f, ok := w.(http.Flusher); ok { f.Flush() }

	for {
		select {
		case <-r.Context().Done(): return
		case msg := <-ch:
			w.Write(msg)
			if f, ok := w.(http.Flusher); ok { f.Flush() }
		case <-time.After(20 * time.Second):
			fmt.Fprint(w, ": keepalive\n\n")
			if f, ok := w.(http.Flusher); ok { f.Flush() }
		}
	}
}

// ── Version registry ──────────────────────────────────────────────────────────

type versionRegistry struct {
	mu    sync.RWMutex
	store map[string]*store.VersionStore
	order []string // insertion order
}

func newVersionRegistry() *versionRegistry {
	return &versionRegistry{store: make(map[string]*store.VersionStore)}
}

func (vr *versionRegistry) Add(vs *store.VersionStore) {
	vr.mu.Lock()
	if _, exists := vr.store[vs.Label]; !exists {
		vr.order = append(vr.order, vs.Label)
	}
	vr.store[vs.Label] = vs
	vr.mu.Unlock()
}

func (vr *versionRegistry) Get(label string) *store.VersionStore {
	vr.mu.RLock(); defer vr.mu.RUnlock()
	if label == "" && len(vr.order) > 0 { return vr.store[vr.order[0]] }
	return vr.store[label]
}

func (vr *versionRegistry) Each(fn func(*store.VersionStore)) {
	vr.mu.RLock()
	keys := make([]string, len(vr.order)); copy(keys, vr.order)
	vr.mu.RUnlock()
	for _, k := range keys {
		vr.mu.RLock(); vs := vr.store[k]; vr.mu.RUnlock()
		if vs != nil { fn(vs) }
	}
}

func (vr *versionRegistry) List() []map[string]interface{} {
	vr.mu.RLock(); defer vr.mu.RUnlock()
	out := make([]map[string]interface{}, 0, len(vr.order))
	for _, k := range vr.order {
		vs := vr.store[k]
		idx := vs.Read()
		out = append(out, map[string]interface{}{
			"label": vs.Label, "root": vs.Root, "git_tag": vs.GitTag,
			"indexed": vs.Indexed,
			"symbols": idx.SymbolCount(),
			"files":   idx.FileCount(),
			"xrefs":   idx.XRefCount(),
		})
	}
	return out
}

// ── helpers ───────────────────────────────────────────────────────────────────

func labelFromRoot(root string) string {
	b := filepath.Base(root)
	if b == "." || b == "/" { return "kernel" }
	return b
}

func writeJSON(w http.ResponseWriter, v interface{}) {
	w.Header().Set("Content-Type", "application/json")
	enc := json.NewEncoder(w); enc.SetIndent("", "  "); _ = enc.Encode(v)
}
