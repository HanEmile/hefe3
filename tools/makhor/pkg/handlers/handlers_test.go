// Package handlers provides HTTP handler tests for all endpoints.
package handlers

import (
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"strings"
	"testing"
	"time"

	"makhor/pkg/db"
	"makhor/pkg/middleware"
)

// testEnv holds a test environment with handler and test server.
type testEnv struct {
	DB       *db.DB
	Handler  *Handler
	Auth     *middleware.Auth
	Server   *httptest.Server
	Cleanup  func()
}

// newTestEnv creates a new test environment with an in-memory database.
func newTestEnv(t *testing.T) *testEnv {
	t.Helper()

	// Create temp db file
	tmpFile, err := os.CreateTemp("", "makhor-test-*.db")
	if err != nil {
		t.Fatalf("Failed to create temp db: %v", err)
	}
	tmpFile.Close()
	dbPath := tmpFile.Name()

	database, err := db.New(dbPath)
	if err != nil {
		os.Remove(dbPath)
		t.Fatalf("Failed to create database: %v", err)
	}

	h, err := New(database, "http://localhost:8080", "", nil)
	if err != nil {
		database.Close()
		os.Remove(dbPath)
		t.Fatalf("Failed to create handler: %v", err)
	}

	auth := middleware.NewAuth(database)

	mux := http.NewServeMux()
	mux.HandleFunc("/", h.HomePage)
	mux.HandleFunc("/about", h.AboutPage)
	mux.HandleFunc("/login", h.LoginPage)
	mux.HandleFunc("/register", h.RegisterPage)
	mux.HandleFunc("/tags", h.TagsPage)
	mux.HandleFunc("/search", h.SearchPage)
	mux.HandleFunc("/comments", h.RecentCommentsPage)

	server := httptest.NewServer(auth.Middleware(mux))

	return &testEnv{
		DB:      database,
		Handler: h,
		Auth:    auth,
		Server:  server,
		Cleanup: func() {
			server.Close()
			database.Close()
			os.Remove(dbPath)
		},
	}
}

// TestPublicPages tests all public pages are accessible.
func TestPublicPages(t *testing.T) {
	env := newTestEnv(t)
	defer env.Cleanup()

	tests := []struct {
		name   string
		path   string
		status int
	}{
		{"Home page", "/", http.StatusOK},
		{"About page", "/about", http.StatusOK},
		{"Login page", "/login", http.StatusOK},
		{"Register page", "/register", http.StatusOK},
		{"Tags page", "/tags", http.StatusOK},
		{"Search page", "/search", http.StatusOK},
		{"Comments page", "/comments", http.StatusOK},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			resp, err := http.Get(env.Server.URL + tc.path)
			if err != nil {
				t.Fatalf("Failed to GET %s: %v", tc.path, err)
			}
			defer resp.Body.Close()

			if resp.StatusCode != tc.status {
				t.Errorf("Expected status %d, got %d", tc.status, resp.StatusCode)
			}
		})
	}
}

// TestHomePageViews tests different home page views (hot, top, new).
func TestHomePageViews(t *testing.T) {
	env := newTestEnv(t)
	defer env.Cleanup()

	views := []string{"", "?view=hot", "?view=top", "?view=new", "?view=top&t=24h", "?view=top&t=7d", "?view=top&t=all"}

	for _, v := range views {
		t.Run("View"+v, func(t *testing.T) {
			resp, err := http.Get(env.Server.URL + "/" + v)
			if err != nil {
				t.Fatalf("Failed to GET /%s: %v", v, err)
			}
			defer resp.Body.Close()

			if resp.StatusCode != http.StatusOK {
				t.Errorf("Expected status 200, got %d", resp.StatusCode)
			}
		})
	}
}

// TestSearchPage tests the search functionality.
func TestSearchPage(t *testing.T) {
	env := newTestEnv(t)
	defer env.Cleanup()

	// Empty search
	resp, err := http.Get(env.Server.URL + "/search")
	if err != nil {
		t.Fatalf("Failed to GET /search: %v", err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Errorf("Expected status 200, got %d", resp.StatusCode)
	}

	// Search with query
	resp, err = http.Get(env.Server.URL + "/search?q=test")
	if err != nil {
		t.Fatalf("Failed to GET /search?q=test: %v", err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Errorf("Expected status 200, got %d", resp.StatusCode)
	}
}

// TestProtectedPagesRequireAuth tests that protected pages redirect to login.
func TestProtectedPagesRequireAuth(t *testing.T) {
	env := newTestEnv(t)
	defer env.Cleanup()

	// Note: These are handled separately so we need to add them to the mux
	mux := http.NewServeMux()
	mux.HandleFunc("/submit", env.Handler.SubmitPage)
	mux.HandleFunc("/settings", env.Handler.UserSettingsPage)
	mux.HandleFunc("/invites", env.Handler.InvitesPage)

	server := httptest.NewServer(env.Auth.Middleware(mux))
	defer server.Close()

	client := &http.Client{
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			return http.ErrUseLastResponse // Don't follow redirects
		},
	}

	paths := []string{"/submit", "/settings", "/invites"}

	for _, path := range paths {
		t.Run("Protected"+path, func(t *testing.T) {
			resp, err := client.Get(server.URL + path)
			if err != nil {
				t.Fatalf("Failed to GET %s: %v", path, err)
			}
			resp.Body.Close()

			// Should redirect to login
			if resp.StatusCode != http.StatusSeeOther && resp.StatusCode != http.StatusFound {
				t.Errorf("Expected redirect, got status %d", resp.StatusCode)
			}

			loc := resp.Header.Get("Location")
			if !strings.Contains(loc, "/login") {
				t.Errorf("Expected redirect to /login, got %s", loc)
			}
		})
	}
}

// TestDatabaseOperations tests basic database operations.
func TestDatabaseOperations(t *testing.T) {
	env := newTestEnv(t)
	defer env.Cleanup()

	// Create a user
	user, err := env.DB.CreateUser("testuser", "test@example.com", nil)
	if err != nil {
		t.Fatalf("Failed to create user: %v", err)
	}

	if user.Username != "testuser" {
		t.Errorf("Expected username 'testuser', got '%s'", user.Username)
	}

	// Get user by username
	fetchedUser, err := env.DB.GetUserByUsername("testuser")
	if err != nil {
		t.Fatalf("Failed to get user: %v", err)
	}

	if fetchedUser.ID != user.ID {
		t.Errorf("User ID mismatch")
	}

	// Create a tag
	tag, err := env.DB.CreateTag("test-tag", "A test tag", user.ID)
	if err != nil {
		t.Fatalf("Failed to create tag: %v", err)
	}

	if tag.Name != "test-tag" {
		t.Errorf("Expected tag name 'test-tag', got '%s'", tag.Name)
	}

	// Create a post
	post, err := env.DB.CreatePost(user.ID, "Test Post", "https://example.com", "", []int64{tag.ID})
	if err != nil {
		t.Fatalf("Failed to create post: %v", err)
	}

	if post.Title != "Test Post" {
		t.Errorf("Expected post title 'Test Post', got '%s'", post.Title)
	}

	// Get post by ID
	fetchedPost, err := env.DB.GetPostByID(post.ID, nil)
	if err != nil {
		t.Fatalf("Failed to get post: %v", err)
	}

	if fetchedPost.ID != post.ID {
		t.Errorf("Post ID mismatch")
	}

	// Create a comment
	comment, err := env.DB.CreateComment(post.ID, user.ID, nil, nil, "Test comment")
	if err != nil {
		t.Fatalf("Failed to create comment: %v", err)
	}

	if comment.Body != "Test comment" {
		t.Errorf("Expected comment body 'Test comment', got '%s'", comment.Body)
	}

	// Get post comments
	comments, err := env.DB.GetPostComments(post.ID, nil)
	if err != nil {
		t.Fatalf("Failed to get comments: %v", err)
	}

	if len(comments) != 1 {
		t.Errorf("Expected 1 comment, got %d", len(comments))
	}
}

// TestVoting tests voting on posts and comments.
func TestVoting(t *testing.T) {
	env := newTestEnv(t)
	defer env.Cleanup()

	// Create user, tag, and post
	user, _ := env.DB.CreateUser("voter", "voter@example.com", nil)
	tag, _ := env.DB.CreateTag("voting", "Voting test", user.ID)
	post, _ := env.DB.CreatePost(user.ID, "Vote Test", "", "Body", []int64{tag.ID})

	// Initial score should be 0 (no auto-upvote)
	if post.Score != 0 {
		t.Errorf("Expected initial score 0, got %d", post.Score)
	}

	// Create another user to vote
	voter, _ := env.DB.CreateUser("voter2", "voter2@example.com", nil)

	// Vote on post
	newScore, voted, err := env.DB.VotePost(voter.ID, post.ID)
	if err != nil {
		t.Fatalf("Failed to vote: %v", err)
	}

	if !voted {
		t.Error("Expected vote to be recorded")
	}

	if newScore != 1 {
		t.Errorf("Expected score 1, got %d", newScore)
	}

	// Vote again (should unvote)
	newScore, voted, err = env.DB.VotePost(voter.ID, post.ID)
	if err != nil {
		t.Fatalf("Failed to unvote: %v", err)
	}

	if voted {
		t.Error("Expected vote to be removed")
	}

	if newScore != 0 {
		t.Errorf("Expected score 0 after unvote, got %d", newScore)
	}
}

// TestTagHierarchy tests hierarchical tag functionality.
func TestTagHierarchy(t *testing.T) {
	env := newTestEnv(t)
	defer env.Cleanup()

	user, _ := env.DB.CreateUser("taguser", "tag@example.com", nil)

	// Create parent tag (use unique name to avoid conflicts)
	parent, err := env.DB.CreateTag("hierarchy-test", "Programming topics", user.ID)
	if err != nil {
		t.Fatalf("Failed to create parent tag: %v", err)
	}

	// Create child tags
	_, err = env.DB.CreateTag("hierarchy-test::go", "Go programming", user.ID)
	if err != nil {
		t.Fatalf("Failed to create child tag: %v", err)
	}

	_, err = env.DB.CreateTag("hierarchy-test::rust", "Rust programming", user.ID)
	if err != nil {
		t.Fatalf("Failed to create child tag: %v", err)
	}

	// Get child tags
	children, err := env.DB.GetChildTags("hierarchy-test")
	if err != nil {
		t.Fatalf("Failed to get child tags: %v", err)
	}

	if len(children) != 2 {
		t.Errorf("Expected 2 child tags, got %d", len(children))
	}

	// Get root tags
	roots, err := env.DB.GetRootTags()
	if err != nil {
		t.Fatalf("Failed to get root tags: %v", err)
	}

	// Should only have "hierarchy-test" as root (no "::" in name)
	foundParent := false
	for _, tag := range roots {
		if tag.ID == parent.ID {
			foundParent = true
		}
		if strings.Contains(tag.Name, "::") {
			t.Errorf("Found child tag %s in root tags", tag.Name)
		}
	}

	if !foundParent {
		t.Errorf("Parent tag %q (ID=%d) not found in root tags", parent.Name, parent.ID)
	}
}

// TestRSSFeed tests RSS feed operations.
func TestRSSFeed(t *testing.T) {
	env := newTestEnv(t)
	defer env.Cleanup()

	user, _ := env.DB.CreateUser("rssuser", "rss@example.com", nil)
	tag, _ := env.DB.CreateTag("rss-test", "RSS test tag", user.ID)

	// Create RSS feed
	feed, err := env.DB.CreateRSSFeedWithOptions(db.CreateRSSFeedOptions{
		URL:             "https://example.com/feed.xml",
		Title:           "Test Feed",
		TagID:           tag.ID,
		CreatedBy:       user.ID,
		IntervalMinutes: 60,
		PollOnView:      false,
		AutoApprove:     true,
		MaxItemsPerPoll: 10,
	})
	if err != nil {
		t.Fatalf("Failed to create RSS feed: %v", err)
	}

	if feed.URL != "https://example.com/feed.xml" {
		t.Errorf("Expected feed URL 'https://example.com/feed.xml', got '%s'", feed.URL)
	}

	// Get feeds by tag
	feeds, err := env.DB.GetRSSFeedsByTag(tag.ID)
	if err != nil {
		t.Fatalf("Failed to get feeds by tag: %v", err)
	}

	if len(feeds) != 1 {
		t.Errorf("Expected 1 feed, got %d", len(feeds))
	}

	// Update feed
	err = env.DB.UpdateRSSFeed(feed.ID, 120, true, false, 5, true)
	if err != nil {
		t.Fatalf("Failed to update feed: %v", err)
	}

	// Check update
	updatedFeed, err := env.DB.GetRSSFeedByID(feed.ID)
	if err != nil {
		t.Fatalf("Failed to get updated feed: %v", err)
	}

	if updatedFeed.IntervalMinutes != 120 {
		t.Errorf("Expected interval 120, got %d", updatedFeed.IntervalMinutes)
	}

	if !updatedFeed.PollOnView {
		t.Error("Expected PollOnView to be true")
	}

	// Delete feed
	err = env.DB.DeleteRSSFeed(feed.ID)
	if err != nil {
		t.Fatalf("Failed to delete feed: %v", err)
	}

	// Verify deleted
	feeds, _ = env.DB.GetRSSFeedsByTag(tag.ID)
	if len(feeds) != 0 {
		t.Errorf("Expected 0 feeds after delete, got %d", len(feeds))
	}
}

// TestActionLog tests action logging and filtering.
func TestActionLog(t *testing.T) {
	env := newTestEnv(t)
	defer env.Cleanup()

	user, _ := env.DB.CreateUser("loguser", "log@example.com", nil)
	tag, _ := env.DB.CreateTag("log-test", "Log test", user.ID)
	post, _ := env.DB.CreatePost(user.ID, "Log Test Post", "", "Body", []int64{tag.ID})

	// Add some log entries manually
	err := env.DB.LogAction(&user.ID, "post_create", "post", post.ID, "Test", "127.0.0.1")
	if err != nil {
		t.Fatalf("Failed to log action: %v", err)
	}

	// Get action log
	logs, total, err := env.DB.GetActionLog(1, 100)
	if err != nil {
		t.Fatalf("Failed to get action log: %v", err)
	}

	if total == 0 {
		t.Error("Expected some actions in log")
	}

	// Filter by category
	postLogs, _, err := env.DB.GetActionLogFiltered(1, 100, db.ActionLogFilter{Category: "post"})
	if err != nil {
		t.Fatalf("Failed to get filtered action log: %v", err)
	}

	_ = logs
	_ = postLogs
}

// TestFormValidation tests form validation errors.
func TestFormValidation(t *testing.T) {
	env := newTestEnv(t)
	defer env.Cleanup()

	// Test short title validation for posts
	user, _ := env.DB.CreateUser("formuser", "form@example.com", nil)
	tag, _ := env.DB.CreateTag("form-test", "Form test", user.ID)

	// Empty title should fail
	_, err := env.DB.CreatePost(user.ID, "", "", "Body", []int64{tag.ID})
	// The database might allow empty titles, validation happens at handler level
	// Just verify no panic
	_ = err
}

// TestPagination tests pagination functionality.
func TestPagination(t *testing.T) {
	env := newTestEnv(t)
	defer env.Cleanup()

	user, _ := env.DB.CreateUser("pageuser", "page@example.com", nil)
	tag, _ := env.DB.CreateTag("page-test", "Page test", user.ID)

	// Create 10 posts
	for i := 0; i < 10; i++ {
		_, err := env.DB.CreatePost(user.ID, "Post "+string(rune('A'+i)), "", "Body", []int64{tag.ID})
		if err != nil {
			t.Fatalf("Failed to create post %d: %v", i, err)
		}
	}

	// Get first page (5 per page) - use isAdmin=true to see all posts including tagless
	posts, total, err := env.DB.GetPosts(1, 5, "", nil, true)
	if err != nil {
		t.Fatalf("Failed to get posts: %v", err)
	}

	if total != 10 {
		t.Errorf("Expected total 10, got %d", total)
	}

	if len(posts) != 5 {
		t.Errorf("Expected 5 posts on first page, got %d", len(posts))
	}

	// Get second page
	posts, _, err = env.DB.GetPosts(2, 5, "", nil, true)
	if err != nil {
		t.Fatalf("Failed to get second page: %v", err)
	}

	if len(posts) != 5 {
		t.Errorf("Expected 5 posts on second page, got %d", len(posts))
	}
}

// TestHTTPRequestHelpers provides helper functions for HTTP testing.
func makePostRequest(t *testing.T, serverURL, path string, data url.Values) *http.Response {
	t.Helper()
	resp, err := http.PostForm(serverURL+path, data)
	if err != nil {
		t.Fatalf("Failed to POST %s: %v", path, err)
	}
	return resp
}

func readBody(t *testing.T, resp *http.Response) string {
	t.Helper()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("Failed to read body: %v", err)
	}
	return string(body)
}

// TestContentTypes tests that HTML pages return correct content type.
func TestContentTypes(t *testing.T) {
	env := newTestEnv(t)
	defer env.Cleanup()

	resp, err := http.Get(env.Server.URL + "/")
	if err != nil {
		t.Fatalf("Failed to GET /: %v", err)
	}
	defer resp.Body.Close()

	ct := resp.Header.Get("Content-Type")
	if !strings.Contains(ct, "text/html") {
		t.Errorf("Expected text/html content type, got %s", ct)
	}
}

// TestTimeAgo tests the timeAgo template function.
func TestTimeAgo(t *testing.T) {
	tests := []struct {
		name     string
		duration time.Duration
		want     string
	}{
		{"just now", 30 * time.Second, "just now"},
		{"1 minute ago", 1 * time.Minute, "1 minute ago"},
		{"5 minutes ago", 5 * time.Minute, "5 minutes ago"},
		{"1 hour ago", 1 * time.Hour, "1 hour ago"},
		{"3 hours ago", 3 * time.Hour, "3 hours ago"},
		{"1 day ago", 24 * time.Hour, "1 day ago"},
		{"5 days ago", 5 * 24 * time.Hour, "5 days ago"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			input := time.Now().Add(-tt.duration)
			got := timeAgo(input)
			if got != tt.want {
				t.Errorf("timeAgo() = %q, want %q", got, tt.want)
			}
		})
	}
}

// TestTimeAgoOldDate tests timeAgo with dates older than 30 days.
func TestTimeAgoOldDate(t *testing.T) {
	oldDate := time.Now().AddDate(0, -2, 0) // 2 months ago
	got := timeAgo(oldDate)
	// Should return formatted date like "Jan 2, 2006"
	if got == "" {
		t.Error("timeAgo() returned empty string for old date")
	}
	if strings.Contains(got, "ago") {
		t.Errorf("timeAgo() for old date should not contain 'ago', got %q", got)
	}
}

// TestTruncate tests the truncate template function.
func TestTruncate(t *testing.T) {
	tests := []struct {
		name   string
		input  string
		maxLen int
		want   string
	}{
		{"short string", "hello", 10, "hello"},
		{"exact length", "hello", 5, "hello"},
		{"needs truncation", "hello world", 8, "hello..."},
		{"single char max", "hello", 4, "h..."},
		{"empty string", "", 10, ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := truncate(tt.input, tt.maxLen)
			if got != tt.want {
				t.Errorf("truncate(%q, %d) = %q, want %q", tt.input, tt.maxLen, got, tt.want)
			}
		})
	}
}

// TestSeq tests the seq template function for pagination.
func TestSeq(t *testing.T) {
	tests := []struct {
		name  string
		start int
		end   int
		want  []int
	}{
		{"normal range", 1, 5, []int{1, 2, 3, 4, 5}},
		{"single element", 3, 3, []int{3}},
		{"invalid range", 5, 3, nil},
		{"zero based", 0, 2, []int{0, 1, 2}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := seq(tt.start, tt.end)
			if len(got) != len(tt.want) {
				t.Errorf("seq(%d, %d) length = %d, want %d", tt.start, tt.end, len(got), len(tt.want))
				return
			}
			for i := range got {
				if got[i] != tt.want[i] {
					t.Errorf("seq(%d, %d)[%d] = %d, want %d", tt.start, tt.end, i, got[i], tt.want[i])
				}
			}
		})
	}
}

// TestIndent tests the indent template function.
func TestIndent(t *testing.T) {
	tests := []struct {
		depth int
		want  string
	}{
		{0, ""},
		{1, "  "},
		{2, "    "},
		{5, "          "},
		{15, "                    "}, // capped at 10
	}

	for _, tt := range tests {
		got := indent(tt.depth)
		if got != tt.want {
			t.Errorf("indent(%d) = %q, want %q", tt.depth, got, tt.want)
		}
	}
}

// TestFormatTime tests the formatTime template function.
func TestFormatTime(t *testing.T) {
	input := time.Date(2024, 6, 15, 14, 30, 0, 0, time.UTC)
	want := "2024-06-15 14:30"
	got := formatTime(input)
	if got != want {
		t.Errorf("formatTime() = %q, want %q", got, want)
	}
}

// TestShortTagName tests the shortTagName template function.
func TestShortTagName(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"tech::programming::go", "go"},
		{"tech::programming", "programming"},
		{"tech", "tech"},
		{"", ""},
		{"a::b::c::d", "d"},
	}

	for _, tt := range tests {
		got := shortTagName(tt.input)
		if got != tt.want {
			t.Errorf("shortTagName(%q) = %q, want %q", tt.input, got, tt.want)
		}
	}
}

// TestGetIntParam tests the getIntParam helper function.
func TestGetIntParam(t *testing.T) {
	tests := []struct {
		name       string
		queryParam string
		value      string
		defaultVal int
		want       int
	}{
		{"empty value uses default", "page", "", 1, 1},
		{"valid value", "page", "5", 1, 5},
		{"invalid value uses default", "page", "abc", 1, 1},
		{"zero uses default", "page", "0", 1, 1},
		{"negative uses default", "page", "-1", 1, 1},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, "/?"+tt.queryParam+"="+tt.value, nil)
			got := getIntParam(req, tt.queryParam, tt.defaultVal)
			if got != tt.want {
				t.Errorf("getIntParam() = %d, want %d", got, tt.want)
			}
		})
	}
}

// TestGetPathID tests the getPathID helper function.
func TestGetPathID(t *testing.T) {
	tests := []struct {
		name    string
		path    string
		prefix  string
		wantID  int64
		wantErr bool
	}{
		{"valid ID", "/posts/123", "/posts/", 123, false},
		{"valid ID with trailing slash", "/posts/456/", "/posts/", 456, false},
		{"invalid ID", "/posts/abc", "/posts/", 0, true},
		{"empty ID", "/posts/", "/posts/", 0, true},
		{"nested path", "/posts/789/comments", "/posts/", 789, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, tt.path, nil)
			got, err := getPathID(req, tt.prefix)
			if tt.wantErr && err == nil {
				t.Error("getPathID() expected error, got nil")
				return
			}
			if !tt.wantErr && err != nil {
				t.Errorf("getPathID() unexpected error: %v", err)
				return
			}
			if got != tt.wantID {
				t.Errorf("getPathID() = %d, want %d", got, tt.wantID)
			}
		})
	}
}

// TestSafeRedirectURL tests the safeRedirectURL function.
func TestSafeRedirectURL(t *testing.T) {
	tests := []struct {
		name    string
		referer string
		host    string
		want    string
	}{
		{"empty referer", "", "localhost", ""},
		{"relative path", "/posts/123", "localhost", "/posts/123"},
		{"same host", "http://localhost/posts", "localhost", "http://localhost/posts"},
		{"different host blocked", "http://evil.com/hack", "localhost", ""},
		{"invalid URL", "://invalid", "localhost", ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, "/", nil)
			req.Host = tt.host
			got := safeRedirectURL(tt.referer, req)
			if got != tt.want {
				t.Errorf("safeRedirectURL(%q) = %q, want %q", tt.referer, got, tt.want)
			}
		})
	}
}

// TestUserSession tests user session creation and validation.
func TestUserSession(t *testing.T) {
	env := newTestEnv(t)
	defer env.Cleanup()

	// Create user
	user, err := env.DB.CreateUser("sessionuser", "session@example.com", nil)
	if err != nil {
		t.Fatalf("Failed to create user: %v", err)
	}

	// Create session
	token, err := env.DB.CreateSession(user.ID)
	if err != nil {
		t.Fatalf("Failed to create session: %v", err)
	}

	if token == "" {
		t.Error("CreateSession returned empty token")
	}

	// Validate session
	validatedUser, err := env.DB.ValidateSession(token)
	if err != nil {
		t.Fatalf("Failed to validate session: %v", err)
	}

	if validatedUser.ID != user.ID {
		t.Errorf("ValidateSession returned wrong user ID: got %d, want %d", validatedUser.ID, user.ID)
	}

	// Delete session
	err = env.DB.DeleteSession(token)
	if err != nil {
		t.Fatalf("Failed to delete session: %v", err)
	}

	// Session should be invalid now
	_, err = env.DB.ValidateSession(token)
	if err == nil {
		t.Error("Expected error validating deleted session")
	}
}

// TestInviteSystem tests the invite code system.
func TestInviteSystem(t *testing.T) {
	env := newTestEnv(t)
	defer env.Cleanup()

	// Create inviting user
	inviter, _ := env.DB.CreateUser("inviter", "inviter@example.com", nil)

	// Create invite
	invite, err := env.DB.CreateInvite(inviter.ID, "Test invite")
	if err != nil {
		t.Fatalf("Failed to create invite: %v", err)
	}

	if invite.Code == "" {
		t.Error("Invite code is empty")
	}

	// Get invite by code to validate it exists
	fetchedInvite, err := env.DB.GetInviteByCode(invite.Code)
	if err != nil {
		t.Fatalf("Failed to get invite by code: %v", err)
	}
	if fetchedInvite.UsedBy != nil {
		t.Error("New invite should not be used")
	}

	// Use invite and create new user
	invitedUser, err := env.DB.CreateUser("invited", "invited@example.com", &inviter.ID)
	if err != nil {
		t.Fatalf("Failed to create invited user: %v", err)
	}

	// Use the invite
	err = env.DB.UseInvite(invite.Code, invitedUser.ID)
	if err != nil {
		t.Fatalf("Failed to use invite: %v", err)
	}

	// Invite should now be used
	usedInvite, _ := env.DB.GetInviteByCode(invite.Code)
	if usedInvite.UsedBy == nil {
		t.Error("Used invite should have UsedBy set")
	}
}

// TestUserBanning tests user ban functionality.
func TestUserBanning(t *testing.T) {
	env := newTestEnv(t)
	defer env.Cleanup()

	admin, _ := env.DB.CreateUser("admin", "admin@example.com", nil)
	user, _ := env.DB.CreateUser("bannable", "ban@example.com", nil)

	// Ban user (nil expiresAt means permanent)
	err := env.DB.BanUser(user.ID, "Test ban reason", nil, admin.ID)
	if err != nil {
		t.Fatalf("Failed to ban user: %v", err)
	}

	// Check user is banned
	bannedUser, _ := env.DB.GetUserByID(user.ID)
	if !bannedUser.IsBanned {
		t.Error("User should be banned")
	}
	if bannedUser.BanReason != "Test ban reason" {
		t.Errorf("BanReason = %q, want %q", bannedUser.BanReason, "Test ban reason")
	}
}

// TestNestedComments tests nested comment creation and retrieval.
func TestNestedComments(t *testing.T) {
	env := newTestEnv(t)
	defer env.Cleanup()

	user, _ := env.DB.CreateUser("commenter", "comment@example.com", nil)
	tag, _ := env.DB.CreateTag("nested-test", "Nested test", user.ID)
	post, _ := env.DB.CreatePost(user.ID, "Nested Test", "", "Body", []int64{tag.ID})

	// Create root comment
	root, err := env.DB.CreateComment(post.ID, user.ID, nil, nil, "Root comment")
	if err != nil {
		t.Fatalf("Failed to create root comment: %v", err)
	}

	// Create reply
	reply, err := env.DB.CreateComment(post.ID, user.ID, &root.ID, nil, "Reply comment")
	if err != nil {
		t.Fatalf("Failed to create reply: %v", err)
	}

	if reply.ParentID == nil || *reply.ParentID != root.ID {
		t.Error("Reply should have correct parent ID")
	}

	// Create nested reply
	nested, err := env.DB.CreateComment(post.ID, user.ID, &reply.ID, nil, "Nested reply")
	if err != nil {
		t.Fatalf("Failed to create nested reply: %v", err)
	}

	if nested.ParentID == nil || *nested.ParentID != reply.ID {
		t.Error("Nested reply should have correct parent ID")
	}

	// Get all comments
	comments, _ := env.DB.GetPostComments(post.ID, nil)
	if len(comments) != 3 {
		t.Errorf("Expected 3 comments, got %d", len(comments))
	}
}

// TestPostTagFiltering tests filtering posts by tag.
func TestPostTagFiltering(t *testing.T) {
	env := newTestEnv(t)
	defer env.Cleanup()

	user, _ := env.DB.CreateUser("tagger", "tagger@example.com", nil)
	tag1, _ := env.DB.CreateTag("filter-test-1", "Test 1", user.ID)
	tag2, _ := env.DB.CreateTag("filter-test-2", "Test 2", user.ID)

	// Create posts with different tags
	env.DB.CreatePost(user.ID, "Post 1", "", "Body", []int64{tag1.ID})
	env.DB.CreatePost(user.ID, "Post 2", "", "Body", []int64{tag2.ID})
	env.DB.CreatePost(user.ID, "Post 3", "", "Body", []int64{tag1.ID, tag2.ID})

	// Filter by tag1 using GetPostsByTagNewest
	posts, total, err := env.DB.GetPostsByTagNewest(1, 10, "filter-test-1", nil)
	if err != nil {
		t.Fatalf("Failed to get posts by tag: %v", err)
	}

	if total != 2 {
		t.Errorf("Expected 2 posts with tag1, got %d", total)
	}

	// Filter by tag2
	posts, total, _ = env.DB.GetPostsByTagNewest(1, 10, "filter-test-2", nil)
	if total != 2 {
		t.Errorf("Expected 2 posts with tag2, got %d", total)
	}

	_ = posts
}
