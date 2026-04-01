// RSS feed handlers.
package handlers

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/xml"
	"fmt"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"makhor/pkg/db"
	"makhor/pkg/middleware"
	"makhor/pkg/models"
)

// RSS feed cache for efficiency under high load.
type rssCache struct {
	mu      sync.RWMutex
	entries map[string]*rssCacheEntry
}

type rssCacheEntry struct {
	data      []byte
	etag      string
	expiresAt time.Time
}

var feedCache = &rssCache{entries: make(map[string]*rssCacheEntry)}

// getCached returns cached RSS data if valid.
func (c *rssCache) getCached(key string) ([]byte, string, bool) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	entry, ok := c.entries[key]
	if !ok || time.Now().After(entry.expiresAt) {
		return nil, "", false
	}
	return entry.data, entry.etag, true
}

// setCache stores RSS data in cache.
func (c *rssCache) setCache(key string, data []byte, ttl time.Duration) string {
	hash := sha256.Sum256(data)
	etag := hex.EncodeToString(hash[:8])

	c.mu.Lock()
	defer c.mu.Unlock()
	c.entries[key] = &rssCacheEntry{
		data:      data,
		etag:      etag,
		expiresAt: time.Now().Add(ttl),
	}
	return etag
}

// RSS represents an RSS 2.0 feed.
type RSS struct {
	XMLName xml.Name   `xml:"rss"`
	Version string     `xml:"version,attr"`
	Channel RSSChannel `xml:"channel"`
}

// RSSChannel represents an RSS channel.
type RSSChannel struct {
	Title       string    `xml:"title"`
	Link        string    `xml:"link"`
	Description string    `xml:"description"`
	Language    string    `xml:"language,omitempty"`
	PubDate     string    `xml:"pubDate,omitempty"`
	Items       []RSSItem `xml:"item"`
}

// RSSItem represents an RSS item.
type RSSItem struct {
	Title       string `xml:"title"`
	Link        string `xml:"link"`
	Description string `xml:"description,omitempty"`
	Author      string `xml:"author,omitempty"`
	GUID        string `xml:"guid"`
	PubDate     string `xml:"pubDate"`
	Comments    string `xml:"comments,omitempty"`
}

// formatRSSDate formats a time for RSS.
func formatRSSDate(t time.Time) string {
	return t.Format(time.RFC1123Z)
}

// RSSFeed serves the main RSS feed.
func (h *Handler) RSSFeed(w http.ResponseWriter, r *http.Request) {
	// RSS feeds are public, so only show posts with tags
	posts, _, err := h.DB.GetNewestPosts(1, 30, nil, false)
	if err != nil {
		http.Error(w, "Internal Server Error", http.StatusInternalServerError)
		return
	}

	h.serveFeed(w, "makhor - Latest Posts", h.BaseURL, "Latest posts from makhor", posts)
}

// RSSFeedByTag serves an RSS feed filtered by tag.
func (h *Handler) RSSFeedByTag(w http.ResponseWriter, r *http.Request) {
	tag := strings.TrimPrefix(r.URL.Path, "/rss/tag/")
	tag = strings.TrimSuffix(tag, ".xml")

	if tag == "" {
		http.Error(w, "Missing tag", http.StatusBadRequest)
		return
	}

	// RSS feeds are public, so we only show posts with tags (isAdmin=false, userID=nil)
	posts, _, err := h.DB.GetPosts(1, 30, tag, nil, false)
	if err != nil {
		http.Error(w, "Internal Server Error", http.StatusInternalServerError)
		return
	}

	title := fmt.Sprintf("makhor - %s", tag)
	desc := fmt.Sprintf("Posts tagged with '%s' on makhor", tag)

	h.serveFeed(w, title, h.BaseURL+"/rss/tag/"+tag+".xml", desc, posts)
}

// RSSFeedByUser serves an RSS feed of a user's posts.
func (h *Handler) RSSFeedByUser(w http.ResponseWriter, r *http.Request) {
	username := strings.TrimPrefix(r.URL.Path, "/rss/user/")
	username = strings.TrimSuffix(username, ".xml")

	if username == "" {
		http.Error(w, "Missing username", http.StatusBadRequest)
		return
	}

	user, err := h.DB.GetUserByUsername(username)
	if err != nil {
		http.Error(w, "User not found", http.StatusNotFound)
		return
	}

	posts, _, err := h.DB.GetUserPosts(user.ID, 1, 30)
	if err != nil {
		http.Error(w, "Internal Server Error", http.StatusInternalServerError)
		return
	}

	title := fmt.Sprintf("makhor - Posts by %s", username)
	desc := fmt.Sprintf("Posts by %s on makhor", username)

	h.serveFeed(w, title, h.BaseURL+"/rss/user/"+username+".xml", desc, posts)
}

// RSSComments serves an RSS feed of recent comments.
func (h *Handler) RSSComments(w http.ResponseWriter, r *http.Request) {
	comments, err := h.DB.GetRecentComments(30)
	if err != nil {
		http.Error(w, "Internal Server Error", http.StatusInternalServerError)
		return
	}

	items := make([]RSSItem, 0, len(comments))
	for _, c := range comments {
		items = append(items, RSSItem{
			Title:       fmt.Sprintf("Comment by %s on: %s", c.Username, c.PostTitle),
			Link:        fmt.Sprintf("%s/posts/%d#c%d", h.BaseURL, c.PostID, c.ID),
			Description: truncate(c.Body, 500),
			Author:      c.Username,
			GUID:        fmt.Sprintf("%s/comments/%d", h.BaseURL, c.ID),
			PubDate:     formatRSSDate(c.CreatedAt),
		})
	}

	feed := RSS{
		Version: "2.0",
		Channel: RSSChannel{
			Title:       "makhor - Recent Comments",
			Link:        h.BaseURL + "/rss/comments.xml",
			Description: "Recent comments on makhor",
			Language:    "en",
			PubDate:     formatRSSDate(time.Now()),
			Items:       items,
		},
	}

	w.Header().Set("Content-Type", "application/rss+xml; charset=utf-8")
	w.Write([]byte(xml.Header))
	enc := xml.NewEncoder(w)
	enc.Indent("", "  ")
	enc.Encode(feed)
}

// serveFeed is a helper to generate and serve an RSS feed of posts.
func (h *Handler) serveFeed(w http.ResponseWriter, title, link, description string, posts []*models.Post) {
	items := make([]RSSItem, 0, len(posts))
	for _, p := range posts {
		itemLink := p.URL
		if itemLink == "" {
			itemLink = fmt.Sprintf("%s/posts/%d", h.BaseURL, p.ID)
		}

		desc := p.Body
		if desc == "" && p.URL != "" {
			desc = p.URL
		}

		items = append(items, RSSItem{
			Title:       p.Title,
			Link:        itemLink,
			Description: truncate(desc, 500),
			Author:      p.Username,
			GUID:        fmt.Sprintf("%s/posts/%d", h.BaseURL, p.ID),
			PubDate:     formatRSSDate(p.CreatedAt),
			Comments:    fmt.Sprintf("%s/posts/%d", h.BaseURL, p.ID),
		})
	}

	var pubDate string
	if len(posts) > 0 {
		pubDate = formatRSSDate(posts[0].CreatedAt)
	} else {
		pubDate = formatRSSDate(time.Now())
	}

	feed := RSS{
		Version: "2.0",
		Channel: RSSChannel{
			Title:       title,
			Link:        link,
			Description: description,
			Language:    "en",
			PubDate:     pubDate,
			Items:       items,
		},
	}

	w.Header().Set("Content-Type", "application/rss+xml; charset=utf-8")
	w.Write([]byte(xml.Header))
	enc := xml.NewEncoder(w)
	enc.Indent("", "  ")
	enc.Encode(feed)
}

// serveCachedRSS serves RSS with caching support for efficiency.
func (h *Handler) serveCachedRSS(w http.ResponseWriter, r *http.Request, cacheKey string, ttl time.Duration, generate func() ([]byte, error)) {
	// Check If-None-Match header
	clientEtag := r.Header.Get("If-None-Match")

	// Try cache first
	if data, etag, ok := feedCache.getCached(cacheKey); ok {
		if clientEtag == etag {
			w.WriteHeader(http.StatusNotModified)
			return
		}
		w.Header().Set("Content-Type", "application/rss+xml; charset=utf-8")
		w.Header().Set("ETag", etag)
		w.Header().Set("Cache-Control", fmt.Sprintf("public, max-age=%d", int(ttl.Seconds())))
		w.Write(data)
		return
	}

	// Generate fresh feed
	data, err := generate()
	if err != nil {
		http.Error(w, "Internal Server Error", http.StatusInternalServerError)
		return
	}

	// Cache and serve
	etag := feedCache.setCache(cacheKey, data, ttl)
	w.Header().Set("Content-Type", "application/rss+xml; charset=utf-8")
	w.Header().Set("ETag", etag)
	w.Header().Set("Cache-Control", fmt.Sprintf("public, max-age=%d", int(ttl.Seconds())))
	w.Write(data)
}

// RSSActionLog serves an RSS feed of the action log.
// Supports category filtering via ?category= query param.
func (h *Handler) RSSActionLog(w http.ResponseWriter, r *http.Request) {
	category := r.URL.Query().Get("category")
	username := r.URL.Query().Get("username")

	cacheKey := fmt.Sprintf("log:%s:%s", category, username)

	h.serveCachedRSS(w, r, cacheKey, 5*time.Minute, func() ([]byte, error) {
		filter := db.ActionLogFilter{
			Category: category,
			Username: username,
		}
		logs, _, err := h.DB.GetActionLogFiltered(1, 50, filter)
		if err != nil {
			return nil, err
		}

		items := make([]RSSItem, 0, len(logs))
		for _, log := range logs {
			title := fmt.Sprintf("[%s] %s", log.Action, log.Username)
			if log.TargetInfo != "" {
				title += ": " + log.TargetInfo
			}

			desc := fmt.Sprintf("Action: %s\nTarget: %s #%d\nDetails: %s",
				log.Action, log.TargetType, log.TargetID, log.Details)

			link := h.BaseURL + "/admin/log"
			if category != "" {
				link += "?category=" + category
			}

			items = append(items, RSSItem{
				Title:       title,
				Link:        link,
				Description: desc,
				Author:      log.Username,
				GUID:        fmt.Sprintf("%s/log/%d", h.BaseURL, log.ID),
				PubDate:     formatRSSDate(log.CreatedAt),
			})
		}

		title := "makhor - Action Log"
		if category != "" {
			title = fmt.Sprintf("makhor - Action Log (%s)", category)
		}
		if username != "" {
			title += fmt.Sprintf(" by %s", username)
		}

		feed := RSS{
			Version: "2.0",
			Channel: RSSChannel{
				Title:       title,
				Link:        h.BaseURL + "/rss/log.xml",
				Description: "Action log from makhor",
				Language:    "en",
				PubDate:     formatRSSDate(time.Now()),
				Items:       items,
			},
		}

		return marshalRSS(feed)
	})
}

// RSSTagsMulti serves an RSS feed for multiple tags.
// Supports comma-separated tags via ?tags= query param.
// Includes subtags automatically.
func (h *Handler) RSSTagsMulti(w http.ResponseWriter, r *http.Request) {
	tagsParam := r.URL.Query().Get("tags")
	if tagsParam == "" {
		http.Error(w, "Missing tags parameter", http.StatusBadRequest)
		return
	}

	// Parse and normalize tags
	requestedTags := strings.Split(tagsParam, ",")
	var normalizedTags []string
	for _, t := range requestedTags {
		t = strings.TrimSpace(t)
		if t != "" {
			normalizedTags = append(normalizedTags, t)
		}
	}
	sort.Strings(normalizedTags)

	if len(normalizedTags) == 0 {
		http.Error(w, "No valid tags provided", http.StatusBadRequest)
		return
	}

	cacheKey := fmt.Sprintf("tags:%s", strings.Join(normalizedTags, ","))

	h.serveCachedRSS(w, r, cacheKey, 5*time.Minute, func() ([]byte, error) {
		// Expand tags to include subtags
		expandedTags := make(map[string]bool)
		for _, tag := range normalizedTags {
			expandedTags[tag] = true
			// Get child tags (subtags)
			children, err := h.DB.GetChildTags(tag)
			if err == nil {
				for _, child := range children {
					expandedTags[child.Name] = true
				}
			}
		}

		// Get posts for all expanded tags
		posts, err := h.DB.GetPostsByTags(expandedTags, 1, 50)
		if err != nil {
			return nil, err
		}

		items := make([]RSSItem, 0, len(posts))
		for _, p := range posts {
			itemLink := p.URL
			if itemLink == "" {
				itemLink = fmt.Sprintf("%s/posts/%d", h.BaseURL, p.ID)
			}

			desc := p.Body
			if desc == "" && p.URL != "" {
				desc = p.URL
			}

			items = append(items, RSSItem{
				Title:       p.Title,
				Link:        itemLink,
				Description: truncate(desc, 500),
				Author:      p.Username,
				GUID:        fmt.Sprintf("%s/posts/%d", h.BaseURL, p.ID),
				PubDate:     formatRSSDate(p.CreatedAt),
				Comments:    fmt.Sprintf("%s/posts/%d", h.BaseURL, p.ID),
			})
		}

		title := fmt.Sprintf("makhor - %s", strings.Join(normalizedTags, ", "))
		desc := fmt.Sprintf("Posts tagged with: %s (including subtags)", strings.Join(normalizedTags, ", "))

		var pubDate string
		if len(posts) > 0 {
			pubDate = formatRSSDate(posts[0].CreatedAt)
		} else {
			pubDate = formatRSSDate(time.Now())
		}

		feed := RSS{
			Version: "2.0",
			Channel: RSSChannel{
				Title:       title,
				Link:        h.BaseURL + "/rss/tags.xml?tags=" + tagsParam,
				Description: desc,
				Language:    "en",
				PubDate:     pubDate,
				Items:       items,
			},
		}

		return marshalRSS(feed)
	})
}

// marshalRSS marshals an RSS feed to bytes.
func marshalRSS(feed RSS) ([]byte, error) {
	var buf strings.Builder
	buf.WriteString(xml.Header)
	enc := xml.NewEncoder(&buf)
	enc.Indent("", "  ")
	if err := enc.Encode(feed); err != nil {
		return nil, err
	}
	return []byte(buf.String()), nil
}

// FeedDetailPage shows a detail page for a specific RSS feed.
func (h *Handler) FeedDetailPage(w http.ResponseWriter, r *http.Request) {
	// Handle POST requests for updates
	if r.Method == http.MethodPost {
		h.UpdateFeedSubmit(w, r)
		return
	}

	// Extract feed ID from path: /feeds/123
	path := strings.TrimPrefix(r.URL.Path, "/feeds/")
	feedID, err := strconv.ParseInt(path, 10, 64)
	if err != nil {
		h.renderError(w, r, http.StatusBadRequest, "Invalid feed ID")
		return
	}

	feed, err := h.DB.GetRSSFeedByID(feedID)
	if err != nil {
		h.renderError(w, r, http.StatusNotFound, "Feed not found")
		return
	}

	// Check if current user can edit this feed
	user := middleware.GetUser(r)
	canEdit := false
	if user != nil {
		canEdit = h.DB.CanEditFeed(user.ID, feedID)
	}

	// Get pagination
	page := 1
	if p := r.URL.Query().Get("page"); p != "" {
		if parsed, err := strconv.Atoi(p); err == nil && parsed > 0 {
			page = parsed
		}
	}

	perPage := DefaultPostsPerPage
	posts, total, err := h.DB.GetPostsByFeed(feedID, page, perPage)
	if err != nil {
		h.renderError(w, r, http.StatusInternalServerError, "Could not load posts")
		return
	}

	pagination := models.NewPagination(page, perPage, total)

	// Get feed stats for admins/editors
	var stats *db.RSSFeedStats
	if canEdit {
		stats, _ = h.DB.GetRSSFeedStats(feedID)
	}

	h.render(w, r, "feed_detail.html", map[string]interface{}{
		"Feed":       feed,
		"Posts":      posts,
		"Total":      total,
		"Pagination": pagination,
		"Page":       page,
		"TotalPages": pagination.TotalPages,
		"HasPrev":    pagination.HasPrev(),
		"HasNext":    pagination.HasNext(),
		"PrevPage":   page - 1,
		"NextPage":   page + 1,
		"CanEdit":    canEdit,
		"Stats":      stats,
	})
}

// UpdateFeedSubmit handles POST requests to update a feed.
func (h *Handler) UpdateFeedSubmit(w http.ResponseWriter, r *http.Request) {
	user := middleware.GetUser(r)
	if user == nil {
		http.Redirect(w, r, "/login", http.StatusSeeOther)
		return
	}

	// Extract feed ID from path: /feeds/123
	path := strings.TrimPrefix(r.URL.Path, "/feeds/")
	feedID, err := strconv.ParseInt(path, 10, 64)
	if err != nil {
		h.renderError(w, r, http.StatusBadRequest, "Invalid feed ID")
		return
	}

	// Check permission
	if !h.DB.CanEditFeed(user.ID, feedID) {
		h.renderError(w, r, http.StatusForbidden, "You don't have permission to edit this feed")
		return
	}

	r.ParseForm()

	url := strings.TrimSpace(r.FormValue("url"))
	title := strings.TrimSpace(r.FormValue("title"))
	intervalStr := r.FormValue("interval_minutes")
	pollOnView := r.FormValue("poll_on_view") == "on"
	autoApprove := r.FormValue("auto_approve") == "on"
	maxItemsStr := r.FormValue("max_items_per_poll")
	isActive := r.FormValue("is_active") == "on"

	interval, _ := strconv.Atoi(intervalStr)
	if interval < 5 {
		interval = 60
	}
	maxItems, _ := strconv.Atoi(maxItemsStr)
	if maxItems < 1 {
		maxItems = 10
	}

	err = h.DB.UpdateRSSFeedFull(feedID, url, title, interval, pollOnView, autoApprove, maxItems, isActive)
	if err != nil {
		h.renderError(w, r, http.StatusInternalServerError, "Failed to update feed")
		return
	}

	http.Redirect(w, r, fmt.Sprintf("/feeds/%d", feedID), http.StatusSeeOther)
}

// DeleteFeedSubmit handles deleting a feed.
func (h *Handler) DeleteFeedSubmit(w http.ResponseWriter, r *http.Request) {
	user := middleware.GetUser(r)
	if user == nil {
		http.Redirect(w, r, "/login", http.StatusSeeOther)
		return
	}

	feedIDStr := r.URL.Query().Get("id")
	feedID, err := strconv.ParseInt(feedIDStr, 10, 64)
	if err != nil {
		h.renderError(w, r, http.StatusBadRequest, "Invalid feed ID")
		return
	}

	// Get feed to know its tag for redirect
	feed, err := h.DB.GetRSSFeedByID(feedID)
	if err != nil {
		h.renderError(w, r, http.StatusNotFound, "Feed not found")
		return
	}

	// Check permission
	if !h.DB.CanEditFeed(user.ID, feedID) {
		h.renderError(w, r, http.StatusForbidden, "You don't have permission to delete this feed")
		return
	}

	err = h.DB.DeleteRSSFeed(feedID)
	if err != nil {
		h.renderError(w, r, http.StatusInternalServerError, "Failed to delete feed")
		return
	}

	http.Redirect(w, r, "/tags/feeds?tag="+strconv.FormatInt(feed.TagID, 10), http.StatusSeeOther)
}

// AdminFeedsPage shows all RSS feeds for admins.
func (h *Handler) AdminFeedsPage(w http.ResponseWriter, r *http.Request) {
	user := h.requireAdmin(w, r)
	if user == nil {
		return
	}

	feeds, err := h.DB.GetAllRSSFeeds()
	if err != nil {
		h.renderError(w, r, http.StatusInternalServerError, "Could not load feeds")
		return
	}

	h.render(w, r, "admin_feeds.html", map[string]interface{}{
		"Feeds": feeds,
	})
}
