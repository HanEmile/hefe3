// Package handlers provides HTTP request handlers for the web application.
package handlers

import (
	"fmt"
	"html/template"
	"io/fs"
	"log"
	"makhor/pkg/db"
	"makhor/pkg/middleware"
	"makhor/pkg/models"
	"makhor/templates"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

// Pagination and display constants.
const (
	DefaultPostsPerPage = 30
	AdminLogsPerPage    = 50
	RecentCommentsLimit = 50
	MaxCommentDepth     = 10
)

// User profile sections.
const (
	SectionPosts    = "posts"
	SectionComments = "comments"
	SectionSettings = "settings"
	SectionInvites  = "invites"
	SectionHats     = "hats"
)

// RSSPoller is an interface for RSS feed polling operations.
type RSSPoller interface {
	PollFeedNow(feedID int64) error
}

// Handler contains all HTTP handlers and their dependencies.
type Handler struct {
	DB        *db.DB
	Templates *template.Template
	BaseURL   string // Base URL for email links
	Subpath   string // URL subpath (e.g., "/makhor" to serve at example.com/makhor/)
	MailFunc  func(to, subject, body string) error
	Poller    RSSPoller
}

// New creates a new Handler with embedded templates.
// Templates are compiled into the binary at build time.
func New(database *db.DB, baseURL, subpath string, mailFunc func(to, subject, body string) error) (*Handler, error) {
	// Parse all templates with custom functions
	// Note: url function is added dynamically in render() with the correct subpath
	funcMap := template.FuncMap{
		"timeAgo":      timeAgo,
		"truncate":     truncate,
		"add":          func(a, b int) int { return a + b },
		"sub":          func(a, b int) int { return a - b },
		"mul":          func(a, b int) int { return a * b },
		"seq":          seq,
		"indent":       indent,
		"formatTime":   formatTime,
		"join":         strings.Join,
		"hasPrefix":    strings.HasPrefix,
		"shortTagName": shortTagName,
	}

	// Load embedded templates
	tmpl := template.New("").Funcs(funcMap)
	err := fs.WalkDir(templates.FS, ".", func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() || !strings.HasSuffix(path, ".html") {
			return nil
		}
		content, err := fs.ReadFile(templates.FS, path)
		if err != nil {
			return fmt.Errorf("reading embedded template %s: %w", path, err)
		}
		_, err = tmpl.Parse(string(content))
		if err != nil {
			return fmt.Errorf("parsing embedded template %s: %w", path, err)
		}
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("loading embedded templates: %w", err)
	}
	log.Printf("Loaded embedded templates")

	return &Handler{
		DB:        database,
		Templates: tmpl,
		BaseURL:   baseURL,
		Subpath:   subpath,
		MailFunc:  mailFunc,
	}, nil
}

// render renders a template with common data.
func (h *Handler) render(w http.ResponseWriter, r *http.Request, name string, data map[string]interface{}) {
	if data == nil {
		data = make(map[string]interface{})
	}

	// Add common data
	data["User"] = middleware.GetUser(r)
	data["Path"] = r.URL.Path
	data["Subpath"] = h.Subpath

	w.Header().Set("Content-Type", "text/html; charset=utf-8")

	if err := h.Templates.ExecuteTemplate(w, name, data); err != nil {
		log.Printf("Error rendering template %s: %v", name, err)
		http.Error(w, "Internal Server Error", http.StatusInternalServerError)
	}
}

// renderError renders an error page.
func (h *Handler) renderError(w http.ResponseWriter, r *http.Request, status int, message string) {
	w.WriteHeader(status)
	h.render(w, r, "error.html", map[string]interface{}{
		"Status":  status,
		"Message": message,
	})
}

// redirect redirects to a URL, prepending the subpath if configured.
func (h *Handler) redirect(w http.ResponseWriter, r *http.Request, redirectURL string) {
	// Prepend subpath for relative URLs
	if h.Subpath != "" && strings.HasPrefix(redirectURL, "/") {
		redirectURL = h.Subpath + redirectURL
	}
	http.Redirect(w, r, redirectURL, http.StatusSeeOther)
}

// safeRedirectURL validates a URL for safe redirection to prevent open redirect attacks.
// Returns the URL if it's safe (same host or relative path), empty string otherwise.
func safeRedirectURL(referer string, r *http.Request) string {
	if referer == "" {
		return ""
	}

	parsed, err := url.Parse(referer)
	if err != nil {
		return ""
	}

	// Allow relative URLs (no host)
	if parsed.Host == "" {
		return referer
	}

	// Only allow same host redirects
	if parsed.Host == r.Host {
		return referer
	}

	return ""
}

// getIntParam gets an integer parameter from the URL query.
func getIntParam(r *http.Request, name string, defaultVal int) int {
	val := r.URL.Query().Get(name)
	if val == "" {
		return defaultVal
	}
	i, err := strconv.Atoi(val)
	if err != nil || i < 1 {
		return defaultVal
	}
	return i
}

// getPathID extracts an ID from URL path like /posts/123
func getPathID(r *http.Request, prefix string) (int64, error) {
	path := strings.TrimPrefix(r.URL.Path, prefix)
	path = strings.TrimSuffix(path, "/")
	path = strings.Split(path, "/")[0]
	return strconv.ParseInt(path, 10, 64)
}

// timeAgo returns a human-readable time difference.
func timeAgo(t time.Time) string {
	diff := time.Since(t)

	switch {
	case diff < time.Minute:
		return "just now"
	case diff < time.Hour:
		mins := int(diff.Minutes())
		if mins == 1 {
			return "1 minute ago"
		}
		return fmt.Sprintf("%d minutes ago", mins)
	case diff < 24*time.Hour:
		hours := int(diff.Hours())
		if hours == 1 {
			return "1 hour ago"
		}
		return fmt.Sprintf("%d hours ago", hours)
	case diff < 30*24*time.Hour:
		days := int(diff.Hours() / 24)
		if days == 1 {
			return "1 day ago"
		}
		return fmt.Sprintf("%d days ago", days)
	default:
		return t.Format("Jan 2, 2006")
	}
}

// truncate truncates a string to a maximum length.
func truncate(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen-3] + "..."
}

// seq generates a sequence of integers for pagination.
func seq(start, end int) []int {
	if start > end {
		return nil
	}
	result := make([]int, end-start+1)
	for i := range result {
		result[i] = start + i
	}
	return result
}

// indent returns spaces for comment indentation.
func indent(depth int) string {
	if depth > 10 {
		depth = 10 // Max indent
	}
	return strings.Repeat("  ", depth)
}

// formatTime formats a time for display.
func formatTime(t time.Time) string {
	return t.Format("2006-01-02 15:04")
}

// shortTagName returns just the last segment of a hierarchical tag name.
// e.g., "compsci::programming::go" returns "go"
func shortTagName(name string) string {
	if idx := strings.LastIndex(name, "::"); idx != -1 {
		return name[idx+2:]
	}
	return name
}

// logAction is a helper to log user actions.
func (h *Handler) logAction(r *http.Request, action, targetType string, targetID int64, details string) {
	user := middleware.GetUser(r)
	var userID *int64
	if user != nil {
		userID = &user.ID
	}
	ip := middleware.GetClientIP(r)

	if err := h.DB.LogAction(userID, action, targetType, targetID, details, ip); err != nil {
		log.Printf("Error logging action: %v", err)
	}
}

// requireLogin checks if user is logged in and redirects to login if not.
// Returns the user if logged in, nil otherwise (after redirect).
func (h *Handler) requireLogin(w http.ResponseWriter, r *http.Request) *models.User {
	user := middleware.GetUser(r)
	if user == nil {
		h.redirect(w, r, "/login")
		return nil
	}
	return user
}

// requireAdmin checks if user is logged in and is an admin.
// Returns the user if admin, nil otherwise (after rendering error).
func (h *Handler) requireAdmin(w http.ResponseWriter, r *http.Request) *models.User {
	user := middleware.GetUser(r)
	if user == nil || !user.IsAdmin {
		h.renderError(w, r, http.StatusForbidden, "Forbidden")
		return nil
	}
	return user
}

// getUserContext returns the user ID pointer and admin status for the current request.
func getUserContext(r *http.Request) (userID *int64, isAdmin bool) {
	user := middleware.GetUser(r)
	if user != nil {
		userID = &user.ID
		isAdmin = user.IsAdmin
	}
	return
}

// verifyUserHat checks if the user owns the hat with the given ID string.
// Returns the hat ID pointer if valid and owned, nil otherwise.
func (h *Handler) verifyUserHat(hatIDStr string, userID int64) *int64 {
	if hatIDStr == "" {
		return nil
	}
	hid, err := strconv.ParseInt(hatIDStr, 10, 64)
	if err != nil {
		return nil
	}
	if h.DB.UserOwnsHat(userID, hid) {
		return &hid
	}
	return nil
}

// MetricsHandler returns Prometheus-formatted metrics. Admin-only.
func (h *Handler) MetricsHandler(w http.ResponseWriter, r *http.Request) {
	user := middleware.GetUser(r)
	if user == nil || !user.IsAdmin {
		http.Error(w, "Forbidden", http.StatusForbidden)
		return
	}

	stats, err := h.DB.GetStats()
	if err != nil {
		log.Printf("Error getting stats: %v", err)
		http.Error(w, "Internal Server Error", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "text/plain; version=0.0.4; charset=utf-8")

	fmt.Fprintln(w, "# HELP makhor_users_total Total number of users")
	fmt.Fprintln(w, "# TYPE makhor_users_total gauge")
	fmt.Fprintf(w, "makhor_users_total %d\n", stats.Users)

	fmt.Fprintln(w, "# HELP makhor_posts_total Total number of posts")
	fmt.Fprintln(w, "# TYPE makhor_posts_total gauge")
	fmt.Fprintf(w, "makhor_posts_total %d\n", stats.Posts)

	fmt.Fprintln(w, "# HELP makhor_comments_total Total number of comments")
	fmt.Fprintln(w, "# TYPE makhor_comments_total gauge")
	fmt.Fprintf(w, "makhor_comments_total %d\n", stats.Comments)

	fmt.Fprintln(w, "# HELP makhor_tags_total Total number of tags")
	fmt.Fprintln(w, "# TYPE makhor_tags_total gauge")
	fmt.Fprintf(w, "makhor_tags_total %d\n", stats.Tags)

	fmt.Fprintln(w, "# HELP makhor_invites_pending Number of unused invites")
	fmt.Fprintln(w, "# TYPE makhor_invites_pending gauge")
	fmt.Fprintf(w, "makhor_invites_pending %d\n", stats.InvitesPending)

	fmt.Fprintln(w, "# HELP makhor_invites_used Number of used invites")
	fmt.Fprintln(w, "# TYPE makhor_invites_used gauge")
	fmt.Fprintf(w, "makhor_invites_used %d\n", stats.InvitesUsed)

	fmt.Fprintln(w, "# HELP makhor_sessions_active Number of active sessions")
	fmt.Fprintln(w, "# TYPE makhor_sessions_active gauge")
	fmt.Fprintf(w, "makhor_sessions_active %d\n", stats.Sessions)

	fmt.Fprintln(w, "# HELP makhor_posts_by_source Number of posts by source")
	fmt.Fprintln(w, "# TYPE makhor_posts_by_source gauge")
	for source, count := range stats.PostsBySource {
		fmt.Fprintf(w, "makhor_posts_by_source{source=\"%s\"} %d\n", source, count)
	}

	fmt.Fprintln(w, "# HELP makhor_database_size_bytes Database size in bytes")
	fmt.Fprintln(w, "# TYPE makhor_database_size_bytes gauge")
	fmt.Fprintf(w, "makhor_database_size_bytes %d\n", stats.DatabaseSize)

	fmt.Fprintln(w, "# HELP makhor_uptime_seconds Server uptime in seconds")
	fmt.Fprintln(w, "# TYPE makhor_uptime_seconds gauge")
	fmt.Fprintf(w, "makhor_uptime_seconds %.0f\n", stats.Uptime.Seconds())
}
