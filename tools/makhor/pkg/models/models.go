// Package models defines all data structures used throughout the application.
// These structs map to database tables and are used for data transfer.
package models

import (
	"time"
)

// User represents a registered user account.
type User struct {
	ID           int64
	Username     string
	Email        string
	CreatedAt    time.Time
	InvitedBy    *int64 // ID of the user who invited this user
	IsAdmin      bool
	IsBanned     bool
	About        string
	Avatar       []byte // Raw avatar image data
	AvatarType   string // MIME type of avatar
	BanReason    string
	BanExpiresAt *time.Time
	BannedBy     *int64

	// Joined fields
	BannedByUsername string
}

// LoginToken represents a passwordless login token sent via email.
type LoginToken struct {
	ID        int64
	Email     string
	Token     string
	CreatedAt time.Time
	ExpiresAt time.Time
	Used      bool
}

// Session represents an active user session.
type Session struct {
	ID        int64
	UserID    int64
	Token     string
	CreatedAt time.Time
	ExpiresAt time.Time
}

// Invite represents an invitation code for new users.
type Invite struct {
	ID             int64
	Code           string
	CreatedBy      int64
	CreatedAt      time.Time
	UsedBy         *int64
	UsedAt         *time.Time
	Note           string
	UsedByUsername string // Username of who used the invite
}

// Tag represents a category for posts.
type Tag struct {
	ID          int64
	Name        string
	Description string
	CreatorID   *int64
	CreatedAt   time.Time
	IsActive    bool

	// Joined fields
	CreatorUsername string
	Admins          []TagAdmin
}

// TagAdmin represents a user who can moderate a specific tag.
type TagAdmin struct {
	ID        int64
	TagID     int64
	UserID    int64
	GrantedBy *int64
	GrantedAt time.Time

	// Joined fields
	Username        string
	GrantedByName   string
}

// PostSource identifies where a post originated from.
type PostSource string

const (
	SourceUser PostSource = "user" // User-submitted post
	SourceRSS  PostSource = "rss"  // Imported from RSS feed
	SourceBot  PostSource = "bot"  // Created by a bot
	SourceAPI  PostSource = "api"  // Created via API
)

// Post represents a submitted link or text post.
type Post struct {
	ID         int64
	UserID     int64
	Title      string
	URL        string // Empty for text-only posts
	Body       string
	CreatedAt  time.Time
	UpdatedAt  time.Time
	Score      int
	IsDeleted  bool
	SourceType PostSource // Where this post came from
	SourceID   *int64     // ID of the source (e.g., rss_feed.id)
	HatID      *int64     // Optional hat worn when posting

	// Joined fields (not stored directly)
	Username     string
	Tags         []Tag
	CommentCount int
	Domain       string // Extracted from URL for display
	UserVoted    bool   // Whether current user voted on this
	SourceName   string // Name of the source (e.g., RSS feed title)
	Hat          *Hat   // Hat worn when posting (if any)
}

// Comment represents a comment on a post.
type Comment struct {
	ID        int64
	PostID    int64
	UserID    int64
	ParentID  *int64 // NULL for top-level comments
	Body      string
	CreatedAt time.Time
	UpdatedAt time.Time
	Score     int
	IsDeleted bool
	IsBlurred bool // Blurred comments appear at bottom
	HatID     *int64

	// Joined fields
	Username  string
	PostTitle string
	Hat       *Hat
	Children  []*Comment // For threaded display
	Depth     int        // Nesting depth for display
	UserVoted bool
	BodyLen   int // Length of body for TOC display
}

// Hat represents a verified identity for official responses.
type Hat struct {
	ID           int64
	UserID       int64
	Name         string // e.g., "Core Team Member"
	Organization string // e.g., "Golang"
	Link         string // Verification URL
	GrantedBy    *int64
	GrantedAt    time.Time
	IsActive     bool

	// Joined fields
	Username string
}

// Vote represents a user's vote on a post or comment.
type Vote struct {
	ID        int64
	UserID    int64
	PostID    *int64
	CommentID *int64
	Value     int // 1 for upvote
	CreatedAt time.Time
}

// PostRevision represents a historical version of a post.
type PostRevision struct {
	ID        int64
	PostID    int64
	UserID    int64
	Title     string
	Body      string
	CreatedAt time.Time

	// Joined fields
	Username string
}

// ActionLog represents an auditable action in the system.
type ActionLog struct {
	ID         int64
	UserID     *int64
	Action     string // e.g., "post_create", "comment_create", "vote"
	TargetType string // "post", "comment", "user", etc.
	TargetID   int64
	Details    string // Additional context
	IPAddress  string
	CreatedAt  time.Time

	// Joined fields
	Username   string
	TargetInfo string // Human-readable target description
}

// SavedItem represents a bookmarked post or comment.
type SavedItem struct {
	ID        int64
	UserID    int64
	PostID    *int64
	CommentID *int64
	CreatedAt time.Time
}

// InviteTreeNode represents a node in the invite tree for display.
type InviteTreeNode struct {
	User     *User
	Children []*InviteTreeNode
}

// ActionType constants for the action log.
const (
	ActionUserCreate     = "user_create"
	ActionUserLogin      = "user_login"
	ActionUserUpdate     = "user_update"
	ActionPostCreate     = "post_create"
	ActionPostUpdate     = "post_update"
	ActionPostDelete     = "post_delete"
	ActionCommentCreate  = "comment_create"
	ActionCommentUpdate  = "comment_update"
	ActionCommentDelete  = "comment_delete"
	ActionVotePost       = "vote_post"
	ActionVoteComment    = "vote_comment"
	ActionInviteCreate   = "invite_create"
	ActionInviteUse      = "invite_use"
	ActionHatGrant       = "hat_grant"
	ActionHatRevoke      = "hat_revoke"
	ActionModBan         = "mod_ban"
	ActionModUnban       = "mod_unban"
	ActionTagCreate       = "tag_create"
	ActionTagUpdate       = "tag_update"
	ActionTagAdminAdd     = "tag_admin_add"
	ActionTagAdminRemove  = "tag_admin_remove"
	ActionRemovePostTag   = "remove_post_tag"
	ActionCommentBlur     = "comment_blur"
	ActionCommentUnblur   = "comment_unblur"
	ActionCommentTreeDel  = "comment_tree_delete"
	ActionFeedAdd         = "feed_add"
	ActionFeedUpdate      = "feed_update"
	ActionFeedDelete      = "feed_delete"
	ActionFeedSync        = "feed_sync"
	ActionTagDelete       = "tag_delete"
)

// Pagination holds pagination parameters.
type Pagination struct {
	Page       int
	PerPage    int
	Total      int
	TotalPages int
}

// NewPagination creates a new pagination with defaults.
func NewPagination(page, perPage, total int) Pagination {
	if page < 1 {
		page = 1
	}
	if perPage < 1 {
		perPage = 30
	}
	if perPage > 100 {
		perPage = 100
	}

	totalPages := total / perPage
	if total%perPage > 0 {
		totalPages++
	}

	return Pagination{
		Page:       page,
		PerPage:    perPage,
		Total:      total,
		TotalPages: totalPages,
	}
}

// Offset returns the SQL offset for this page.
func (p Pagination) Offset() int {
	return (p.Page - 1) * p.PerPage
}

// HasPrev returns true if there's a previous page.
func (p Pagination) HasPrev() bool {
	return p.Page > 1
}

// HasNext returns true if there's a next page.
func (p Pagination) HasNext() bool {
	return p.Page < p.TotalPages
}

// PageWindow returns a sliding window of page numbers centered on the current page.
// Shows up to maxPages pages, with the current page roughly in the middle.
// Returns (startPage, endPage, showFirstEllipsis, showLastEllipsis).
func (p Pagination) PageWindow(maxPages int) (start, end int, showFirst, showLast bool) {
	if p.TotalPages <= maxPages {
		// Show all pages if total is within limit
		return 1, p.TotalPages, false, false
	}

	// Calculate window centered on current page
	// For maxPages=10: show 4 pages before and 5 pages after current (10 total including current)
	pagesBefore := (maxPages - 1) / 2
	pagesAfter := maxPages - 1 - pagesBefore

	start = p.Page - pagesBefore
	end = p.Page + pagesAfter

	// Adjust if window extends past boundaries
	if start < 1 {
		// Shift window right
		end += (1 - start)
		start = 1
	}
	if end > p.TotalPages {
		// Shift window left
		start -= (end - p.TotalPages)
		end = p.TotalPages
	}
	// Ensure start doesn't go below 1 after adjustment
	if start < 1 {
		start = 1
	}

	// Determine if we need ellipses
	showFirst = start > 1
	showLast = end < p.TotalPages

	return start, end, showFirst, showLast
}

// WindowPages returns a slice of page numbers for the sliding window.
// Uses a window of 10 pages by default.
func (p Pagination) WindowPages() []int {
	start, end, _, _ := p.PageWindow(10)
	pages := make([]int, 0, end-start+1)
	for i := start; i <= end; i++ {
		pages = append(pages, i)
	}
	return pages
}

// ShowFirstPage returns true if the first page should be shown separately (with ellipsis).
func (p Pagination) ShowFirstPage() bool {
	_, _, showFirst, _ := p.PageWindow(10)
	return showFirst
}

// ShowLastPage returns true if the last page should be shown separately (with ellipsis).
func (p Pagination) ShowLastPage() bool {
	_, _, _, showLast := p.PageWindow(10)
	return showLast
}
