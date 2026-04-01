// makhor is a minimal link aggregator inspired by Lobste.rs and Hacker News.
// Pure Go, SQLite storage, no JavaScript.
package main

import (
	"context"
	"flag"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"makhor/pkg/api"
	"makhor/pkg/config"
	"makhor/pkg/db"
	"makhor/pkg/handlers"
	"makhor/pkg/logger"
	"makhor/pkg/mail"
	"makhor/pkg/middleware"
	"makhor/pkg/rsspoll"
)

func main() {
	// Command line flags
	addr := flag.String("addr", ":8080", "HTTP listen address")
	dbPath := flag.String("db", "makhor.db", "SQLite database path")
	baseURL := flag.String("base-url", "http://localhost:8080", "Base URL for links")
	subpath := flag.String("subpath", "", "URL subpath (e.g., /makhor to serve at example.com/makhor/)")
	createAdmin := flag.String("create-admin", "", "Create admin user with this email (format: username:email)")
	rssInterval := flag.Duration("rss-interval", 5*time.Minute, "RSS polling interval")
	enableRSS := flag.Bool("enable-rss", true, "Enable RSS feed polling")
	configPath := flag.String("config", "", "Path to config file (JSON)")

	// Backup flags
	backupPath := flag.String("backup", "", "Create backup at this path and exit")
	restorePath := flag.String("restore", "", "Restore from backup at this path and exit")
	backupDir := flag.String("backup-dir", "./backups", "Directory for automatic backups")
	backupInterval := flag.Duration("backup-interval", 0, "Automatic backup interval (e.g., 24h). 0 to disable")
	backupRetain := flag.Int("backup-retain", 7, "Number of automatic backups to retain")

	// Logging flags
	logLevel := flag.String("log-level", "info", "Log level: debug, info, warn, error")

	flag.Parse()

	// Setup logging
	logger.SetLevel(logger.ParseLevel(*logLevel))

	// Handle backup command
	if *backupPath != "" {
		database, err := db.New(*dbPath)
		if err != nil {
			logger.Fatal("Failed to open database: %v", err)
		}
		if err := database.Backup(*backupPath); err != nil {
			logger.Fatal("Backup failed: %v", err)
		}
		logger.Info("Backup created: %s", *backupPath)
		database.Close()
		return
	}

	// Handle restore command
	if *restorePath != "" {
		if err := db.Restore(*restorePath, *dbPath); err != nil {
			logger.Fatal("Restore failed: %v", err)
		}
		logger.Info("Database restored from: %s", *restorePath)
		return
	}

	// Load config if provided, otherwise print example format
	var cfg *config.Config
	var mailFunc func(to, subject, body string) error

	if *configPath != "" {
		var err error
		cfg, err = config.Load(*configPath)
		if err != nil {
			logger.Fatal("Failed to load config: %v", err)
		}
		// Setup mailer from config
		mailer := mail.New(cfg.Email)
		mailFunc = mailer.SendFunc()
		logger.Info("Email configured: %s:%d", cfg.Email.Host, cfg.Email.Port)
	} else {
		logger.Warn("No config file provided. Email login will be disabled.")
		logger.Info("To enable email login, create a config file and pass it with -config flag.")
		config.PrintExampleConfig()
	}

	// Initialize database
	database, err := db.New(*dbPath)
	if err != nil {
		logger.Fatal("Failed to initialize database: %v", err)
	}
	defer database.Close()

	// Create admin user if requested
	if *createAdmin != "" {
		parts := strings.SplitN(*createAdmin, ":", 2)
		if len(parts) != 2 {
			logger.Fatal("Invalid create-admin format. Use: username:email")
		}
		username, email := parts[0], parts[1]

		// Check if user exists
		_, err := database.GetUserByEmail(email)
		if err == db.ErrUserNotFound {
			user, err := database.CreateUser(username, email, nil)
			if err != nil {
				logger.Fatal("Failed to create admin user: %v", err)
			}

			// Make admin
			_, err = database.Exec("UPDATE users SET is_admin = TRUE WHERE id = ?", user.ID)
			if err != nil {
				logger.Fatal("Failed to make user admin: %v", err)
			}

			logger.Info("Created admin user: %s (%s)", username, email)
		} else if err != nil {
			logger.Fatal("Error checking for user: %v", err)
		} else {
			logger.Info("User %s already exists", email)
		}
	}

	// Normalize subpath from CLI flag or config
	normalizedSubpath := config.NormalizeSubpath(*subpath)
	if cfg != nil && cfg.Subpath != "" && normalizedSubpath == "" {
		normalizedSubpath = cfg.Subpath // Config file subpath (already normalized during load)
	}

	// Initialize handlers (templates are embedded in the binary)
	h, err := handlers.New(database, *baseURL, normalizedSubpath, mailFunc)
	if err != nil {
		logger.Fatal("Failed to initialize handlers: %v", err)
	}

	// Authentication middleware with session caching
	auth := middleware.NewAuth(database)

	// Setup routes
	mux := http.NewServeMux()

	// Static pages
	mux.HandleFunc("/", routeHandler(h))
	mux.HandleFunc("/newest", h.HomePage) // handled by view param
	mux.HandleFunc("/about", h.AboutPage)
	mux.HandleFunc("/metrics", h.MetricsHandler)
	mux.HandleFunc("/tags", h.TagsPage)
	mux.HandleFunc("/tags/new", methodHandler(h.CreateTagPage, h.CreateTagSubmit))
	mux.HandleFunc("/tags/admins", methodHandler(h.TagAdminsPage, h.AddTagAdmin))
	mux.HandleFunc("/tags/admins/remove", h.RemoveTagAdmin)
	mux.HandleFunc("/tags/feeds/view", h.ViewTagFeedsPage) // Public view of feeds
	mux.HandleFunc("/tags/feeds", methodHandler(h.TagFeedsPage, h.AddTagFeed))
	mux.HandleFunc("/tags/feeds/update", h.UpdateTagFeed)
	mux.HandleFunc("/tags/feeds/delete", h.DeleteTagFeed)
	mux.HandleFunc("/tags/feeds/sync", h.SyncTagFeed)
	mux.HandleFunc("/tags/update-description", h.UpdateTagDescription)
	mux.HandleFunc("/tags/delete", h.DeleteTag)
	mux.HandleFunc("/tags/", h.TagDetailPage)
	mux.HandleFunc("/search", h.SearchPage)
	mux.HandleFunc("/domain/", h.DomainPage)
	mux.HandleFunc("/comments", h.RecentCommentsPage)
	mux.HandleFunc("/invite-tree", h.InviteTreePage)
	mux.HandleFunc("/feeds/delete", h.DeleteFeedSubmit)
	mux.HandleFunc("/feeds/", h.FeedDetailPage)

	// Collection routes
	mux.HandleFunc("/collections", h.CollectionsPage)
	mux.HandleFunc("/collections/new", methodHandler(h.CreateCollectionPage, h.CreateCollectionSubmit))
	mux.HandleFunc("/collections/delete", h.DeleteCollectionSubmit)
	mux.HandleFunc("/collections/", collectionRouter(h))

	// Rate limiter for auth endpoints (5 requests per minute per IP)
	authRateLimiter := middleware.NewRateLimiter(5, time.Minute)

	// Auth routes (with rate limiting)
	mux.Handle("/login", authRateLimiter.Middleware(http.HandlerFunc(methodHandler(h.LoginPage, h.LoginSubmit))))
	mux.HandleFunc("/login/verify", h.LoginVerify)
	mux.HandleFunc("/logout", h.Logout)
	mux.Handle("/register", authRateLimiter.Middleware(http.HandlerFunc(methodHandler(h.RegisterPage, h.RegisterSubmit))))

	// User routes - profile handles settings/invites/hats as sections
	mux.HandleFunc("/users/", h.UserProfilePage)
	mux.HandleFunc("/avatar/", h.ServeAvatar)
	// Backward compatibility redirects
	mux.HandleFunc("/settings", redirectToProfile("settings"))
	mux.HandleFunc("/settings/avatar", redirectToProfile("settings"))
	mux.HandleFunc("/settings/email/change", h.ChangeEmailSubmit)
	mux.HandleFunc("/settings/email/verify", h.VerifyEmailChange)
	mux.HandleFunc("/invites", redirectToProfile("invites"))

	// Post routes
	mux.HandleFunc("/submit", methodHandler(h.SubmitPage, h.SubmitPost))
	mux.HandleFunc("/posts/remove-tag", h.RemovePostTag)
	mux.HandleFunc("/posts/", postRouter(h))

	// Comment routes
	mux.HandleFunc("/comments/", commentRouter(h))

	// RSS feeds
	mux.HandleFunc("/rss.xml", h.RSSFeed)
	mux.HandleFunc("/rss/tag/", h.RSSFeedByTag)
	mux.HandleFunc("/rss/tags.xml", h.RSSTagsMulti)   // Multi-tag with subtags: /rss/tags.xml?tags=go,rust
	mux.HandleFunc("/rss/user/", h.RSSFeedByUser)
	mux.HandleFunc("/rss/comments.xml", h.RSSComments)
	mux.HandleFunc("/rss/log.xml", h.RSSActionLog)    // Action log: /rss/log.xml?category=post

	// Public moderation log
	mux.HandleFunc("/modlog", h.ModerationLogPage)

	// Admin routes
	mux.HandleFunc("/admin/feeds", h.AdminFeedsPage)
	mux.HandleFunc("/admin/log", h.AdminLogPage)
	mux.HandleFunc("/admin/modlog", h.ModerationLogPage)
	mux.HandleFunc("/admin/hats", h.AdminHatsPage)
	mux.HandleFunc("/admin/hats/grant", methodHandler(h.AdminGrantHatPage, h.AdminGrantHatSubmit))
	mux.HandleFunc("/admin/hats/edit", methodHandler(h.AdminEditHatPage, h.AdminEditHatSubmit))
	mux.HandleFunc("/admin/hats/revoke", h.AdminRevokeHat)
	mux.HandleFunc("/admin/hats/reactivate", h.AdminReactivateHat)
	mux.HandleFunc("/admin/ban", methodHandler(h.AdminBanUserPage, h.AdminBanUserSubmit))
	mux.HandleFunc("/admin/unban", h.AdminUnbanUser)

	// API routes
	apiHandler := api.New(database)
	mux.Handle("/api/", auth.Middleware(apiHandler.Router()))

	// Apply middleware (order: logger -> subpath strip -> compress -> auth)
	handler := auth.Middleware(mux)
	handler = middleware.Compress(handler)
	handler = middleware.StripSubpath(normalizedSubpath)(handler)
	handler = middleware.Logger(handler)

	// Start cleanup goroutine
	go func() {
		for {
			time.Sleep(1 * time.Hour)
			if err := database.CleanupExpired(); err != nil {
				logger.Error("Cleanup error: %v", err)
			}
		}
	}()

	// Start automatic backup goroutine if enabled
	if *backupInterval > 0 {
		go func() {
			ticker := time.NewTicker(*backupInterval)
			defer ticker.Stop()
			for range ticker.C {
				path, err := database.BackupWithTimestamp(*backupDir)
				if err != nil {
					logger.Error("Automatic backup failed: %v", err)
					continue
				}
				logger.Info("Automatic backup created: %s", path)
				if err := db.CleanOldBackups(*backupDir, *backupRetain); err != nil {
					logger.Error("Failed to clean old backups: %v", err)
				}
			}
		}()
		logger.Info("Automatic backups enabled: every %v to %s (retain %d)", *backupInterval, *backupDir, *backupRetain)
	}

	// Start RSS poller if enabled
	var poller *rsspoll.Poller
	if *enableRSS {
		// Get bot user ID (first admin)
		var botUserID int64
		err := database.QueryRow("SELECT id FROM users WHERE is_admin = TRUE ORDER BY id LIMIT 1").Scan(&botUserID)
		if err != nil {
			logger.Warn("No admin user for RSS bot, RSS polling disabled")
		} else {
			poller = rsspoll.New(database, botUserID, *rssInterval)
			poller.Start()
			// Wire up poller to handler for manual sync
			h.Poller = poller
			logger.Debug("RSS poller started with interval %v", *rssInterval)
		}
	}

	// Create HTTP server
	server := &http.Server{
		Addr:    *addr,
		Handler: handler,
	}

	// Handle graceful shutdown
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)

	go func() {
		sig := <-sigChan
		logger.Info("Received signal %v, shutting down...", sig)

		// Create shutdown context with timeout
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()

		// Stop RSS poller
		if poller != nil {
			logger.Debug("Stopping RSS poller...")
			poller.Stop()
		}

		// Shutdown HTTP server gracefully
		logger.Debug("Shutting down HTTP server...")
		if err := server.Shutdown(ctx); err != nil {
			logger.Error("HTTP server shutdown error: %v", err)
		}

		logger.Info("Shutdown complete")
	}()

	// Start server
	logger.Info("Starting server on %s", *addr)
	logger.Info("Base URL: %s", *baseURL)
	if normalizedSubpath != "" {
		logger.Info("Subpath: %s", normalizedSubpath)
	}
	logger.Debug("API available at %s/api/", *baseURL)
	logger.Debug("Log level: %s", *logLevel)

	if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		logger.Fatal("Server failed: %v", err)
	}
}

// routeHandler handles the root route and distinguishes between home and 404.
func routeHandler(h *handlers.Handler) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/" {
			h.HomePage(w, r)
		} else {
			http.NotFound(w, r)
		}
	}
}

// methodHandler returns a handler that routes GET and POST to different handlers.
func methodHandler(get, post http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodGet:
			get(w, r)
		case http.MethodPost:
			post(w, r)
		default:
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		}
	}
}

// postRouter routes post-related URLs.
func postRouter(h *handlers.Handler) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		path := r.URL.Path

		switch {
		case strings.HasSuffix(path, "/vote"):
			h.VotePost(w, r)
		case strings.HasSuffix(path, "/edit"):
			if r.Method == http.MethodPost {
				h.EditPostSubmit(w, r)
			} else {
				h.EditPostPage(w, r)
			}
		case strings.HasSuffix(path, "/delete"):
			h.DeletePost(w, r)
		case strings.HasSuffix(path, "/comment"):
			h.SubmitComment(w, r)
		case strings.HasSuffix(path, "/history"):
			h.PostRevisionsPage(w, r)
		default:
			h.ViewPost(w, r)
		}
	}
}

// commentRouter routes comment-related URLs.
func commentRouter(h *handlers.Handler) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		path := r.URL.Path

		switch {
		case strings.HasSuffix(path, "/vote"):
			h.VoteComment(w, r)
		case strings.HasSuffix(path, "/edit"):
			if r.Method == http.MethodPost {
				h.EditCommentSubmit(w, r)
			} else {
				h.EditCommentPage(w, r)
			}
		case strings.HasSuffix(path, "/delete"):
			h.DeleteComment(w, r)
		case strings.HasSuffix(path, "/delete-tree"):
			h.DeleteCommentTree(w, r)
		case strings.HasSuffix(path, "/blur"):
			h.BlurComment(w, r)
		case strings.HasSuffix(path, "/unblur"):
			h.UnblurComment(w, r)
		case strings.HasSuffix(path, "/reply"):
			h.ReplyPage(w, r)
		default:
			// Redirect to post
			http.Redirect(w, r, "/", http.StatusSeeOther)
		}
	}
}

// redirectToProfile creates a handler that redirects to the user's profile with a section.
func redirectToProfile(section string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		user := middleware.GetUser(r)
		if user == nil {
			http.Redirect(w, r, "/login", http.StatusSeeOther)
			return
		}
		http.Redirect(w, r, "/users/"+user.Username+"?section="+section, http.StatusSeeOther)
	}
}

// collectionRouter routes collection-related URLs.
func collectionRouter(h *handlers.Handler) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		path := r.URL.Path

		switch {
		case strings.HasSuffix(path, "/edit"):
			if r.Method == http.MethodPost {
				h.EditCollectionSubmit(w, r)
			} else {
				h.EditCollectionPage(w, r)
			}
		default:
			h.CollectionDetailPage(w, r)
		}
	}
}

func init() {
	// Ensure we have a working directory printed on startup
	if wd, err := os.Getwd(); err == nil {
		fmt.Printf("Working directory: %s\n", wd)
	}
}
