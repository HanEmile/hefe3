package web

import "net/http"

func (s *Server) indexHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Redirect(w, r, "/", http.StatusMethodNotAllowed)
		return
	}

	data := s.newPageData()
	data["pagelink1"] = ""
	data["pagelinknext"] = []Link{
		{Name: "user/", Target: "/user"},
		{Name: "bot/", Target: "/bot"},
		{Name: "battle/", Target: "/battle"},
	}
	data["pagelinkauth"] = []Link{
		{Name: "login/", Target: "/login"},
		{Name: "register/", Target: "/register"},
	}

	if user, ok := s.currentUser(r); ok {
		data["logged_in"] = true
		data["user"] = user
	}

	s.render(w, "index", data)
}
