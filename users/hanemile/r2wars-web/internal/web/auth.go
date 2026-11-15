package web

import (
	"log"
	"net/http"
	"net/mail"
	"time"

	"git.emile.space/r2wars-web/internal/model"
	"git.emile.space/r2wars-web/internal/store"
	"git.emile.space/r2wars-web/internal/token"
)

// There is no password: logging in means proving you control the email on
// file by clicking a one-time link mailed to it. loginHandler issues the
// link, loginVerifyHandler consumes it.

func (s *Server) loginHandler(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		data := s.newPageData()
		data["pagelink1"] = Link{Name: "login", Target: "/login"}
		data["pagelink1options"] = []Link{
			{Name: "register", Target: "/register"},
		}
		data["pagelinkauth"] = []Link{
			{Name: "register/", Target: "/register"},
		}

		if user, ok := s.currentUser(r); ok {
			data["user"] = user
			data["logged_in"] = true
		}

		if msg := r.URL.Query().Get("res"); msg != "" {
			data["res"] = msg
		}

		s.render(w, "login", data)

	case http.MethodPost:
		r.ParseForm()
		email := r.Form.Get("email")
		if email == "" {
			http.Redirect(w, r, "/login?res=Please+enter+your+email", http.StatusSeeOther)
			return
		}

		user, err := s.Store.GetUserByEmail(email)
		if err != nil {
			if err != store.ErrNotFound {
				log.Println(err)
			}
			// Same message whether or not the address is registered: a
			// different one would let anyone enumerate who has an account.
			http.Redirect(w, r, "/login?res=If+that+email+is+registered%2C+a+login+link+is+on+its+way.", http.StatusSeeOther)
			return
		}

		if err := s.sendLoginLink(user); err != nil {
			log.Println(err)
			http.Redirect(w, r, "/login?res=ERROR:+Could+not+send+the+login+email", http.StatusSeeOther)
			return
		}

		http.Redirect(w, r, "/login?res=If+that+email+is+registered%2C+a+login+link+is+on+its+way.", http.StatusSeeOther)

	default:
		http.Redirect(w, r, "/", http.StatusMethodNotAllowed)
	}
}

// loginVerifyHandler consumes a magic-link token from the query string and,
// if valid and unexpired, starts a session for the user it belongs to.
func (s *Server) loginVerifyHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Redirect(w, r, "/", http.StatusMethodNotAllowed)
		return
	}

	raw := r.URL.Query().Get("token")
	if raw == "" {
		http.Redirect(w, r, "/login?res=Missing+login+token", http.StatusSeeOther)
		return
	}

	userID, err := s.Store.ConsumeMagicLink(token.Hash(raw))
	if err != nil {
		if err != store.ErrNotFound {
			log.Println(err)
		}
		http.Redirect(w, r, "/login?res=That+login+link+is+invalid+or+has+expired", http.StatusSeeOther)
		return
	}

	user, err := s.Store.GetUserByID(userID)
	if err != nil {
		log.Println(err)
		http.Redirect(w, r, "/login?res=ERROR:+Could+not+find+your+account", http.StatusSeeOther)
		return
	}

	sess := s.session(r)
	sess.Values["username"] = user.Name
	if err := sess.Save(r, w); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	http.Redirect(w, r, "/", http.StatusSeeOther)
}

func (s *Server) registerHandler(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		data := s.newPageData()
		data["pagelink1"] = Link{Name: "register", Target: "/register"}
		data["pagelink1options"] = []Link{
			{Name: "login", Target: "/login"},
		}
		data["pagelinkauth"] = []Link{
			{Name: "login/", Target: "/login"},
		}
		if _, ok := s.currentUser(r); ok {
			data["logged_in"] = true
		}
		if msg := r.URL.Query().Get("res"); msg != "" {
			data["res"] = msg
		}
		s.render(w, "register", data)

	case http.MethodPost:
		r.ParseForm()
		username := r.Form.Get("username")
		email := r.Form.Get("email")

		if username == "" {
			http.Redirect(w, r, "/register?res=ERROR:+Please+provide+a+username", http.StatusSeeOther)
			return
		}
		if len(username) >= 64 {
			http.Redirect(w, r, "/register?res=ERROR:+Username+must+be+under+64+chars", http.StatusSeeOther)
			return
		}
		if _, err := mail.ParseAddress(email); err != nil {
			http.Redirect(w, r, "/register?res=ERROR:+Please+provide+a+valid+email+address", http.StatusSeeOther)
			return
		}

		id, err := s.Store.InsertUser(model.User{Name: username, Email: email})
		if store.IsDuplicateUser(err) {
			http.Redirect(w, r, "/register?res=ERROR:+That+username+or+email+is+already+registered", http.StatusSeeOther)
			return
		}
		if err != nil {
			log.Println(err)
			http.Redirect(w, r, "/register?res=ERROR:+Could+not+create+your+account", http.StatusSeeOther)
			return
		}

		if err := s.sendLoginLink(model.User{ID: id, Name: username, Email: email}); err != nil {
			log.Println(err)
			http.Redirect(w, r, "/login?res=Account+created+-+but+sending+the+login+email+failed%2C+please+try+logging+in+again+shortly", http.StatusSeeOther)
			return
		}

		http.Redirect(w, r, "/login?res=Check+your+email+for+a+login+link!", http.StatusSeeOther)

	default:
		http.Redirect(w, r, "/", http.StatusMethodNotAllowed)
	}
}

// sendLoginLink generates a fresh single-use token for user, stores its
// hash, and emails the raw token as a clickable link.
func (s *Server) sendLoginLink(user model.User) error {
	raw, hash, err := token.New()
	if err != nil {
		return err
	}
	if err := s.Store.CreateMagicLink(user.ID, hash, time.Now().Add(s.MagicLinkTTL)); err != nil {
		return err
	}

	link := s.BaseURL + "/login/verify?token=" + raw
	body := "Hi " + user.Name + ",\n\n" +
		"Click this link to log in to r2wars:\n\n" +
		link + "\n\n" +
		"This link expires in " + s.MagicLinkTTL.String() + " and can only be used once. " +
		"If you didn't request this, you can ignore this email.\n"

	return s.Mailer.Send(user.Email, "Your r2wars login link", body)
}

func (s *Server) logoutHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Redirect(w, r, "/", http.StatusMethodNotAllowed)
		return
	}
	sess := s.session(r)
	sess.Options.MaxAge = -1
	if err := sess.Save(r, w); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	http.Redirect(w, r, "/", http.StatusSeeOther)
}
