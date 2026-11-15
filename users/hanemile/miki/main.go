package main

import (
	"embed"
	"encoding/json"
	"fmt"
	"html/template"
	"log"
	"net"
	"net/http"
	"path/filepath"
	"strings"
	"time"

	"miki/auth"
	"miki/wiki"
)

//go:embed templates/*.html
var templatesFS embed.FS

var (
	viewTmpl        *template.Template
	editTmpl        *template.Template
	listTmpl        *template.Template
	loginTmpl       *template.Template
	histTmpl        *template.Template
	diffTmpl        *template.Template
	commentTmpl     *template.Template
	commentViewTmpl *template.Template
)

func init() {
	// Simple hardcoded admin for now. Nix module will supply via flag or config later.
	auth.Init([]string{"admin", "hanemile"})

	// Ensure repo exists
	if err := wiki.EnsureRepo(wiki.PageDir); err != nil {
		log.Printf("Warning: Failed to ensure repo: %v", err)
	}

	// Init Cache
	wiki.InitCache()

	funcMap := template.FuncMap{
		"dict": func(values ...interface{}) (map[string]interface{}, error) {
			if len(values)%2 != 0 {
				return nil, fmt.Errorf("invalid dict call")
			}
			dict := make(map[string]interface{}, len(values)/2)
			for i := 0; i < len(values); i += 2 {
				key, ok := values[i].(string)
				if !ok {
					return nil, fmt.Errorf("dict keys must be strings")
				}
				dict[key] = values[i+1]
			}
			return dict, nil
		},
	}

	// Helper to parse with func map
	parse := func(files ...string) *template.Template {
		// Use the first file's base name as the template name seems standard for ParseFS behavior emulation if we used New()
		// But ParseFS uses the filenames.
		// template.ParseFS documentation says it returns a template that executes the first file by default?
		// Actually, we usually execute by name "view", "edit" etc defined in the file.
		// Layout defines "layout".
		// We want to associate the FuncMap.
		name := filepath.Base(files[0])
		t := template.New(name).Funcs(funcMap)
		t, err := t.ParseFS(templatesFS, files...)
		if err != nil {
			log.Panic(err)
		}
		return t
	}

	// Parse templates
	viewTmpl = parse("templates/layout.html", "templates/view.html")
	editTmpl = parse("templates/layout.html", "templates/edit.html")
	listTmpl = parse("templates/layout.html", "templates/list.html")
	loginTmpl = parse("templates/layout.html", "templates/login.html")
	histTmpl = parse("templates/layout.html", "templates/history.html")
	diffTmpl = parse("templates/layout.html", "templates/diff.html")
	commentTmpl = parse("templates/layout.html", "templates/comment.html")
	commentViewTmpl = parse("templates/layout.html", "templates/comment_view.html")
}

type PageData struct {
	Title     string
	Body      template.HTML
	Raw       string
	Backlinks []string
	User      string
	IsAdmin   bool
	New       bool
	Pages     []string
	Error     string
	Commits   []wiki.Commit
	Diff      string
	TOC       []wiki.TOCEntry
	Comments  []*wiki.Comment
	// Login specific
	Username   string
	SSHCommand string
	Token      string
	ShowSSH    bool

	// Helper for Comment Form
	TargetName     string
	ContextSnippet string // Preview of what we are replying to/editing
	PageTitle      string // Distinct from Title (which is page title of the view)
	ParentID       string
}

type PageDataWrapper struct {
	TargetName string
}

func main() {
	http.HandleFunc("/list", handleList)
	http.HandleFunc("/auth/reveal", handleReveal)
	http.HandleFunc("/comment", auth.Middleware(handleComment))

	http.HandleFunc("/login", handleLogin)
	http.HandleFunc("/logout", handleLogout)

	// Catch-all for pages
	http.HandleFunc("/", auth.Middleware(handlePage))

	// Start periodic jobs
	go func() {
		wiki.InitCache()
		err := wiki.UpdateSpecialPages()
		if err != nil {
			log.Println(err)
		}
		err = wiki.UpdateAdminPages()
		if err != nil {
			log.Println(err)
		}

		ticker := time.NewTicker(30 * time.Minute)
		defer ticker.Stop()
		for range ticker.C {
			wiki.InitCache()
			err = wiki.UpdateSpecialPages()
			if err != nil {
				log.Println(err)
			}
			err = wiki.UpdateAdminPages()
			if err != nil {
				log.Println(err)
			}
		}
	}()

	log.Println("Server started at :8080")
	log.Fatal(http.ListenAndServe(":8080", nil))
}

func handlePage(w http.ResponseWriter, r *http.Request) {
	// /Title
	title := strings.TrimPrefix(r.URL.Path, "/")
	if title == "" {
		http.Redirect(w, r, "/Home", http.StatusFound)
		return
	}

	query := r.URL.Query()
	if query.Has("edit") {
		doEdit(w, r, title)
		return
	}
	if query.Has("save") {
		doSave(w, r, title)
		return
	}
	if query.Has("delete") {
		doDelete(w, r, title)
		return
	}
	if query.Has("history") {
		doHistory(w, r, title)
		return
	}
	if query.Has("diff") {
		doDiff(w, r, title)
		return
	}
	if query.Has("refresh_cache") {
		wiki.RefreshPageCache(title)
		http.Redirect(w, r, "/"+title, http.StatusFound)
		return
	}

	// Admin actions (only for Admin page and admin users)
	if title == "Admin" {
		user, _ := auth.GetUser(r)
		if !auth.IsAdmin(user) {
			http.Error(w, "Forbidden: Admin access required", http.StatusForbidden)
			return
		}

		var err error

		if query.Has("run_all_jobs") {
			wiki.InitCache()
			err = wiki.UpdateSpecialPages()
			if err != nil {
				log.Println(err)
			}
			err = wiki.UpdateAdminPages()
			if err != nil {
				log.Println(err)
			}
			http.Redirect(w, r, "/Admin", http.StatusFound)
			return
		}
		if query.Has("rebuild_cache") {
			wiki.InitCache()
			err = wiki.UpdateAdminPages()
			if err != nil {
				log.Println(err)
			}
			http.Redirect(w, r, "/Admin", http.StatusFound)
			return
		}
		if query.Has("purge_cache") {
			wiki.PurgeCache()
			err = wiki.UpdateAdminPages()
			if err != nil {
				log.Println(err)
			}
			http.Redirect(w, r, "/Admin", http.StatusFound)
			return
		}
		if query.Has("view_cache_json") {
			w.Header().Set("Content-Type", "application/json")
			cache := wiki.GetCacheStats()
			if err := json.NewEncoder(w).Encode(cache); err != nil {
				http.Error(w, err.Error(), http.StatusInternalServerError)
			}
			return
		}
	}

	// Default: View
	doView(w, r, title)
}

func renderTemplate(w http.ResponseWriter, r *http.Request, tmpl *template.Template, data PageData) {
	user, _ := auth.GetUser(r)
	data.User = user
	data.IsAdmin = auth.IsAdmin(user)
	if err := tmpl.ExecuteTemplate(w, strings.TrimSuffix(tmpl.Name(), ".html"), data); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}

func doView(w http.ResponseWriter, r *http.Request, title string) {
	p, err := wiki.LoadPage(title)
	if err != nil {
		http.Redirect(w, r, "/"+title+"?edit", http.StatusFound)
		return
	}

	renderTemplate(w, r, viewTmpl, PageData{
		Title:     p.Title,
		Body:      template.HTML(p.Body),
		Backlinks: p.Backlinks,
		TOC:       p.TOC,
		Comments:  p.Comments,
	})
}

func handleComment(w http.ResponseWriter, r *http.Request) {
	pageTitle := r.FormValue("page")
	parentID := r.FormValue("parent")
	action := r.FormValue("action")
	user, _ := auth.GetUser(r)

	if action == "delete" {
		if !auth.IsAdmin(user) {
			http.Error(w, "Forbidden", http.StatusForbidden)
			return
		}
		// parentID here acts as CommentID
		if err := wiki.DeleteComment(parentID, user); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		http.Redirect(w, r, "/"+pageTitle, http.StatusFound)
		return
	}

	if action == "history" {
		// parentID here acts as CommentID
		// But files are in comments/ID.md
		relativePath := filepath.Join(wiki.CommentDir, parentID+".md")
		commits, err := wiki.GetFileHistory(relativePath)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}

		renderTemplate(w, r, histTmpl, PageData{
			Title:   "History of Comment " + parentID,
			Commits: commits,
		})
		return
	}

	// Helper to find valid comment
	findComment := func(id string) (*wiki.Comment, error) {
		comments, err := wiki.LoadComments(pageTitle)
		if err != nil {
			return nil, err
		}
		var find func([]*wiki.Comment) *wiki.Comment
		find = func(list []*wiki.Comment) *wiki.Comment {
			for _, c := range list {
				if c.ID == id {
					return c
				}
				if found := find(c.Children); found != nil {
					return found
				}
			}
			return nil
		}
		if found := find(comments); found != nil {
			return found, nil
		}
		return nil, nil // Not found
	}

	if action == "view" {
		targetComment, err := findComment(parentID)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		if targetComment == nil {
			http.Error(w, "Comment not found", http.StatusNotFound)
			return
		}

		// Fetch history for this comment to display in details
		relativePath := filepath.Join(wiki.CommentDir, parentID+".md")
		commits, err := wiki.GetFileHistory(relativePath)
		if err == nil {
			// Attach history to PageData.Commits
		}

		renderTemplate(w, r, commentViewTmpl, PageData{
			Title:     "Comment " + parentID,
			PageTitle: pageTitle,
			Comments:  []*wiki.Comment{targetComment},
			Commits:   commits,
		})
		return
	}

	if action == "diff" {
		hash := r.URL.Query().Get("diff") // or hash param
		relativePath := filepath.Join(wiki.CommentDir, parentID+".md")
		diff, err := wiki.GetFileDiff(relativePath, hash)
		if err != nil {
			http.Error(w, "Failed to get diff: "+err.Error(), http.StatusInternalServerError)
			return
		}

		renderTemplate(w, r, diffTmpl, PageData{
			Title:     "Diff for Comment " + parentID,
			Diff:      diff,
			PageTitle: pageTitle, // To allow back navigation if template uses it
		})
		return
	}

	if action == "edit" {
		targetComment, err := findComment(parentID)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		if targetComment == nil {
			http.Error(w, "Comment not found", http.StatusNotFound)
			return
		}

		// Context: The parent of the comment we are editing
		contextSnippet := ""
		if targetComment.ParentID == pageTitle {
			contextSnippet = "Root Page: " + pageTitle
		} else {
			parentC, _ := findComment(targetComment.ParentID)
			if parentC != nil {
				// Snippet of parent content
				limit := 100
				body := parentC.Content
				if len(body) > limit {
					body = body[:limit] + "..."
				}
				contextSnippet = fmt.Sprintf("Replying solely to %s: \"%s\"", parentC.Author, body)
			} else {
				contextSnippet = "Parent comment not found."
			}
		}

		renderTemplate(w, r, commentTmpl, PageData{
			Title:          "Edit Comment",
			PageTitle:      pageTitle,
			ParentID:       parentID,
			TargetName:     targetComment.Author,
			Raw:            targetComment.Content,
			New:            false,
			ContextSnippet: contextSnippet,
		})
		return
	}

	if r.Method == http.MethodGet {
		// New Comment (Reply)
		contextSnippet := ""
		if parentID == pageTitle {
			contextSnippet = "Root Page: " + pageTitle
		} else {
			// Load parent comment for context
			parentC, _ := findComment(parentID)
			if parentC != nil {
				limit := 100
				body := parentC.Content
				if len(body) > limit {
					body = body[:limit] + "..."
				}
				contextSnippet = fmt.Sprintf("Replying to %s: \"%s\"", parentC.Author, body)
			}
		}

		renderTemplate(w, r, commentTmpl, PageData{
			Title:          "Add Comment",
			TargetName:     parentID,
			PageTitle:      pageTitle,
			ParentID:       parentID,
			New:            true,
			ContextSnippet: contextSnippet,
		})
		return
	}

	if r.Method == http.MethodPost {
		content := r.FormValue("content")

		if action == "update" {
			// Update existing comment
			if err := wiki.UpdateComment(parentID, content, user); err != nil {
				http.Error(w, err.Error(), http.StatusInternalServerError)
				return
			}
			http.Redirect(w, r, "/comment?action=view&page="+pageTitle+"&parent="+parentID, http.StatusFound)
			return
		}

		if err := wiki.SaveComment(pageTitle, parentID, content, user); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}

		http.Redirect(w, r, "/"+pageTitle, http.StatusFound)
		return
	}
}

func doEdit(w http.ResponseWriter, r *http.Request, title string) {
	p, err := wiki.LoadPage(title)
	data := PageData{Title: title}
	if err != nil {
		data.New = true
	} else {
		data.Raw = string(p.Raw)
	}
	renderTemplate(w, r, editTmpl, data)
}

func doSave(w http.ResponseWriter, r *http.Request, title string) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// If title is empty (from /new), get it from form
	if title == "" {
		title = r.FormValue("title")
	}

	content := r.FormValue("content")
	message := r.FormValue("message") // New
	user, _ := auth.GetUser(r)

	if err := wiki.SavePage(title, content, user, message); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	// Trigger updates
	go func() {
		var err error
		err = wiki.UpdateSpecialPages()
		if err != nil {
			log.Println(err)
		}
		err = wiki.UpdateAdminPages()
		if err != nil {
			log.Println(err)
		}
	}()

	http.Redirect(w, r, "/"+title, http.StatusFound)
}

func doDelete(w http.ResponseWriter, r *http.Request, title string) {
	// Accept both to make the Simple [Delete] link work
	user, _ := auth.GetUser(r)
	if !auth.IsAdmin(user) {
		http.Error(w, "Forbidden", http.StatusForbidden)
		return
	}

	if err := wiki.DeletePage(title, user); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	// Trigger updates
	go func() {
		var err error
		err = wiki.UpdateSpecialPages()
		if err != nil {
			log.Println(err)
		}
		err = wiki.UpdateAdminPages()
		if err != nil {
			log.Println(err)
		}
	}()

	http.Redirect(w, r, "/", http.StatusFound)
}

func doHistory(w http.ResponseWriter, r *http.Request, title string) {
	commits, err := wiki.GetPageHistory(title)
	if err != nil {
		http.Error(w, "Failed to get history: "+err.Error(), http.StatusInternalServerError)
		return
	}

	renderTemplate(w, r, histTmpl, PageData{
		Title:   title,
		Commits: commits,
	})
}

func doDiff(w http.ResponseWriter, r *http.Request, title string) {
	hash := r.URL.Query().Get("diff")
	diff, err := wiki.GetDiff(title, hash)
	if err != nil {
		http.Error(w, "Failed to get diff: "+err.Error(), http.StatusInternalServerError)
		return
	}

	renderTemplate(w, r, diffTmpl, PageData{
		Title: title,
		Diff:  diff,
	})
}

func handleList(w http.ResponseWriter, r *http.Request) {
	pages, err := wiki.ListPages()
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	renderTemplate(w, r, listTmpl, PageData{Pages: pages})
}

func handleLogin(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodGet {
		renderTemplate(w, r, loginTmpl, PageData{Title: "Login"})
		return
	}

	action := r.FormValue("action")
	username := r.FormValue("username")

	switch action {
	case "init_ssh":
		token, _ := auth.StartChallenge(username)
		renderTemplate(w, r, loginTmpl, PageData{
			Title:      "Login",
			SSHCommand: fmt.Sprintf("ssh %s@miki.pinto-pike.ts.net \"curl -s 'http://localhost:8080/auth/reveal?token=%s'\"", username, token),
			Token:      token,
			Username:   username,
			ShowSSH:    true,
		})
	case "verify_ssh":
		token := r.FormValue("token")
		code := r.FormValue("code")
		if username, ok := auth.VerifyChallenge(token, code); ok {
			http.SetCookie(w, &http.Cookie{
				Name:  "session_token",
				Value: auth.CreateSession(username),
				Path:  "/",
			})
			http.Redirect(w, r, "/Home", http.StatusFound)
		} else {
			http.Error(w, "Invalid code or token expired", http.StatusUnauthorized)
		}
	default:
		http.Error(w, "Invalid action", http.StatusBadRequest)
	}
}

func handleReveal(w http.ResponseWriter, r *http.Request) {
	host, _, _ := net.SplitHostPort(r.RemoteAddr)
	if host != "127.0.0.1" && host != "::1" && host != "" {
		http.Error(w, "Access denied: must be local", http.StatusForbidden)
		return
	}

	if code, ok := auth.GetChallengeCode(r.URL.Query().Get("token")); ok {
		_, err := w.Write([]byte(code))
		if err != nil {
			log.Println(err)
		}
	} else {
		http.Error(w, "Invalid token", http.StatusNotFound)
	}
}

func handleLogout(w http.ResponseWriter, r *http.Request) {
	if c, err := r.Cookie("session_token"); err == nil {
		auth.Logout(c.Value)
	}
	http.SetCookie(w, &http.Cookie{
		Name:   "session_token",
		Value:  "",
		Path:   "/",
		MaxAge: -1,
	})
	http.Redirect(w, r, "/", http.StatusFound)
}
