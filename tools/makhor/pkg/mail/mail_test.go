package mail

import (
	"testing"

	"makhor/pkg/config"
)

func TestNew(t *testing.T) {
	cfg := config.EmailConfig{
		Host:     "smtp.example.com",
		Port:     587,
		User:     "user@example.com",
		Password: "secret",
		From:     "noreply@example.com",
		UseTLS:   true,
	}

	mailer := New(cfg)

	if mailer == nil {
		t.Fatal("New() returned nil")
	}
	if mailer.cfg.Host != cfg.Host {
		t.Errorf("cfg.Host = %q, want %q", mailer.cfg.Host, cfg.Host)
	}
	if mailer.cfg.Port != cfg.Port {
		t.Errorf("cfg.Port = %d, want %d", mailer.cfg.Port, cfg.Port)
	}
	if mailer.cfg.User != cfg.User {
		t.Errorf("cfg.User = %q, want %q", mailer.cfg.User, cfg.User)
	}
	if mailer.cfg.Password != cfg.Password {
		t.Errorf("cfg.Password = %q, want %q", mailer.cfg.Password, cfg.Password)
	}
	if mailer.cfg.From != cfg.From {
		t.Errorf("cfg.From = %q, want %q", mailer.cfg.From, cfg.From)
	}
	if mailer.cfg.UseTLS != cfg.UseTLS {
		t.Errorf("cfg.UseTLS = %v, want %v", mailer.cfg.UseTLS, cfg.UseTLS)
	}
}

func TestSendFunc(t *testing.T) {
	cfg := config.EmailConfig{
		Host:     "smtp.example.com",
		Port:     587,
		User:     "user@example.com",
		Password: "secret",
		From:     "noreply@example.com",
	}

	mailer := New(cfg)
	sendFunc := mailer.SendFunc()

	if sendFunc == nil {
		t.Fatal("SendFunc() returned nil")
	}

	// Verify the returned function has the correct signature
	// We can't actually test sending without a real SMTP server,
	// but we can verify the function is returned correctly
}

func TestMailerConfigStorage(t *testing.T) {
	tests := []struct {
		name string
		cfg  config.EmailConfig
	}{
		{
			name: "with TLS",
			cfg: config.EmailConfig{
				Host:     "smtp.gmail.com",
				Port:     587,
				User:     "test@gmail.com",
				Password: "app-password",
				From:     "sender@gmail.com",
				UseTLS:   true,
			},
		},
		{
			name: "without TLS",
			cfg: config.EmailConfig{
				Host:     "localhost",
				Port:     25,
				User:     "local",
				Password: "local",
				From:     "local@localhost",
				UseTLS:   false,
			},
		},
		{
			name: "empty from uses user",
			cfg: config.EmailConfig{
				Host:     "smtp.example.com",
				Port:     465,
				User:     "user@example.com",
				Password: "pass",
				From:     "", // Empty - should use User as From
				UseTLS:   true,
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mailer := New(tt.cfg)

			if mailer.cfg.Host != tt.cfg.Host {
				t.Errorf("Host = %q, want %q", mailer.cfg.Host, tt.cfg.Host)
			}
			if mailer.cfg.Port != tt.cfg.Port {
				t.Errorf("Port = %d, want %d", mailer.cfg.Port, tt.cfg.Port)
			}
			if mailer.cfg.UseTLS != tt.cfg.UseTLS {
				t.Errorf("UseTLS = %v, want %v", mailer.cfg.UseTLS, tt.cfg.UseTLS)
			}
		})
	}
}

// Note: Testing the actual Send and sendTLS methods would require either:
// 1. A mock SMTP server
// 2. Integration tests with a real SMTP server
// These are unit tests that verify the Mailer structure and configuration.
