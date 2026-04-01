package middleware

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"makhor/pkg/models"
)

func TestGetUser(t *testing.T) {
	t.Run("user in context", func(t *testing.T) {
		user := &models.User{ID: 1, Username: "testuser"}
		req := httptest.NewRequest(http.MethodGet, "/", nil)
		ctx := context.WithValue(req.Context(), UserContextKey, user)
		req = req.WithContext(ctx)

		got := GetUser(req)
		if got == nil {
			t.Fatal("GetUser() returned nil, expected user")
		}
		if got.ID != 1 {
			t.Errorf("GetUser().ID = %d, want 1", got.ID)
		}
		if got.Username != "testuser" {
			t.Errorf("GetUser().Username = %q, want %q", got.Username, "testuser")
		}
	})

	t.Run("no user in context", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/", nil)
		got := GetUser(req)
		if got != nil {
			t.Errorf("GetUser() = %v, want nil", got)
		}
	})

	t.Run("wrong type in context", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/", nil)
		ctx := context.WithValue(req.Context(), UserContextKey, "not a user")
		req = req.WithContext(ctx)

		got := GetUser(req)
		if got != nil {
			t.Errorf("GetUser() = %v, want nil for wrong type", got)
		}
	})
}

func TestRequireAuth(t *testing.T) {
	t.Run("authenticated user passes through", func(t *testing.T) {
		called := false
		handler := RequireAuth(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			called = true
			w.WriteHeader(http.StatusOK)
		}))

		req := httptest.NewRequest(http.MethodGet, "/protected", nil)
		user := &models.User{ID: 1, Username: "testuser"}
		ctx := context.WithValue(req.Context(), UserContextKey, user)
		req = req.WithContext(ctx)

		rr := httptest.NewRecorder()
		handler.ServeHTTP(rr, req)

		if !called {
			t.Error("handler was not called for authenticated user")
		}
		if rr.Code != http.StatusOK {
			t.Errorf("status = %d, want %d", rr.Code, http.StatusOK)
		}
	})

	t.Run("unauthenticated user redirected", func(t *testing.T) {
		called := false
		handler := RequireAuth(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			called = true
		}))

		req := httptest.NewRequest(http.MethodGet, "/protected", nil)
		rr := httptest.NewRecorder()
		handler.ServeHTTP(rr, req)

		if called {
			t.Error("handler was called for unauthenticated user")
		}
		if rr.Code != http.StatusSeeOther {
			t.Errorf("status = %d, want %d", rr.Code, http.StatusSeeOther)
		}
		if loc := rr.Header().Get("Location"); loc != "/login" {
			t.Errorf("redirect location = %q, want %q", loc, "/login")
		}
	})
}

func TestRequireAdmin(t *testing.T) {
	t.Run("admin user passes through", func(t *testing.T) {
		called := false
		handler := RequireAdmin(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			called = true
			w.WriteHeader(http.StatusOK)
		}))

		req := httptest.NewRequest(http.MethodGet, "/admin", nil)
		user := &models.User{ID: 1, Username: "admin", IsAdmin: true}
		ctx := context.WithValue(req.Context(), UserContextKey, user)
		req = req.WithContext(ctx)

		rr := httptest.NewRecorder()
		handler.ServeHTTP(rr, req)

		if !called {
			t.Error("handler was not called for admin user")
		}
		if rr.Code != http.StatusOK {
			t.Errorf("status = %d, want %d", rr.Code, http.StatusOK)
		}
	})

	t.Run("non-admin user forbidden", func(t *testing.T) {
		called := false
		handler := RequireAdmin(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			called = true
		}))

		req := httptest.NewRequest(http.MethodGet, "/admin", nil)
		user := &models.User{ID: 1, Username: "regular", IsAdmin: false}
		ctx := context.WithValue(req.Context(), UserContextKey, user)
		req = req.WithContext(ctx)

		rr := httptest.NewRecorder()
		handler.ServeHTTP(rr, req)

		if called {
			t.Error("handler was called for non-admin user")
		}
		if rr.Code != http.StatusForbidden {
			t.Errorf("status = %d, want %d", rr.Code, http.StatusForbidden)
		}
	})

	t.Run("unauthenticated user forbidden", func(t *testing.T) {
		called := false
		handler := RequireAdmin(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			called = true
		}))

		req := httptest.NewRequest(http.MethodGet, "/admin", nil)
		rr := httptest.NewRecorder()
		handler.ServeHTTP(rr, req)

		if called {
			t.Error("handler was called for unauthenticated user")
		}
		if rr.Code != http.StatusForbidden {
			t.Errorf("status = %d, want %d", rr.Code, http.StatusForbidden)
		}
	})
}

func TestLogger(t *testing.T) {
	t.Run("logs request and passes through", func(t *testing.T) {
		called := false
		handler := Logger(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			called = true
			w.WriteHeader(http.StatusCreated)
		}))

		req := httptest.NewRequest(http.MethodPost, "/api/test", nil)
		rr := httptest.NewRecorder()
		handler.ServeHTTP(rr, req)

		if !called {
			t.Error("handler was not called")
		}
		if rr.Code != http.StatusCreated {
			t.Errorf("status = %d, want %d", rr.Code, http.StatusCreated)
		}
	})

	t.Run("default status is 200", func(t *testing.T) {
		handler := Logger(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			// Don't call WriteHeader
			w.Write([]byte("ok"))
		}))

		req := httptest.NewRequest(http.MethodGet, "/", nil)
		rr := httptest.NewRecorder()
		handler.ServeHTTP(rr, req)

		if rr.Code != http.StatusOK {
			t.Errorf("status = %d, want %d", rr.Code, http.StatusOK)
		}
	})
}

func TestResponseWriter(t *testing.T) {
	t.Run("captures status code", func(t *testing.T) {
		rr := httptest.NewRecorder()
		rw := &responseWriter{ResponseWriter: rr, status: http.StatusOK}

		rw.WriteHeader(http.StatusNotFound)

		if rw.status != http.StatusNotFound {
			t.Errorf("status = %d, want %d", rw.status, http.StatusNotFound)
		}
		if rr.Code != http.StatusNotFound {
			t.Errorf("underlying writer status = %d, want %d", rr.Code, http.StatusNotFound)
		}
	})

	t.Run("default status is OK", func(t *testing.T) {
		rr := httptest.NewRecorder()
		rw := &responseWriter{ResponseWriter: rr, status: http.StatusOK}

		if rw.status != http.StatusOK {
			t.Errorf("default status = %d, want %d", rw.status, http.StatusOK)
		}
	})
}

func TestGetClientIP(t *testing.T) {
	tests := []struct {
		name           string
		xForwardedFor  string
		xRealIP        string
		remoteAddr     string
		expectedIP     string
	}{
		{
			name:       "X-Forwarded-For takes priority",
			xForwardedFor: "192.168.1.100",
			xRealIP:      "10.0.0.1",
			remoteAddr:   "127.0.0.1:8080",
			expectedIP:   "192.168.1.100",
		},
		{
			name:       "X-Forwarded-For with multiple IPs takes first",
			xForwardedFor: "192.168.1.100, 10.0.0.1, 172.16.0.1",
			xRealIP:      "",
			remoteAddr:   "127.0.0.1:8080",
			expectedIP:   "192.168.1.100",
		},
		{
			name:       "X-Real-IP used when no X-Forwarded-For",
			xForwardedFor: "",
			xRealIP:      "10.0.0.1",
			remoteAddr:   "127.0.0.1:8080",
			expectedIP:   "10.0.0.1",
		},
		{
			name:       "RemoteAddr strips port",
			xForwardedFor: "",
			xRealIP:      "",
			remoteAddr:   "127.0.0.1:8080",
			expectedIP:   "127.0.0.1",
		},
		{
			name:       "empty X-Forwarded-For is ignored",
			xForwardedFor: "",
			xRealIP:      "10.0.0.5",
			remoteAddr:   "127.0.0.1:8080",
			expectedIP:   "10.0.0.5",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, "/", nil)
			req.RemoteAddr = tt.remoteAddr
			if tt.xForwardedFor != "" {
				req.Header.Set("X-Forwarded-For", tt.xForwardedFor)
			}
			if tt.xRealIP != "" {
				req.Header.Set("X-Real-IP", tt.xRealIP)
			}

			got := GetClientIP(req)
			if got != tt.expectedIP {
				t.Errorf("GetClientIP() = %q, want %q", got, tt.expectedIP)
			}
		})
	}
}

func TestUserContextKey(t *testing.T) {
	// Verify the context key type and value
	if UserContextKey != contextKey("user") {
		t.Errorf("UserContextKey = %v, want %v", UserContextKey, contextKey("user"))
	}
}

func TestRateLimiter(t *testing.T) {
	t.Run("allows requests under limit", func(t *testing.T) {
		rl := NewRateLimiter(5, time.Minute)
		for i := 0; i < 5; i++ {
			if !rl.Allow("192.168.1.1") {
				t.Errorf("request %d should be allowed", i+1)
			}
		}
	})

	t.Run("blocks requests over limit", func(t *testing.T) {
		rl := NewRateLimiter(3, time.Minute)
		for i := 0; i < 3; i++ {
			rl.Allow("192.168.1.1")
		}
		if rl.Allow("192.168.1.1") {
			t.Error("4th request should be blocked")
		}
	})

	t.Run("different IPs have separate limits", func(t *testing.T) {
		rl := NewRateLimiter(2, time.Minute)
		rl.Allow("192.168.1.1")
		rl.Allow("192.168.1.1")
		// First IP is now at limit

		// Second IP should still be allowed
		if !rl.Allow("192.168.1.2") {
			t.Error("different IP should be allowed")
		}
	})

	t.Run("middleware blocks over limit", func(t *testing.T) {
		rl := NewRateLimiter(1, time.Minute)

		called := 0
		handler := rl.Middleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			called++
			w.WriteHeader(http.StatusOK)
		}))

		// First request should pass
		req := httptest.NewRequest(http.MethodGet, "/", nil)
		req.RemoteAddr = "192.168.1.1:8080"
		rr := httptest.NewRecorder()
		handler.ServeHTTP(rr, req)

		if rr.Code != http.StatusOK {
			t.Errorf("first request status = %d, want %d", rr.Code, http.StatusOK)
		}
		if called != 1 {
			t.Errorf("handler called %d times, want 1", called)
		}

		// Second request should be blocked
		rr = httptest.NewRecorder()
		handler.ServeHTTP(rr, req)

		if rr.Code != http.StatusTooManyRequests {
			t.Errorf("second request status = %d, want %d", rr.Code, http.StatusTooManyRequests)
		}
		if called != 1 {
			t.Errorf("handler called %d times after block, want 1", called)
		}
	})
}

// TestRateLimiterConcurrency tests rate limiter under concurrent access.
func TestRateLimiterConcurrency(t *testing.T) {
	rl := NewRateLimiter(100, time.Minute)

	// Run concurrent requests
	done := make(chan bool, 50)
	for i := 0; i < 50; i++ {
		go func(ip string) {
			for j := 0; j < 10; j++ {
				rl.Allow(ip)
			}
			done <- true
		}("192.168.1." + string(rune('0'+i%10)))
	}

	// Wait for all goroutines
	for i := 0; i < 50; i++ {
		<-done
	}

	// Should not panic or deadlock
}

// TestGetClientIPIPv6 tests IP extraction with IPv6 addresses.
func TestGetClientIPIPv6(t *testing.T) {
	tests := []struct {
		name       string
		remoteAddr string
		expected   string
	}{
		{"IPv6 with port", "[::1]:8080", "[::1]"},
		{"IPv6 loopback bracket no port", "[::1]", "[::1]"},
		{"IPv6 full address", "[2001:db8::1]:8080", "[2001:db8::1]"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, "/", nil)
			req.RemoteAddr = tt.remoteAddr
			got := GetClientIP(req)
			if got != tt.expected {
				t.Errorf("GetClientIP() = %q, want %q", got, tt.expected)
			}
		})
	}
}

// TestLoggerMiddlewareStatus tests Logger captures different status codes.
func TestLoggerMiddlewareStatus(t *testing.T) {
	statuses := []int{
		http.StatusOK,
		http.StatusCreated,
		http.StatusBadRequest,
		http.StatusNotFound,
		http.StatusInternalServerError,
	}

	for _, status := range statuses {
		t.Run(http.StatusText(status), func(t *testing.T) {
			handler := Logger(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(status)
			}))

			req := httptest.NewRequest(http.MethodGet, "/test", nil)
			rr := httptest.NewRecorder()
			handler.ServeHTTP(rr, req)

			if rr.Code != status {
				t.Errorf("status = %d, want %d", rr.Code, status)
			}
		})
	}
}

// TestRequireAuthWithAPIPath tests RequireAuth with different paths.
func TestRequireAuthWithAPIPath(t *testing.T) {
	paths := []string{"/api/posts", "/settings", "/submit", "/profile"}

	for _, path := range paths {
		t.Run(path, func(t *testing.T) {
			handler := RequireAuth(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(http.StatusOK)
			}))

			req := httptest.NewRequest(http.MethodGet, path, nil)
			rr := httptest.NewRecorder()
			handler.ServeHTTP(rr, req)

			// Should redirect to login
			if rr.Code != http.StatusSeeOther {
				t.Errorf("status = %d, want %d", rr.Code, http.StatusSeeOther)
			}
			if loc := rr.Header().Get("Location"); loc != "/login" {
				t.Errorf("redirect = %q, want /login", loc)
			}
		})
	}
}

// TestRequireAdminWithNonAdmin tests RequireAdmin with regular users.
func TestRequireAdminWithNonAdmin(t *testing.T) {
	handler := RequireAdmin(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	// Test with regular user
	req := httptest.NewRequest(http.MethodGet, "/admin", nil)
	user := &models.User{ID: 1, Username: "regular", IsAdmin: false}
	ctx := context.WithValue(req.Context(), UserContextKey, user)
	req = req.WithContext(ctx)

	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusForbidden {
		t.Errorf("status = %d, want %d", rr.Code, http.StatusForbidden)
	}
}

// TestResponseWriterMultipleWriteHeader tests WriteHeader called multiple times.
func TestResponseWriterMultipleWriteHeader(t *testing.T) {
	rr := httptest.NewRecorder()
	rw := &responseWriter{ResponseWriter: rr, status: http.StatusOK}

	// First write should work
	rw.WriteHeader(http.StatusNotFound)
	if rw.status != http.StatusNotFound {
		t.Errorf("first WriteHeader status = %d, want %d", rw.status, http.StatusNotFound)
	}

	// Second write - behavior depends on underlying writer
	// but should not panic
	rw.WriteHeader(http.StatusOK)
}

// TestContextKeyType tests that context key is properly typed.
func TestContextKeyType(t *testing.T) {
	// Ensure different string values don't collide
	key1 := contextKey("user")
	key2 := contextKey("other")

	if key1 == key2 {
		t.Error("different context keys should not be equal")
	}

	// But same string should be equal
	key3 := contextKey("user")
	if key1 != key3 {
		t.Error("same context key string should be equal")
	}
}

// TestGetUserWithBannedUser tests that banned user info is still retrievable.
func TestGetUserWithBannedUser(t *testing.T) {
	user := &models.User{
		ID:        1,
		Username:  "banned",
		IsBanned:  true,
		BanReason: "Spam",
	}

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	ctx := context.WithValue(req.Context(), UserContextKey, user)
	req = req.WithContext(ctx)

	got := GetUser(req)
	if got == nil {
		t.Fatal("GetUser() returned nil")
	}
	if !got.IsBanned {
		t.Error("User should be marked as banned")
	}
	if got.BanReason != "Spam" {
		t.Errorf("BanReason = %q, want %q", got.BanReason, "Spam")
	}
}

// TestGetClientIPWithWhitespace tests IP extraction with whitespace.
func TestGetClientIPWithWhitespace(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("X-Forwarded-For", "  192.168.1.1  , 10.0.0.1  ")

	got := GetClientIP(req)
	if got != "192.168.1.1" {
		t.Errorf("GetClientIP() = %q, want %q", got, "192.168.1.1")
	}
}
