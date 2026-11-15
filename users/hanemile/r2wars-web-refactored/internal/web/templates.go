package web

import (
	"fmt"
	"log"
	"net/http"

	"git.emile.space/r2wars-web/internal/model"
)

// Link is a single breadcrumb / navigation entry. Moved here from the original
// http.go; the nav template reads its .Name and .Target fields.
type Link struct {
	Name   string
	Target string
}

// render executes a pre-parsed template by name with the given data, writing a
// 500 on failure. No ParseGlob happens here: templates are parsed once in
// NewServer.
func (s *Server) render(w http.ResponseWriter, name string, data map[string]any) {
	if err := s.Templates.ExecuteTemplate(w, name, data); err != nil {
		log.Printf("error executing template %q: %v", name, err)
		w.WriteHeader(http.StatusInternalServerError)
		w.Write([]byte("500 - Error rendering template"))
	}
}

// currentUsername returns the logged-in username from the session, if any.
func (s *Server) currentUsername(r *http.Request) (string, bool) {
	session, _ := s.Sessions.Get(r, "session")
	username := session.Values["username"]
	if username == nil {
		return "", false
	}
	name, ok := username.(string)
	if !ok || name == "" {
		return "", false
	}
	return name, true
}

// currentUser resolves the logged-in user from the session into a model.User.
func (s *Server) currentUser(r *http.Request) (model.User, bool) {
	name, ok := s.currentUsername(r)
	if !ok {
		return model.User{}, false
	}
	user, err := s.Store.UserByName(name)
	if err != nil {
		return model.User{}, false
	}
	return user, true
}

// baseData builds the data map common to every page: the version string and,
// when logged in, the current user (the nav template keys off .user). Handlers
// add their own breadcrumb links and page-specific keys on top.
func (s *Server) baseData(r *http.Request) map[string]any {
	data := map[string]any{}
	data["version"] = s.Version
	if user, ok := s.currentUser(r); ok {
		data["logged_in"] = true
		data["user"] = user
	}
	return data
}

// logAndRedir logs err and redirects to target (a format string containing a
// single %s) with msg substituted in. Port of the original
// log_and_redir_with_msg.
func (s *Server) logAndRedir(w http.ResponseWriter, r *http.Request, err error, target, msg string) {
	log.Println(err)
	http.Redirect(w, r, fmt.Sprintf(target, msg), http.StatusSeeOther)
}
