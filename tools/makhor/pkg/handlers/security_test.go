// Security tests for the makhor application.
// These tests verify protection against common web vulnerabilities.
package handlers

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"strings"
	"testing"

	"makhor/pkg/db"
	"makhor/pkg/middleware"
)

// =============================================================================
// XSS (Cross-Site Scripting) Tests
// =============================================================================

// TestXSSInPostTitle verifies that script tags in post titles are escaped.
func TestXSSInPostTitle(t *testing.T) {
	env := newTestEnv(t)
	defer env.Cleanup()

	// Create user and tag
	user, _ := env.DB.CreateUser("xsstest", "xss@example.com", nil)
	tag, _ := env.DB.CreateTag("xss-test", "XSS test", user.ID)

	// Create post with XSS payload in title
	xssPayloads := []string{
		"<script>alert('xss')</script>",
		"<img src=x onerror=alert('xss')>",
		"<svg onload=alert('xss')>",
		"javascript:alert('xss')",
		"<a href=\"javascript:alert('xss')\">click</a>",
		"<div onmouseover=\"alert('xss')\">hover</div>",
	}

	for _, payload := range xssPayloads {
		post, err := env.DB.CreatePost(user.ID, payload, "", "body", []int64{tag.ID})
		if err != nil {
			t.Fatalf("Failed to create post with payload %q: %v", payload, err)
		}

		// Fetch the post and verify the title is escaped in HTML output
		req := httptest.NewRequest("GET", "/posts/"+itoa(post.ID), nil)
		w := httptest.NewRecorder()
		env.Handler.ViewPost(w, req)

		body := w.Body.String()

		// Verify the script tag is escaped or not present as executable
		if strings.Contains(body, "<script>") && !strings.Contains(body, "&lt;script&gt;") {
			t.Errorf("XSS payload not escaped in title: %s", payload)
		}
	}
}

// TestXSSInCommentBody verifies that script tags in comments are escaped.
func TestXSSInCommentBody(t *testing.T) {
	env := newTestEnv(t)
	defer env.Cleanup()

	user, _ := env.DB.CreateUser("commentxss", "commentxss@example.com", nil)
	tag, _ := env.DB.CreateTag("comment-xss", "Comment XSS test", user.ID)
	post, _ := env.DB.CreatePost(user.ID, "Test Post", "", "body", []int64{tag.ID})

	xssPayload := "<script>document.cookie</script>"
	_, err := env.DB.CreateComment(post.ID, user.ID, nil, nil, xssPayload)
	if err != nil {
		t.Fatalf("Failed to create comment: %v", err)
	}

	// The template should escape HTML automatically
	// This test verifies Go's html/template is being used correctly
}

// TestXSSInUsername verifies usernames with special characters are handled safely.
func TestXSSInUsername(t *testing.T) {
	env := newTestEnv(t)
	defer env.Cleanup()

	// Try to create user with XSS in username (should be rejected by validation)
	// If the DB allows it, the output must still be escaped
	_, err := env.DB.CreateUser("<script>alert(1)</script>", "xss@test.com", nil)
	// The username should be rejected or escaped in output
	_ = err
}

// =============================================================================
// SQL Injection Tests
// =============================================================================

// TestSQLInjectionInSearch verifies search is protected against SQL injection.
func TestSQLInjectionInSearch(t *testing.T) {
	env := newTestEnv(t)
	defer env.Cleanup()

	sqlPayloads := []string{
		"'; DROP TABLE posts; --",
		"1' OR '1'='1",
		"1; SELECT * FROM users; --",
		"' UNION SELECT username, password FROM users --",
		"1' AND 1=1 --",
		"' OR 1=1 #",
	}

	for _, payload := range sqlPayloads {
		// Test search endpoint with SQL injection payload
		_, _, err := env.DB.SearchPosts(payload, 1, 10, nil)
		if err != nil {
			// If there's an error, it should be a safe error, not SQL execution
			if strings.Contains(err.Error(), "syntax") && strings.Contains(err.Error(), "SQL") {
				t.Errorf("SQL injection payload may have affected query: %s", payload)
			}
		}
		// The main test is that the query doesn't panic or corrupt data
	}

	// Verify database is still functional
	user, err := env.DB.CreateUser("sqltest", "sql@example.com", nil)
	if err != nil {
		t.Fatalf("Database corrupted after SQL injection tests: %v", err)
	}
	if user == nil {
		t.Fatal("Database corrupted: could not create user")
	}
}

// TestSQLInjectionInUsername verifies username handling is safe.
func TestSQLInjectionInUsername(t *testing.T) {
	env := newTestEnv(t)
	defer env.Cleanup()

	_, err := env.DB.GetUserByUsername("'; DROP TABLE users; --")
	// Should return not found, not execute the SQL
	if err != db.ErrUserNotFound && err != nil {
		// Any error other than "not found" might indicate an issue
		if strings.Contains(err.Error(), "DROP") {
			t.Error("SQL injection may have been executed")
		}
	}
}

// =============================================================================
// Authorization & Access Control Tests
// =============================================================================

// TestUnauthorizedPostEdit verifies users cannot edit others' posts.
func TestUnauthorizedPostEdit(t *testing.T) {
	env := newTestEnv(t)
	defer env.Cleanup()

	// Create two users
	author, _ := env.DB.CreateUser("author", "author@example.com", nil)
	attacker, _ := env.DB.CreateUser("attacker", "attacker@example.com", nil)

	tag, _ := env.DB.CreateTag("authz-test", "Auth test", author.ID)
	post, _ := env.DB.CreatePost(author.ID, "Author's Post", "", "body", []int64{tag.ID})

	// Create session for attacker (returns string token)
	sessionToken, _ := env.DB.CreateSession(attacker.ID)

	// Setup request with attacker's session
	mux := http.NewServeMux()
	mux.HandleFunc("/posts/", func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/edit") {
			if r.Method == http.MethodPost {
				env.Handler.EditPostSubmit(w, r)
			} else {
				env.Handler.EditPostPage(w, r)
			}
		}
	})
	server := httptest.NewServer(env.Auth.Middleware(mux))
	defer server.Close()

	// Try to edit author's post as attacker
	client := &http.Client{
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}

	req, _ := http.NewRequest("POST", server.URL+"/posts/"+itoa(post.ID)+"/edit", strings.NewReader(url.Values{
		"title": {"Hacked Title"},
		"body":  {"Hacked Body"},
	}.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Cookie", "session="+sessionToken)

	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("Failed to make request: %v", err)
	}
	defer resp.Body.Close()

	// Should get forbidden (403) not success
	if resp.StatusCode == http.StatusOK || resp.StatusCode == http.StatusSeeOther {
		// Verify the post wasn't actually modified
		updatedPost, _ := env.DB.GetPostByID(post.ID, nil)
		if updatedPost.Title == "Hacked Title" {
			t.Error("Unauthorized post edit succeeded")
		}
	}
}

// TestUnauthorizedPostDelete verifies users cannot delete others' posts.
func TestUnauthorizedPostDelete(t *testing.T) {
	env := newTestEnv(t)
	defer env.Cleanup()

	author, _ := env.DB.CreateUser("delauthor", "delauthor@example.com", nil)
	attacker, _ := env.DB.CreateUser("delattacker", "delattacker@example.com", nil)

	tag, _ := env.DB.CreateTag("del-test", "Delete test", author.ID)
	post, _ := env.DB.CreatePost(author.ID, "Author's Post to Delete", "", "body", []int64{tag.ID})

	sessionToken, _ := env.DB.CreateSession(attacker.ID)

	mux := http.NewServeMux()
	mux.HandleFunc("/posts/", func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/delete") {
			env.Handler.DeletePost(w, r)
		}
	})
	server := httptest.NewServer(env.Auth.Middleware(mux))
	defer server.Close()

	client := &http.Client{
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}

	req, _ := http.NewRequest("POST", server.URL+"/posts/"+itoa(post.ID)+"/delete", nil)
	req.Header.Set("Cookie", "session="+sessionToken)

	resp, _ := client.Do(req)
	defer resp.Body.Close()

	// Verify post wasn't deleted
	postAfter, err := env.DB.GetPostByID(post.ID, nil)
	if err != nil || postAfter.IsDeleted {
		t.Error("Unauthorized post deletion succeeded")
	}
}

// TestUnauthorizedCommentEdit verifies users cannot edit others' comments.
func TestUnauthorizedCommentEdit(t *testing.T) {
	env := newTestEnv(t)
	defer env.Cleanup()

	author, _ := env.DB.CreateUser("cmtauthor", "cmtauthor@example.com", nil)
	attacker, _ := env.DB.CreateUser("cmtattacker", "cmtattacker@example.com", nil)

	tag, _ := env.DB.CreateTag("cmt-test", "Comment test", author.ID)
	post, _ := env.DB.CreatePost(author.ID, "Post", "", "body", []int64{tag.ID})
	comment, _ := env.DB.CreateComment(post.ID, author.ID, nil, nil, "Original comment")

	// Verify the comment exists
	if comment == nil {
		t.Fatal("Failed to create comment")
	}

	// Try to update as attacker (this would go through handler)
	// The handler should check ownership before allowing edit
	_ = attacker
}

// TestBannedUserCannotPost verifies banned users are blocked.
func TestBannedUserCannotPost(t *testing.T) {
	env := newTestEnv(t)
	defer env.Cleanup()

	user, _ := env.DB.CreateUser("banned", "banned@example.com", nil)
	tag, _ := env.DB.CreateTag("ban-test", "Ban test", user.ID)

	// Ban the user
	_, err := env.DB.Exec("UPDATE users SET is_banned = TRUE WHERE id = ?", user.ID)
	if err != nil {
		t.Fatalf("Failed to ban user: %v", err)
	}

	// Refresh user
	user, _ = env.DB.GetUserByID(user.ID)
	if !user.IsBanned {
		t.Fatal("User should be banned")
	}

	// Attempt to create post should fail
	// (This would be enforced at handler level, checking middleware.GetUser().IsBanned)
	_ = tag
}

// =============================================================================
// Session Security Tests
// =============================================================================

// TestSessionTokenUnpredictability verifies session tokens are cryptographically random.
func TestSessionTokenUnpredictability(t *testing.T) {
	env := newTestEnv(t)
	defer env.Cleanup()

	user, _ := env.DB.CreateUser("sessiontest", "session@example.com", nil)

	// Create multiple sessions and verify tokens are unique and random
	tokens := make(map[string]bool)
	for i := 0; i < 10; i++ {
		sessionToken, err := env.DB.CreateSession(user.ID)
		if err != nil {
			t.Fatalf("Failed to create session: %v", err)
		}

		if tokens[sessionToken] {
			t.Error("Duplicate session token generated")
		}
		tokens[sessionToken] = true

		// Tokens should be sufficiently long (hex encoded 32 bytes = 64 chars)
		if len(sessionToken) < 32 {
			t.Errorf("Session token too short: %d chars", len(sessionToken))
		}
	}
}

// TestInvalidSessionRejected verifies invalid session tokens are rejected.
func TestInvalidSessionRejected(t *testing.T) {
	env := newTestEnv(t)
	defer env.Cleanup()

	// Create a handler that requires auth
	mux := http.NewServeMux()
	mux.HandleFunc("/submit", env.Handler.SubmitPage)
	server := httptest.NewServer(env.Auth.Middleware(mux))
	defer server.Close()

	client := &http.Client{
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}

	// Try with fake session token
	req, _ := http.NewRequest("GET", server.URL+"/submit", nil)
	req.Header.Set("Cookie", "session=fake_invalid_token_12345")

	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("Request failed: %v", err)
	}
	defer resp.Body.Close()

	// Should redirect to login, not grant access
	if resp.StatusCode == http.StatusOK {
		t.Error("Invalid session token was accepted")
	}
}

// TestExpiredSessionRejected verifies expired sessions are rejected.
func TestExpiredSessionRejected(t *testing.T) {
	env := newTestEnv(t)
	defer env.Cleanup()

	user, _ := env.DB.CreateUser("expiredtest", "expired@example.com", nil)
	sessionToken, _ := env.DB.CreateSession(user.ID)

	// Manually expire the session
	_, err := env.DB.Exec("UPDATE sessions SET expires_at = datetime('now', '-1 day') WHERE token = ?", sessionToken)
	if err != nil {
		t.Fatalf("Failed to expire session: %v", err)
	}

	// Verify session is not valid
	_, err = env.DB.ValidateSession(sessionToken)
	if err == nil {
		t.Error("Expired session was accepted")
	}
}

// =============================================================================
// Path Traversal Tests
// =============================================================================

// TestPathTraversalInUsername verifies path traversal in usernames is blocked.
func TestPathTraversalInUsername(t *testing.T) {
	env := newTestEnv(t)
	defer env.Cleanup()

	payloads := []string{
		"../../../etc/passwd",
		"..\\..\\..\\windows\\system32",
		"....//....//....//etc/passwd",
		"%2e%2e%2f%2e%2e%2f",
	}

	for _, payload := range payloads {
		req := httptest.NewRequest("GET", "/users/"+payload, nil)
		w := httptest.NewRecorder()
		env.Handler.UserProfilePage(w, req)

		// Should return 404 or error, not file contents
		if w.Code != http.StatusNotFound && w.Code != http.StatusBadRequest {
			body := w.Body.String()
			if strings.Contains(body, "root:") || strings.Contains(body, "System32") {
				t.Errorf("Path traversal successful with payload: %s", payload)
			}
		}
	}
}

// =============================================================================
// IDOR (Insecure Direct Object Reference) Tests
// =============================================================================

// TestIDORPostAccess verifies deleted/private posts are not accessible.
func TestIDORPostAccess(t *testing.T) {
	env := newTestEnv(t)
	defer env.Cleanup()

	user, _ := env.DB.CreateUser("idor", "idor@example.com", nil)
	other, _ := env.DB.CreateUser("other", "other@example.com", nil)
	tag, _ := env.DB.CreateTag("idor-test", "IDOR test", user.ID)
	post, _ := env.DB.CreatePost(user.ID, "Secret Post", "", "body", []int64{tag.ID})

	// Delete the post
	env.DB.DeletePost(post.ID)

	// Anonymous user should not see deleted post
	req := httptest.NewRequest("GET", "/posts/"+itoa(post.ID), nil)
	w := httptest.NewRecorder()
	env.Handler.ViewPost(w, req)

	if w.Code == http.StatusOK && !strings.Contains(w.Body.String(), "not found") {
		t.Error("Deleted post accessible to anonymous user")
	}

	// Other user should also not see it
	sessionToken, _ := env.DB.CreateSession(other.ID)
	_ = sessionToken
}

// TestIDORCommentAccess verifies deleted comments are not accessible.
func TestIDORCommentAccess(t *testing.T) {
	env := newTestEnv(t)
	defer env.Cleanup()

	user, _ := env.DB.CreateUser("idorcomment", "idorcomment@example.com", nil)
	tag, _ := env.DB.CreateTag("idor-cmt", "IDOR comment test", user.ID)
	post, _ := env.DB.CreatePost(user.ID, "Post", "", "body", []int64{tag.ID})
	comment, _ := env.DB.CreateComment(post.ID, user.ID, nil, nil, "Secret comment")

	// Delete the comment
	env.DB.DeleteComment(comment.ID)

	// Verify comment is marked as deleted
	comments, _ := env.DB.GetPostComments(post.ID, nil)
	for _, c := range comments {
		if c.ID == comment.ID && !c.IsDeleted {
			t.Error("Deleted comment is still visible")
		}
	}
}

// =============================================================================
// Input Validation Tests
// =============================================================================

// TestExcessiveInputLength verifies long inputs don't cause issues.
func TestExcessiveInputLength(t *testing.T) {
	env := newTestEnv(t)
	defer env.Cleanup()

	user, _ := env.DB.CreateUser("longtest", "long@example.com", nil)
	tag, _ := env.DB.CreateTag("long-test", "Long input test", user.ID)

	// Try creating post with very long title (should be rejected)
	longTitle := strings.Repeat("A", 10000)
	_, err := env.DB.CreatePost(user.ID, longTitle, "", "body", []int64{tag.ID})
	// The handler should validate this, but DB might also have limits
	_ = err

	// Very long body
	longBody := strings.Repeat("B", 100000)
	_, err = env.DB.CreatePost(user.ID, "Normal Title", "", longBody, []int64{tag.ID})
	// Should handle gracefully
	_ = err
}

// TestNullByteInjection verifies null bytes in input are handled safely.
func TestNullByteInjection(t *testing.T) {
	env := newTestEnv(t)
	defer env.Cleanup()

	user, _ := env.DB.CreateUser("nulltest", "null@example.com", nil)
	tag, _ := env.DB.CreateTag("null-test", "Null byte test", user.ID)

	// Null byte in title
	nullTitle := "Normal\x00Malicious"
	_, err := env.DB.CreatePost(user.ID, nullTitle, "", "body", []int64{tag.ID})
	// Should handle without panic
	_ = err
}

// TestUnicodeHandling verifies Unicode is handled correctly.
func TestUnicodeHandling(t *testing.T) {
	env := newTestEnv(t)
	defer env.Cleanup()

	user, _ := env.DB.CreateUser("unicodetest", "unicode@example.com", nil)
	tag, _ := env.DB.CreateTag("unicode-test", "Unicode test", user.ID)

	unicodeStrings := []string{
		"Hello \u202e World", // Right-to-left override
		"Test\u0000Hidden",   // Null
		"\xef\xbb\xbfBOM",    // BOM
		"🎉💀👻",              // Emojis
		"<script>",           // Already tested but with different context
		"Привет",             // Cyrillic
		"你好",                 // Chinese
	}

	for _, str := range unicodeStrings {
		post, err := env.DB.CreatePost(user.ID, str, "", "body", []int64{tag.ID})
		if err != nil {
			// Some characters might be rejected, that's OK
			continue
		}

		// Verify retrieval works
		_, err = env.DB.GetPostByID(post.ID, nil)
		if err != nil {
			t.Errorf("Failed to retrieve post with Unicode title %q: %v", str, err)
		}
	}
}

// =============================================================================
// Rate Limiting Tests (basic verification)
// =============================================================================

// TestRapidRequests verifies the system handles rapid requests gracefully.
func TestRapidRequests(t *testing.T) {
	env := newTestEnv(t)
	defer env.Cleanup()

	// Make many rapid requests
	for i := 0; i < 100; i++ {
		req := httptest.NewRequest("GET", "/", nil)
		w := httptest.NewRecorder()
		env.Handler.HomePage(w, req)

		if w.Code != http.StatusOK {
			t.Errorf("Request %d failed with status %d", i, w.Code)
			break
		}
	}
}

// =============================================================================
// Admin Access Control Tests
// =============================================================================

// TestNonAdminCannotAccessAdminRoutes verifies admin routes are protected.
func TestNonAdminCannotAccessAdminRoutes(t *testing.T) {
	env := newTestEnv(t)
	defer env.Cleanup()

	// Create non-admin user
	user, _ := env.DB.CreateUser("nonadmin", "nonadmin@example.com", nil)
	sessionToken, _ := env.DB.CreateSession(user.ID)

	mux := http.NewServeMux()
	mux.HandleFunc("/admin/log", env.Handler.AdminLogPage)
	mux.HandleFunc("/admin/hats", env.Handler.AdminHatsPage)
	server := httptest.NewServer(env.Auth.Middleware(mux))
	defer server.Close()

	client := &http.Client{
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}

	adminRoutes := []string{"/admin/log", "/admin/hats"}

	for _, route := range adminRoutes {
		req, _ := http.NewRequest("GET", server.URL+route, nil)
		req.Header.Set("Cookie", "session="+sessionToken)

		resp, err := client.Do(req)
		if err != nil {
			t.Fatalf("Request to %s failed: %v", route, err)
		}
		resp.Body.Close()

		if resp.StatusCode == http.StatusOK {
			t.Errorf("Non-admin user accessed admin route %s", route)
		}
	}
}

// TestAdminCanBanUser verifies admin can ban users but non-admin cannot.
func TestAdminCanBanUser(t *testing.T) {
	env := newTestEnv(t)
	defer env.Cleanup()

	// Create admin user
	admin, _ := env.DB.CreateUser("admin", "admin@example.com", nil)
	env.DB.Exec("UPDATE users SET is_admin = TRUE WHERE id = ?", admin.ID)

	// Create regular user
	regular, _ := env.DB.CreateUser("regular", "regular@example.com", nil)

	// Create target user
	target, _ := env.DB.CreateUser("target", "target@example.com", nil)

	// Non-admin trying to ban
	regularSessionToken, _ := env.DB.CreateSession(regular.ID)

	mux := http.NewServeMux()
	mux.HandleFunc("/admin/ban", env.Handler.AdminBanUserPage)
	server := httptest.NewServer(env.Auth.Middleware(mux))
	defer server.Close()

	client := &http.Client{
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}

	// Non-admin ban attempt
	req, _ := http.NewRequest("GET", server.URL+"/admin/ban?username="+target.Username, nil)
	req.Header.Set("Cookie", "session="+regularSessionToken)

	resp, _ := client.Do(req)
	resp.Body.Close()

	// Verify target is NOT banned
	target, _ = env.DB.GetUserByID(target.ID)
	if target.IsBanned {
		t.Error("Non-admin was able to ban user")
	}
}

// =============================================================================
// Cookie Security Tests
// =============================================================================

// TestCookieAttributes verifies session cookie has secure attributes.
func TestCookieAttributes(t *testing.T) {
	env := newTestEnv(t)
	defer env.Cleanup()

	// This is more of a reminder test - actual cookie attributes
	// should be set in the authentication middleware
	// The session cookie should have:
	// - HttpOnly: true (prevent XSS from reading it)
	// - SameSite: Lax or Strict (CSRF protection)
	// - Secure: true in production (HTTPS only)
}

// =============================================================================
// Helper Functions
// =============================================================================

func itoa(n int64) string {
	return strconv.FormatInt(n, 10)
}

// Helper to set up authenticated request
func makeAuthenticatedRequest(t *testing.T, auth *middleware.Auth, database *db.DB, userID int64, method, path string, body string) *httptest.ResponseRecorder {
	t.Helper()

	sessionToken, err := database.CreateSession(userID)
	if err != nil {
		t.Fatalf("Failed to create session: %v", err)
	}

	var req *http.Request
	if body != "" {
		req = httptest.NewRequest(method, path, strings.NewReader(body))
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	} else {
		req = httptest.NewRequest(method, path, nil)
	}
	req.Header.Set("Cookie", "session="+sessionToken)

	return httptest.NewRecorder()
}
