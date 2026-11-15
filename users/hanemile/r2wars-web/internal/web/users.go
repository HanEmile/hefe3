package web

import (
	"fmt"
	"log"
	"net/http"
	"net/mail"
	"strconv"

	"github.com/gorilla/mux"

	"git.emile.space/r2wars-web/internal/store"
)

func (s *Server) usersHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Redirect(w, r, "/", http.StatusMethodNotAllowed)
		return
	}

	data := s.newPageData()
	data["pagelink1"] = Link{Name: "user", Target: "/user"}
	data["pagelink1options"] = []Link{
		{Name: "bot", Target: "/bot"},
		{Name: "battle", Target: "/battle"},
	}

	user, ok := s.currentUser(r)
	if !ok {
		http.Redirect(w, r, "/login", http.StatusSeeOther)
		return
	}
	data["user"] = user

	users, err := s.Store.GetAllUsers()
	if err != nil {
		log.Println(err)
		data["err"] = "Could not fetch users"
	}
	data["users"] = users

	s.render(w, "users", data)
}

func (s *Server) userHandler(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.Atoi(mux.Vars(r)["id"])
	if err != nil {
		http.Redirect(w, r, "/user", http.StatusSeeOther)
		return
	}

	if r.Method != http.MethodGet {
		http.Redirect(w, r, "/", http.StatusMethodNotAllowed)
		return
	}

	data := s.newPageData()
	data["pagelink1"] = Link{Name: "user", Target: "/user"}
	data["pagelink1options"] = []Link{
		{Name: "bot", Target: "/bot"},
		{Name: "battle", Target: "/battle"},
	}

	user, ok := s.currentUser(r)
	if !ok {
		http.Redirect(w, r, "/login", http.StatusSeeOther)
		return
	}
	data["user"] = user

	targetUser, err := s.Store.GetUserByID(id)
	if err != nil {
		data["err"] = "Could not find that user"
	} else {
		data["targetUser"] = targetUser
	}
	data["pagelink2"] = Link{Name: targetUser.Name, Target: fmt.Sprintf("/%s", targetUser.Name)}

	allUsers, err := s.Store.GetAllUsers()
	if err != nil {
		log.Println(err)
	}
	var opts []Link
	for _, u := range allUsers {
		opts = append(opts, Link{Name: u.Name, Target: fmt.Sprintf("/%d", u.ID)})
	}
	data["pagelink2options"] = opts

	bots, err := s.Store.GetUserBotsByID(id)
	if err != nil {
		log.Println(err)
		http.Redirect(w, r, "/user", http.StatusSeeOther)
		return
	}
	data["bots"] = bots

	s.render(w, "user", data)
}

func (s *Server) profileHandler(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.Atoi(mux.Vars(r)["id"])
	if err != nil {
		http.Error(w, "400 - Invalid profile id", http.StatusBadRequest)
		return
	}

	username, ok := s.sessionUsername(r)
	if !ok {
		http.Redirect(w, r, "/login", http.StatusSeeOther)
		return
	}

	switch r.Method {
	case http.MethodGet:
		data := s.newPageData()
		data["pagelink1"] = Link{Name: "user", Target: "/user"}
		data["pagelink1options"] = []Link{
			{Name: "bot", Target: "/bot"},
			{Name: "battle", Target: "/battle"},
		}

		targetUser, err := s.Store.GetUserByID(id)
		if err != nil {
			data["err"] = "Error getting user with the given id"
		}

		if username != targetUser.Name {
			data["err"] = "You aren't allowed to edit any user except yourself"
		}

		editingUser, err := s.Store.GetUserByUsername(username)
		if err != nil {
			data["err"] = "Couldn't get a user for that id"
		}
		data["user"] = editingUser
		data["target_user"] = targetUser
		data["username"] = username

		data["pagelink2"] = Link{Name: targetUser.Name, Target: fmt.Sprintf("/%d", id)}
		allUsers, err := s.Store.GetAllUsers()
		if err != nil {
			log.Println(err)
		}
		var opts []Link
		for _, u := range allUsers {
			opts = append(opts, Link{Name: u.Name, Target: fmt.Sprintf("/%d", u.ID)})
		}
		data["pagelink2options"] = opts
		data["pagelink3"] = Link{Name: "profile", Target: "/profile"}

		s.render(w, "profile", data)

	case http.MethodPost:
		targetUser, err := s.Store.GetUserByID(id)
		if err != nil || username != targetUser.Name {
			http.Error(w, "403 - You aren't allowed to edit any user except yourself", http.StatusForbidden)
			return
		}

		r.ParseForm()
		newUsername := r.Form.Get("username")
		newEmail := r.Form.Get("email")

		if len(newUsername) >= 64 {
			http.Error(w, "400 - Please enter a username under 64 chars", http.StatusBadRequest)
			return
		}

		if newEmail != "" {
			if _, err := mail.ParseAddress(newEmail); err != nil {
				http.Error(w, "400 - Please enter a valid email address", http.StatusBadRequest)
				return
			}
			if err := s.Store.UpdateUserEmail(id, newEmail); err != nil {
				if store.IsDuplicateUser(err) {
					http.Error(w, "409 - That email is already in use", http.StatusConflict)
					return
				}
				log.Println(err)
				http.Error(w, "500 - We had problems updating your email", http.StatusInternalServerError)
				return
			}
		}

		if newUsername != "" && newUsername != username {
			if err := s.Store.UpdateUserName(id, newUsername); err != nil {
				if store.IsDuplicateUser(err) {
					http.Error(w, "409 - That username is already taken", http.StatusConflict)
					return
				}
				log.Println(err)
				http.Error(w, "500 - We had problems updating your username", http.StatusInternalServerError)
				return
			}

			sess := s.session(r)
			sess.Values["username"] = newUsername
			if err := sess.Save(r, w); err != nil {
				http.Error(w, err.Error(), http.StatusInternalServerError)
				return
			}
		}

		http.Redirect(w, r, fmt.Sprintf("/user/%d/profile", id), http.StatusSeeOther)

	default:
		http.Redirect(w, r, "/", http.StatusMethodNotAllowed)
	}
}
