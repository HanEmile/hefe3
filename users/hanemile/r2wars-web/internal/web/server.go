// Package web wires the HTTP handlers, session handling and templates
// together against the store and battle packages.
package web

import (
	"context"
	"fmt"
	"html/template"
	"log"
	"net/http"
	"strings"
	"time"

	"github.com/gorilla/mux"

	"git.emile.space/r2wars-web/internal/mailer"
	"git.emile.space/r2wars-web/internal/r2client"
	"git.emile.space/r2wars-web/internal/sessionstore"
	"git.emile.space/r2wars-web/internal/store"
)

// Server holds every dependency HTTP handlers need. It replaces the old
// package-level `globalState` - handlers are methods on *Server instead of
// free functions reaching into a global, which is what let tests construct
// an isolated Server per test instead of sharing mutable global state.
type Server struct {
	Store         *store.Store
	Sessions      *sessionstore.Store
	Mailer        mailer.Mailer
	TemplatesPath string
	Version       string

	// BaseURL (e.g. "https://r2wa.rs") prefixes the links emailed for
	// magic-link login; no trailing slash.
	BaseURL string
	// MagicLinkTTL is how long an emailed login link stays valid.
	MagicLinkTTL time.Duration

	// OpenR2 starts a radare2 pipe against target (e.g. "--" or
	// "malloc://4096"). Defaults to r2client.Open (spawns a real r2 process);
	// tests substitute a fake so the whole HTTP stack - including assembling
	// a bot and running a battle - is exercisable without radare2 installed.
	OpenR2 func(target string) (r2client.Pipe, error)

	// DevMode disables login entirely: every request is treated as a fixed
	// "dev" account (auto-created on first use) regardless of session state,
	// and sending real email isn't required (see cmd/r2wars-web's -devmode
	// flag, which also swaps in a logging mailer). This exists so the whole
	// app - every page, bot/battle CRUD, the actual battle run - can be
	// driven end to end in tests or local runs with no SMTP server, no
	// SESSION_KEY, and no login step. Must never be true in production: it
	// is a complete authentication bypass.
	DevMode bool

	tmpl *template.Template
}

// Link is a breadcrumb/nav entry rendered by nearly every page template.
type Link struct {
	Name   string
	Target string
}

// New constructs a Server and parses templates once up front: the old code
// re-parsed the entire template glob from disk on every single request,
// which is needless I/O on every page view and means a bad template is only
// discovered when a user happens to hit that page.
func New(st *store.Store, sessions *sessionstore.Store, mail mailer.Mailer, templatesPath, version, baseURL string, magicLinkTTL time.Duration) (*Server, error) {
	s := &Server{
		Store:         st,
		Sessions:      sessions,
		Mailer:        mail,
		TemplatesPath: templatesPath,
		Version:       version,
		BaseURL:       strings.TrimRight(baseURL, "/"),
		MagicLinkTTL:  magicLinkTTL,
		OpenR2:        r2client.Open,
	}
	if err := s.loadTemplates(); err != nil {
		return nil, err
	}
	return s, nil
}

func (s *Server) loadTemplates() error {
	t, err := template.ParseGlob(fmt.Sprintf("%s/*.html", s.TemplatesPath))
	if err != nil {
		return fmt.Errorf("web: parsing templates in %q: %w", s.TemplatesPath, err)
	}
	s.tmpl = t
	return nil
}

func (s *Server) render(w http.ResponseWriter, name string, data map[string]interface{}) {
	if err := s.tmpl.ExecuteTemplate(w, name, data); err != nil {
		log.Printf("web: rendering template %q: %v", name, err)
	}
}

func (s *Server) newPageData() map[string]interface{} {
	return map[string]interface{}{"version": s.Version}
}

// Routes builds the full mux router.
func (s *Server) Routes() http.Handler {
	r := mux.NewRouter()
	r.Use(s.recoverMiddleware)
	r.Use(loggingMiddleware)

	// unauthenticated endpoints
	r.HandleFunc("/", s.indexHandler)
	r.HandleFunc("/login", s.loginHandler)
	r.HandleFunc("/login/verify", s.loginVerifyHandler)
	r.HandleFunc("/register", s.registerHandler)

	// endpoints with auth needed
	authNeeded := r.PathPrefix("/").Subrouter()
	authNeeded.Use(s.authMiddleware)
	authNeeded.HandleFunc("/logout", s.logoutHandler)

	authNeeded.HandleFunc("/bot", s.botsHandler)
	authNeeded.HandleFunc("/bot/new", s.botNewHandler)
	authNeeded.HandleFunc("/bot/{id}", s.botSingleHandler)

	authNeeded.HandleFunc("/user", s.usersHandler)
	authNeeded.HandleFunc("/user/{id}", s.userHandler)
	authNeeded.HandleFunc("/user/{id}/profile", s.profileHandler)

	r.HandleFunc("/battle", s.battlesHandler)
	r.HandleFunc("/battle/{id}", s.battleSingleHandler)
	authNeeded.HandleFunc("/battle/new", s.battleNewHandler)
	authNeeded.HandleFunc("/battle/quick", s.battleQuickHandler)
	authNeeded.HandleFunc("/battle/{id}/submit", s.battleSubmitHandler)
	authNeeded.HandleFunc("/battle/{id}/run", s.battleRunHandler)
	authNeeded.HandleFunc("/battle/{id}/delete", s.battleDeleteHandler)

	return r
}

// RunBackups starts a goroutine that snapshots the database to backupDir
// every interval, keeping at most keep snapshots. It stops when ctx is
// cancelled.
func (s *Server) RunBackups(ctx context.Context, backupDir string, interval time.Duration, keep int) {
	if interval <= 0 {
		return
	}
	ticker := time.NewTicker(interval)
	go func() {
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				if err := backupOnce(s.Store, backupDir, keep); err != nil {
					log.Printf("[!] backup failed: %v", err)
				}
			}
		}
	}()
}

// RunMagicLinkCleanup periodically deletes expired, never-clicked login
// links so the table doesn't grow unbounded. It stops when ctx is cancelled.
func (s *Server) RunMagicLinkCleanup(ctx context.Context, interval time.Duration) {
	if interval <= 0 {
		return
	}
	ticker := time.NewTicker(interval)
	go func() {
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				if err := s.Store.PruneExpiredMagicLinks(); err != nil {
					log.Printf("[!] pruning expired magic links failed: %v", err)
				}
			}
		}
	}()
}
