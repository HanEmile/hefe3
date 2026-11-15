// Command r2wars-web runs the r2wars HTTP server.
package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"git.emile.space/r2wars-web/internal/mailer"
	"git.emile.space/r2wars-web/internal/sessionstore"
	"git.emile.space/r2wars-web/internal/store"
	"git.emile.space/r2wars-web/internal/token"
	"git.emile.space/r2wars-web/internal/web"
)

type config struct {
	host           string
	port           int
	logFilePath    string
	databasePath   string
	sessiondbPath  string
	templatesPath  string
	backupDir      string
	backupInterval time.Duration
	backupKeep     int

	baseURL          string
	magicLinkTTL     time.Duration
	smtpHost         string
	smtpPort         int
	smtpUsername     string
	smtpFrom         string
	smtpPasswordFile string

	// devMode turns on a cluster of conveniences for running/testing the
	// whole app with zero external setup - no real SMTP server, no SESSION_KEY
	// to generate, and no login required to reach pages that normally need
	// one. It must never be set in production: see web.Server.DevMode's doc
	// comment for exactly what it disables.
	devMode bool
}

func main() {
	var cfg config

	flag.StringVar(&cfg.host, "host", "127.0.0.1", "The host to listen on")
	flag.StringVar(&cfg.host, "h", "127.0.0.1", "The host to listen on (shorthand)")
	flag.IntVar(&cfg.port, "port", 8080, "The port to listen on")
	flag.IntVar(&cfg.port, "p", 8080, "The port to listen on (shorthand)")
	flag.StringVar(&cfg.logFilePath, "logfilepath", "./server.log", "The path to the log file")
	flag.StringVar(&cfg.databasePath, "databasepath", "./main.db", "The path to the main database")
	flag.StringVar(&cfg.sessiondbPath, "sessiondbpath", "./sessions.db", "The path to the session database")
	flag.StringVar(&cfg.templatesPath, "templates", "./templates", "The path to the templates used")
	flag.StringVar(&cfg.backupDir, "backupdir", "./backups", "Directory periodic database backups are written to")
	flag.DurationVar(&cfg.backupInterval, "backupinterval", time.Hour, "How often to back up the database (0 disables backups)")
	flag.IntVar(&cfg.backupKeep, "backupkeep", 24, "How many database backups to retain")

	flag.StringVar(&cfg.baseURL, "baseurl", "http://127.0.0.1:8080", "Public base URL, used to build the links emailed for login")
	flag.DurationVar(&cfg.magicLinkTTL, "magiclinkttl", 15*time.Minute, "How long an emailed login link stays valid")
	flag.StringVar(&cfg.smtpHost, "smtphost", "", "SMTP server host used to send login-link emails (required)")
	flag.IntVar(&cfg.smtpPort, "smtpport", 587, "SMTP server port")
	flag.StringVar(&cfg.smtpUsername, "smtpusername", "", "SMTP auth username (required)")
	flag.StringVar(&cfg.smtpFrom, "smtpfrom", "", "From address for login-link emails (defaults to -smtpusername)")
	flag.StringVar(&cfg.smtpPasswordFile, "smtppasswordfile", "", "Path to a file containing the SMTP auth password (required)")
	flag.BoolVar(&cfg.devMode, "devmode", false, "DANGER: disables login and falls back to logging emails instead of sending them. For local/offline testing only - never set this in production.")
	flag.Parse()

	if err := run(cfg); err != nil {
		// Not log.Fatal: run() points the standard logger at logFile and then
		// (via defer) closes it before returning, so by the time we get here
		// log.Fatal would silently write to an already-closed file handle -
		// exactly the startup failures an operator most needs to see land
		// nowhere. Write directly to stderr instead, which systemd/journald
		// always captures regardless of where the app's own log file points.
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run(cfg config) error {
	log.Println("[i] Setting up logging...")
	logFile, err := os.OpenFile(cfg.logFilePath, os.O_WRONLY|os.O_CREATE|os.O_APPEND, 0664)
	if err != nil {
		return fmt.Errorf("opening log file: %w", err)
	}
	defer logFile.Close()
	log.SetOutput(logFile)

	if cfg.devMode {
		log.Println("[!!!] DEVMODE ENABLED: login is disabled and every request is treated as a fixed dev user. Never set -devmode in production.")
	}

	log.Println("[i] Opening the database...")
	st, err := store.Open(cfg.databasePath)
	if err != nil {
		return fmt.Errorf("opening database: %w", err)
	}
	defer st.Close()

	log.Println("[i] Setting up session storage...")
	sessionKey := os.Getenv("SESSION_KEY")
	if sessionKey == "" {
		if !cfg.devMode {
			return fmt.Errorf("SESSION_KEY environment variable must be set")
		}
		// Zero-config offline testing: a throwaway key is fine since devmode
		// sessions don't protect anything real.
		log.Println("[!] devmode: no SESSION_KEY set, generating a throwaway one")
		sessionKey, err = randomSessionKey()
		if err != nil {
			return err
		}
	}
	sessions, err := sessionstore.New(cfg.sessiondbPath, "sessions", "/", 3600, []byte(sessionKey))
	if err != nil {
		return fmt.Errorf("opening session store: %w", err)
	}
	defer sessions.Close()

	log.Println("[i] Setting up mailer...")
	mail, err := newMailer(cfg)
	if err != nil {
		return err
	}

	log.Println("[i] Setting up HTTP routes...")
	srv, err := web.New(st, sessions, mail, cfg.templatesPath, os.Getenv("VERSION"), cfg.baseURL, cfg.magicLinkTTL)
	if err != nil {
		return fmt.Errorf("setting up web server: %w", err)
	}
	srv.DevMode = cfg.devMode

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	srv.RunBackups(ctx, cfg.backupDir, cfg.backupInterval, cfg.backupKeep)
	srv.RunMagicLinkCleanup(ctx, time.Hour)

	httpServer := &http.Server{
		Addr:    fmt.Sprintf("%s:%d", cfg.host, cfg.port),
		Handler: srv.Routes(),
	}

	serveErr := make(chan error, 1)
	go func() {
		log.Printf("[i] HTTP server running on %s:%d\n", cfg.host, cfg.port)
		if err := httpServer.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			serveErr <- err
		}
		close(serveErr)
	}()

	// Graceful shutdown: on SIGINT/SIGTERM (what systemd sends on stop/
	// restart), stop accepting new connections and give in-flight requests -
	// including a running battle simulation - a chance to finish instead of
	// being cut off mid-write.
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, os.Interrupt, syscall.SIGTERM)

	select {
	case err := <-serveErr:
		if err != nil {
			return fmt.Errorf("http server: %w", err)
		}
	case sig := <-sigCh:
		log.Printf("[i] Received %s, shutting down...\n", sig)
		shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer shutdownCancel()
		if err := httpServer.Shutdown(shutdownCtx); err != nil {
			log.Printf("[!] Error during shutdown: %v\n", err)
		}
	}

	return nil
}

// randomSessionKey generates a throwaway session signing key for -devmode,
// where there's nothing real to protect, so a fresh key on every start
// (meaning old dev sessions quietly stop validating) is an acceptable
// trade for not having to set SESSION_KEY at all. token.New is built for
// magic-link tokens, but "32 random bytes, URL-safe encoded" is exactly
// what's needed here too.
func randomSessionKey() (string, error) {
	raw, _, err := token.New()
	if err != nil {
		return "", fmt.Errorf("generating session key: %w", err)
	}
	return raw, nil
}

// newMailer builds the mailer login links are sent through. Normally that
// means -smtphost/-smtpusername/-smtppasswordfile are required - there is no
// password-based login to fall back to, so without mail delivery nobody can
// log in at all. In -devmode, missing SMTP config falls back to logging the
// email instead of sending it, so the login flow is still fully testable
// with zero mail setup.
func newMailer(cfg config) (mailer.Mailer, error) {
	if cfg.smtpHost == "" {
		if cfg.devMode {
			log.Println("[!] devmode: no -smtphost set, logging emails instead of sending them")
			return mailer.Console{}, nil
		}
		return nil, fmt.Errorf("-smtphost must be set")
	}
	if cfg.smtpUsername == "" {
		return nil, fmt.Errorf("-smtpusername must be set")
	}
	if cfg.smtpPasswordFile == "" {
		return nil, fmt.Errorf("-smtppasswordfile must be set")
	}

	passwordBytes, err := os.ReadFile(cfg.smtpPasswordFile)
	if err != nil {
		return nil, fmt.Errorf("reading -smtppasswordfile: %w", err)
	}

	from := cfg.smtpFrom
	if from == "" {
		from = cfg.smtpUsername
	}

	return mailer.SMTP{
		Host:     cfg.smtpHost,
		Port:     cfg.smtpPort,
		Username: cfg.smtpUsername,
		Password: strings.TrimSpace(string(passwordBytes)),
		From:     from,
	}, nil
}
