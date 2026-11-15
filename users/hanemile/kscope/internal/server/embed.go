// internal/server/embed.go — Embeds the web/ directory into the binary.
// Each JS/CSS file is served as a separate static file so that browsers can
// cache them individually and editors/linters can work on them directly.
// index.html uses <link rel="stylesheet"> and <script src="..."> tags.
//
// Part of github.com/hanemile/kscope
package server

import (
	"embed"
	"io/fs"
	"net/http"
	"strings"
)

//go:embed web
var webFS embed.FS

// staticFileServer returns an http.Handler that serves the embedded web/ tree.
func staticFileServer() http.Handler {
	sub, _ := fs.Sub(webFS, "web")
	return http.FileServer(http.FS(sub))
}

// handleIndex serves index.html for the SPA root and all non-API routes.
func (s *Server) handleIndex(w http.ResponseWriter, r *http.Request) {
	// Serve static assets (js/, css, etc.) directly
	path := strings.TrimPrefix(r.URL.Path, "/")
	if path != "" && path != "index.html" {
		// Let the static file server handle it
		staticFileServer().ServeHTTP(w, r)
		return
	}
	// Serve index.html
	b, err := fs.ReadFile(webFS, "web/index.html")
	if err != nil {
		http.Error(w, "index.html not found", 500)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Write(b)
}
