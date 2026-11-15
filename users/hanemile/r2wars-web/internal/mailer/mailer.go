// Package mailer sends the plain-text emails magic-link login depends on.
package mailer

import (
	"fmt"
	"log"
	"net/smtp"
	"strings"
)

// Mailer sends an email. The real implementation talks to an SMTP server;
// tests use a fake that just records what would have been sent.
type Mailer interface {
	Send(to, subject, body string) error
}

// SMTP sends mail via a standard SMTP+STARTTLS submission server (port 587),
// authenticating with PLAIN auth. This covers the common case (Postfix,
// Fastmail, Google Workspace, ...) without pulling in a third-party library.
type SMTP struct {
	Host     string
	Port     int
	Username string
	Password string
	From     string
}

func (s SMTP) Send(to, subject, body string) error {
	addr := fmt.Sprintf("%s:%d", s.Host, s.Port)
	auth := smtp.PlainAuth("", s.Username, s.Password, s.Host)

	msg := buildMessage(s.From, to, subject, body)

	if err := smtp.SendMail(addr, auth, s.From, []string{to}, msg); err != nil {
		return fmt.Errorf("mailer: sending to %q via %s: %w", to, addr, err)
	}
	return nil
}

// Console "sends" mail by writing it to the log instead of an SMTP server.
// This is what makes running (or testing) the whole app possible without
// any mail credentials: the magic link that would have been emailed shows
// up in the log line instead, so the login flow is still fully exercisable
// end to end - just by reading stdout instead of an inbox.
type Console struct{}

func (Console) Send(to, subject, body string) error {
	log.Printf("[console-mail] To: %s | Subject: %s\n%s", to, subject, body)
	return nil
}

func buildMessage(from, to, subject, body string) []byte {
	var b strings.Builder
	fmt.Fprintf(&b, "From: %s\r\n", from)
	fmt.Fprintf(&b, "To: %s\r\n", to)
	fmt.Fprintf(&b, "Subject: %s\r\n", subject)
	b.WriteString("Content-Type: text/plain; charset=utf-8\r\n")
	b.WriteString("\r\n")
	b.WriteString(body)
	return []byte(b.String())
}
