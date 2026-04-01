package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoad(t *testing.T) {
	// Create a temp directory for test files
	tmpDir := t.TempDir()

	t.Run("valid config", func(t *testing.T) {
		configPath := filepath.Join(tmpDir, "valid.json")
		content := `{
			"email": {
				"host": "smtp.example.com",
				"port": 587,
				"user": "user@example.com",
				"password": "secret123",
				"from": "noreply@example.com",
				"use_tls": true
			}
		}`
		if err := os.WriteFile(configPath, []byte(content), 0644); err != nil {
			t.Fatalf("failed to write test config: %v", err)
		}

		cfg, err := Load(configPath)
		if err != nil {
			t.Fatalf("Load() error = %v", err)
		}

		if cfg.Email.Host != "smtp.example.com" {
			t.Errorf("Email.Host = %q, want %q", cfg.Email.Host, "smtp.example.com")
		}
		if cfg.Email.Port != 587 {
			t.Errorf("Email.Port = %d, want %d", cfg.Email.Port, 587)
		}
		if cfg.Email.User != "user@example.com" {
			t.Errorf("Email.User = %q, want %q", cfg.Email.User, "user@example.com")
		}
		if cfg.Email.Password != "secret123" {
			t.Errorf("Email.Password = %q, want %q", cfg.Email.Password, "secret123")
		}
		if cfg.Email.From != "noreply@example.com" {
			t.Errorf("Email.From = %q, want %q", cfg.Email.From, "noreply@example.com")
		}
		if !cfg.Email.UseTLS {
			t.Error("Email.UseTLS = false, want true")
		}
	})

	t.Run("config with password file", func(t *testing.T) {
		// Create password file
		pwFile := filepath.Join(tmpDir, "password.txt")
		if err := os.WriteFile(pwFile, []byte("file-secret\n"), 0644); err != nil {
			t.Fatalf("failed to write password file: %v", err)
		}

		configPath := filepath.Join(tmpDir, "pw_file.json")
		content := `{
			"email": {
				"host": "smtp.example.com",
				"port": 587,
				"user": "user@example.com",
				"password_file": "` + pwFile + `",
				"from": "noreply@example.com",
				"use_tls": true
			}
		}`
		if err := os.WriteFile(configPath, []byte(content), 0644); err != nil {
			t.Fatalf("failed to write test config: %v", err)
		}

		cfg, err := Load(configPath)
		if err != nil {
			t.Fatalf("Load() error = %v", err)
		}

		// Password should be loaded from file and trimmed
		if cfg.Email.Password != "file-secret" {
			t.Errorf("Email.Password = %q, want %q", cfg.Email.Password, "file-secret")
		}
	})

	t.Run("missing config file", func(t *testing.T) {
		_, err := Load(filepath.Join(tmpDir, "nonexistent.json"))
		if err == nil {
			t.Error("Load() expected error for missing file")
		}
	})

	t.Run("invalid JSON", func(t *testing.T) {
		configPath := filepath.Join(tmpDir, "invalid.json")
		if err := os.WriteFile(configPath, []byte("{invalid json}"), 0644); err != nil {
			t.Fatalf("failed to write test config: %v", err)
		}

		_, err := Load(configPath)
		if err == nil {
			t.Error("Load() expected error for invalid JSON")
		}
	})

	t.Run("missing password file", func(t *testing.T) {
		configPath := filepath.Join(tmpDir, "missing_pw.json")
		content := `{
			"email": {
				"host": "smtp.example.com",
				"port": 587,
				"user": "user@example.com",
				"password_file": "/nonexistent/password.txt",
				"from": "noreply@example.com"
			}
		}`
		if err := os.WriteFile(configPath, []byte(content), 0644); err != nil {
			t.Fatalf("failed to write test config: %v", err)
		}

		_, err := Load(configPath)
		if err == nil {
			t.Error("Load() expected error for missing password file")
		}
	})

	t.Run("empty config", func(t *testing.T) {
		configPath := filepath.Join(tmpDir, "empty.json")
		if err := os.WriteFile(configPath, []byte("{}"), 0644); err != nil {
			t.Fatalf("failed to write test config: %v", err)
		}

		cfg, err := Load(configPath)
		if err != nil {
			t.Fatalf("Load() error = %v", err)
		}

		// Should have zero values
		if cfg.Email.Host != "" {
			t.Errorf("Email.Host = %q, want empty", cfg.Email.Host)
		}
		if cfg.Email.Port != 0 {
			t.Errorf("Email.Port = %d, want 0", cfg.Email.Port)
		}
	})
}

func TestEmailConfigFields(t *testing.T) {
	// Test that EmailConfig struct has all expected fields
	cfg := EmailConfig{
		Host:         "smtp.test.com",
		Port:         465,
		User:         "test@test.com",
		Password:     "testpass",
		PasswordFile: "/path/to/file",
		From:         "from@test.com",
		UseTLS:       true,
	}

	if cfg.Host != "smtp.test.com" {
		t.Errorf("Host field not set correctly")
	}
	if cfg.Port != 465 {
		t.Errorf("Port field not set correctly")
	}
	if cfg.User != "test@test.com" {
		t.Errorf("User field not set correctly")
	}
	if cfg.Password != "testpass" {
		t.Errorf("Password field not set correctly")
	}
	if cfg.PasswordFile != "/path/to/file" {
		t.Errorf("PasswordFile field not set correctly")
	}
	if cfg.From != "from@test.com" {
		t.Errorf("From field not set correctly")
	}
	if !cfg.UseTLS {
		t.Errorf("UseTLS field not set correctly")
	}
}
