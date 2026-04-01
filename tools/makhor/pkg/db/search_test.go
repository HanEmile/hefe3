package db

import (
	"os"
	"testing"
)

func setupFTSTestDB(t *testing.T) (*DB, func()) {
	t.Helper()

	tmpFile, err := os.CreateTemp("", "makhor-fts-test-*.db")
	if err != nil {
		t.Fatalf("Failed to create temp db: %v", err)
	}
	tmpFile.Close()
	dbPath := tmpFile.Name()

	db, err := New(dbPath)
	if err != nil {
		os.Remove(dbPath)
		t.Fatalf("Failed to create database: %v", err)
	}

	cleanup := func() {
		db.Close()
		os.Remove(dbPath)
	}

	return db, cleanup
}

func TestFTSSetup(t *testing.T) {
	db, cleanup := setupFTSTestDB(t)
	defer cleanup()

	// FTS should be set up automatically
	err := db.FTSHealthCheck()
	if err != nil {
		t.Errorf("FTS health check failed: %v", err)
	}
}

func TestSearchPostsFTS(t *testing.T) {
	db, cleanup := setupFTSTestDB(t)
	defer cleanup()

	// Create test user and tag
	user, err := db.CreateUser("searcher", "search@example.com", nil)
	if err != nil {
		t.Fatalf("Failed to create user: %v", err)
	}

	tag, err := db.CreateTag("fts-test", "FTS test tag", user.ID)
	if err != nil {
		t.Fatalf("Failed to create tag: %v", err)
	}

	// Create test posts
	posts := []struct {
		title string
		body  string
	}{
		{"Introduction to Go Programming", "Learn the basics of Go programming language"},
		{"Advanced Go Patterns", "Design patterns and best practices in Go"},
		{"Python for Beginners", "Getting started with Python programming"},
		{"Rust Systems Programming", "Build fast systems with Rust"},
		{"Go vs Rust Comparison", "Comparing Go and Rust for backend development"},
	}

	for _, p := range posts {
		_, err := db.CreatePost(user.ID, p.title, "", p.body, []int64{tag.ID})
		if err != nil {
			t.Fatalf("Failed to create post: %v", err)
		}
	}

	// Test simple search
	t.Run("simple search", func(t *testing.T) {
		results, total, err := db.SearchPostsFTS("Go", 1, 10, nil)
		if err != nil {
			t.Fatalf("Search failed: %v", err)
		}
		if total != 3 {
			t.Errorf("Expected 3 results for 'Go', got %d", total)
		}
		if len(results) != 3 {
			t.Errorf("Expected 3 posts, got %d", len(results))
		}
	})

	// Test prefix search
	t.Run("prefix search", func(t *testing.T) {
		results, total, err := db.SearchPostsFTS("prog", 1, 10, nil)
		if err != nil {
			t.Fatalf("Search failed: %v", err)
		}
		// Should match "Programming" in multiple posts
		if total < 3 {
			t.Errorf("Expected at least 3 results for 'prog*', got %d", total)
		}
		_ = results
	})

	// Test phrase search
	t.Run("phrase search", func(t *testing.T) {
		results, total, err := db.SearchPostsFTS("\"Go Programming\"", 1, 10, nil)
		if err != nil {
			t.Fatalf("Search failed: %v", err)
		}
		if total != 1 {
			t.Errorf("Expected 1 result for '\"Go Programming\"', got %d", total)
		}
		_ = results
	})

	// Test no results
	t.Run("no results", func(t *testing.T) {
		results, total, err := db.SearchPostsFTS("nonexistent", 1, 10, nil)
		if err != nil {
			t.Fatalf("Search failed: %v", err)
		}
		if total != 0 {
			t.Errorf("Expected 0 results, got %d", total)
		}
		if len(results) != 0 {
			t.Errorf("Expected empty results, got %d posts", len(results))
		}
	})

	// Test empty query
	t.Run("empty query", func(t *testing.T) {
		results, total, err := db.SearchPostsFTS("", 1, 10, nil)
		if err != nil {
			t.Fatalf("Search failed: %v", err)
		}
		if total != 0 {
			t.Errorf("Expected 0 results for empty query, got %d", total)
		}
		if results != nil && len(results) != 0 {
			t.Errorf("Expected nil or empty results, got %d posts", len(results))
		}
	})

	// Test pagination
	t.Run("pagination", func(t *testing.T) {
		results, total, err := db.SearchPostsFTS("programming", 1, 2, nil)
		if err != nil {
			t.Fatalf("Search failed: %v", err)
		}
		if len(results) > 2 {
			t.Errorf("Expected at most 2 results per page, got %d", len(results))
		}
		_ = total
	})
}

func TestSearchPostsFTSRanking(t *testing.T) {
	db, cleanup := setupFTSTestDB(t)
	defer cleanup()

	user, _ := db.CreateUser("ranker", "rank@example.com", nil)
	tag, _ := db.CreateTag("rank-test", "Ranking test", user.ID)

	// Create posts with different relevance
	// Post with "golang" in title should rank higher than in body
	db.CreatePost(user.ID, "Golang Tutorial", "", "Learn programming", []int64{tag.ID})
	db.CreatePost(user.ID, "Programming Basics", "", "Introduction to golang", []int64{tag.ID})

	results, _, err := db.SearchPostsFTS("golang", 1, 10, nil)
	if err != nil {
		t.Fatalf("Search failed: %v", err)
	}

	if len(results) < 2 {
		t.Fatalf("Expected at least 2 results, got %d", len(results))
	}

	// First result should have "golang" in title (higher relevance)
	if results[0].Title != "Golang Tutorial" {
		t.Errorf("Expected 'Golang Tutorial' to rank first, got '%s'", results[0].Title)
	}
}

func TestSanitizeFTSQuery(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{"simple word", "hello", "hello*"},
		{"multiple words", "hello world", "hello* world*"},
		{"trim whitespace", "  hello  ", "hello*"},
		{"empty string", "", ""},
		{"only spaces", "   ", ""},
		{"preserve phrase", "\"hello world\"", "\"hello world\""},
		{"preserve AND", "hello AND world", "hello AND world"},
		{"preserve OR", "hello OR world", "hello OR world"},
		{"preserve NOT", "hello NOT world", "hello NOT world"},
		{"preserve prefix", "hello*", "hello*"},
		{"preserve column search", "title:hello", "title:hello"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := sanitizeFTSQuery(tt.input)
			if got != tt.expected {
				t.Errorf("sanitizeFTSQuery(%q) = %q, want %q", tt.input, got, tt.expected)
			}
		})
	}
}

func TestFTSTriggersOnInsert(t *testing.T) {
	db, cleanup := setupFTSTestDB(t)
	defer cleanup()

	user, _ := db.CreateUser("trigger", "trigger@example.com", nil)
	tag, _ := db.CreateTag("trigger-test", "Trigger test", user.ID)

	// Create a post
	post, err := db.CreatePost(user.ID, "FTS Trigger Test", "", "Testing FTS triggers", []int64{tag.ID})
	if err != nil {
		t.Fatalf("Failed to create post: %v", err)
	}

	// Search should find it immediately
	results, total, err := db.SearchPostsFTS("FTS Trigger", 1, 10, nil)
	if err != nil {
		t.Fatalf("Search failed: %v", err)
	}

	if total != 1 {
		t.Errorf("Expected 1 result after insert, got %d", total)
	}
	if len(results) != 1 || results[0].ID != post.ID {
		t.Error("Search should return the newly inserted post")
	}
}

func TestFTSTriggersOnDelete(t *testing.T) {
	db, cleanup := setupFTSTestDB(t)
	defer cleanup()

	user, _ := db.CreateUser("deleter", "delete@example.com", nil)
	tag, _ := db.CreateTag("delete-test", "Delete test", user.ID)

	// Create and then delete a post
	post, _ := db.CreatePost(user.ID, "Delete Me", "", "This will be deleted", []int64{tag.ID})

	// Mark as deleted
	_, err := db.Exec("UPDATE posts SET is_deleted = TRUE WHERE id = ?", post.ID)
	if err != nil {
		t.Fatalf("Failed to delete post: %v", err)
	}

	// Search should not find it
	_, total, err := db.SearchPostsFTS("Delete Me", 1, 10, nil)
	if err != nil {
		t.Fatalf("Search failed: %v", err)
	}

	if total != 0 {
		t.Errorf("Expected 0 results after delete, got %d", total)
	}
}

func TestFTSTriggersOnUpdate(t *testing.T) {
	db, cleanup := setupFTSTestDB(t)
	defer cleanup()

	user, _ := db.CreateUser("updater", "update@example.com", nil)
	tag, _ := db.CreateTag("update-test", "Update test", user.ID)

	// Create a post
	post, _ := db.CreatePost(user.ID, "Original Title", "", "Original body", []int64{tag.ID})

	// Update the post
	_, err := db.Exec("UPDATE posts SET title = ?, body = ? WHERE id = ?",
		"Updated Title", "Updated body content", post.ID)
	if err != nil {
		t.Fatalf("Failed to update post: %v", err)
	}

	// Search for old title should not find it
	_, total, _ := db.SearchPostsFTS("Original Title", 1, 10, nil)
	if total != 0 {
		t.Errorf("Expected 0 results for old title, got %d", total)
	}

	// Search for new title should find it
	results, total, _ := db.SearchPostsFTS("Updated Title", 1, 10, nil)
	if total != 1 {
		t.Errorf("Expected 1 result for new title, got %d", total)
	}
	if len(results) == 1 && results[0].ID != post.ID {
		t.Error("Search should return the updated post")
	}
}

func TestSearchSuggestions(t *testing.T) {
	db, cleanup := setupFTSTestDB(t)
	defer cleanup()

	user, _ := db.CreateUser("suggester", "suggest@example.com", nil)
	tag, _ := db.CreateTag("suggest-test", "Suggestion test", user.ID)

	// Create posts with similar titles
	db.CreatePost(user.ID, "Go Programming Guide", "", "", []int64{tag.ID})
	db.CreatePost(user.ID, "Go Best Practices", "", "", []int64{tag.ID})
	db.CreatePost(user.ID, "Go Concurrency Patterns", "", "", []int64{tag.ID})

	suggestions, err := db.SearchSuggestions("Go", 5)
	if err != nil {
		t.Fatalf("Failed to get suggestions: %v", err)
	}

	if len(suggestions) != 3 {
		t.Errorf("Expected 3 suggestions, got %d", len(suggestions))
	}
}

func TestRebuildFTSIndex(t *testing.T) {
	db, cleanup := setupFTSTestDB(t)
	defer cleanup()

	user, _ := db.CreateUser("rebuilder", "rebuild@example.com", nil)
	tag, _ := db.CreateTag("rebuild-test", "Rebuild test", user.ID)

	// Create posts
	db.CreatePost(user.ID, "Test Post One", "", "Body one", []int64{tag.ID})
	db.CreatePost(user.ID, "Test Post Two", "", "Body two", []int64{tag.ID})

	// Rebuild index
	err := db.RebuildFTSIndex()
	if err != nil {
		t.Fatalf("Failed to rebuild FTS index: %v", err)
	}

	// Health check should pass
	err = db.FTSHealthCheck()
	if err != nil {
		t.Errorf("FTS health check failed after rebuild: %v", err)
	}

	// Search should still work
	results, total, err := db.SearchPostsFTS("Test Post", 1, 10, nil)
	if err != nil {
		t.Fatalf("Search failed after rebuild: %v", err)
	}
	if total != 2 {
		t.Errorf("Expected 2 results after rebuild, got %d", total)
	}
	_ = results
}

func TestFTSHealthCheck(t *testing.T) {
	db, cleanup := setupFTSTestDB(t)
	defer cleanup()

	err := db.FTSHealthCheck()
	if err != nil {
		t.Errorf("FTS health check should pass on fresh db: %v", err)
	}
}

func TestFTSWithSpecialCharacters(t *testing.T) {
	db, cleanup := setupFTSTestDB(t)
	defer cleanup()

	user, _ := db.CreateUser("special", "special@example.com", nil)
	tag, _ := db.CreateTag("special-test", "Special char test", user.ID)

	// Create post with special characters
	db.CreatePost(user.ID, "C++ Programming Guide", "", "Learn C++ basics", []int64{tag.ID})
	db.CreatePost(user.ID, "C# for Beginners", "", "Getting started with C#", []int64{tag.ID})

	// Search should handle special characters
	results, total, err := db.SearchPostsFTS("C++", 1, 10, nil)
	if err != nil {
		t.Fatalf("Search failed: %v", err)
	}
	// FTS may or may not match C++ depending on tokenization
	// The important thing is it doesn't error
	_ = results
	_ = total
}

func TestFTSWithUnicode(t *testing.T) {
	db, cleanup := setupFTSTestDB(t)
	defer cleanup()

	user, _ := db.CreateUser("unicode", "unicode@example.com", nil)
	tag, _ := db.CreateTag("unicode-test", "Unicode test", user.ID)

	// Create posts with unicode
	db.CreatePost(user.ID, "日本語プログラミング", "", "Japanese programming", []int64{tag.ID})
	db.CreatePost(user.ID, "Café Development", "", "Programming at cafés", []int64{tag.ID})

	// Search unicode text
	results, _, err := db.SearchPostsFTS("プログラミング", 1, 10, nil)
	if err != nil {
		t.Fatalf("Search failed: %v", err)
	}
	// Unicode search support depends on tokenizer
	_ = results

	// Latin with diacritics
	results, total, _ := db.SearchPostsFTS("Café", 1, 10, nil)
	if total < 1 {
		t.Logf("Note: Café search returned %d results (diacritic handling varies)", total)
	}
	_ = results
}

func TestFTSBooleanOperators(t *testing.T) {
	db, cleanup := setupFTSTestDB(t)
	defer cleanup()

	user, _ := db.CreateUser("boolean", "bool@example.com", nil)
	tag, _ := db.CreateTag("bool-test", "Boolean test", user.ID)

	db.CreatePost(user.ID, "Go and Rust", "", "Comparing Go and Rust", []int64{tag.ID})
	db.CreatePost(user.ID, "Only Go", "", "Just Go content", []int64{tag.ID})
	db.CreatePost(user.ID, "Only Rust", "", "Just Rust content", []int64{tag.ID})

	// Test AND
	results, total, err := db.SearchPostsFTS("Go AND Rust", 1, 10, nil)
	if err != nil {
		t.Fatalf("AND search failed: %v", err)
	}
	if total != 1 {
		t.Errorf("Expected 1 result for 'Go AND Rust', got %d", total)
	}
	_ = results

	// Test NOT
	results, total, err = db.SearchPostsFTS("Go NOT Rust", 1, 10, nil)
	if err != nil {
		t.Fatalf("NOT search failed: %v", err)
	}
	if total != 1 {
		t.Errorf("Expected 1 result for 'Go NOT Rust', got %d", total)
	}
	_ = results
}
