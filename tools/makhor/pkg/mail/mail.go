// Package mail provides email sending functionality.
package mail

import (
	"crypto/tls"
	"fmt"
	"net/smtp"
	"strings"

	"makhor/pkg/config"
)

// Mailer handles sending emails.
type Mailer struct {
	cfg config.EmailConfig
}

// New creates a new Mailer from config.
func New(cfg config.EmailConfig) *Mailer {
	return &Mailer{cfg: cfg}
}

// Send sends an email.
func (m *Mailer) Send(to, subject, body string) error {
	from := m.cfg.From
	if from == "" {
		from = m.cfg.User
	}

	msg := fmt.Sprintf("From: %s\r\nTo: %s\r\nSubject: %s\r\nContent-Type: text/plain; charset=UTF-8\r\n\r\n%s",
		from, to, subject, body)

	addr := fmt.Sprintf("%s:%d", m.cfg.Host, m.cfg.Port)

	auth := smtp.PlainAuth("", m.cfg.User, m.cfg.Password, m.cfg.Host)

	if m.cfg.UseTLS {
		return m.sendTLS(addr, auth, from, to, msg)
	}

	return smtp.SendMail(addr, auth, from, []string{to}, []byte(msg))
}

// sendTLS sends email using TLS (STARTTLS).
func (m *Mailer) sendTLS(addr string, auth smtp.Auth, from, to, msg string) error {
	c, err := smtp.Dial(addr)
	if err != nil {
		return fmt.Errorf("dial: %w", err)
	}
	defer c.Close()

	// STARTTLS
	host := strings.Split(addr, ":")[0]
	tlsConfig := &tls.Config{ServerName: host}
	if err := c.StartTLS(tlsConfig); err != nil {
		return fmt.Errorf("starttls: %w", err)
	}

	// Auth
	if err := c.Auth(auth); err != nil {
		return fmt.Errorf("auth: %w", err)
	}

	// Set sender and recipient
	if err := c.Mail(from); err != nil {
		return fmt.Errorf("mail from: %w", err)
	}
	if err := c.Rcpt(to); err != nil {
		return fmt.Errorf("rcpt to: %w", err)
	}

	// Send message
	w, err := c.Data()
	if err != nil {
		return fmt.Errorf("data: %w", err)
	}
	_, err = w.Write([]byte(msg))
	if err != nil {
		return fmt.Errorf("write: %w", err)
	}
	if err := w.Close(); err != nil {
		return fmt.Errorf("close: %w", err)
	}

	return c.Quit()
}

// SendFunc returns a function compatible with Handler.MailFunc.
func (m *Mailer) SendFunc() func(to, subject, body string) error {
	return m.Send
}
