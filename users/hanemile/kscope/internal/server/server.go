// internal/server/server.go — Server wiring: creates the worker pool,
// registers job handlers, mounts routes, and starts background services.
// Split: api.go (handlers), sse.go (SSE hub), git.go (git ops), html*.go (SPA).
//
// Part of github.com/hanemile/kscope
package server

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"runtime"
	"time"

	"github.com/gorilla/mux"
	"github.com/hanemile/kscope/internal/indexer"
	"github.com/hanemile/kscope/internal/plugin"
	"github.com/hanemile/kscope/internal/protocol"
	"github.com/hanemile/kscope/internal/store"
	"github.com/hanemile/kscope/internal/worker"
)

// Config holds server startup options.
type Config struct {
	KernelRoot string
	Addr       string
	PluginDir  string
	Workers    int
	Verbose    bool
}

// Server is the KScope HTTP server.
type Server struct {
	cfg     Config
	router  *mux.Router
	pool    *worker.Pool
	sse     *sseHub
	plugins *plugin.Registry
	vs      *versionRegistry
	ctx     context.Context
	cancel  context.CancelFunc
}

// New creates and starts a Server.
func New(cfg Config) (*Server, error) {
	ctx, cancel := context.WithCancel(context.Background())
	workers := cfg.Workers
	if workers < 1 { workers = runtime.NumCPU() * 2 }

	pool := worker.New(workers)
	hub  := newSSEHub()
	vreg := newVersionRegistry()
	reg  := plugin.NewRegistry(cfg.PluginDir)

	s := &Server{cfg: cfg, pool: pool, sse: hub, plugins: reg, vs: vreg, ctx: ctx, cancel: cancel}

	// Index handler: payload → parse → atomic swap
	pool.Register(protocol.KindIndexStart, func(pctx context.Context, job *worker.Job, emit func(*protocol.Msg)) error {
		p, ok := job.Payload.(protocol.IndexStartPayload)
		if !ok { return fmt.Errorf("bad payload") }
		vstore := vreg.Get(p.Label)
		if vstore == nil {
			vstore = store.NewVersionStore(p.Label, p.Root, p.GitTag)
			vreg.Add(vstore)
		}
		return indexer.Handler(
			func(idx *store.Index) { vstore.Swap(idx) },
			vstore.Annotations,
		)(pctx, job, emit)
	})

	// Wire pool broadcast → SSE hub
	pool.SetEmit(func(m *protocol.Msg) { hub.broadcast(m) })

	pool.Start(ctx)
	hub.Start()
	s.setupRoutes()
	s.enqueueIndex(cfg.KernelRoot, "", "")
	return s, nil
}

// Run starts the HTTP server (blocking).
func (s *Server) Run() error {
	srv := &http.Server{Addr: s.cfg.Addr, Handler: s.router,
		ReadTimeout: 60 * time.Second, IdleTimeout: 120 * time.Second}
	log.Printf("[server] Listening on %s  (%d workers)", s.cfg.Addr, s.cfg.Workers)
	return srv.ListenAndServe()
}

// Shutdown gracefully stops workers.
func (s *Server) Shutdown() { s.cancel(); s.pool.Stop(); s.sse.Stop() }

func (s *Server) setupRoutes() {
	r := mux.NewRouter()
	r.HandleFunc("/api/status",           s.handleStatus).Methods("GET")
	r.HandleFunc("/api/jobs",             s.handleJobs).Methods("GET")
	r.HandleFunc("/api/jobs/{id}/cancel", s.handleJobCancel).Methods("POST")
	r.HandleFunc("/api/events",           s.handleSSE)
	r.HandleFunc("/api/versions",         s.handleVersions).Methods("GET")
	r.HandleFunc("/api/git/tags",         s.handleGitTags).Methods("GET")
	r.HandleFunc("/api/git/checkout",     s.handleGitCheckout).Methods("POST")
	r.HandleFunc("/api/reindex",          s.handleReindex).Methods("POST")
	r.HandleFunc("/api/index-dir",         s.handleIndexDir).Methods("POST")
	r.HandleFunc("/api/search",           s.handleSearch).Methods("GET")
	r.HandleFunc("/api/symbol/{name}",    s.handleSymbol).Methods("GET")
	r.HandleFunc("/api/file",             s.handleFile).Methods("GET")
	r.HandleFunc("/api/diff",             s.handleDiff).Methods("GET")
	r.HandleFunc("/api/tree",             s.handleTree).Methods("GET")
	r.HandleFunc("/api/duplicates",       s.handleDuplicates).Methods("GET")
	r.HandleFunc("/api/graph",            s.handleGraph).Methods("GET")
	r.HandleFunc("/api/xref/{name}",      s.handleXRef).Methods("GET")
	r.HandleFunc("/api/xrefgraph/{name}", s.handleXRefGraph).Methods("GET")
	r.HandleFunc("/api/callgraph/{name}",    s.handleCallGraph).Methods("GET")
	r.HandleFunc("/api/constants",           s.handleConstants).Methods("GET")
	r.HandleFunc("/api/annotations",         s.handleAnnotations).Methods("GET")
	r.HandleFunc("/api/annotations",         s.handleAddAnnotation).Methods("POST")
	r.HandleFunc("/api/annotations/{id}",    s.handleDeleteAnnotation).Methods("DELETE")
	r.HandleFunc("/api/plugins/reload",      s.handlePluginReload).Methods("POST")
	r.HandleFunc("/api/symbols",             s.handleSymbols).Methods("GET")
	r.HandleFunc("/api/symbol/{name}/view",  s.handleSymbolView).Methods("GET")
	r.HandleFunc("/api/plugin-actions",      s.handlePluginActions).Methods("GET")
	r.HandleFunc("/api/plugin-dispatch",     s.handlePluginDispatch).Methods("POST")
	r.HandleFunc("/api/plugins",             s.handlePluginList).Methods("GET")
	r.HandleFunc("/api/plugin/{name}",       s.handlePluginExec).Methods("POST")
	r.PathPrefix("/").HandlerFunc(s.handleIndex)
	s.router = r
}

func (s *Server) enqueueIndex(root, label, gitTag string) string {
	if label == "" { label = labelFromRoot(root) }
	return s.pool.Submit(protocol.KindIndexStart, protocol.PriorityNormal,
		protocol.IndexStartPayload{Root: root, Label: label, GitTag: gitTag, Workers: s.cfg.Workers},
		"index:"+label)
}
