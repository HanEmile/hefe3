package api

import (
	"fmt"
	"io/fs"
	"log/slog"
	"net/http"
	"os"
	"strings"

	"github.com/hanemile/xilebo/internal/api/ws"
	"github.com/hanemile/xilebo/internal/config"
	"github.com/hanemile/xilebo/internal/sfu"
	"github.com/hanemile/xilebo/internal/storage"
)

// Server holds the API dependencies.
type Server struct {
	store         storage.Store
	cfg           *config.Config
	hub           *ws.Hub
	sfuInstance   *sfu.SFU
	signalHandler *sfu.SignalHandler
	logger        *slog.Logger
}

// New creates a new API server.
func New(store storage.Store, cfg *config.Config, logger *slog.Logger) *Server {
	hub := ws.NewHub(logger)
	go hub.Run()

	s := &Server{
		store:  store,
		cfg:    cfg,
		hub:    hub,
		logger: logger,
	}

	// Initialize SFU if enabled
	if cfg.SFU.Enabled {
		s.sfuInstance = sfu.New(cfg.SFU.ICEServers, logger)
		s.signalHandler = sfu.NewSignalHandler(s.sfuInstance, logger)
	}

	return s
}

// Handler returns the top-level HTTP handler with all routes registered.
func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()

	// Client-Server API
	api := "/_xb/client/v1"

	// Auth (public)
	mux.HandleFunc("POST "+api+"/register", s.handleRegister)
	mux.HandleFunc("POST "+api+"/login", s.handleLogin)

	// Authenticated routes
	mux.HandleFunc("GET "+api+"/sync", s.auth(s.handleSync))
	mux.HandleFunc("POST "+api+"/rooms", s.auth(s.handleCreateRoom))
	mux.HandleFunc("GET "+api+"/rooms", s.auth(s.handleListRooms))
	mux.HandleFunc("GET "+api+"/rooms/{roomID}", s.auth(s.handleGetRoom))
	mux.HandleFunc("POST "+api+"/rooms/{roomID}/join", s.auth(s.handleJoinRoom))
	mux.HandleFunc("POST "+api+"/rooms/{roomID}/leave", s.auth(s.handleLeaveRoom))
	mux.HandleFunc("POST "+api+"/rooms/{roomID}/send", s.auth(s.handleSendMessage))
	mux.HandleFunc("GET "+api+"/rooms/{roomID}/messages", s.auth(s.handleGetMessages))
	mux.HandleFunc("GET "+api+"/rooms/{roomID}/members", s.auth(s.handleGetMembers))
	mux.HandleFunc("PUT "+api+"/rooms/{roomID}/state/{eventType}", s.auth(s.handleSetRoomState))
	mux.HandleFunc("GET "+api+"/profile/{handle}", s.auth(s.handleGetProfile))
	mux.HandleFunc("PUT "+api+"/profile", s.auth(s.handleUpdateProfile))
	mux.HandleFunc("POST "+api+"/keys/upload", s.auth(s.handleUploadKeys))
	mux.HandleFunc("POST "+api+"/keys/claim", s.auth(s.handleClaimKeys))

	// WebSocket
	mux.HandleFunc("GET "+api+"/ws", s.auth(s.handleWebSocket))

	// Calls / WebRTC signaling
	mux.HandleFunc("POST "+api+"/calls/offer", s.auth(s.handleCallOffer))
	mux.HandleFunc("POST "+api+"/calls/answer", s.auth(s.handleCallAnswer))
	mux.HandleFunc("POST "+api+"/calls/candidate", s.auth(s.handleCallCandidate))
	mux.HandleFunc("POST "+api+"/calls/leave", s.auth(s.handleCallLeave))
	mux.HandleFunc("GET "+api+"/calls/ice-servers", s.auth(s.handleCallICEServers))

	// Federation well-known (served on the main port too)
	mux.HandleFunc("GET /.well-known/xilebo-server", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Access-Control-Allow-Origin", "*")
		fmt.Fprintf(w, `{"server":"%s:8448","public_key":""}`, s.cfg.Server.Hostname)
	})

	// Serve web frontend — fall back to index.html for SPA routing
	webDir := "web"
	if _, err := os.Stat(webDir); err == nil {
		fileServer := http.FileServer(http.Dir(webDir))
		mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
			// API routes are already matched above, so this only handles non-API paths
			path := r.URL.Path

			// Try to serve the file directly
			if path != "/" && !strings.HasPrefix(path, "/_xb/") {
				if f, err := fs.Stat(os.DirFS(webDir), strings.TrimPrefix(path, "/")); err == nil && !f.IsDir() {
					fileServer.ServeHTTP(w, r)
					return
				}
			}

			// SPA fallback — serve index.html
			http.ServeFile(w, r, webDir+"/index.html")
		})
	}

	// Apply global middleware
	var handler http.Handler = mux
	handler = s.corsMiddleware(handler)
	handler = s.loggingMiddleware(handler)

	return handler
}

// Hub returns the WebSocket hub for broadcasting events.
func (s *Server) Hub() *ws.Hub {
	return s.hub
}
