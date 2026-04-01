// Tag handlers.
package handlers

import (
	"fmt"
	"log"
	"makhor/pkg/db"
	"makhor/pkg/middleware"
	"makhor/pkg/models"
	"net/http"
	"regexp"
	"strconv"
	"strings"
)

// Valid tag name pattern: lowercase letters, numbers, hyphens, and :: for hierarchy
var tagNamePattern = regexp.MustCompile(`^[a-z0-9]+(-[a-z0-9]+)*(::([a-z0-9]+(-[a-z0-9]+)*))*$`)

// TagsPage shows all root-level tags.
func (h *Handler) TagsPage(w http.ResponseWriter, r *http.Request) {
	tags, err := h.DB.GetRootTags()
	if err != nil {
		h.renderError(w, r, http.StatusInternalServerError, "Could not load tags")
		return
	}

	h.render(w, r, "tags.html", map[string]interface{}{
		"Tags": tags,
	})
}


// TagDetailPage shows posts for a specific tag and its subtags.
func (h *Handler) TagDetailPage(w http.ResponseWriter, r *http.Request) {
	// Extract tag name from path /tags/programming::web
	tagName := strings.TrimPrefix(r.URL.Path, "/tags/")
	tagName = strings.TrimSuffix(tagName, "/")

	if tagName == "" {
		h.redirect(w, r, "/tags")
		return
	}

	// Get the tag with ownership info
	tag, err := h.DB.GetTagByNameWithOwnership(tagName)
	if err != nil {
		h.renderError(w, r, http.StatusNotFound, "Tag not found")
		return
	}

	// Get child tags
	childTags, err := h.DB.GetChildTags(tagName)
	if err != nil {
		childTags = nil
	}

	// Get view and period parameters
	view := r.URL.Query().Get("view") // "hot", "top", "new" or "" (default hot)
	period := r.URL.Query().Get("t")  // time period for "top" view

	// Get posts for this tag (and subtags)
	page := getIntParam(r, "page", 1)
	perPage := DefaultPostsPerPage

	userID, isAdmin := getUserContext(r)

	var posts []*models.Post
	var total int

	switch view {
	case "new":
		posts, total, err = h.DB.GetPostsByTagNewest(page, perPage, tagName, userID)
	case "top":
		hours := 24 * 7 // default 7 days
		if hr, ok := TimeFilters[period]; ok {
			hours = hr
		}
		posts, total, err = h.DB.GetPostsByTagTop(page, perPage, tagName, hours, userID)
	default:
		view = "hot"
		posts, total, err = h.DB.GetPosts(page, perPage, tagName, userID, isAdmin)
	}

	if err != nil {
		h.renderError(w, r, http.StatusInternalServerError, "Could not load posts")
		return
	}

	pagination := models.NewPagination(page, perPage, total)

	// Build breadcrumb from tag hierarchy
	var breadcrumbs []struct {
		Name string
		Path string
	}
	parts := strings.Split(tagName, "::")
	for i := range parts {
		path := strings.Join(parts[:i+1], "::")
		breadcrumbs = append(breadcrumbs, struct {
			Name string
			Path string
		}{parts[i], path})
	}

	// Check if user can manage this tag
	canManage := false
	canManageAdmins := false
	canDelete := false
	canCreateSubtag := false
	if userID != nil {
		canManage = h.DB.CanModerateTag(tag.ID, *userID)
		canManageAdmins = h.DB.CanManageTagAdmins(tag.ID, *userID)
		canDelete = h.DB.CanDeleteTag(tag.ID, *userID)
		canCreateSubtag = h.DB.CanCreateSubtag(tagName, *userID)
	}

	// Filter out creator from admins list to avoid duplication
	var filteredAdmins []models.TagAdmin
	for _, admin := range tag.Admins {
		if admin.Username != tag.CreatorUsername {
			filteredAdmins = append(filteredAdmins, admin)
		}
	}
	tag.Admins = filteredAdmins

	h.render(w, r, "tag_detail.html", map[string]interface{}{
		"Tag":             tag,
		"ChildTags":       childTags,
		"Posts":           posts,
		"Pagination":      pagination,
		"Total":           total,
		"View":            view,
		"Period":          period,
		"Breadcrumbs":     breadcrumbs,
		"CanManage":       canManage,
		"CanManageAdmins": canManageAdmins,
		"CanDelete":       canDelete,
		"CanCreateSubtag": canCreateSubtag,
	})
}

// UpdateTagDescription handles updating a tag's description.
func (h *Handler) UpdateTagDescription(w http.ResponseWriter, r *http.Request) {
	user := middleware.GetUser(r)
	if user == nil {
		h.redirect(w, r, "/login")
		return
	}

	if err := r.ParseForm(); err != nil {
		h.renderError(w, r, http.StatusBadRequest, "Invalid form data")
		return
	}

	tagID, err := strconv.ParseInt(r.FormValue("tag_id"), 10, 64)
	if err != nil {
		h.renderError(w, r, http.StatusBadRequest, "Invalid tag ID")
		return
	}

	tag, err := h.DB.GetTagByID(tagID)
	if err != nil {
		h.renderError(w, r, http.StatusNotFound, "Tag not found")
		return
	}

	// Check if user can manage this tag (creator, tag admin, or site admin)
	if !h.DB.CanModerateTag(tagID, user.ID) {
		h.renderError(w, r, http.StatusForbidden, "You cannot edit this tag")
		return
	}

	description := strings.TrimSpace(r.FormValue("description"))
	if len(description) > 500 {
		description = description[:500]
	}

	if err := h.DB.UpdateTagDescription(tagID, description); err != nil {
		h.renderError(w, r, http.StatusInternalServerError, "Could not update tag description")
		return
	}

	h.logAction(r, models.ActionTagUpdate, "tag", tagID, "description updated")

	h.redirect(w, r, fmt.Sprintf("/tags/%s", tag.Name))
}

// CreateTagPage shows the tag creation form.
func (h *Handler) CreateTagPage(w http.ResponseWriter, r *http.Request) {
	user := middleware.GetUser(r)
	if user == nil {
		h.redirect(w, r, "/login")
		return
	}

	// Get parent tag if specified
	parentName := r.URL.Query().Get("parent")
	var parentTag *models.Tag
	if parentName != "" {
		parentTag, _ = h.DB.GetTagByName(parentName)
	}

	h.render(w, r, "create_tag.html", map[string]interface{}{
		"ParentTag": parentTag,
	})
}

// CreateTagSubmit handles tag creation.
// Root-level tags can only be created by site admins.
// Subtags can be created by anyone who can moderate the parent tag.
func (h *Handler) CreateTagSubmit(w http.ResponseWriter, r *http.Request) {
	user := middleware.GetUser(r)
	if user == nil {
		h.redirect(w, r, "/login")
		return
	}

	if err := r.ParseForm(); err != nil {
		h.renderError(w, r, http.StatusBadRequest, "Invalid form data")
		return
	}

	name := strings.ToLower(strings.TrimSpace(r.FormValue("name")))
	description := strings.TrimSpace(r.FormValue("description"))
	parentName := strings.TrimSpace(r.FormValue("parent"))

	// Build full tag name
	fullName := name
	if parentName != "" {
		fullName = parentName + "::" + name
	}

	var errors []string

	// Permission check: root tags require site admin, subtags require parent moderation
	if parentName == "" {
		// Creating root-level tag - only site admins allowed
		if !user.IsAdmin {
			h.renderError(w, r, http.StatusForbidden, "Only site admins can create root-level tags")
			return
		}
	} else {
		// Creating subtag - need to be able to moderate the parent
		if !h.DB.CanCreateSubtag(parentName, user.ID) {
			h.renderError(w, r, http.StatusForbidden, "You cannot create subtags under this tag")
			return
		}
	}

	// Validate name
	if len(name) < 2 || len(name) > 50 {
		errors = append(errors, "Tag name must be between 2 and 50 characters")
	}
	if !tagNamePattern.MatchString(fullName) {
		errors = append(errors, "Tag name can only contain lowercase letters, numbers, and hyphens")
	}

	// Check if tag already exists
	if h.DB.TagExists(fullName) {
		errors = append(errors, "A tag with this name already exists")
	}

	// If parent specified, verify it exists
	var parentTag *models.Tag
	if parentName != "" {
		var err error
		parentTag, err = h.DB.GetTagByName(parentName)
		if err != nil {
			errors = append(errors, "Parent tag does not exist")
		}
	}

	if len(errors) > 0 {
		h.render(w, r, "create_tag.html", map[string]interface{}{
			"Errors":      errors,
			"Name":        name,
			"Description": description,
			"ParentTag":   parentTag,
		})
		return
	}

	// Create the tag
	tag, err := h.DB.CreateTag(fullName, description, user.ID)
	if err != nil {
		h.renderError(w, r, http.StatusInternalServerError, "Could not create tag")
		return
	}

	h.logAction(r, models.ActionTagCreate, "tag", tag.ID, fullName)

	h.redirect(w, r, fmt.Sprintf("/tags/%s", fullName))
}

// DeleteTag handles tag deletion.
func (h *Handler) DeleteTag(w http.ResponseWriter, r *http.Request) {
	user := middleware.GetUser(r)
	if user == nil {
		h.redirect(w, r, "/login")
		return
	}

	tagID, err := strconv.ParseInt(r.URL.Query().Get("id"), 10, 64)
	if err != nil {
		h.renderError(w, r, http.StatusBadRequest, "Invalid tag ID")
		return
	}

	tag, err := h.DB.GetTagByID(tagID)
	if err != nil {
		h.renderError(w, r, http.StatusNotFound, "Tag not found")
		return
	}

	// Check if user can delete this tag
	if !h.DB.CanDeleteTag(tagID, user.ID) {
		h.renderError(w, r, http.StatusForbidden, "You cannot delete this tag")
		return
	}

	// Check if tag has child tags
	childCount := h.DB.GetTagChildCount(tag.Name)
	if childCount > 0 {
		h.renderError(w, r, http.StatusBadRequest, fmt.Sprintf("Cannot delete tag with %d child tags. Delete children first.", childCount))
		return
	}

	// Delete the tag
	if err := h.DB.DeleteTag(tagID); err != nil {
		h.renderError(w, r, http.StatusInternalServerError, "Could not delete tag")
		return
	}

	h.logAction(r, models.ActionTagDelete, "tag", tagID, tag.Name)

	// Redirect to parent tag or tags list
	parentName := db.GetParentTagName(tag.Name)
	if parentName != "" {
		h.redirect(w, r, fmt.Sprintf("/tags/%s", parentName))
	} else {
		h.redirect(w, r, "/tags")
	}
}

// TagAdminsPage shows the tag admin management page.
func (h *Handler) TagAdminsPage(w http.ResponseWriter, r *http.Request) {
	user := middleware.GetUser(r)
	if user == nil {
		h.redirect(w, r, "/login")
		return
	}

	tagIDStr := r.URL.Query().Get("tag")
	tagID, err := strconv.ParseInt(tagIDStr, 10, 64)
	if err != nil {
		h.renderError(w, r, http.StatusBadRequest, "Invalid tag ID")
		return
	}

	tag, err := h.DB.GetTagByID(tagID)
	if err != nil {
		h.renderError(w, r, http.StatusNotFound, "Tag not found")
		return
	}

	// Check if user can manage admins
	if !h.DB.CanManageTagAdmins(tagID, user.ID) {
		h.renderError(w, r, http.StatusForbidden, "You cannot manage admins for this tag")
		return
	}

	h.render(w, r, "tag_admins.html", map[string]interface{}{
		"Tag": tag,
	})
}

// AddTagAdmin handles adding a user as a tag admin.
func (h *Handler) AddTagAdmin(w http.ResponseWriter, r *http.Request) {
	user := middleware.GetUser(r)
	if user == nil {
		h.redirect(w, r, "/login")
		return
	}

	if err := r.ParseForm(); err != nil {
		h.renderError(w, r, http.StatusBadRequest, "Invalid form data")
		return
	}

	tagID, err := strconv.ParseInt(r.FormValue("tag_id"), 10, 64)
	if err != nil {
		h.renderError(w, r, http.StatusBadRequest, "Invalid tag ID")
		return
	}

	username := strings.TrimSpace(r.FormValue("username"))

	// Check if user can manage admins
	if !h.DB.CanManageTagAdmins(tagID, user.ID) {
		h.renderError(w, r, http.StatusForbidden, "You cannot manage admins for this tag")
		return
	}

	// Find the user to add
	targetUser, err := h.DB.GetUserByUsername(username)
	if err != nil {
		tag, _ := h.DB.GetTagByID(tagID)
		h.render(w, r, "tag_admins.html", map[string]interface{}{
			"Tag":   tag,
			"Error": "User not found",
		})
		return
	}

	// Add as admin
	if err := h.DB.AddTagAdmin(tagID, targetUser.ID, user.ID); err != nil {
		h.renderError(w, r, http.StatusInternalServerError, "Could not add admin")
		return
	}

	h.logAction(r, models.ActionTagAdminAdd, "tag", tagID, username)

	h.redirect(w, r, fmt.Sprintf("/tags/admins?tag=%d", tagID))
}

// RemoveTagAdmin handles removing a user as a tag admin.
func (h *Handler) RemoveTagAdmin(w http.ResponseWriter, r *http.Request) {
	user := middleware.GetUser(r)
	if user == nil {
		h.redirect(w, r, "/login")
		return
	}

	tagID, err := strconv.ParseInt(r.URL.Query().Get("tag"), 10, 64)
	if err != nil {
		h.renderError(w, r, http.StatusBadRequest, "Invalid tag ID")
		return
	}

	targetUserID, err := strconv.ParseInt(r.URL.Query().Get("user"), 10, 64)
	if err != nil {
		h.renderError(w, r, http.StatusBadRequest, "Invalid user ID")
		return
	}

	// Check if user can manage admins
	if !h.DB.CanManageTagAdmins(tagID, user.ID) {
		h.renderError(w, r, http.StatusForbidden, "You cannot manage admins for this tag")
		return
	}

	// Remove admin
	if err := h.DB.RemoveTagAdmin(tagID, targetUserID); err != nil {
		h.renderError(w, r, http.StatusInternalServerError, "Could not remove admin")
		return
	}

	h.logAction(r, models.ActionTagAdminRemove, "tag", tagID, "")

	h.redirect(w, r, fmt.Sprintf("/tags/admins?tag=%d", tagID))
}

// ViewTagFeedsPage shows the RSS feeds for a tag (read-only, accessible to all users).
func (h *Handler) ViewTagFeedsPage(w http.ResponseWriter, r *http.Request) {
	tagIDStr := r.URL.Query().Get("tag")
	tagID, err := strconv.ParseInt(tagIDStr, 10, 64)
	if err != nil {
		h.renderError(w, r, http.StatusBadRequest, "Invalid tag ID")
		return
	}

	tag, err := h.DB.GetTagByID(tagID)
	if err != nil {
		h.renderError(w, r, http.StatusNotFound, "Tag not found")
		return
	}

	feeds, _ := h.DB.GetRSSFeedsByTag(tagID)

	// Check if current user can manage feeds
	user := middleware.GetUser(r)
	canManage := false
	if user != nil {
		canManage = h.DB.CanManageTagFeed(user.ID, tagID)
	}

	h.render(w, r, "view_tag_feeds.html", map[string]interface{}{
		"Tag":       tag,
		"Feeds":     feeds,
		"CanManage": canManage,
	})
}

// TagFeedsPage shows the RSS feeds management page for a tag.
func (h *Handler) TagFeedsPage(w http.ResponseWriter, r *http.Request) {
	user := middleware.GetUser(r)
	if user == nil {
		h.redirect(w, r, "/login")
		return
	}

	tagIDStr := r.URL.Query().Get("tag")
	tagID, err := strconv.ParseInt(tagIDStr, 10, 64)
	if err != nil {
		h.renderError(w, r, http.StatusBadRequest, "Invalid tag ID")
		return
	}

	tag, err := h.DB.GetTagByID(tagID)
	if err != nil {
		h.renderError(w, r, http.StatusNotFound, "Tag not found")
		return
	}

	// Check if user can manage feeds for this tag
	if !h.DB.CanManageTagFeed(user.ID, tagID) {
		h.renderError(w, r, http.StatusForbidden, "You cannot manage feeds for this tag")
		return
	}

	feeds, _ := h.DB.GetRSSFeedsByTag(tagID)

	h.render(w, r, "tag_feeds.html", map[string]interface{}{
		"Tag":   tag,
		"Feeds": feeds,
	})
}

// AddTagFeed handles adding a new RSS feed to a tag.
func (h *Handler) AddTagFeed(w http.ResponseWriter, r *http.Request) {
	user := middleware.GetUser(r)
	if user == nil {
		h.redirect(w, r, "/login")
		return
	}

	if err := r.ParseForm(); err != nil {
		h.renderError(w, r, http.StatusBadRequest, "Invalid form data")
		return
	}

	tagID, err := strconv.ParseInt(r.FormValue("tag_id"), 10, 64)
	if err != nil {
		h.renderError(w, r, http.StatusBadRequest, "Invalid tag ID")
		return
	}

	// Check if user can manage feeds for this tag
	if !h.DB.CanManageTagFeed(user.ID, tagID) {
		h.renderError(w, r, http.StatusForbidden, "You cannot manage feeds for this tag")
		return
	}

	url := strings.TrimSpace(r.FormValue("url"))
	title := strings.TrimSpace(r.FormValue("title"))
	intervalStr := r.FormValue("interval")
	pollOnView := r.FormValue("poll_on_view") == "on"
	autoApprove := r.FormValue("auto_approve") != "off" // Default true
	maxItemsStr := r.FormValue("max_items")

	interval := 60
	if intervalStr != "" {
		if i, err := strconv.Atoi(intervalStr); err == nil && i >= 5 {
			interval = i
		}
	}

	maxItems := 10
	if maxItemsStr != "" {
		if m, err := strconv.Atoi(maxItemsStr); err == nil && m >= 0 {
			maxItems = m
		}
	}

	var errors []string
	if url == "" {
		errors = append(errors, "Feed URL is required")
	}

	tag, err := h.DB.GetTagByID(tagID)
	if err != nil {
		h.renderError(w, r, http.StatusNotFound, "Tag not found")
		return
	}

	if len(errors) > 0 {
		feeds, _ := h.DB.GetRSSFeedsByTag(tagID)
		h.render(w, r, "tag_feeds.html", map[string]interface{}{
			"Tag":    tag,
			"Feeds":  feeds,
			"Errors": errors,
			"URL":    url,
			"Title":  title,
		})
		return
	}

	// Create the feed
	_, err = h.DB.CreateRSSFeedWithOptions(db.CreateRSSFeedOptions{
		URL:             url,
		Title:           title,
		TagID:           tagID,
		CreatedBy:       user.ID,
		IntervalMinutes: interval,
		PollOnView:      pollOnView,
		AutoApprove:     autoApprove,
		MaxItemsPerPoll: maxItems,
	})
	if err != nil {
		feeds, _ := h.DB.GetRSSFeedsByTag(tagID)
		h.render(w, r, "tag_feeds.html", map[string]interface{}{
			"Tag":    tag,
			"Feeds":  feeds,
			"Error":  "Could not add feed: " + err.Error(),
			"URL":    url,
			"Title":  title,
		})
		return
	}

	h.logAction(r, models.ActionFeedAdd, "tag", tagID, url)

	h.redirect(w, r, fmt.Sprintf("/tags/feeds?tag=%d", tagID))
}

// UpdateTagFeed handles updating an RSS feed.
func (h *Handler) UpdateTagFeed(w http.ResponseWriter, r *http.Request) {
	user := middleware.GetUser(r)
	if user == nil {
		h.redirect(w, r, "/login")
		return
	}

	if err := r.ParseForm(); err != nil {
		h.renderError(w, r, http.StatusBadRequest, "Invalid form data")
		return
	}

	feedID, err := strconv.ParseInt(r.FormValue("feed_id"), 10, 64)
	if err != nil {
		h.renderError(w, r, http.StatusBadRequest, "Invalid feed ID")
		return
	}

	feed, err := h.DB.GetRSSFeedByID(feedID)
	if err != nil {
		h.renderError(w, r, http.StatusNotFound, "Feed not found")
		return
	}

	// Check if user can manage feeds for this tag
	if !h.DB.CanManageTagFeed(user.ID, feed.TagID) {
		h.renderError(w, r, http.StatusForbidden, "You cannot manage this feed")
		return
	}

	intervalStr := r.FormValue("interval")
	pollOnView := r.FormValue("poll_on_view") == "on"
	autoApprove := r.FormValue("auto_approve") != "off"
	maxItemsStr := r.FormValue("max_items")
	isActive := r.FormValue("is_active") != "off"

	interval := feed.IntervalMinutes
	if intervalStr != "" {
		if i, err := strconv.Atoi(intervalStr); err == nil && i >= 5 {
			interval = i
		}
	}

	maxItems := feed.MaxItemsPerPoll
	if maxItemsStr != "" {
		if m, err := strconv.Atoi(maxItemsStr); err == nil && m >= 0 {
			maxItems = m
		}
	}

	err = h.DB.UpdateRSSFeed(feedID, interval, pollOnView, autoApprove, maxItems, isActive)
	if err != nil {
		h.renderError(w, r, http.StatusInternalServerError, "Could not update feed")
		return
	}

	h.logAction(r, models.ActionFeedUpdate, "feed", feedID, "")

	h.redirect(w, r, fmt.Sprintf("/tags/feeds?tag=%d", feed.TagID))
}

// DeleteTagFeed handles deleting an RSS feed.
func (h *Handler) DeleteTagFeed(w http.ResponseWriter, r *http.Request) {
	user := middleware.GetUser(r)
	if user == nil {
		h.redirect(w, r, "/login")
		return
	}

	feedID, err := strconv.ParseInt(r.URL.Query().Get("id"), 10, 64)
	if err != nil {
		h.renderError(w, r, http.StatusBadRequest, "Invalid feed ID")
		return
	}

	feed, err := h.DB.GetRSSFeedByID(feedID)
	if err != nil {
		h.renderError(w, r, http.StatusNotFound, "Feed not found")
		return
	}

	// Check if user can manage feeds for this tag
	if !h.DB.CanManageTagFeed(user.ID, feed.TagID) {
		h.renderError(w, r, http.StatusForbidden, "You cannot manage this feed")
		return
	}

	err = h.DB.DeleteRSSFeed(feedID)
	if err != nil {
		h.renderError(w, r, http.StatusInternalServerError, "Could not delete feed")
		return
	}

	h.logAction(r, models.ActionFeedDelete, "feed", feedID, feed.URL)

	h.redirect(w, r, fmt.Sprintf("/tags/feeds?tag=%d", feed.TagID))
}

// SyncTagFeed handles force syncing an RSS feed.
func (h *Handler) SyncTagFeed(w http.ResponseWriter, r *http.Request) {
	user := middleware.GetUser(r)
	if user == nil {
		h.redirect(w, r, "/login")
		return
	}

	feedID, err := strconv.ParseInt(r.URL.Query().Get("id"), 10, 64)
	if err != nil {
		h.renderError(w, r, http.StatusBadRequest, "Invalid feed ID")
		return
	}

	feed, err := h.DB.GetRSSFeedByID(feedID)
	if err != nil {
		h.renderError(w, r, http.StatusNotFound, "Feed not found")
		return
	}

	// Check if user can manage feeds for this tag
	if !h.DB.CanManageTagFeed(user.ID, feed.TagID) {
		h.renderError(w, r, http.StatusForbidden, "You cannot manage this feed")
		return
	}

	// Check if poller is available
	if h.Poller == nil {
		h.renderError(w, r, http.StatusInternalServerError, "RSS poller not available")
		return
	}

	// Sync the feed now
	if err := h.Poller.PollFeedNow(feedID); err != nil {
		tag, _ := h.DB.GetTagByID(feed.TagID)
		feeds, _ := h.DB.GetRSSFeedsByTag(feed.TagID)
		h.render(w, r, "tag_feeds.html", map[string]interface{}{
			"Tag":   tag,
			"Feeds": feeds,
			"Error": "Sync failed: " + err.Error(),
		})
		return
	}

	h.logAction(r, models.ActionFeedSync, "feed", feedID, feed.URL)

	// Redirect back with success message
	tag, _ := h.DB.GetTagByID(feed.TagID)
	feeds, _ := h.DB.GetRSSFeedsByTag(feed.TagID)
	h.render(w, r, "tag_feeds.html", map[string]interface{}{
		"Tag":     tag,
		"Feeds":   feeds,
		"Success": "Feed synced successfully",
	})
}

// AboutPage shows the about page with statistics.
func (h *Handler) AboutPage(w http.ResponseWriter, r *http.Request) {
	stats, err := h.DB.GetStats()
	if err != nil {
		log.Printf("Error getting stats for about page: %v", err)
		stats = nil
	}
	h.render(w, r, "about.html", map[string]interface{}{
		"Stats": stats,
	})
}
