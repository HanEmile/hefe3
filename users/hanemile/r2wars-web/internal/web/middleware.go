package web

import (
	"log"
	"net/http"
	"runtime/debug"

	"github.com/gorilla/handlers"
)

// recoverMiddleware stops a panic in any handler (a bad type assertion, an
// r2 subprocess surprise, an out-of-range index, ...) from taking down the
// whole server process or leaving a connection half-written. Go's net/http
// already recovers panics per-request, but it does so by abruptly closing
// the connection with no logging context and no response body; this gives
// callers a 500 and us a stack trace tied to the request that caused it.
func (s *Server) recoverMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer func() {
			if rec := recover(); rec != nil {
				log.Printf("[!] panic handling %s %s: %v\n%s", r.Method, r.URL.Path, rec, debug.Stack())
				http.Error(w, "500 - Internal Server Error", http.StatusInternalServerError)
			}
		}()
		next.ServeHTTP(w, r)
	})
}

func loggingMiddleware(next http.Handler) http.Handler {
	return handlers.LoggingHandler(log.Writer(), next)
}

// authMiddleware redirects to /login unless a valid session with a username
// is present.
func (s *Server) authMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if _, ok := s.sessionUsername(r); !ok {
			http.Redirect(w, r, "/login", http.StatusSeeOther)
			return
		}
		next.ServeHTTP(w, r)
	})
}
