// Package config handles application configuration.
package config

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"
)

// Config holds the application configuration.
type Config struct {
	Email   EmailConfig `json:"email"`
	Subpath string      `json:"subpath"` // URL subpath (e.g., "/makhor" to serve at example.com/makhor/)
}

// EmailConfig holds SMTP configuration.
type EmailConfig struct {
	Host         string `json:"host"`
	Port         int    `json:"port"`
	User         string `json:"user"`
	Password     string `json:"password"`
	PasswordFile string `json:"password_file"`
	From         string `json:"from"`
	UseTLS       bool   `json:"use_tls"`
}

// Load reads configuration from a JSON file.
func Load(path string) (*Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("reading config file: %w", err)
	}

	var cfg Config
	if err := json.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("parsing config file: %w", err)
	}

	// Load password from file if password_file is set
	if cfg.Email.PasswordFile != "" {
		pwData, err := os.ReadFile(cfg.Email.PasswordFile)
		if err != nil {
			return nil, fmt.Errorf("reading password file: %w", err)
		}
		cfg.Email.Password = strings.TrimSpace(string(pwData))
	}

	// Normalize subpath: ensure it starts with / and doesn't end with /
	cfg.Subpath = NormalizeSubpath(cfg.Subpath)

	return &cfg, nil
}

// NormalizeSubpath ensures the subpath starts with / and doesn't end with /.
// Empty string means no subpath (root).
func NormalizeSubpath(s string) string {
	s = strings.TrimSpace(s)
	if s == "" || s == "/" {
		return ""
	}
	// Ensure starts with /
	if !strings.HasPrefix(s, "/") {
		s = "/" + s
	}
	// Remove trailing /
	s = strings.TrimSuffix(s, "/")
	return s
}

// PrintExampleConfig prints an example configuration to stdout.
func PrintExampleConfig() {
	fmt.Println("Example configuration file format:")
	fmt.Println(`{
  "email": {
    "host": "smtp.example.com",
    "port": 587,
    "user": "user@example.com",
    "password": "your-password",
    "from": "noreply@example.com",
    "use_tls": true
  },
  "subpath": ""
}`)
	fmt.Println("\nAlternatively, use password_file to read password from a file:")
	fmt.Println(`{
  "email": {
    "host": "smtp.example.com",
    "port": 587,
    "user": "user@example.com",
    "password_file": "/path/to/password.txt",
    "from": "noreply@example.com",
    "use_tls": true
  },
  "subpath": "/makhor"
}`)
	fmt.Println("\nThe subpath option allows running makhor at a URL like example.com/makhor/")
	fmt.Println("\nSave this to a file and pass it with -config flag:")
	fmt.Println("  ./makhor -config config.json")
}
