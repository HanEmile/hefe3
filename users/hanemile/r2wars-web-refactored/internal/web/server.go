// Package web is the HTTP layer of r2wars-web. It replaces the original flat
// package-main handlers (global state + a template.ParseGlob on every request)
// with a Server struct that owns its dependencies and parses its templates
// exactly once at startup.
package web

import (
	"fmt"
	"html/template"
	"io"
	"net/http"

	"git.emile.space/r2wars-web/internal/store"
	"github.com/gorilla/handlers"
	"github.com/gorilla/mux"
)

// Server holds everything the HTTP handlers need. It is created once at startup
// and is safe to share across requests (the store and session store are
// concurrency-safe, templates are read-only after parsing).
type Server struct {
	Store     *store.Store
	Sessions  *store.SessionStore
	Templates *template.Template
	Version   string
	Salt      []byte

	// logOut is where the gorilla LoggingHandler writes its access log. nil
	// disables request logging (handy in tests).
	logOut io.Writer
}

// NewServer builds a Server, parsing every template under templatesGlob once.
// version and salt come from the VERSION and SALT environment variables in the
// real wiring; salt is used for argon2 password hashing.
func NewServer(st *store.Store, sess *store.SessionStore, templatesGlob, version, salt string) (*Server, error) {
	tmpl, err := template.ParseGlob(templatesGlob)
	if err != nil {
		return nil, fmt.Errorf("parsing templates %q: %w", templatesGlob, err)
	}

	return &Server{
		Store:     st,
		Sessions:  sess,
		Templates: tmpl,
		Version:   version,
		Salt:      []byte(salt),
	}, nil
}

// SetLogOutput sets the writer used by the request-logging middleware. Call it
// before Router(). If never called, requests are not logged.
func (s *Server) SetLogOutput(w io.Writer) {
	s.logOut = w
}

// Router builds the gorilla/mux router with exactly the original route table.
// The non-authenticated routes (/, /login, /register, /battle, /battle/{id})
// live on the root router; everything else lives on an auth subrouter guarded
// by authMiddleware. Request logging (if configured) wraps the whole tree.
func (s *Server) Router() http.Handler {
	r := mux.NewRouter()

	if s.logOut != nil {
		r.Use(func(next http.Handler) http.Handler {
			return handlers.LoggingHandler(s.logOut, next)
		})
	}

	// unauthenticated endpoints
	r.HandleFunc("/", s.indexHandler)
	r.HandleFunc("/login", s.loginHandler)
	r.HandleFunc("/register", s.registerHandler)

	// the battle list is intentionally NOT behind auth, matching the original
	// (the handler itself redirects to /login when no session is present).
	r.HandleFunc("/battle", s.battlesHandler)

	// endpoints requiring auth. These are registered before the catch-all
	// /battle/{id} below so that the more specific /battle/new, /battle/quick
	// and /battle/{id}/{action} routes win — gorilla/mux matches in
	// registration order, and {id} would otherwise swallow "new"/"quick".
	authed := r.PathPrefix("/").Subrouter()
	authed.Use(s.authMiddleware)
	authed.HandleFunc("/logout", s.logoutHandler)

	authed.HandleFunc("/bot", s.botsHandler)
	authed.HandleFunc("/bot/new", s.botNewHandler)
	authed.HandleFunc("/bot/{id}", s.botSingleHandler)

	authed.HandleFunc("/user", s.usersHandler)
	authed.HandleFunc("/user/{id}", s.userHandler)
	authed.HandleFunc("/user/{id}/profile", s.profileHandler)

	authed.HandleFunc("/battle/new", s.battleNewHandler)
	authed.HandleFunc("/battle/quick", s.battleQuickHandler)
	authed.HandleFunc("/battle/{id}/submit", s.battleSubmitHandler)
	authed.HandleFunc("/battle/{id}/run", s.battleRunHandler)
	authed.HandleFunc("/battle/{id}/delete", s.battleDeleteHandler)

	// the single battle page is NOT behind auth (handler redirects to /login
	// itself). Registered last so the specific battle routes above take
	// precedence over this catch-all.
	r.HandleFunc("/battle/{id}", s.battleSingleHandler)

	return r
}

// authMiddleware redirects to /login when the session has no "username".
func (s *Server) authMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if _, ok := s.currentUsername(r); !ok {
			http.Redirect(w, r, "/login", http.StatusSeeOther)
			return
		}
		next.ServeHTTP(w, r)
	})
}
