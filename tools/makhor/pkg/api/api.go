// Package api provides a JSON REST API for makhor.
package api

import (
	"encoding/json"
	"net/http"
	"strconv"
	"strings"

	"makhor/pkg/db"
	"makhor/pkg/middleware"
	"makhor/pkg/models"
)

// API handles all API requests.
type API struct {
	DB *db.DB
}

// New creates a new API handler.
func New(database *db.DB) *API {
	return &API{DB: database}
}

// Response is a standard API response wrapper.
type Response struct {
	Success bool        `json:"success"`
	Data    interface{} `json:"data,omitempty"`
	Error   string      `json:"error,omitempty"`
	Meta    *Meta       `json:"meta,omitempty"`
}

// Meta contains pagination info.
type Meta struct {
	Page       int `json:"page"`
	PerPage    int `json:"per_page"`
	Total      int `json:"total"`
	TotalPages int `json:"total_pages"`
}

// writeJSON writes a JSON response.
func writeJSON(w http.ResponseWriter, status int, data interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(data)
}

// success writes a successful JSON response.
func success(w http.ResponseWriter, data interface{}) {
	writeJSON(w, http.StatusOK, Response{Success: true, Data: data})
}

// successWithMeta writes a successful JSON response with pagination.
func successWithMeta(w http.ResponseWriter, data interface{}, meta *Meta) {
	writeJSON(w, http.StatusOK, Response{Success: true, Data: data, Meta: meta})
}

// errorResponse writes an error JSON response.
func errorResponse(w http.ResponseWriter, status int, message string) {
	writeJSON(w, status, Response{Success: false, Error: message})
}

// Router returns an http.Handler for the API routes.
func (a *API) Router() http.Handler {
	mux := http.NewServeMux()

	// Posts
	mux.HandleFunc("/api/posts", a.handlePosts)
	mux.HandleFunc("/api/posts/", a.handlePost)

	// Comments
	mux.HandleFunc("/api/comments", a.handleComments)
	mux.HandleFunc("/api/comments/", a.handleComment)

	// Tags
	mux.HandleFunc("/api/tags", a.handleTags)
	mux.HandleFunc("/api/tags/", a.handleTag)

	// Users
	mux.HandleFunc("/api/users/", a.handleUser)

	// RSS Feeds (for polling)
	mux.HandleFunc("/api/feeds", a.handleFeeds)
	mux.HandleFunc("/api/feeds/", a.handleFeed)

	return mux
}

// handlePosts handles GET /api/posts
func (a *API) handlePosts(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		errorResponse(w, http.StatusMethodNotAllowed, "Method not allowed")
		return
	}

	page, _ := strconv.Atoi(r.URL.Query().Get("page"))
	if page < 1 {
		page = 1
	}
	perPage, _ := strconv.Atoi(r.URL.Query().Get("per_page"))
	if perPage < 1 || perPage > 100 {
		perPage = 30
	}
	tag := r.URL.Query().Get("tag")
	view := r.URL.Query().Get("view") // hot, top, new

	user := middleware.GetUser(r)
	var userID *int64
	isAdmin := false
	if user != nil {
		userID = &user.ID
		isAdmin = user.IsAdmin
	}

	var posts []*models.Post
	var total int
	var err error

	switch view {
	case "new":
		posts, total, err = a.DB.GetNewestPosts(page, perPage, userID, isAdmin)
	case "top":
		posts, total, err = a.DB.GetTopPosts(page, perPage, 24*7, userID, isAdmin)
	default:
		if tag != "" {
			posts, total, err = a.DB.GetPosts(page, perPage, tag, userID, isAdmin)
		} else {
			posts, total, err = a.DB.GetHotPosts(page, perPage, userID, isAdmin)
		}
	}

	if err != nil {
		errorResponse(w, http.StatusInternalServerError, "Failed to fetch posts")
		return
	}

	pagination := models.NewPagination(page, perPage, total)
	meta := &Meta{
		Page:       pagination.Page,
		PerPage:    pagination.PerPage,
		Total:      pagination.Total,
		TotalPages: pagination.TotalPages,
	}

	successWithMeta(w, posts, meta)
}

// handlePost handles GET/DELETE /api/posts/{id}
func (a *API) handlePost(w http.ResponseWriter, r *http.Request) {
	idStr := strings.TrimPrefix(r.URL.Path, "/api/posts/")
	idStr = strings.Split(idStr, "/")[0]
	id, err := strconv.ParseInt(idStr, 10, 64)
	if err != nil {
		errorResponse(w, http.StatusBadRequest, "Invalid post ID")
		return
	}

	switch r.Method {
	case http.MethodGet:
		user := middleware.GetUser(r)
		var userID *int64
		if user != nil {
			userID = &user.ID
		}

		post, err := a.DB.GetPostByID(id, userID)
		if err != nil {
			errorResponse(w, http.StatusNotFound, "Post not found")
			return
		}
		success(w, post)

	case http.MethodDelete:
		user := middleware.GetUser(r)
		if user == nil {
			errorResponse(w, http.StatusUnauthorized, "Authentication required")
			return
		}

		post, err := a.DB.GetPostByID(id, nil)
		if err != nil {
			errorResponse(w, http.StatusNotFound, "Post not found")
			return
		}

		if post.UserID != user.ID && !user.IsAdmin {
			errorResponse(w, http.StatusForbidden, "Permission denied")
			return
		}

		if err := a.DB.DeletePost(id); err != nil {
			errorResponse(w, http.StatusInternalServerError, "Failed to delete post")
			return
		}
		success(w, map[string]string{"status": "deleted"})

	default:
		errorResponse(w, http.StatusMethodNotAllowed, "Method not allowed")
	}
}

// handleComments handles GET /api/comments
func (a *API) handleComments(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		errorResponse(w, http.StatusMethodNotAllowed, "Method not allowed")
		return
	}

	comments, err := a.DB.GetRecentComments(50)
	if err != nil {
		errorResponse(w, http.StatusInternalServerError, "Failed to fetch comments")
		return
	}

	success(w, comments)
}

// handleComment handles GET/DELETE /api/comments/{id}
func (a *API) handleComment(w http.ResponseWriter, r *http.Request) {
	idStr := strings.TrimPrefix(r.URL.Path, "/api/comments/")
	idStr = strings.Split(idStr, "/")[0]
	id, err := strconv.ParseInt(idStr, 10, 64)
	if err != nil {
		errorResponse(w, http.StatusBadRequest, "Invalid comment ID")
		return
	}

	switch r.Method {
	case http.MethodGet:
		user := middleware.GetUser(r)
		var userID *int64
		if user != nil {
			userID = &user.ID
		}

		comment, err := a.DB.GetCommentByID(id, userID)
		if err != nil {
			errorResponse(w, http.StatusNotFound, "Comment not found")
			return
		}
		success(w, comment)

	case http.MethodDelete:
		user := middleware.GetUser(r)
		if user == nil {
			errorResponse(w, http.StatusUnauthorized, "Authentication required")
			return
		}

		comment, err := a.DB.GetCommentByID(id, nil)
		if err != nil {
			errorResponse(w, http.StatusNotFound, "Comment not found")
			return
		}

		if comment.UserID != user.ID && !user.IsAdmin {
			errorResponse(w, http.StatusForbidden, "Permission denied")
			return
		}

		if err := a.DB.DeleteComment(id); err != nil {
			errorResponse(w, http.StatusInternalServerError, "Failed to delete comment")
			return
		}
		success(w, map[string]string{"status": "deleted"})

	default:
		errorResponse(w, http.StatusMethodNotAllowed, "Method not allowed")
	}
}

// handleTags handles GET /api/tags
func (a *API) handleTags(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		errorResponse(w, http.StatusMethodNotAllowed, "Method not allowed")
		return
	}

	tags, err := a.DB.GetAllTagsWithCounts()
	if err != nil {
		errorResponse(w, http.StatusInternalServerError, "Failed to fetch tags")
		return
	}

	success(w, tags)
}

// handleTag handles GET /api/tags/{name}
func (a *API) handleTag(w http.ResponseWriter, r *http.Request) {
	name := strings.TrimPrefix(r.URL.Path, "/api/tags/")
	name = strings.TrimSuffix(name, "/")

	if r.Method != http.MethodGet {
		errorResponse(w, http.StatusMethodNotAllowed, "Method not allowed")
		return
	}

	tag, err := a.DB.GetTagByNameWithOwnership(name)
	if err != nil {
		errorResponse(w, http.StatusNotFound, "Tag not found")
		return
	}

	success(w, tag)
}

// handleUser handles GET /api/users/{username}
func (a *API) handleUser(w http.ResponseWriter, r *http.Request) {
	username := strings.TrimPrefix(r.URL.Path, "/api/users/")
	username = strings.TrimSuffix(username, "/")

	if r.Method != http.MethodGet {
		errorResponse(w, http.StatusMethodNotAllowed, "Method not allowed")
		return
	}

	user, err := a.DB.GetUserByUsername(username)
	if err != nil {
		errorResponse(w, http.StatusNotFound, "User not found")
		return
	}

	// Return safe user info (no email)
	safeUser := map[string]interface{}{
		"id":         user.ID,
		"username":   user.Username,
		"created_at": user.CreatedAt,
		"is_admin":   user.IsAdmin,
		"about":      user.About,
	}

	success(w, safeUser)
}

// handleFeeds handles GET/POST /api/feeds
func (a *API) handleFeeds(w http.ResponseWriter, r *http.Request) {
	user := middleware.GetUser(r)
	if user == nil {
		errorResponse(w, http.StatusUnauthorized, "Authentication required")
		return
	}
	// Only admins can list all feeds or create new ones via API
	if !user.IsAdmin {
		errorResponse(w, http.StatusForbidden, "Admin access required")
		return
	}

	switch r.Method {
	case http.MethodGet:
		feeds, err := a.DB.GetAllRSSFeeds()
		if err != nil {
			errorResponse(w, http.StatusInternalServerError, "Failed to fetch feeds")
			return
		}
		success(w, feeds)

	case http.MethodPost:
		var req struct {
			URL      string  `json:"url"`
			TagID    int64   `json:"tag_id"`
			Interval int     `json:"interval_minutes"`
			Title    *string `json:"title,omitempty"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			errorResponse(w, http.StatusBadRequest, "Invalid JSON")
			return
		}

		if req.URL == "" || req.TagID == 0 {
			errorResponse(w, http.StatusBadRequest, "url and tag_id are required")
			return
		}
		if req.Interval < 5 {
			req.Interval = 60 // default 1 hour
		}

		title := ""
		if req.Title != nil {
			title = *req.Title
		}

		feed, err := a.DB.CreateRSSFeed(req.URL, title, req.TagID, req.Interval)
		if err != nil {
			errorResponse(w, http.StatusInternalServerError, "Failed to create feed: "+err.Error())
			return
		}
		success(w, feed)

	default:
		errorResponse(w, http.StatusMethodNotAllowed, "Method not allowed")
	}
}

// handleFeed handles GET/DELETE /api/feeds/{id}
func (a *API) handleFeed(w http.ResponseWriter, r *http.Request) {
	user := middleware.GetUser(r)
	if user == nil {
		errorResponse(w, http.StatusUnauthorized, "Authentication required")
		return
	}

	idStr := strings.TrimPrefix(r.URL.Path, "/api/feeds/")
	idStr = strings.Split(idStr, "/")[0]
	id, err := strconv.ParseInt(idStr, 10, 64)
	if err != nil {
		errorResponse(w, http.StatusBadRequest, "Invalid feed ID")
		return
	}

	switch r.Method {
	case http.MethodGet:
		// Anyone authenticated can view feed info
		feed, err := a.DB.GetRSSFeedByID(id)
		if err != nil {
			errorResponse(w, http.StatusNotFound, "Feed not found")
			return
		}
		success(w, feed)

	case http.MethodDelete:
		// Check if user can edit this feed (admin, creator, or tag moderator)
		if !a.DB.CanEditFeed(user.ID, id) {
			errorResponse(w, http.StatusForbidden, "Permission denied")
			return
		}
		if err := a.DB.DeleteRSSFeed(id); err != nil {
			errorResponse(w, http.StatusInternalServerError, "Failed to delete feed")
			return
		}
		success(w, map[string]string{"status": "deleted"})

	default:
		errorResponse(w, http.StatusMethodNotAllowed, "Method not allowed")
	}
}
