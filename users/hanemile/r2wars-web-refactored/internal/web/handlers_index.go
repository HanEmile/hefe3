package web

import "net/http"

// indexHandler renders the landing page.
func (s *Server) indexHandler(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		data := s.baseData(r)
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
		s.render(w, "index", data)
	default:
		http.Redirect(w, r, "/", http.StatusMethodNotAllowed)
	}
}
