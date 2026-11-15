package web

import (
	"fmt"
	"log"
	"net/http"

	"github.com/gorilla/sessions"

	"git.emile.space/r2wars-web/internal/model"
)

// session fetches the request's session, logging (rather than ignoring) any
// error the underlying store runs into decoding/loading it.
func (s *Server) session(r *http.Request) *sessions.Session {
	sess, err := s.Sessions.Get(r, "session")
	if err != nil {
		// gorilla still returns a usable new session alongside a decode
		// error (e.g. an expired or tampered cookie); logging keeps that
		// visible without treating the request as unauthenticated-and-broken.
		log.Printf("web: loading session: %v", err)
	}
	return sess
}

// sessionUsername safely extracts the logged-in username from a session, if
// any. The original code did `session.Values["username"].(string)` with no
// comma-ok, which panicked on every request from a session predating a
// schema change or holding an unexpected type.
//
// In DevMode a missing session resolves to the fixed dev user rather than
// "not logged in" - this is the single choke point nearly every handler's
// auth check goes through, so putting the DevMode bypass here (instead of
// only in currentUser) covers the handlers that call this directly too.
func (s *Server) sessionUsername(r *http.Request) (string, bool) {
	sess := s.session(r)
	v, ok := sess.Values["username"]
	if ok && v != nil {
		if name, ok := v.(string); ok && name != "" {
			return name, true
		}
	}
	if s.DevMode {
		if _, ok := s.devUser(); ok {
			return devUsername, true
		}
	}
	return "", false
}

// devUsername/devEmail identify the single fixed account DevMode operates
// as. Using one well-known account (rather than e.g. a random one per
// request) means things a dev user creates - bots, battles - stay owned by
// the same identity across requests, so ownership/edit checks still behave
// sensibly instead of every request looking like a different stranger.
const (
	devUsername = "dev"
	devEmail    = "dev@localhost"
)

// currentUser resolves the logged-in user for a request, if any. In DevMode
// a missing session resolves to the fixed dev user instead of "not logged
// in" (via sessionUsername) - see Server.DevMode's doc comment.
func (s *Server) currentUser(r *http.Request) (model.User, bool) {
	username, ok := s.sessionUsername(r)
	if !ok {
		return model.User{}, false
	}
	user, err := s.Store.GetUserByUsername(username)
	if err != nil {
		log.Printf("web: resolving session user %q: %v", username, err)
		return model.User{}, false
	}
	return user, true
}

// devUser gets-or-creates the fixed DevMode account.
func (s *Server) devUser() (model.User, bool) {
	user, err := s.Store.GetUserByUsername(devUsername)
	if err == nil {
		return user, true
	}

	id, err := s.Store.InsertUser(model.User{Name: devUsername, Email: devEmail})
	if err != nil {
		// Lost a race with another request creating it concurrently - that's
		// fine, just look it up.
		if user, lookupErr := s.Store.GetUserByUsername(devUsername); lookupErr == nil {
			return user, true
		}
		log.Printf("web: devmode: creating dev user: %v", err)
		return model.User{}, false
	}
	return model.User{ID: id, Name: devUsername, Email: devEmail}, true
}

// redirectWithError logs err and redirects to target with a human-readable
// message appended as a query parameter. target must be a format string
// with exactly one %s for the message, e.g. "/battle/5?res=%s".
func redirectWithError(w http.ResponseWriter, r *http.Request, err error, target, msg string) {
	if err != nil {
		log.Println(err)
	}
	http.Redirect(w, r, fmt.Sprintf(target, msg), http.StatusSeeOther)
}
