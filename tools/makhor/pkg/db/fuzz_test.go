package db

import (
	"os"
	"testing"
	"unicode/utf8"
)

// fuzzDB creates a temporary database for fuzzing.
func fuzzDB(f *testing.F) *DB {
	f.Helper()

	file, err := os.CreateTemp("", "makhor_fuzz_*.db")
	if err != nil {
		f.Fatalf("Failed to create temp file: %v", err)
	}
	path := file.Name()
	file.Close()

	f.Cleanup(func() {
		os.Remove(path)
	})

	db, err := New(path)
	if err != nil {
		f.Fatalf("Failed to create database: %v", err)
	}

	f.Cleanup(func() {
		db.Close()
	})

	return db
}

// FuzzCreateUser tests user creation with fuzzing.
func FuzzCreateUser(f *testing.F) {
	// Add seed corpus
	f.Add("validuser", "valid@example.com")
	f.Add("a", "a@b.c")
	f.Add("user_with_underscore", "user@domain.org")
	f.Add("", "")
	f.Add("user123", "user123@test.com")
	f.Add("UPPERCASE", "UPPER@CASE.COM")
	f.Add("user with spaces", "invalid email")
	f.Add("'--sql-injection", "sql@inject.com")
	f.Add("<script>xss</script>", "xss@test.com")

	db := fuzzDB(f)
	counter := 0

	f.Fuzz(func(t *testing.T, username, email string) {
		// Skip empty inputs that would definitely fail
		if len(username) == 0 || len(email) == 0 {
			return
		}

		// Skip invalid UTF-8
		if !utf8.ValidString(username) || !utf8.ValidString(email) {
			return
		}

		// CreateUser should not panic
		counter++
		uniqueUsername := username + "_" + string(rune('a'+counter%26))
		uniqueEmail := string(rune('a'+counter%26)) + "_" + email

		user, err := db.CreateUser(uniqueUsername, uniqueEmail, nil)
		if err != nil {
			// Errors are expected for invalid input
			return
		}

		// If successful, verify the user was created correctly
		if user.Username != uniqueUsername {
			t.Errorf("Username mismatch: expected %q, got %q", uniqueUsername, user.Username)
		}
		if user.Email != uniqueEmail {
			t.Errorf("Email mismatch: expected %q, got %q", uniqueEmail, user.Email)
		}
	})
}

// FuzzCreatePost tests post creation with fuzzing.
func FuzzCreatePost(f *testing.F) {
	// Add seed corpus
	f.Add("Valid Title", "https://example.com", "Body content")
	f.Add("", "", "")
	f.Add("Title with 'quotes'", "http://test.com/path?q=1", "Body with\nnewlines")
	f.Add("<script>XSS</script>", "javascript:alert(1)", "SQL' OR '1'='1")
	f.Add("Very long title with lots of text here for testing purposes", "", "")
	f.Add("Unicode: K� =�", "https://�H.jp/ƹ�", "Emoji =M and CJK -�")

	db := fuzzDB(f)
	user, _ := db.CreateUser("fuzzuser", "fuzz@example.com", nil)
	tags, _ := db.GetAllTags()
	tagID := tags[0].ID

	f.Fuzz(func(t *testing.T, title, url, body string) {
		// Skip invalid UTF-8
		if !utf8.ValidString(title) || !utf8.ValidString(url) || !utf8.ValidString(body) {
			return
		}

		// CreatePost should not panic
		post, err := db.CreatePost(user.ID, title, url, body, []int64{tagID})
		if err != nil {
			// Errors may be expected for certain inputs
			return
		}

		// If successful, verify post was created
		if post == nil {
			t.Error("Post should not be nil on success")
			return
		}

		// Retrieve and verify
		retrieved, err := db.GetPostByID(post.ID, nil)
		if err != nil {
			t.Errorf("Failed to retrieve created post: %v", err)
			return
		}
		if retrieved.Title != title {
			t.Errorf("Title mismatch: expected %q, got %q", title, retrieved.Title)
		}
		if retrieved.Body != body {
			t.Errorf("Body mismatch: expected %q, got %q", body, retrieved.Body)
		}
	})
}

// FuzzCreateComment tests comment creation with fuzzing.
func FuzzCreateComment(f *testing.F) {
	// Add seed corpus
	f.Add("Valid comment body")
	f.Add("")
	f.Add("Comment with 'quotes' and \"double quotes\"")
	f.Add("<script>XSS</script>")
	f.Add("SQL' OR '1'='1")
	f.Add("Unicode: K� =� emoji")
	f.Add("Very long comment body with lots of text to test longer inputs and edge cases")

	db := fuzzDB(f)
	user, _ := db.CreateUser("fuzzuser", "fuzz@example.com", nil)
	tags, _ := db.GetAllTags()
	post, _ := db.CreatePost(user.ID, "Test Post", "", "Body", []int64{tags[0].ID})

	f.Fuzz(func(t *testing.T, body string) {
		// Skip invalid UTF-8
		if !utf8.ValidString(body) {
			return
		}

		// CreateComment should not panic
		comment, err := db.CreateComment(post.ID, user.ID, nil, nil, body)
		if err != nil {
			// Errors may be expected for certain inputs
			return
		}

		// If successful, verify comment was created
		if comment == nil {
			t.Error("Comment should not be nil on success")
			return
		}

		// Retrieve and verify
		retrieved, err := db.GetCommentByID(comment.ID, nil)
		if err != nil {
			t.Errorf("Failed to retrieve created comment: %v", err)
			return
		}
		if retrieved.Body != body {
			t.Errorf("Body mismatch: expected %q, got %q", body, retrieved.Body)
		}
	})
}

// FuzzSearchPosts tests search with fuzzing.
func FuzzSearchPosts(f *testing.F) {
	// Add seed corpus
	f.Add("valid search")
	f.Add("")
	f.Add("'--sql")
	f.Add("test OR 1=1")
	f.Add("unicode: -�")
	f.Add("*")
	f.Add("?")
	f.Add("test%")
	f.Add("a AND b OR c NOT d")

	db := fuzzDB(f)
	user, _ := db.CreateUser("fuzzuser", "fuzz@example.com", nil)
	tags, _ := db.GetAllTags()

	// Create some searchable posts
	for i := 0; i < 10; i++ {
		db.CreatePost(user.ID, "Searchable Post", "", "Content to search", []int64{tags[0].ID})
	}

	f.Fuzz(func(t *testing.T, query string) {
		// Skip invalid UTF-8
		if !utf8.ValidString(query) {
			return
		}

		// SearchPosts should not panic
		posts, total, err := db.SearchPosts(query, 1, 30, nil)
		if err != nil {
			// Some queries may fail (e.g., FTS syntax errors)
			return
		}

		// Basic validation
		if total < 0 {
			t.Errorf("Total should not be negative: %d", total)
		}
		if len(posts) > 30 {
			t.Errorf("Should not return more than perPage posts: %d", len(posts))
		}
	})
}

// FuzzCreateTag tests tag creation with fuzzing.
func FuzzCreateTag(f *testing.F) {
	// Add seed corpus
	f.Add("validtag", "Description")
	f.Add("parent::child", "Child tag")
	f.Add("", "")
	f.Add("tag-with-dash", "Description")
	f.Add("tag_underscore", "Description")
	f.Add("TAG", "UPPERCASE")
	f.Add("unicode-~", "Unicode tag")

	db := fuzzDB(f)
	user, _ := db.CreateUser("fuzzuser", "fuzz@example.com", nil)
	counter := 0

	f.Fuzz(func(t *testing.T, name, description string) {
		// Skip invalid UTF-8
		if !utf8.ValidString(name) || !utf8.ValidString(description) {
			return
		}

		// Skip empty names
		if len(name) == 0 {
			return
		}

		// CreateTag should not panic
		counter++
		uniqueName := name + "_" + string(rune('a'+counter%26)) + string(rune('0'+counter%10))

		tag, err := db.CreateTag(uniqueName, description, user.ID)
		if err != nil {
			// Errors are expected for invalid input
			return
		}

		// If successful, verify the tag was created
		if tag.Name != uniqueName {
			t.Errorf("Name mismatch: expected %q, got %q", uniqueName, tag.Name)
		}
		if tag.Description != description {
			t.Errorf("Description mismatch: expected %q, got %q", description, tag.Description)
		}
	})
}
