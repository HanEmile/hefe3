package db

import (
	"os"
	"strings"
	"testing"
	"time"

	"makhor/pkg/models"
)

// testDB creates a temporary database for testing.
func testDB(t *testing.T) *DB {
	t.Helper()

	// Use a unique temp file for each test
	f, err := os.CreateTemp("", "makhor_test_*.db")
	if err != nil {
		t.Fatalf("Failed to create temp file: %v", err)
	}
	path := f.Name()
	f.Close()

	// Clean up after test
	t.Cleanup(func() {
		os.Remove(path)
	})

	db, err := New(path)
	if err != nil {
		t.Fatalf("Failed to create database: %v", err)
	}

	t.Cleanup(func() {
		db.Close()
	})

	return db
}

// TestDatabaseCreation tests that a new database can be created with schema.
func TestDatabaseCreation(t *testing.T) {
	db := testDB(t)

	// Check that tables exist
	tables := []string{"users", "posts", "comments", "tags", "hats", "votes", "action_log"}
	for _, table := range tables {
		var name string
		err := db.QueryRow(
			"SELECT name FROM sqlite_master WHERE type='table' AND name=?",
			table,
		).Scan(&name)
		if err != nil {
			t.Errorf("Table %s not found: %v", table, err)
		}
	}
}

// TestDefaultTags tests that default tags are created.
func TestDefaultTags(t *testing.T) {
	db := testDB(t)

	tags, err := db.GetAllTags()
	if err != nil {
		t.Fatalf("Failed to get tags: %v", err)
	}

	if len(tags) == 0 {
		t.Error("Expected default tags to be created")
	}

	// Check for specific default tags (root categories)
	tagNames := make(map[string]bool)
	for _, tag := range tags {
		tagNames[tag.Name] = true
	}

	// These are the root category tags in db.go
	expectedTags := []string{"threat-intel", "research", "tools", "community", "learning"}
	for _, expected := range expectedTags {
		if !tagNames[expected] {
			t.Errorf("Expected default tag '%s' not found", expected)
		}
	}

	// Verify hierarchy - check some subtags exist
	expectedSubtags := []string{"threat-intel::malware", "threat-intel::malware::ransomware", "research::reversing", "tools::detection", "learning::tutorials"}
	for _, expected := range expectedSubtags {
		if !tagNames[expected] {
			t.Errorf("Expected subtag '%s' not found", expected)
		}
	}
}

// TestTagHierarchy tests hierarchical tag matching.
func TestTagHierarchy(t *testing.T) {
	db := testDB(t)

	user, _ := db.CreateUser("testuser", "test@example.com", nil)

	// Get threat-intel and threat-intel::malware tags
	parentTag, _ := db.GetTagByName("threat-intel")
	childTag, _ := db.GetTagByName("threat-intel::malware")

	if parentTag == nil || childTag == nil {
		t.Fatal("Could not find threat-intel tags")
	}

	// Create post with threat-intel::malware tag
	db.CreatePost(user.ID, "Malware Analysis", "", "Analysis of new ransomware", []int64{childTag.ID})

	// Query for "threat-intel" should also return posts tagged with "threat-intel::malware"
	posts, total, err := db.GetPosts(1, 10, "threat-intel", nil, true)
	if err != nil {
		t.Fatalf("Failed to get posts: %v", err)
	}

	if total != 1 {
		t.Errorf("Expected 1 post when querying parent tag 'threat-intel', got %d", total)
	}

	// Query for "threat-intel::malware" should return the same post
	posts, total, _ = db.GetPosts(1, 10, "threat-intel::malware", nil, true)
	if total != 1 {
		t.Errorf("Expected 1 post when querying exact tag 'threat-intel::malware', got %d", total)
	}

	// Query for different tag should return 0
	posts, total, _ = db.GetPosts(1, 10, "research", nil, true)
	if total != 0 {
		t.Errorf("Expected 0 posts for 'research' tag, got %d", total)
	}
	_ = posts
}

// TestUserCreation tests creating and retrieving users.
func TestUserCreation(t *testing.T) {
	db := testDB(t)

	// Create a user
	user, err := db.CreateUser("testuser", "test@example.com", nil)
	if err != nil {
		t.Fatalf("Failed to create user: %v", err)
	}

	if user.Username != "testuser" {
		t.Errorf("Expected username 'testuser', got '%s'", user.Username)
	}

	if user.Email != "test@example.com" {
		t.Errorf("Expected email 'test@example.com', got '%s'", user.Email)
	}

	// Retrieve by ID
	retrieved, err := db.GetUserByID(user.ID)
	if err != nil {
		t.Fatalf("Failed to get user by ID: %v", err)
	}
	if retrieved.Username != user.Username {
		t.Error("Retrieved user doesn't match")
	}

	// Retrieve by username
	retrieved, err = db.GetUserByUsername("testuser")
	if err != nil {
		t.Fatalf("Failed to get user by username: %v", err)
	}
	if retrieved.ID != user.ID {
		t.Error("Retrieved user doesn't match")
	}

	// Retrieve by email
	retrieved, err = db.GetUserByEmail("test@example.com")
	if err != nil {
		t.Fatalf("Failed to get user by email: %v", err)
	}
	if retrieved.ID != user.ID {
		t.Error("Retrieved user doesn't match")
	}
}

// TestDuplicateUser tests that duplicate users are rejected.
func TestDuplicateUser(t *testing.T) {
	db := testDB(t)

	_, err := db.CreateUser("testuser", "test@example.com", nil)
	if err != nil {
		t.Fatalf("Failed to create first user: %v", err)
	}

	// Try duplicate username
	_, err = db.CreateUser("testuser", "other@example.com", nil)
	if err != ErrUserExists {
		t.Errorf("Expected ErrUserExists, got %v", err)
	}

	// Try duplicate email
	_, err = db.CreateUser("otheruser", "test@example.com", nil)
	if err != ErrUserExists {
		t.Errorf("Expected ErrUserExists, got %v", err)
	}
}

// TestLoginTokens tests login token creation and validation.
func TestLoginTokens(t *testing.T) {
	db := testDB(t)

	email := "test@example.com"

	// Create token
	token, err := db.CreateLoginToken(email)
	if err != nil {
		t.Fatalf("Failed to create login token: %v", err)
	}

	if len(token) == 0 {
		t.Error("Token should not be empty")
	}

	// Validate token
	validatedEmail, err := db.ValidateLoginToken(token)
	if err != nil {
		t.Fatalf("Failed to validate token: %v", err)
	}

	if validatedEmail != email {
		t.Errorf("Expected email '%s', got '%s'", email, validatedEmail)
	}

	// Token should be marked as used
	_, err = db.ValidateLoginToken(token)
	if err != ErrInvalidToken {
		t.Errorf("Expected ErrInvalidToken for used token, got %v", err)
	}
}

// TestInvalidToken tests validation of invalid tokens.
func TestInvalidToken(t *testing.T) {
	db := testDB(t)

	_, err := db.ValidateLoginToken("nonexistent-token")
	if err != ErrInvalidToken {
		t.Errorf("Expected ErrInvalidToken, got %v", err)
	}
}

// TestSession tests session creation and validation.
func TestSession(t *testing.T) {
	db := testDB(t)

	// Create user
	user, err := db.CreateUser("testuser", "test@example.com", nil)
	if err != nil {
		t.Fatalf("Failed to create user: %v", err)
	}

	// Create session
	sessionToken, err := db.CreateSession(user.ID)
	if err != nil {
		t.Fatalf("Failed to create session: %v", err)
	}

	// Validate session
	validatedUser, err := db.ValidateSession(sessionToken)
	if err != nil {
		t.Fatalf("Failed to validate session: %v", err)
	}

	if validatedUser.ID != user.ID {
		t.Error("Validated user doesn't match")
	}

	// Delete session
	err = db.DeleteSession(sessionToken)
	if err != nil {
		t.Fatalf("Failed to delete session: %v", err)
	}

	// Should fail now
	_, err = db.ValidateSession(sessionToken)
	if err != ErrInvalidToken {
		t.Errorf("Expected ErrInvalidToken after deletion, got %v", err)
	}
}

// TestInvites tests invite creation and usage.
func TestInvites(t *testing.T) {
	db := testDB(t)

	// Create inviter
	inviter, err := db.CreateUser("inviter", "inviter@example.com", nil)
	if err != nil {
		t.Fatalf("Failed to create inviter: %v", err)
	}

	// Create invite
	invite, err := db.CreateInvite(inviter.ID, "For a friend")
	if err != nil {
		t.Fatalf("Failed to create invite: %v", err)
	}

	if invite.UsedBy != nil {
		t.Error("New invite should not be used")
	}

	// Get invite by code
	retrieved, err := db.GetInviteByCode(invite.Code)
	if err != nil {
		t.Fatalf("Failed to get invite by code: %v", err)
	}
	if retrieved.ID != invite.ID {
		t.Error("Retrieved invite doesn't match")
	}

	// Create invitee
	invitee, err := db.CreateUser("invitee", "invitee@example.com", &inviter.ID)
	if err != nil {
		t.Fatalf("Failed to create invitee: %v", err)
	}

	// Use invite
	err = db.UseInvite(invite.Code, invitee.ID)
	if err != nil {
		t.Fatalf("Failed to use invite: %v", err)
	}

	// Check invite is used
	retrieved, err = db.GetInviteByCode(invite.Code)
	if err != nil {
		t.Fatalf("Failed to get used invite: %v", err)
	}
	if retrieved.UsedBy == nil || *retrieved.UsedBy != invitee.ID {
		t.Error("Invite should be marked as used")
	}

	// Try to use again
	err = db.UseInvite(invite.Code, invitee.ID)
	if err != ErrInvalidInvite {
		t.Errorf("Expected ErrInvalidInvite for used invite, got %v", err)
	}
}

// TestInviteTree tests the invite tree functionality.
func TestInviteTree(t *testing.T) {
	db := testDB(t)

	// Create root user
	root, _ := db.CreateUser("root", "root@example.com", nil)

	// Create child users
	child1, _ := db.CreateUser("child1", "child1@example.com", &root.ID)
	child2, _ := db.CreateUser("child2", "child2@example.com", &root.ID)
	grandchild, _ := db.CreateUser("grandchild", "grandchild@example.com", &child1.ID)

	// Get tree
	tree, err := db.GetInviteTree(root.ID)
	if err != nil {
		t.Fatalf("Failed to get invite tree: %v", err)
	}

	if tree.User.ID != root.ID {
		t.Error("Tree root should be root user")
	}

	if len(tree.Children) != 2 {
		t.Errorf("Expected 2 children, got %d", len(tree.Children))
	}

	// Check grandchild
	found := false
	for _, child := range tree.Children {
		if child.User.ID == child1.ID {
			if len(child.Children) == 1 && child.Children[0].User.ID == grandchild.ID {
				found = true
			}
		}
	}
	if !found {
		t.Error("Grandchild not found in tree")
	}

	_ = child2 // Used in tree
}

// TestPostCreation tests creating posts with tags.
func TestPostCreation(t *testing.T) {
	db := testDB(t)

	// Create user
	user, _ := db.CreateUser("testuser", "test@example.com", nil)

	// Get a tag
	tags, _ := db.GetAllTags()
	tagID := tags[0].ID

	// Create post
	post, err := db.CreatePost(user.ID, "Test Post", "https://example.com", "Body text", []int64{tagID})
	if err != nil {
		t.Fatalf("Failed to create post: %v", err)
	}

	if post.Title != "Test Post" {
		t.Errorf("Expected title 'Test Post', got '%s'", post.Title)
	}

	if post.Score != 0 {
		t.Errorf("Expected initial score 0, got %d", post.Score)
	}

	// Retrieve post
	retrieved, err := db.GetPostByID(post.ID, nil)
	if err != nil {
		t.Fatalf("Failed to get post: %v", err)
	}

	if retrieved.Username != user.Username {
		t.Error("Post should have user's username")
	}

	if len(retrieved.Tags) == 0 {
		t.Error("Post should have tags")
	}
}

// TestPostVoting tests voting on posts.
func TestPostVoting(t *testing.T) {
	db := testDB(t)

	user, _ := db.CreateUser("testuser", "test@example.com", nil)
	voter, _ := db.CreateUser("voter", "voter@example.com", nil)

	tags, _ := db.GetAllTags()
	post, _ := db.CreatePost(user.ID, "Test Post", "", "Body", []int64{tags[0].ID})

	initialScore := post.Score

	// Vote
	newScore, voted, err := db.VotePost(voter.ID, post.ID)
	if err != nil {
		t.Fatalf("Failed to vote: %v", err)
	}

	if !voted {
		t.Error("Should indicate vote was added")
	}

	if newScore != initialScore+1 {
		t.Errorf("Expected score %d, got %d", initialScore+1, newScore)
	}

	// Vote again (should unvote)
	newScore, voted, err = db.VotePost(voter.ID, post.ID)
	if err != nil {
		t.Fatalf("Failed to unvote: %v", err)
	}

	if voted {
		t.Error("Should indicate vote was removed")
	}

	if newScore != initialScore {
		t.Errorf("Expected score %d after unvote, got %d", initialScore, newScore)
	}
}

// TestComments tests comment creation and threading.
func TestComments(t *testing.T) {
	db := testDB(t)

	user, _ := db.CreateUser("testuser", "test@example.com", nil)
	tags, _ := db.GetAllTags()
	post, _ := db.CreatePost(user.ID, "Test Post", "", "Body", []int64{tags[0].ID})

	// Create top-level comment
	comment1, err := db.CreateComment(post.ID, user.ID, nil, nil, "First comment")
	if err != nil {
		t.Fatalf("Failed to create comment: %v", err)
	}

	if comment1.Score != 0 {
		t.Error("Comment should have initial score of 0")
	}

	// Create reply
	comment2, err := db.CreateComment(post.ID, user.ID, &comment1.ID, nil, "Reply to first")
	if err != nil {
		t.Fatalf("Failed to create reply: %v", err)
	}

	if comment2.ParentID == nil || *comment2.ParentID != comment1.ID {
		t.Error("Reply should have parent ID set")
	}

	// Get threaded comments
	comments, err := db.GetPostComments(post.ID, nil)
	if err != nil {
		t.Fatalf("Failed to get comments: %v", err)
	}

	if len(comments) != 2 {
		t.Errorf("Expected 2 comments, got %d", len(comments))
	}

	// Check threading
	if comments[0].Depth != 0 {
		t.Error("First comment should have depth 0")
	}
	if comments[1].Depth != 1 {
		t.Error("Reply should have depth 1")
	}
}

// TestHats tests hat creation and retrieval.
func TestHats(t *testing.T) {
	db := testDB(t)

	user, _ := db.CreateUser("testuser", "test@example.com", nil)
	admin, _ := db.CreateUser("admin", "admin@example.com", nil)

	// Grant hat
	hat, err := db.CreateHat(user.ID, "Core Dev", "Golang", "https://golang.org/team", &admin.ID)
	if err != nil {
		t.Fatalf("Failed to create hat: %v", err)
	}

	if hat.Name != "Core Dev" {
		t.Errorf("Expected hat name 'Core Dev', got '%s'", hat.Name)
	}

	if !hat.IsActive {
		t.Error("New hat should be active")
	}

	// Get user hats
	hats, err := db.GetUserHats(user.ID)
	if err != nil {
		t.Fatalf("Failed to get user hats: %v", err)
	}

	if len(hats) != 1 {
		t.Errorf("Expected 1 hat, got %d", len(hats))
	}

	// Comment with hat
	tags, _ := db.GetAllTags()
	post, _ := db.CreatePost(user.ID, "Test", "", "Body", []int64{tags[0].ID})
	comment, err := db.CreateComment(post.ID, user.ID, nil, &hat.ID, "Official comment")
	if err != nil {
		t.Fatalf("Failed to create comment with hat: %v", err)
	}

	if comment.HatID == nil || *comment.HatID != hat.ID {
		t.Error("Comment should have hat ID set")
	}

	// Revoke hat
	err = db.RevokeHat(hat.ID)
	if err != nil {
		t.Fatalf("Failed to revoke hat: %v", err)
	}

	// Should not appear in active hats
	hats, _ = db.GetUserHats(user.ID)
	if len(hats) != 0 {
		t.Error("Revoked hat should not appear in active hats")
	}
}

// TestActionLog tests action logging.
func TestActionLog(t *testing.T) {
	db := testDB(t)

	user, _ := db.CreateUser("testuser", "test@example.com", nil)

	// Log action
	err := db.LogAction(&user.ID, "post_create", "post", 1, "Test post", "127.0.0.1")
	if err != nil {
		t.Fatalf("Failed to log action: %v", err)
	}

	// Get log
	logs, total, err := db.GetActionLog(1, 10)
	if err != nil {
		t.Fatalf("Failed to get action log: %v", err)
	}

	if total != 1 {
		t.Errorf("Expected 1 log entry, got %d", total)
	}

	if len(logs) != 1 {
		t.Errorf("Expected 1 log entry returned, got %d", len(logs))
	}

	if logs[0].Action != "post_create" {
		t.Errorf("Expected action 'post_create', got '%s'", logs[0].Action)
	}
}

// TestUserAvatar tests avatar storage.
func TestUserAvatar(t *testing.T) {
	db := testDB(t)

	user, _ := db.CreateUser("testuser", "test@example.com", nil)

	// Initially no avatar
	if user.Avatar != nil {
		t.Error("New user should not have avatar")
	}

	// Upload avatar
	avatarData := []byte{0x89, 0x50, 0x4E, 0x47} // PNG header
	err := db.UpdateUserAvatar(user.ID, avatarData, "image/png")
	if err != nil {
		t.Fatalf("Failed to update avatar: %v", err)
	}

	// Retrieve user
	user, _ = db.GetUserByID(user.ID)
	if user.Avatar == nil {
		t.Error("User should have avatar after upload")
	}
	if user.AvatarType != "image/png" {
		t.Errorf("Expected avatar type 'image/png', got '%s'", user.AvatarType)
	}
}

// TestCleanupExpired tests cleanup of expired sessions and tokens.
func TestCleanupExpired(t *testing.T) {
	db := testDB(t)

	// Create expired token manually
	expiredTime := time.Now().Add(-1 * time.Hour)
	_, err := db.Exec(
		"INSERT INTO login_tokens (email, token, expires_at) VALUES (?, ?, ?)",
		"test@example.com", "expired-token", expiredTime,
	)
	if err != nil {
		t.Fatalf("Failed to create expired token: %v", err)
	}

	// Verify it exists
	var count int
	db.QueryRow("SELECT COUNT(*) FROM login_tokens").Scan(&count)
	if count != 1 {
		t.Error("Expired token should exist before cleanup")
	}

	// Run cleanup
	err = db.CleanupExpired()
	if err != nil {
		t.Fatalf("Cleanup failed: %v", err)
	}

	// Verify it's gone
	db.QueryRow("SELECT COUNT(*) FROM login_tokens").Scan(&count)
	if count != 0 {
		t.Error("Expired token should be removed after cleanup")
	}
}

// TestSearchPosts tests post search functionality.
func TestSearchPosts(t *testing.T) {
	db := testDB(t)

	user, _ := db.CreateUser("testuser", "test@example.com", nil)
	tags, _ := db.GetAllTags()

	// Create posts
	db.CreatePost(user.ID, "Golang Tutorial", "", "Learn Go", []int64{tags[0].ID})
	db.CreatePost(user.ID, "Python Guide", "", "Learn Python", []int64{tags[0].ID})
	db.CreatePost(user.ID, "Go Patterns", "", "Design patterns in Go", []int64{tags[0].ID})

	// Search for Go
	posts, total, err := db.SearchPosts("Go", 1, 10, nil)
	if err != nil {
		t.Fatalf("Search failed: %v", err)
	}

	if total != 2 {
		t.Errorf("Expected 2 results for 'Go', got %d", total)
	}

	if len(posts) != 2 {
		t.Errorf("Expected 2 posts returned, got %d", len(posts))
	}
}

// TestPostsByTag tests filtering posts by tag.
func TestPostsByTag(t *testing.T) {
	db := testDB(t)

	user, _ := db.CreateUser("testuser", "test@example.com", nil)
	tags, _ := db.GetAllTags()

	// Find two tags from different hierarchies to avoid hierarchical matching
	var tag1, tag2 *int64
	var tag1Name, tag2Name string
	for _, tag := range tags {
		if tag.Name == "threat-intel" && tag1 == nil {
			id := tag.ID
			tag1 = &id
			tag1Name = tag.Name
		} else if tag.Name == "research" && tag2 == nil {
			id := tag.ID
			tag2 = &id
			tag2Name = tag.Name
		}
	}

	if tag1 == nil || tag2 == nil {
		t.Fatal("Could not find required test tags")
	}

	// Create posts with different root tags (no hierarchy overlap)
	db.CreatePost(user.ID, "Post 1", "", "Body", []int64{*tag1})
	db.CreatePost(user.ID, "Post 2", "", "Body", []int64{*tag2})
	db.CreatePost(user.ID, "Post 3", "", "Body", []int64{*tag1})

	// Get posts by first tag (isAdmin=true to see all posts)
	posts, total, err := db.GetPosts(1, 10, tag1Name, nil, true)
	if err != nil {
		t.Fatalf("Failed to get posts by tag: %v", err)
	}

	if total != 2 {
		t.Errorf("Expected 2 posts with tag '%s', got %d", tag1Name, total)
	}

	if len(posts) != 2 {
		t.Errorf("Expected 2 posts returned, got %d", len(posts))
	}

	// Verify filtering works - tag2 should only have 1 post
	posts2, total2, _ := db.GetPosts(1, 10, tag2Name, nil, true)
	if total2 != 1 {
		t.Errorf("Expected 1 post with tag '%s', got %d", tag2Name, total2)
	}
	if len(posts2) != 1 {
		t.Errorf("Expected 1 post returned for tag2, got %d", len(posts2))
	}
}

// TestBanUser tests user banning.
func TestBanUser(t *testing.T) {
	db := testDB(t)

	user, _ := db.CreateUser("testuser", "test@example.com", nil)

	if user.IsBanned {
		t.Error("New user should not be banned")
	}

	// Ban user
	err := db.SetUserBanned(user.ID, true)
	if err != nil {
		t.Fatalf("Failed to ban user: %v", err)
	}

	// Verify
	user, _ = db.GetUserByID(user.ID)
	if !user.IsBanned {
		t.Error("User should be banned")
	}

	// Unban
	err = db.SetUserBanned(user.ID, false)
	if err != nil {
		t.Fatalf("Failed to unban user: %v", err)
	}

	user, _ = db.GetUserByID(user.ID)
	if user.IsBanned {
		t.Error("User should be unbanned")
	}
}

// TestBanUserWithReason tests banning with reason and expiration.
func TestBanUserWithReason(t *testing.T) {
	db := testDB(t)

	user, _ := db.CreateUser("testuser", "test@example.com", nil)
	admin, _ := db.CreateUser("admin", "admin@example.com", nil)

	// Ban with reason and expiration
	expiresAt := time.Now().Add(24 * time.Hour)
	err := db.BanUser(user.ID, "Spamming", &expiresAt, admin.ID)
	if err != nil {
		t.Fatalf("Failed to ban user: %v", err)
	}

	user, _ = db.GetUserByID(user.ID)
	if !user.IsBanned {
		t.Error("User should be banned")
	}
	if user.BanReason != "Spamming" {
		t.Errorf("Expected ban reason 'Spamming', got '%s'", user.BanReason)
	}
	if user.BannedBy == nil || *user.BannedBy != admin.ID {
		t.Error("BannedBy should be set to admin ID")
	}
}

// TestCommentVoting tests voting on comments.
func TestCommentVoting(t *testing.T) {
	db := testDB(t)

	user, _ := db.CreateUser("testuser", "test@example.com", nil)
	voter, _ := db.CreateUser("voter", "voter@example.com", nil)
	tags, _ := db.GetAllTags()
	post, _ := db.CreatePost(user.ID, "Test Post", "", "Body", []int64{tags[0].ID})
	comment, _ := db.CreateComment(post.ID, user.ID, nil, nil, "Test comment")

	// Vote on comment
	newScore, voted, err := db.VoteComment(voter.ID, comment.ID)
	if err != nil {
		t.Fatalf("Failed to vote: %v", err)
	}
	if !voted {
		t.Error("Should indicate vote was added")
	}
	if newScore != 1 {
		t.Errorf("Expected score 1, got %d", newScore)
	}

	// Unvote
	newScore, voted, _ = db.VoteComment(voter.ID, comment.ID)
	if voted {
		t.Error("Should indicate vote was removed")
	}
	if newScore != 0 {
		t.Errorf("Expected score 0, got %d", newScore)
	}
}

// TestPostUpdate tests updating posts with revisions.
func TestPostUpdate(t *testing.T) {
	db := testDB(t)

	user, _ := db.CreateUser("testuser", "test@example.com", nil)
	tags, _ := db.GetAllTags()
	post, _ := db.CreatePost(user.ID, "Original Title", "", "Original Body", []int64{tags[0].ID})

	// Update post with revision
	err := db.UpdatePostWithRevision(post.ID, user.ID, "Original Title", "Original Body", "New Title", "New Body")
	if err != nil {
		t.Fatalf("Failed to update post: %v", err)
	}

	// Check updated post
	updated, _ := db.GetPostByID(post.ID, nil)
	if updated.Title != "New Title" {
		t.Errorf("Expected title 'New Title', got '%s'", updated.Title)
	}
	if updated.Body != "New Body" {
		t.Errorf("Expected body 'New Body', got '%s'", updated.Body)
	}

	// Check revision was created
	revisions, _ := db.GetPostRevisions(post.ID)
	if len(revisions) != 1 {
		t.Errorf("Expected 1 revision, got %d", len(revisions))
	}
	if revisions[0].Title != "Original Title" {
		t.Errorf("Revision should have original title")
	}
}

// TestCommentUpdate tests updating comments.
func TestCommentUpdate(t *testing.T) {
	db := testDB(t)

	user, _ := db.CreateUser("testuser", "test@example.com", nil)
	tags, _ := db.GetAllTags()
	post, _ := db.CreatePost(user.ID, "Test Post", "", "Body", []int64{tags[0].ID})
	comment, _ := db.CreateComment(post.ID, user.ID, nil, nil, "Original comment")

	err := db.UpdateComment(comment.ID, "Updated comment")
	if err != nil {
		t.Fatalf("Failed to update comment: %v", err)
	}

	updated, _ := db.GetCommentByID(comment.ID, nil)
	if updated.Body != "Updated comment" {
		t.Errorf("Expected body 'Updated comment', got '%s'", updated.Body)
	}
}

// TestCommentDeletion tests soft-deleting comments.
func TestCommentDeletion(t *testing.T) {
	db := testDB(t)

	user, _ := db.CreateUser("testuser", "test@example.com", nil)
	tags, _ := db.GetAllTags()
	post, _ := db.CreatePost(user.ID, "Test Post", "", "Body", []int64{tags[0].ID})
	comment, _ := db.CreateComment(post.ID, user.ID, nil, nil, "Test comment")

	err := db.DeleteComment(comment.ID)
	if err != nil {
		t.Fatalf("Failed to delete comment: %v", err)
	}

	deleted, _ := db.GetCommentByID(comment.ID, nil)
	if !deleted.IsDeleted {
		t.Error("Comment should be marked as deleted")
	}
}

// TestCommentTreeDeletion tests deleting comment trees.
func TestCommentTreeDeletion(t *testing.T) {
	db := testDB(t)

	user, _ := db.CreateUser("testuser", "test@example.com", nil)
	tags, _ := db.GetAllTags()
	post, _ := db.CreatePost(user.ID, "Test Post", "", "Body", []int64{tags[0].ID})

	// Create parent and child comments
	parent, _ := db.CreateComment(post.ID, user.ID, nil, nil, "Parent")
	child, _ := db.CreateComment(post.ID, user.ID, &parent.ID, nil, "Child")

	// Delete tree
	err := db.DeleteCommentTree(parent.ID)
	if err != nil {
		t.Fatalf("Failed to delete comment tree: %v", err)
	}

	// Both should be deleted
	deletedParent, _ := db.GetCommentByID(parent.ID, nil)
	deletedChild, _ := db.GetCommentByID(child.ID, nil)

	if !deletedParent.IsDeleted {
		t.Error("Parent should be deleted")
	}
	if !deletedChild.IsDeleted {
		t.Error("Child should be deleted")
	}
}

// TestBlurComment tests blurring comments.
func TestBlurComment(t *testing.T) {
	db := testDB(t)

	user, _ := db.CreateUser("testuser", "test@example.com", nil)
	tags, _ := db.GetAllTags()
	post, _ := db.CreatePost(user.ID, "Test Post", "", "Body", []int64{tags[0].ID})
	comment, _ := db.CreateComment(post.ID, user.ID, nil, nil, "Test comment")

	// Blur
	err := db.BlurComment(comment.ID)
	if err != nil {
		t.Fatalf("Failed to blur comment: %v", err)
	}

	blurred, _ := db.GetCommentByID(comment.ID, nil)
	if !blurred.IsBlurred {
		t.Error("Comment should be blurred")
	}

	// Unblur
	err = db.UnblurComment(comment.ID)
	if err != nil {
		t.Fatalf("Failed to unblur comment: %v", err)
	}

	unblurred, _ := db.GetCommentByID(comment.ID, nil)
	if unblurred.IsBlurred {
		t.Error("Comment should not be blurred")
	}
}

// TestRecentComments tests fetching recent comments.
func TestRecentComments(t *testing.T) {
	db := testDB(t)

	user, _ := db.CreateUser("testuser", "test@example.com", nil)
	tags, _ := db.GetAllTags()
	post, _ := db.CreatePost(user.ID, "Test Post", "", "Body", []int64{tags[0].ID})

	db.CreateComment(post.ID, user.ID, nil, nil, "Comment 1")
	db.CreateComment(post.ID, user.ID, nil, nil, "Comment 2")
	db.CreateComment(post.ID, user.ID, nil, nil, "Comment 3")

	comments, err := db.GetRecentComments(2)
	if err != nil {
		t.Fatalf("Failed to get recent comments: %v", err)
	}

	if len(comments) != 2 {
		t.Errorf("Expected 2 comments, got %d", len(comments))
	}
}

// TestBatchTagLoading tests batch loading of tags for posts.
func TestBatchTagLoading(t *testing.T) {
	db := testDB(t)

	user, _ := db.CreateUser("testuser", "test@example.com", nil)
	tags, _ := db.GetAllTags()

	// Create posts with different tags
	post1, _ := db.CreatePost(user.ID, "Post 1", "", "Body", []int64{tags[0].ID, tags[1].ID})
	post2, _ := db.CreatePost(user.ID, "Post 2", "", "Body", []int64{tags[2].ID})

	// Batch load
	tagsByPost, err := db.GetTagsForPosts([]int64{post1.ID, post2.ID})
	if err != nil {
		t.Fatalf("Failed to batch load tags: %v", err)
	}

	if len(tagsByPost[post1.ID]) != 2 {
		t.Errorf("Expected 2 tags for post1, got %d", len(tagsByPost[post1.ID]))
	}
	if len(tagsByPost[post2.ID]) != 1 {
		t.Errorf("Expected 1 tag for post2, got %d", len(tagsByPost[post2.ID]))
	}
}

// TestBatchVoteLoading tests batch loading of votes.
func TestBatchVoteLoading(t *testing.T) {
	db := testDB(t)

	user, _ := db.CreateUser("testuser", "test@example.com", nil)
	voter, _ := db.CreateUser("voter", "voter@example.com", nil)
	tags, _ := db.GetAllTags()

	post1, _ := db.CreatePost(user.ID, "Post 1", "", "Body", []int64{tags[0].ID})
	post2, _ := db.CreatePost(user.ID, "Post 2", "", "Body", []int64{tags[0].ID})
	post3, _ := db.CreatePost(user.ID, "Post 3", "", "Body", []int64{tags[0].ID})

	// Vote on post1 and post3
	db.VotePost(voter.ID, post1.ID)
	db.VotePost(voter.ID, post3.ID)

	// Batch check votes
	voted, err := db.GetUserVotedPosts(voter.ID, []int64{post1.ID, post2.ID, post3.ID})
	if err != nil {
		t.Fatalf("Failed to batch check votes: %v", err)
	}

	if !voted[post1.ID] {
		t.Error("Should have voted on post1")
	}
	if voted[post2.ID] {
		t.Error("Should not have voted on post2")
	}
	if !voted[post3.ID] {
		t.Error("Should have voted on post3")
	}
}

// TestUserComments tests getting user's comments.
func TestUserComments(t *testing.T) {
	db := testDB(t)

	user1, _ := db.CreateUser("user1", "user1@example.com", nil)
	user2, _ := db.CreateUser("user2", "user2@example.com", nil)
	tags, _ := db.GetAllTags()
	post, _ := db.CreatePost(user1.ID, "Test Post", "", "Body", []int64{tags[0].ID})

	db.CreateComment(post.ID, user1.ID, nil, nil, "User1 comment 1")
	db.CreateComment(post.ID, user1.ID, nil, nil, "User1 comment 2")
	db.CreateComment(post.ID, user2.ID, nil, nil, "User2 comment")

	comments, total, err := db.GetUserComments(user1.ID, 1, 10)
	if err != nil {
		t.Fatalf("Failed to get user comments: %v", err)
	}

	if total != 2 {
		t.Errorf("Expected 2 comments for user1, got %d", total)
	}
	if len(comments) != 2 {
		t.Errorf("Expected 2 comments returned, got %d", len(comments))
	}
}

// TestRootTags tests getting only root-level tags.
func TestRootTags(t *testing.T) {
	db := testDB(t)

	rootTags, err := db.GetRootTags()
	if err != nil {
		t.Fatalf("Failed to get root tags: %v", err)
	}

	// All root tags should NOT contain ::
	for _, tag := range rootTags {
		if strings.Contains(tag.Name, "::") {
			t.Errorf("Root tag '%s' should not contain '::'", tag.Name)
		}
	}

	// Should have at least the main categories
	if len(rootTags) < 5 {
		t.Errorf("Expected at least 5 root tags, got %d", len(rootTags))
	}
}

// TestChildTags tests getting child tags of a parent.
func TestChildTags(t *testing.T) {
	db := testDB(t)

	childTags, err := db.GetChildTags("threat-intel")
	if err != nil {
		t.Fatalf("Failed to get child tags: %v", err)
	}

	// Should have threat-intel subtags
	if len(childTags) == 0 {
		t.Error("Expected child tags for 'threat-intel'")
	}

	// All should start with "threat-intel::"
	for _, tag := range childTags {
		if !strings.HasPrefix(tag.Name, "threat-intel::") {
			t.Errorf("Child tag '%s' should start with 'threat-intel::'", tag.Name)
		}
	}
}

// TestEmailChangeToken tests email change token flow.
func TestEmailChangeToken(t *testing.T) {
	db := testDB(t)

	user, _ := db.CreateUser("testuser", "old@example.com", nil)

	// Create token
	token := "test-email-change-token-123"
	err := db.CreateEmailChangeToken(user.ID, "new@example.com", token)
	if err != nil {
		t.Fatalf("Failed to create email change token: %v", err)
	}

	// Validate token
	userID, newEmail, err := db.GetEmailChangeToken(token)
	if err != nil {
		t.Fatalf("Failed to get email change token: %v", err)
	}
	if userID != user.ID {
		t.Error("Token user ID mismatch")
	}
	if newEmail != "new@example.com" {
		t.Errorf("Expected new email 'new@example.com', got '%s'", newEmail)
	}

	// Use token
	err = db.UseEmailChangeToken(token)
	if err != nil {
		t.Fatalf("Failed to use email change token: %v", err)
	}

	// Verify email changed
	updatedUser, _ := db.GetUserByID(user.ID)
	if updatedUser.Email != "new@example.com" {
		t.Errorf("Email should be updated to 'new@example.com', got '%s'", updatedUser.Email)
	}

	// Token should not work again
	err = db.UseEmailChangeToken(token)
	if err == nil {
		t.Error("Token should not be reusable")
	}
}

// TestPostCreationWithCustomTimestamp tests that posts can be created with custom timestamps.
// This specifically tests the SQLite datetime storage fix where time.Time was incompatible.
func TestPostCreationWithCustomTimestamp(t *testing.T) {
	db := testDB(t)

	user, _ := db.CreateUser("testuser", "test@example.com", nil)
	tags, _ := db.GetAllTags()
	tagID := tags[0].ID

	// Create a specific time in the past
	customTime := time.Date(2020, 6, 15, 10, 30, 0, 0, time.UTC)

	// Create post with custom timestamp (simulating RSS import)
	post, err := db.CreatePostWithSourceAndTime(
		user.ID,
		"Old Post",
		"https://example.com/old",
		"Body from the past",
		[]int64{tagID},
		models.SourceRSS,
		nil,
		&customTime,
	)
	if err != nil {
		t.Fatalf("Failed to create post with custom timestamp: %v", err)
	}

	// Retrieve post and verify timestamp is preserved
	retrieved, err := db.GetPostByID(post.ID, nil)
	if err != nil {
		t.Fatalf("Failed to get post: %v", err)
	}

	// Verify the timestamp matches (within 1 second to account for formatting)
	timeDiff := retrieved.CreatedAt.Sub(customTime)
	if timeDiff < -time.Second || timeDiff > time.Second {
		t.Errorf("Expected created_at ~%v, got %v (diff: %v)", customTime, retrieved.CreatedAt, timeDiff)
	}
}

// TestPostCreationWithNilTimestamp tests that posts without custom timestamp use current time.
func TestPostCreationWithNilTimestamp(t *testing.T) {
	db := testDB(t)

	user, _ := db.CreateUser("testuser", "test@example.com", nil)
	tags, _ := db.GetAllTags()
	tagID := tags[0].ID

	beforeCreate := time.Now().Add(-time.Second)

	// Create post without custom timestamp
	post, err := db.CreatePostWithSourceAndTime(
		user.ID,
		"New Post",
		"https://example.com/new",
		"Body",
		[]int64{tagID},
		models.SourceUser,
		nil,
		nil, // nil timestamp should use current time
	)
	if err != nil {
		t.Fatalf("Failed to create post: %v", err)
	}

	afterCreate := time.Now().Add(time.Second)

	// Retrieve and verify timestamp is between before and after
	retrieved, err := db.GetPostByID(post.ID, nil)
	if err != nil {
		t.Fatalf("Failed to get post: %v", err)
	}

	if retrieved.CreatedAt.Before(beforeCreate) {
		t.Errorf("Post created_at %v should be after %v", retrieved.CreatedAt, beforeCreate)
	}
	if retrieved.CreatedAt.After(afterCreate) {
		t.Errorf("Post created_at %v should be before %v", retrieved.CreatedAt, afterCreate)
	}
}

// TestPostWithSourceColumns tests that source_type and source_id are properly stored and retrieved.
func TestPostWithSourceColumns(t *testing.T) {
	db := testDB(t)

	user, _ := db.CreateUser("testuser", "test@example.com", nil)
	tags, _ := db.GetAllTags()
	tagID := tags[0].ID

	// Create a user post
	userPost, err := db.CreatePost(user.ID, "User Post", "", "Body", []int64{tagID})
	if err != nil {
		t.Fatalf("Failed to create user post: %v", err)
	}

	// Create an RSS post with source
	feedID := int64(42)
	rssPost, err := db.CreatePostWithSourceAndTime(
		user.ID,
		"RSS Post",
		"https://example.com/rss",
		"RSS Body",
		[]int64{tagID},
		models.SourceRSS,
		&feedID,
		nil,
	)
	if err != nil {
		t.Fatalf("Failed to create RSS post: %v", err)
	}

	// Verify user post source
	retrievedUser, _ := db.GetPostByID(userPost.ID, nil)
	if retrievedUser.SourceType != models.SourceUser {
		t.Errorf("Expected SourceUser, got %v", retrievedUser.SourceType)
	}
	if retrievedUser.SourceID != nil {
		t.Errorf("Expected nil SourceID for user post, got %v", retrievedUser.SourceID)
	}

	// Verify RSS post source
	retrievedRSS, _ := db.GetPostByID(rssPost.ID, nil)
	if retrievedRSS.SourceType != models.SourceRSS {
		t.Errorf("Expected SourceRSS, got %v", retrievedRSS.SourceType)
	}
	if retrievedRSS.SourceID == nil || *retrievedRSS.SourceID != feedID {
		t.Errorf("Expected SourceID %d, got %v", feedID, retrievedRSS.SourceID)
	}
}

// TestGetTopPostsSourceColumns tests that GetTopPosts includes source information.
func TestGetTopPostsSourceColumns(t *testing.T) {
	db := testDB(t)

	user, _ := db.CreateUser("testuser", "test@example.com", nil)
	tags, _ := db.GetAllTags()
	tagID := tags[0].ID

	// Create RSS post
	feedID := int64(42)
	rssPost, _ := db.CreatePostWithSourceAndTime(
		user.ID,
		"RSS Post",
		"https://example.com/rss",
		"RSS Body",
		[]int64{tagID},
		models.SourceRSS,
		&feedID,
		nil,
	)

	// Vote to make it appear in top posts
	db.VotePost(user.ID, rssPost.ID)

	// Get top posts (page, perPage, hoursAgo, currentUserID, isAdmin)
	posts, _, err := db.GetTopPosts(1, 10, 24, nil, true)
	if err != nil {
		t.Fatalf("Failed to get top posts: %v", err)
	}

	if len(posts) == 0 {
		t.Fatal("Expected at least 1 post")
	}

	// Find our RSS post
	var found *models.Post
	for _, p := range posts {
		if p.ID == rssPost.ID {
			found = p
			break
		}
	}

	if found == nil {
		t.Fatal("RSS post not found in top posts")
	}

	if found.SourceType != models.SourceRSS {
		t.Errorf("Expected SourceRSS in top posts, got %v", found.SourceType)
	}
	if found.SourceID == nil || *found.SourceID != feedID {
		t.Errorf("Expected SourceID %d in top posts, got %v", feedID, found.SourceID)
	}
}

// TestDatetimeScanFromSQLite tests that datetime values stored in SQLite can be scanned properly.
// This is a regression test for the "unsupported Scan" error.
func TestDatetimeScanFromSQLite(t *testing.T) {
	db := testDB(t)

	// Insert a row with datetime directly via SQL to test scanning
	_, err := db.Exec(`
		INSERT INTO users (username, email, is_admin, is_banned, created_at)
		VALUES (?, ?, FALSE, FALSE, ?)
	`, "datetestuser", "datetime@test.com", "2023-05-15 14:30:45")
	if err != nil {
		t.Fatalf("Failed to insert user with datetime string: %v", err)
	}

	// Retrieve and verify we can scan the datetime
	user, err := db.GetUserByEmail("datetime@test.com")
	if err != nil {
		t.Fatalf("Failed to get user (datetime scan failed): %v", err)
	}

	expectedYear := 2023
	expectedMonth := time.May
	expectedDay := 15

	if user.CreatedAt.Year() != expectedYear ||
		user.CreatedAt.Month() != expectedMonth ||
		user.CreatedAt.Day() != expectedDay {
		t.Errorf("Expected date 2023-05-15, got %v", user.CreatedAt)
	}
}

// TestNullableColumnScanning tests that nullable columns are handled correctly.
func TestNullableColumnScanning(t *testing.T) {
	db := testDB(t)

	// Create user without inviter (nullable invited_by)
	user1, err := db.CreateUser("user1", "user1@test.com", nil)
	if err != nil {
		t.Fatalf("Failed to create user without inviter: %v", err)
	}

	// Create user with inviter
	user2, err := db.CreateUser("user2", "user2@test.com", &user1.ID)
	if err != nil {
		t.Fatalf("Failed to create user with inviter: %v", err)
	}

	// Verify nullable field handling
	retrieved1, _ := db.GetUserByID(user1.ID)
	if retrieved1.InvitedBy != nil {
		t.Error("user1 InvitedBy should be nil")
	}

	retrieved2, _ := db.GetUserByID(user2.ID)
	if retrieved2.InvitedBy == nil || *retrieved2.InvitedBy != user1.ID {
		t.Errorf("user2 InvitedBy should be %d, got %v", user1.ID, retrieved2.InvitedBy)
	}
}

// TestLongTextStorage tests that long text values are stored and retrieved correctly.
func TestLongTextStorage(t *testing.T) {
	db := testDB(t)

	user, _ := db.CreateUser("testuser", "test@example.com", nil)
	tags, _ := db.GetAllTags()
	tagID := tags[0].ID

	// Create a very long body (>5000 chars)
	longBody := strings.Repeat("This is a test. ", 500) // ~8000 chars

	post, err := db.CreatePost(user.ID, "Long Body Post", "", longBody, []int64{tagID})
	if err != nil {
		t.Fatalf("Failed to create post with long body: %v", err)
	}

	retrieved, _ := db.GetPostByID(post.ID, nil)
	if retrieved.Body != longBody {
		t.Errorf("Long body was truncated. Expected %d chars, got %d chars",
			len(longBody), len(retrieved.Body))
	}
}

// TestSpecialCharacterStorage tests that special characters are stored correctly.
func TestSpecialCharacterStorage(t *testing.T) {
	db := testDB(t)

	user, _ := db.CreateUser("testuser", "test@example.com", nil)
	tags, _ := db.GetAllTags()
	tagID := tags[0].ID

	// Test various special characters
	specialTitle := `Test "quotes" & <html> 'apostrophe' — em-dash • bullet`
	specialBody := "Line1\nLine2\n\nParagraph\tTab\rCarriage\u0000Null"

	post, err := db.CreatePost(user.ID, specialTitle, "", specialBody, []int64{tagID})
	if err != nil {
		t.Fatalf("Failed to create post with special chars: %v", err)
	}

	retrieved, _ := db.GetPostByID(post.ID, nil)
	if retrieved.Title != specialTitle {
		t.Errorf("Title not preserved: expected %q, got %q", specialTitle, retrieved.Title)
	}
	if retrieved.Body != specialBody {
		t.Errorf("Body not preserved: expected %q, got %q", specialBody, retrieved.Body)
	}
}

// TestUnicodeStorage tests that unicode characters are stored correctly.
func TestUnicodeStorage(t *testing.T) {
	db := testDB(t)

	user, _ := db.CreateUser("testuser", "test@example.com", nil)
	tags, _ := db.GetAllTags()
	tagID := tags[0].ID

	// Test unicode: emoji, CJK, Arabic, Hebrew, etc.
	unicodeTitle := "测试 テスト 테스트 🚀 مرحبا שלום"
	unicodeBody := "Emoji: 👍🎉🔥\nMath: ∑∏∫\nSymbols: ™©®"

	post, err := db.CreatePost(user.ID, unicodeTitle, "", unicodeBody, []int64{tagID})
	if err != nil {
		t.Fatalf("Failed to create post with unicode: %v", err)
	}

	retrieved, _ := db.GetPostByID(post.ID, nil)
	if retrieved.Title != unicodeTitle {
		t.Errorf("Unicode title not preserved: expected %q, got %q", unicodeTitle, retrieved.Title)
	}
	if retrieved.Body != unicodeBody {
		t.Errorf("Unicode body not preserved: expected %q, got %q", unicodeBody, retrieved.Body)
	}
}
