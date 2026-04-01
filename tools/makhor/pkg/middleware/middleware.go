// Package middleware provides HTTP middleware for authentication and logging.
package middleware

import (
	"compress/gzip"
	"context"
	"io"
	"log"
	"makhor/pkg/db"
	"makhor/pkg/models"
	"net/http"
	"strings"
	"sync"
	"time"
)

// contextKey is a type for context keys to avoid collisions.
type contextKey string

const (
	UserContextKey contextKey = "user"
)

// SessionCacheTTL is how long sessions are cached in memory.
const SessionCacheTTL = 30 * time.Second

// cachedSession holds a session with its expiry time.
type cachedSession struct {
	user      *models.User
	expiresAt time.Time
}

// SessionCache provides in-memory caching for session lookups.
type SessionCache struct {
	mu    sync.RWMutex
	cache map[string]*cachedSession
}

// NewSessionCache creates a new session cache with cleanup.
func NewSessionCache() *SessionCache {
	sc := &SessionCache{
		cache: make(map[string]*cachedSession),
	}
	go sc.cleanup()
	return sc
}

// Get retrieves a cached session if valid.
func (sc *SessionCache) Get(token string) (*models.User, bool) {
	sc.mu.RLock()
	defer sc.mu.RUnlock()

	if entry, ok := sc.cache[token]; ok && time.Now().Before(entry.expiresAt) {
		return entry.user, true
	}
	return nil, false
}

// Set caches a session.
func (sc *SessionCache) Set(token string, user *models.User) {
	sc.mu.Lock()
	defer sc.mu.Unlock()

	sc.cache[token] = &cachedSession{
		user:      user,
		expiresAt: time.Now().Add(SessionCacheTTL),
	}
}

// Invalidate removes a session from cache.
func (sc *SessionCache) Invalidate(token string) {
	sc.mu.Lock()
	defer sc.mu.Unlock()
	delete(sc.cache, token)
}

// cleanup periodically removes expired entries.
func (sc *SessionCache) cleanup() {
	ticker := time.NewTicker(time.Minute)
	defer ticker.Stop()

	for range ticker.C {
		sc.mu.Lock()
		now := time.Now()
		for token, entry := range sc.cache {
			if now.After(entry.expiresAt) {
				delete(sc.cache, token)
			}
		}
		sc.mu.Unlock()
	}
}

// Auth is middleware that loads the current user from session cookie.
type Auth struct {
	DB    *db.DB
	Cache *SessionCache
}

// NewAuth creates a new Auth middleware with session caching.
func NewAuth(database *db.DB) *Auth {
	return &Auth{
		DB:    database,
		Cache: NewSessionCache(),
	}
}

// Middleware returns an HTTP middleware that populates the user context.
func (a *Auth) Middleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		cookie, err := r.Cookie("session")
		if err == nil && cookie.Value != "" {
			// Check cache first
			if user, ok := a.Cache.Get(cookie.Value); ok {
				if user != nil && !user.IsBanned {
					ctx := context.WithValue(r.Context(), UserContextKey, user)
					r = r.WithContext(ctx)
				}
			} else {
				// Cache miss - query database
				user, err := a.DB.ValidateSession(cookie.Value)
				if err == nil && user != nil {
					a.Cache.Set(cookie.Value, user)
					if !user.IsBanned {
						ctx := context.WithValue(r.Context(), UserContextKey, user)
						r = r.WithContext(ctx)
					}
				}
			}
		}
		next.ServeHTTP(w, r)
	})
}

// InvalidateSession removes a session from the cache (call on logout).
func (a *Auth) InvalidateSession(token string) {
	a.Cache.Invalidate(token)
}

// GetUser retrieves the current user from request context.
func GetUser(r *http.Request) *models.User {
	user, _ := r.Context().Value(UserContextKey).(*models.User)
	return user
}

// RequireAuth is middleware that requires authentication.
func RequireAuth(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		user := GetUser(r)
		if user == nil {
			http.Redirect(w, r, "/login", http.StatusSeeOther)
			return
		}
		next.ServeHTTP(w, r)
	})
}

// RequireAdmin is middleware that requires admin privileges.
func RequireAdmin(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		user := GetUser(r)
		if user == nil || !user.IsAdmin {
			http.Error(w, "Forbidden", http.StatusForbidden)
			return
		}
		next.ServeHTTP(w, r)
	})
}

// Logger is middleware that logs HTTP requests.
func Logger(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()

		// Wrap response writer to capture status code
		wrapped := &responseWriter{ResponseWriter: w, status: http.StatusOK}

		next.ServeHTTP(wrapped, r)

		log.Printf("%s %s %d %v", r.Method, r.URL.Path, wrapped.status, time.Since(start))
	})
}

// responseWriter wraps http.ResponseWriter to capture the status code.
type responseWriter struct {
	http.ResponseWriter
	status int
}

func (rw *responseWriter) WriteHeader(code int) {
	rw.status = code
	rw.ResponseWriter.WriteHeader(code)
}

// GetClientIP extracts the client IP from the request.
// When behind a reverse proxy, use the first IP from X-Forwarded-For.
// This is the client IP as seen by the first proxy in the chain.
func GetClientIP(r *http.Request) string {
	// Check X-Forwarded-For header first (for proxies)
	// Format: "client, proxy1, proxy2" - take the first (leftmost) IP
	if xff := r.Header.Get("X-Forwarded-For"); xff != "" {
		ips := strings.Split(xff, ",")
		if len(ips) > 0 {
			// Return trimmed first IP (the original client)
			return strings.TrimSpace(ips[0])
		}
	}
	// Check X-Real-IP (set by some proxies like nginx)
	if xri := r.Header.Get("X-Real-IP"); xri != "" {
		return strings.TrimSpace(xri)
	}
	// Fall back to RemoteAddr, strip port if present
	addr := r.RemoteAddr
	if idx := strings.LastIndex(addr, ":"); idx != -1 {
		// Check if this is IPv6 [::1]:port format
		if strings.HasPrefix(addr, "[") {
			if bracketIdx := strings.LastIndex(addr, "]"); bracketIdx != -1 && idx > bracketIdx {
				return addr[:idx]
			}
		} else {
			// IPv4 host:port format
			return addr[:idx]
		}
	}
	return addr
}

// MaxTrackedIPs is the maximum number of IPs to track in the rate limiter.
const MaxTrackedIPs = 10000

// RateLimiter provides per-IP rate limiting for sensitive endpoints.
type RateLimiter struct {
	mu       sync.Mutex
	requests map[string][]time.Time
	limit    int           // Max requests per window
	window   time.Duration // Time window
}

// NewRateLimiter creates a new rate limiter.
func NewRateLimiter(limit int, window time.Duration) *RateLimiter {
	rl := &RateLimiter{
		requests: make(map[string][]time.Time),
		limit:    limit,
		window:   window,
	}
	// Start cleanup goroutine
	go rl.cleanup()
	return rl
}

// Allow checks if a request from the given IP should be allowed.
func (rl *RateLimiter) Allow(ip string) bool {
	rl.mu.Lock()
	defer rl.mu.Unlock()

	now := time.Now()
	cutoff := now.Add(-rl.window)

	// Filter old requests
	recent := rl.requests[ip][:0] // Reuse slice backing array
	for _, t := range rl.requests[ip] {
		if t.After(cutoff) {
			recent = append(recent, t)
		}
	}

	if len(recent) >= rl.limit {
		rl.requests[ip] = recent
		return false
	}

	// Prevent unbounded growth - if too many IPs tracked, reject new ones
	if len(recent) == 0 && len(rl.requests) >= MaxTrackedIPs {
		return true // Allow but don't track (better than OOM)
	}

	rl.requests[ip] = append(recent, now)
	return true
}

// cleanup periodically removes old entries to prevent memory growth.
func (rl *RateLimiter) cleanup() {
	ticker := time.NewTicker(time.Minute) // Run more frequently
	defer ticker.Stop()

	for range ticker.C {
		rl.mu.Lock()
		cutoff := time.Now().Add(-rl.window)
		for ip, times := range rl.requests {
			var recent []time.Time
			for _, t := range times {
				if t.After(cutoff) {
					recent = append(recent, t)
				}
			}
			if len(recent) == 0 {
				delete(rl.requests, ip)
			} else {
				rl.requests[ip] = recent
			}
		}
		rl.mu.Unlock()
	}
}

// Middleware returns HTTP middleware that enforces rate limiting.
func (rl *RateLimiter) Middleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ip := GetClientIP(r)
		if !rl.Allow(ip) {
			http.Error(w, "Too Many Requests", http.StatusTooManyRequests)
			return
		}
		next.ServeHTTP(w, r)
	})
}

// StripSubpath returns middleware that strips a subpath prefix from request URLs.
// This allows the application to be served at a subpath like /makhor/.
func StripSubpath(subpath string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		if subpath == "" {
			return next // No subpath, pass through
		}
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			// Strip subpath from the URL path
			path := r.URL.Path
			if strings.HasPrefix(path, subpath) {
				r.URL.Path = strings.TrimPrefix(path, subpath)
				if r.URL.Path == "" {
					r.URL.Path = "/"
				}
			}
			next.ServeHTTP(w, r)
		})
	}
}

// gzipResponseWriter wraps http.ResponseWriter with gzip compression.
type gzipResponseWriter struct {
	io.Writer
	http.ResponseWriter
}

func (grw *gzipResponseWriter) Write(b []byte) (int, error) {
	return grw.Writer.Write(b)
}

// Compress returns middleware that gzip-compresses responses for clients that support it.
func Compress(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Check if client accepts gzip
		if !strings.Contains(r.Header.Get("Accept-Encoding"), "gzip") {
			next.ServeHTTP(w, r)
			return
		}

		// Skip compression for small responses or already compressed content
		// We'll compress HTML, JSON, CSS, JS - skip images, etc.
		gz, err := gzip.NewWriterLevel(w, gzip.BestSpeed)
		if err != nil {
			next.ServeHTTP(w, r)
			return
		}
		defer gz.Close()

		w.Header().Set("Content-Encoding", "gzip")
		w.Header().Del("Content-Length") // Will be set by gzip writer

		grw := &gzipResponseWriter{Writer: gz, ResponseWriter: w}
		next.ServeHTTP(grw, r)
	})
}
