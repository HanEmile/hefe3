package db

import (
	"os"
	"testing"
)

// benchmarkDB creates a temporary database for benchmarking.
func benchmarkDB(b *testing.B) *DB {
	b.Helper()

	f, err := os.CreateTemp("", "makhor_bench_*.db")
	if err != nil {
		b.Fatalf("Failed to create temp file: %v", err)
	}
	path := f.Name()
	f.Close()

	b.Cleanup(func() {
		os.Remove(path)
	})

	db, err := New(path)
	if err != nil {
		b.Fatalf("Failed to create database: %v", err)
	}

	b.Cleanup(func() {
		db.Close()
	})

	return db
}

// BenchmarkCreateUser benchmarks user creation.
func BenchmarkCreateUser(b *testing.B) {
	db := benchmarkDB(b)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		username := "user" + string(rune('a'+i%26)) + string(rune('0'+i%10))
		email := username + "@example.com"
		_, err := db.CreateUser(username, email, nil)
		if err != nil && err != ErrUserExists {
			b.Fatalf("Failed to create user: %v", err)
		}
	}
}

// BenchmarkCreatePost benchmarks post creation.
func BenchmarkCreatePost(b *testing.B) {
	db := benchmarkDB(b)
	user, _ := db.CreateUser("benchuser", "bench@example.com", nil)
	tags, _ := db.GetAllTags()
	tagID := tags[0].ID

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, err := db.CreatePost(user.ID, "Benchmark Post", "https://example.com", "Body", []int64{tagID})
		if err != nil {
			b.Fatalf("Failed to create post: %v", err)
		}
	}
}

// BenchmarkGetPosts benchmarks post retrieval.
func BenchmarkGetPosts(b *testing.B) {
	db := benchmarkDB(b)
	user, _ := db.CreateUser("benchuser", "bench@example.com", nil)
	tags, _ := db.GetAllTags()
	tagID := tags[0].ID

	// Create some posts
	for i := 0; i < 100; i++ {
		db.CreatePost(user.ID, "Benchmark Post", "https://example.com", "Body", []int64{tagID})
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _, err := db.GetPosts(1, 30, "", nil, true)
		if err != nil {
			b.Fatalf("Failed to get posts: %v", err)
		}
	}
}

// BenchmarkGetPostsWithTag benchmarks post retrieval with tag filter.
func BenchmarkGetPostsWithTag(b *testing.B) {
	db := benchmarkDB(b)
	user, _ := db.CreateUser("benchuser", "bench@example.com", nil)
	tags, _ := db.GetAllTags()
	tagID := tags[0].ID
	tagName := tags[0].Name

	// Create some posts
	for i := 0; i < 100; i++ {
		db.CreatePost(user.ID, "Benchmark Post", "https://example.com", "Body", []int64{tagID})
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _, err := db.GetPosts(1, 30, tagName, nil, true)
		if err != nil {
			b.Fatalf("Failed to get posts: %v", err)
		}
	}
}

// BenchmarkSearchPosts benchmarks post search.
func BenchmarkSearchPosts(b *testing.B) {
	db := benchmarkDB(b)
	user, _ := db.CreateUser("benchuser", "bench@example.com", nil)
	tags, _ := db.GetAllTags()
	tagID := tags[0].ID

	// Create posts with varied content
	titles := []string{"Go Programming", "Rust Tutorial", "Python Basics", "JavaScript Guide"}
	for i := 0; i < 100; i++ {
		db.CreatePost(user.ID, titles[i%len(titles)], "https://example.com", "Body content here", []int64{tagID})
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _, err := db.SearchPosts("programming", 1, 30, nil)
		if err != nil {
			b.Fatalf("Search failed: %v", err)
		}
	}
}

// BenchmarkCreateComment benchmarks comment creation.
func BenchmarkCreateComment(b *testing.B) {
	db := benchmarkDB(b)
	user, _ := db.CreateUser("benchuser", "bench@example.com", nil)
	tags, _ := db.GetAllTags()
	post, _ := db.CreatePost(user.ID, "Test Post", "", "Body", []int64{tags[0].ID})

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, err := db.CreateComment(post.ID, user.ID, nil, nil, "Test comment body")
		if err != nil {
			b.Fatalf("Failed to create comment: %v", err)
		}
	}
}

// BenchmarkGetPostComments benchmarks comment retrieval.
func BenchmarkGetPostComments(b *testing.B) {
	db := benchmarkDB(b)
	user, _ := db.CreateUser("benchuser", "bench@example.com", nil)
	tags, _ := db.GetAllTags()
	post, _ := db.CreatePost(user.ID, "Test Post", "", "Body", []int64{tags[0].ID})

	// Create comments with threading
	var parentID *int64
	for i := 0; i < 50; i++ {
		comment, _ := db.CreateComment(post.ID, user.ID, parentID, nil, "Comment body")
		if i%5 == 0 {
			parentID = &comment.ID
		}
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, err := db.GetPostComments(post.ID, nil)
		if err != nil {
			b.Fatalf("Failed to get comments: %v", err)
		}
	}
}

// BenchmarkVotePost benchmarks post voting.
func BenchmarkVotePost(b *testing.B) {
	db := benchmarkDB(b)
	user, _ := db.CreateUser("benchuser", "bench@example.com", nil)
	tags, _ := db.GetAllTags()
	post, _ := db.CreatePost(user.ID, "Test Post", "", "Body", []int64{tags[0].ID})

	// Create voters
	voters := make([]int64, 100)
	for i := 0; i < 100; i++ {
		u, _ := db.CreateUser("voter"+string(rune('a'+i%26))+string(rune('0'+i/26)), "voter"+string(rune('a'+i%26))+string(rune('0'+i/26))+"@example.com", nil)
		voters[i] = u.ID
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _, err := db.VotePost(voters[i%100], post.ID)
		if err != nil {
			b.Fatalf("Failed to vote: %v", err)
		}
	}
}

// BenchmarkGetHotPosts benchmarks hot posts retrieval with scoring.
func BenchmarkGetHotPosts(b *testing.B) {
	db := benchmarkDB(b)
	user, _ := db.CreateUser("benchuser", "bench@example.com", nil)
	tags, _ := db.GetAllTags()
	tagID := tags[0].ID

	// Create posts with votes
	for i := 0; i < 100; i++ {
		post, _ := db.CreatePost(user.ID, "Hot Post", "https://example.com", "Body", []int64{tagID})
		for j := 0; j < i%10; j++ {
			voter, _ := db.CreateUser("hotvoter"+string(rune('a'+j))+string(rune('0'+i)), "hotvoter"+string(rune('a'+j))+string(rune('0'+i))+"@example.com", nil)
			db.VotePost(voter.ID, post.ID)
		}
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _, err := db.GetHotPosts(1, 30, nil, true)
		if err != nil {
			b.Fatalf("Failed to get hot posts: %v", err)
		}
	}
}

// BenchmarkBatchTagLoading benchmarks batch tag loading.
func BenchmarkBatchTagLoading(b *testing.B) {
	db := benchmarkDB(b)
	user, _ := db.CreateUser("benchuser", "bench@example.com", nil)
	tags, _ := db.GetAllTags()

	// Create posts with multiple tags
	postIDs := make([]int64, 50)
	for i := 0; i < 50; i++ {
		tagIDs := []int64{tags[i%len(tags)].ID}
		if i+1 < len(tags) {
			tagIDs = append(tagIDs, tags[(i+1)%len(tags)].ID)
		}
		post, _ := db.CreatePost(user.ID, "Tagged Post", "", "Body", tagIDs)
		postIDs[i] = post.ID
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, err := db.GetTagsForPosts(postIDs)
		if err != nil {
			b.Fatalf("Failed to batch load tags: %v", err)
		}
	}
}

// BenchmarkValidateSession benchmarks session validation.
func BenchmarkValidateSession(b *testing.B) {
	db := benchmarkDB(b)
	user, _ := db.CreateUser("benchuser", "bench@example.com", nil)
	sessionToken, _ := db.CreateSession(user.ID)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, err := db.ValidateSession(sessionToken)
		if err != nil {
			b.Fatalf("Failed to validate session: %v", err)
		}
	}
}
