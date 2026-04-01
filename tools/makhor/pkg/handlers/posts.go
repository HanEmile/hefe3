// Post handlers: listing, creating, viewing posts.
package handlers

import (
	"fmt"
	"makhor/pkg/middleware"
	"makhor/pkg/models"
	"net/http"
	"net/url"
	"strconv"
	"strings"
)

// TimeFilters defines time filter presets in hours for top posts sorting.
var TimeFilters = map[string]int{
	"24h":  24,
	"7d":   24 * 7,
	"30d":  24 * 30,
	"90d":  24 * 90,
	"180d": 24 * 180,
	"1y":   24 * 365,
	"all":  0,
}

// HomePage shows the main post listing with support for hot/top/new views.
func (h *Handler) HomePage(w http.ResponseWriter, r *http.Request) {
	page := getIntParam(r, "page", 1)
	perPage := DefaultPostsPerPage
	tag := r.URL.Query().Get("tag")
	view := r.URL.Query().Get("view") // "hot", "top", "new" or "" (default hot)
	period := r.URL.Query().Get("t")  // time period for "top" view

	// Handle /newest URL as view=new
	if r.URL.Path == "/newest" {
		view = "new"
	}

	userID, isAdmin := getUserContext(r)

	var posts []*models.Post
	var total int
	var err error

	switch view {
	case "new":
		// Newest posts first
		posts, total, err = h.DB.GetNewestPosts(page, perPage, userID, isAdmin)
	case "top":
		// Top posts by score with time filter
		hours := 24 * 7 // default to 7 days
		if hr, ok := TimeFilters[period]; ok {
			hours = hr
		}
		posts, total, err = h.DB.GetTopPosts(page, perPage, hours, userID, isAdmin)
	default:
		// Default: hot posts (score-weighted recent) or filtered by tag
		view = "hot" // normalize
		if tag != "" {
			posts, total, err = h.DB.GetPosts(page, perPage, tag, userID, isAdmin)
		} else {
			posts, total, err = h.DB.GetHotPosts(page, perPage, userID, isAdmin)
		}
	}

	if err != nil {
		h.renderError(w, r, http.StatusInternalServerError, "Could not load posts")
		return
	}

	pagination := models.NewPagination(page, perPage, total)

	h.render(w, r, "home.html", map[string]interface{}{
		"Posts":      posts,
		"Pagination": pagination,
		"ActiveTag":  tag,
		"View":       view,
		"Period":     period,
	})
}

// NewestPage shows posts sorted by newest first.
func (h *Handler) NewestPage(w http.ResponseWriter, r *http.Request) {
	page := getIntParam(r, "page", 1)
	perPage := DefaultPostsPerPage

	userID, isAdmin := getUserContext(r)

	posts, total, err := h.DB.GetNewestPosts(page, perPage, userID, isAdmin)
	if err != nil {
		h.renderError(w, r, http.StatusInternalServerError, "Could not load posts")
		return
	}

	pagination := models.NewPagination(page, perPage, total)

	h.render(w, r, "home.html", map[string]interface{}{
		"Posts":      posts,
		"Pagination": pagination,
		"View":       "new",
	})
}

// SubmitPage shows the post submission form.
func (h *Handler) SubmitPage(w http.ResponseWriter, r *http.Request) {
	user := h.requireLogin(w, r)
	if user == nil {
		return
	}

	tags, _ := h.DB.GetAllTags()
	hats, _ := h.DB.GetUserHats(user.ID)

	h.render(w, r, "submit.html", map[string]interface{}{
		"Tags": tags,
		"Hats": hats,
	})
}

// SubmitPost handles post submission.
func (h *Handler) SubmitPost(w http.ResponseWriter, r *http.Request) {
	user := h.requireLogin(w, r)
	if user == nil {
		return
	}

	if err := r.ParseForm(); err != nil {
		h.renderError(w, r, http.StatusBadRequest, "Invalid form data")
		return
	}

	title := strings.TrimSpace(r.FormValue("title"))
	postURL := strings.TrimSpace(r.FormValue("url"))
	body := strings.TrimSpace(r.FormValue("body"))
	tagsInput := strings.TrimSpace(r.FormValue("tags"))
	hatIDStr := r.FormValue("hat_id")

	// Validation
	var errors []string

	if len(title) < 3 || len(title) > 200 {
		errors = append(errors, "Title must be between 3 and 200 characters")
	}

	if postURL != "" {
		if _, err := url.ParseRequestURI(postURL); err != nil {
			errors = append(errors, "Invalid URL")
		}
	}

	if postURL == "" && body == "" {
		errors = append(errors, "Either URL or body text is required")
	}

	// Parse comma-separated tag names
	var tagIDs []int64
	var invalidTags []string
	if tagsInput != "" {
		tagNames := strings.Split(tagsInput, ",")
		for _, name := range tagNames {
			name = strings.TrimSpace(name)
			if name == "" {
				continue
			}
			tag, err := h.DB.GetTagByName(name)
			if err != nil {
				invalidTags = append(invalidTags, name)
			} else {
				tagIDs = append(tagIDs, tag.ID)
			}
		}
	}

	if len(invalidTags) > 0 {
		errors = append(errors, fmt.Sprintf("Unknown tag(s): %s", strings.Join(invalidTags, ", ")))
	}

	if len(tagIDs) == 0 {
		errors = append(errors, "At least one valid tag is required")
	}

	// Parse and validate hat ID
	hatID := h.verifyUserHat(hatIDStr, user.ID)

	if len(errors) > 0 {
		tags, _ := h.DB.GetAllTags()
		hats, _ := h.DB.GetUserHats(user.ID)
		h.render(w, r, "submit.html", map[string]interface{}{
			"Errors":       errors,
			"Title":        title,
			"URL":          postURL,
			"Body":         body,
			"Tags":         tags,
			"Hats":         hats,
			"SelectedTags": tagsInput,
		})
		return
	}

	post, err := h.DB.CreatePostWithHat(user.ID, title, postURL, body, tagIDs, hatID)
	if err != nil {
		h.renderError(w, r, http.StatusInternalServerError, "Could not create post")
		return
	}

	h.logAction(r, models.ActionPostCreate, "post", post.ID, title)

	h.redirect(w, r, fmt.Sprintf("/posts/%d", post.ID))
}

// ViewPost shows a single post with comments.
func (h *Handler) ViewPost(w http.ResponseWriter, r *http.Request) {
	postID, err := getPathID(r, "/posts/")
	if err != nil {
		h.renderError(w, r, http.StatusBadRequest, "Invalid post ID")
		return
	}

	user := middleware.GetUser(r)
	userID, _ := getUserContext(r)

	post, err := h.DB.GetPostByID(postID, userID)
	if err != nil {
		h.renderError(w, r, http.StatusNotFound, "Post not found")
		return
	}

	if post.IsDeleted && (user == nil || (!user.IsAdmin && user.ID != post.UserID)) {
		h.renderError(w, r, http.StatusNotFound, "Post not found")
		return
	}

	comments, err := h.DB.GetPostComments(postID, userID)
	if err != nil {
		h.renderError(w, r, http.StatusInternalServerError, "Could not load comments")
		return
	}

	// Get user's hats if logged in
	var hats []*models.Hat
	if user != nil {
		hats, _ = h.DB.GetUserHats(user.ID)
	}

	// Check if user can moderate any tag on this post
	canModerate := false
	if user != nil {
		if user.IsAdmin {
			canModerate = true
		} else {
			for _, tag := range post.Tags {
				if h.DB.CanModerateTag(tag.ID, user.ID) {
					canModerate = true
					break
				}
			}
		}
	}

	// Get revision count for edit history
	revisionCount := h.DB.GetPostRevisionCount(postID)

	h.render(w, r, "post.html", map[string]interface{}{
		"Post":          post,
		"Comments":      comments,
		"Hats":          hats,
		"CanModerate":   canModerate,
		"RevisionCount": revisionCount,
	})
}

// EditPostPage shows the post edit form.
func (h *Handler) EditPostPage(w http.ResponseWriter, r *http.Request) {
	user := h.requireLogin(w, r)
	if user == nil {
		return
	}

	postID, err := getPathID(r, "/posts/")
	if err != nil {
		h.renderError(w, r, http.StatusBadRequest, "Invalid post ID")
		return
	}

	post, err := h.DB.GetPostByID(postID, nil)
	if err != nil {
		h.renderError(w, r, http.StatusNotFound, "Post not found")
		return
	}

	if post.UserID != user.ID && !user.IsAdmin {
		h.renderError(w, r, http.StatusForbidden, "You cannot edit this post")
		return
	}

	h.render(w, r, "edit_post.html", map[string]interface{}{
		"Post": post,
	})
}

// EditPostSubmit handles post edit submission.
func (h *Handler) EditPostSubmit(w http.ResponseWriter, r *http.Request) {
	user := h.requireLogin(w, r)
	if user == nil {
		return
	}

	postID, err := getPathID(r, "/posts/")
	if err != nil {
		h.renderError(w, r, http.StatusBadRequest, "Invalid post ID")
		return
	}

	post, err := h.DB.GetPostByID(postID, nil)
	if err != nil {
		h.renderError(w, r, http.StatusNotFound, "Post not found")
		return
	}

	if post.UserID != user.ID && !user.IsAdmin {
		h.renderError(w, r, http.StatusForbidden, "You cannot edit this post")
		return
	}

	if err := r.ParseForm(); err != nil {
		h.renderError(w, r, http.StatusBadRequest, "Invalid form data")
		return
	}

	title := strings.TrimSpace(r.FormValue("title"))
	body := strings.TrimSpace(r.FormValue("body"))

	if len(title) < 3 || len(title) > 200 {
		h.render(w, r, "edit_post.html", map[string]interface{}{
			"Post":  post,
			"Error": "Title must be between 3 and 200 characters",
		})
		return
	}

	// Only save revision if content actually changed
	if title != post.Title || body != post.Body {
		if err := h.DB.UpdatePostWithRevision(postID, user.ID, post.Title, post.Body, title, body); err != nil {
			h.renderError(w, r, http.StatusInternalServerError, "Could not update post")
			return
		}
	}

	h.logAction(r, models.ActionPostUpdate, "post", postID, "")

	h.redirect(w, r, fmt.Sprintf("/posts/%d", postID))
}

// DeletePost handles post deletion.
func (h *Handler) DeletePost(w http.ResponseWriter, r *http.Request) {
	user := h.requireLogin(w, r)
	if user == nil {
		return
	}

	postID, err := getPathID(r, "/posts/")
	if err != nil {
		h.renderError(w, r, http.StatusBadRequest, "Invalid post ID")
		return
	}

	post, err := h.DB.GetPostByID(postID, nil)
	if err != nil {
		h.renderError(w, r, http.StatusNotFound, "Post not found")
		return
	}

	if post.UserID != user.ID && !user.IsAdmin {
		h.renderError(w, r, http.StatusForbidden, "You cannot delete this post")
		return
	}

	if err := h.DB.DeletePost(postID); err != nil {
		h.renderError(w, r, http.StatusInternalServerError, "Could not delete post")
		return
	}

	h.logAction(r, models.ActionPostDelete, "post", postID, post.Title)

	h.redirect(w, r, "/")
}

// VotePost handles voting on a post.
func (h *Handler) VotePost(w http.ResponseWriter, r *http.Request) {
	user := h.requireLogin(w, r)
	if user == nil {
		return
	}

	postID, err := getPathID(r, "/posts/")
	if err != nil {
		h.renderError(w, r, http.StatusBadRequest, "Invalid post ID")
		return
	}

	_, voted, err := h.DB.VotePost(user.ID, postID)
	if err != nil {
		h.renderError(w, r, http.StatusInternalServerError, "Could not vote")
		return
	}

	if voted {
		h.logAction(r, models.ActionVotePost, "post", postID, "upvote")
	}

	// Redirect back to referrer or post page (with open redirect protection)
	if safeURL := safeRedirectURL(r.Header.Get("Referer"), r); safeURL != "" {
		h.redirect(w, r, safeURL)
	} else {
		h.redirect(w, r, fmt.Sprintf("/posts/%d", postID))
	}
}

// RemovePostTag handles removing a tag from a post (moderation action or post owner).
func (h *Handler) RemovePostTag(w http.ResponseWriter, r *http.Request) {
	user := h.requireLogin(w, r)
	if user == nil {
		return
	}

	postID, err := strconv.ParseInt(r.URL.Query().Get("post"), 10, 64)
	if err != nil {
		h.renderError(w, r, http.StatusBadRequest, "Invalid post ID")
		return
	}

	tagID, err := strconv.ParseInt(r.URL.Query().Get("tag"), 10, 64)
	if err != nil {
		h.renderError(w, r, http.StatusBadRequest, "Invalid tag ID")
		return
	}

	// Get the post to check ownership
	post, err := h.DB.GetPostByID(postID, nil)
	if err != nil {
		h.renderError(w, r, http.StatusNotFound, "Post not found")
		return
	}

	// Check if user can remove this tag: post owner, tag moderator, or site admin
	isPostOwner := post.UserID == user.ID
	canModerate := h.DB.CanModerateTag(tagID, user.ID)
	if !isPostOwner && !canModerate && !user.IsAdmin {
		h.renderError(w, r, http.StatusForbidden, "You cannot remove this tag")
		return
	}

	// Remove the tag
	if err := h.DB.RemoveTagFromPost(postID, tagID); err != nil {
		h.renderError(w, r, http.StatusInternalServerError, "Could not remove tag")
		return
	}

	h.logAction(r, models.ActionRemovePostTag, "post", postID, fmt.Sprintf("tag_id=%d", tagID))

	h.redirect(w, r, fmt.Sprintf("/posts/%d", postID))
}

// PostRevisionsPage shows the edit history of a post.
func (h *Handler) PostRevisionsPage(w http.ResponseWriter, r *http.Request) {
	postID, err := getPathID(r, "/posts/")
	if err != nil {
		h.renderError(w, r, http.StatusBadRequest, "Invalid post ID")
		return
	}

	post, err := h.DB.GetPostByID(postID, nil)
	if err != nil {
		h.renderError(w, r, http.StatusNotFound, "Post not found")
		return
	}

	revisions, err := h.DB.GetPostRevisions(postID)
	if err != nil {
		h.renderError(w, r, http.StatusInternalServerError, "Could not load revisions")
		return
	}

	h.render(w, r, "post_revisions.html", map[string]interface{}{
		"Post":      post,
		"Revisions": revisions,
	})
}

// SearchPage handles search using FTS5 full-text search.
// Supports:
//   - Simple queries: "golang tutorial"
//   - Phrase search: "\"exact phrase\""
//   - Prefix matching (automatic): "prog" matches "programming"
//   - Boolean operators: "go AND tutorial", "go NOT java"
//   - Column-specific: "title:golang"
func (h *Handler) SearchPage(w http.ResponseWriter, r *http.Request) {
	query := strings.TrimSpace(r.URL.Query().Get("q"))
	page := getIntParam(r, "page", 1)
	perPage := DefaultPostsPerPage

	if query == "" {
		h.render(w, r, "search.html", nil)
		return
	}

	userID, _ := getUserContext(r)

	// Use FTS5 search (falls back to LIKE search if FTS fails)
	posts, total, err := h.DB.SearchPostsFTS(query, page, perPage, userID)
	if err != nil {
		h.renderError(w, r, http.StatusInternalServerError, "Search failed")
		return
	}

	pagination := models.NewPagination(page, perPage, total)

	h.render(w, r, "search.html", map[string]interface{}{
		"Query":      query,
		"Posts":      posts,
		"Total":      total,
		"Pagination": pagination,
	})
}

// DomainPage shows all posts from a specific domain.
func (h *Handler) DomainPage(w http.ResponseWriter, r *http.Request) {
	domain := strings.TrimPrefix(r.URL.Path, "/domain/")
	if domain == "" {
		h.renderError(w, r, http.StatusBadRequest, "Domain required")
		return
	}

	page := getIntParam(r, "page", 1)
	perPage := DefaultPostsPerPage

	userID, isAdmin := getUserContext(r)

	posts, total, err := h.DB.GetPostsByDomain(domain, page, perPage, userID, isAdmin)
	if err != nil {
		h.renderError(w, r, http.StatusInternalServerError, "Could not load posts")
		return
	}

	pagination := models.NewPagination(page, perPage, total)

	h.render(w, r, "domain.html", map[string]interface{}{
		"Domain":     domain,
		"Posts":      posts,
		"Total":      total,
		"Pagination": pagination,
	})
}
