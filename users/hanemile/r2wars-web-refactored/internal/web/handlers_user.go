package web

import (
	"fmt"
	"net/http"
	"strconv"

	"github.com/gorilla/mux"
	"golang.org/x/crypto/argon2"
)

// hashPassword applies the same argon2id parameters the original used, keyed by
// the server's configured salt.
func (s *Server) hashPassword(pw string) []byte {
	return argon2.IDKey([]byte(pw), s.Salt, 1, 64*1024, 4, 32)
}

func (s *Server) loginHandler(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		data := s.baseData(r)
		data["pagelink1"] = Link{"login", "/login"}
		data["pagelink1options"] = []Link{
			{Name: "register", Target: "/register"},
		}
		data["pagelinkauth"] = []Link{
			{Name: "register/", Target: "/register"},
		}

		if queryres := r.URL.Query().Get("res"); queryres != "" {
			data["res"] = queryres
		}

		s.render(w, "login", data)

	case http.MethodPost:
		r.ParseForm()
		username := r.Form.Get("username")
		password := r.Form.Get("password")

		if password == "" {
			http.Redirect(w, r, "/login?err=Empty+Password", http.StatusSeeOther)
			return
		}

		passwordHash := s.hashPassword(password)
		if !s.Store.CheckUserPassword(username, passwordHash) {
			http.Redirect(w, r, "/login?err=Invalid+Password", http.StatusSeeOther)
			return
		}

		session, _ := s.Sessions.Get(r, "session")
		session.Values["username"] = username
		if err := session.Save(r, w); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		http.Redirect(w, r, "/", http.StatusSeeOther)

	default:
		http.Redirect(w, r, "/", http.StatusMethodNotAllowed)
	}
}

func (s *Server) registerHandler(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		data := s.baseData(r)
		data["pagelink1"] = Link{"register", "/register"}
		data["pagelink1options"] = []Link{
			{Name: "login", Target: "/login"},
		}
		data["pagelinkauth"] = []Link{
			{Name: "login/", Target: "/login"},
		}
		s.render(w, "register", data)

	case http.MethodPost:
		r.ParseForm()
		username := r.Form.Get("username")
		password1 := r.Form.Get("password1")
		password2 := r.Form.Get("password2")

		if len(username) >= 64 {
			w.WriteHeader(http.StatusInternalServerError)
			w.Write([]byte("500 - Oi', Backend here! Please enter less than 64 chars!"))
			return
		}
		if len(password1) >= 256 {
			w.WriteHeader(http.StatusInternalServerError)
			w.Write([]byte("500 - Oi', Backend here! Don't overdo with the length please!"))
			return
		}
		if password1 != password2 {
			w.WriteHeader(http.StatusInternalServerError)
			w.Write([]byte("500 - Oi', Backend here! The passwords you entered don't match!"))
			return
		}

		// FIX: the original errored when the count lookup *succeeded*; the
		// actual intent is to reject a username that already exists.
		count, err := s.Store.UsernameCount(username)
		if err != nil {
			w.WriteHeader(http.StatusInternalServerError)
			w.Write([]byte("500 - We had problems checking the username"))
			return
		}
		if count > 0 {
			w.WriteHeader(http.StatusInternalServerError)
			w.Write([]byte("500 - Oi', Backend here! That username has already been taken!"))
			return
		}

		if password1 != "" {
			passwordHash := s.hashPassword(password1)
			if _, err := s.Store.InsertUser(username, passwordHash); err != nil {
				w.WriteHeader(http.StatusInternalServerError)
				w.Write([]byte("500 - We had problems inserting you into the DB"))
				return
			}
			http.Redirect(w, r, "/login", http.StatusSeeOther)
		}

	default:
		http.Redirect(w, r, "/", http.StatusMethodNotAllowed)
	}
}

func (s *Server) logoutHandler(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodPost:
		session, _ := s.Sessions.Get(r, "session")
		session.Options.MaxAge = -1
		if err := session.Save(r, w); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
		}
		http.Redirect(w, r, "/", http.StatusSeeOther)
	default:
		http.Redirect(w, r, "/", http.StatusMethodNotAllowed)
	}
}

func (s *Server) userHandler(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	id, err := strconv.Atoi(vars["id"])
	if err != nil {
		http.Redirect(w, r, "/user", http.StatusSeeOther)
		return
	}

	switch r.Method {
	case http.MethodGet:
		data := s.baseData(r)
		data["pagelink1"] = Link{"user", "/user"}
		data["pagelink1options"] = []Link{
			{Name: "bot", Target: "/bot"},
			{Name: "battle", Target: "/battle"},
		}

		if _, ok := s.currentUser(r); !ok {
			http.Redirect(w, r, "/login", http.StatusSeeOther)
			return
		}

		targetUser, err := s.Store.UserByID(id)
		if err != nil {
			data["err"] = "Could not find that user"
		} else {
			data["targetUser"] = targetUser
		}

		data["pagelink2"] = Link{targetUser.Name, fmt.Sprintf("/%s", targetUser.Name)}

		allUsers, _ := s.Store.AllUsers()
		var opts []Link
		for _, u := range allUsers {
			opts = append(opts, Link{Name: u.Name, Target: fmt.Sprintf("/%d", u.ID)})
		}
		data["pagelink2options"] = opts

		bots, err := s.Store.BotsForUserID(id)
		if err != nil {
			http.Redirect(w, r, "/user", http.StatusSeeOther)
			return
		}
		data["bots"] = bots

		s.render(w, "user", data)
	default:
		http.Redirect(w, r, "/", http.StatusMethodNotAllowed)
	}
}

func (s *Server) usersHandler(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		data := s.baseData(r)
		data["pagelink1"] = Link{Name: "user", Target: "/user"}
		data["pagelink1options"] = []Link{
			{Name: "bot", Target: "/bot"},
			{Name: "battle", Target: "/battle"},
		}

		if _, ok := s.currentUser(r); !ok {
			http.Redirect(w, r, "/login", http.StatusSeeOther)
			return
		}

		users, _ := s.Store.AllUsers()
		data["users"] = users

		s.render(w, "users", data)
	default:
		http.Redirect(w, r, "/", http.StatusMethodNotAllowed)
	}
}

func (s *Server) profileHandler(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	id, err := strconv.Atoi(vars["id"])
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		w.Write([]byte("500 - Error reading the profile id"))
		return
	}

	switch r.Method {
	case http.MethodGet:
		data := s.baseData(r)
		data["pagelink1"] = Link{"user", "/user"}
		data["pagelink1options"] = []Link{
			{Name: "bot", Target: "/bot"},
			{Name: "battle", Target: "/battle"},
		}

		username, _ := s.currentUsername(r)

		targetUser, err := s.Store.UserByID(id)
		if err != nil {
			data["err"] = "Error getting with the given id"
		}
		if username != targetUser.Name {
			data["err"] = "You aren't allowed to edit any user except yourself"
		}

		editingUser, err := s.Store.UserByName(username)
		if err != nil {
			data["err"] = "Coulnd't get a user for that id"
		}
		data["user"] = editingUser
		data["target_user"] = targetUser

		data["pagelink2"] = Link{targetUser.Name, fmt.Sprintf("/%d", id)}
		allUsers, _ := s.Store.AllUsers()
		var opts []Link
		for _, u := range allUsers {
			opts = append(opts, Link{Name: u.Name, Target: fmt.Sprintf("/%d", u.ID)})
		}
		data["pagelink2options"] = opts
		data["pagelink3"] = Link{"profile", "/profile"}

		if username != "" {
			data["username"] = username
		}

		s.render(w, "profile", data)

	case http.MethodPost:
		origUsername, _ := s.currentUsername(r)

		r.ParseForm()
		newUsername := r.Form.Get("username")
		password1 := r.Form.Get("password1")
		password2 := r.Form.Get("password2")

		if len(newUsername) >= 64 {
			w.WriteHeader(http.StatusInternalServerError)
			w.Write([]byte("500 - Oi', Backend here! Please enter less than 64 chars!"))
			return
		}
		if len(password1) >= 256 {
			w.WriteHeader(http.StatusInternalServerError)
			w.Write([]byte("500 - Oi', Backend here! Don't overdo with the length please!"))
			return
		}
		if password1 != password2 {
			w.WriteHeader(http.StatusInternalServerError)
			w.Write([]byte("500 - Oi', Backend here! The passwords you entered don't match!"))
			return
		}

		// update password first, since the username might change too
		if password1 != "" {
			passwordHash := s.hashPassword(password1)
			if err := s.Store.UpdateUserPasswordHash(origUsername, passwordHash); err != nil {
				w.WriteHeader(http.StatusInternalServerError)
				w.Write([]byte("500 - We had problems inserting your new pw into the DB"))
				return
			}
		}

		if newUsername != "" {
			if err := s.Store.UpdateUsername(id, newUsername); err != nil {
				w.WriteHeader(http.StatusInternalServerError)
				w.Write([]byte("500 - We had problems inserting your new uname into the DB"))
				return
			}
			session, _ := s.Sessions.Get(r, "session")
			session.Values["username"] = newUsername
			if err := session.Save(r, w); err != nil {
				http.Error(w, err.Error(), http.StatusInternalServerError)
				return
			}
		}

		http.Redirect(w, r, fmt.Sprintf("/user/%d/profile", id), http.StatusSeeOther)

	default:
		http.Redirect(w, r, "/", http.StatusMethodNotAllowed)
	}
}
