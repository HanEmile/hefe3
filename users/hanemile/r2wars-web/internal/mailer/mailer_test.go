package mailer

import (
	"bufio"
	"net"
	"strconv"
	"strings"
	"testing"
	"time"
)

// fakeSMTPServer is a minimal plaintext SMTP server - just enough protocol
// to drive net/smtp.SendMail's real client code end to end (EHLO, AUTH
// PLAIN, MAIL FROM, RCPT TO, DATA) without a real mail server. It
// deliberately doesn't advertise STARTTLS: net/smtp.PlainAuth only allows
// auth without TLS when the server name is "localhost", which is exactly
// how this test connects, so this exercises the real auth path too.
type fakeSMTPServer struct {
	ln       net.Listener
	received chan string
}

func startFakeSMTPServer(t *testing.T) (addr string, received <-chan string) {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("net.Listen() error = %v", err)
	}
	s := &fakeSMTPServer{ln: ln, received: make(chan string, 1)}
	t.Cleanup(func() { ln.Close() })

	go s.serveOne(t)

	return ln.Addr().String(), s.received
}

func (s *fakeSMTPServer) serveOne(t *testing.T) {
	conn, err := s.ln.Accept()
	if err != nil {
		return // listener closed during cleanup
	}
	defer conn.Close()
	conn.SetDeadline(time.Now().Add(5 * time.Second))

	r := bufio.NewReader(conn)
	send := func(line string) {
		if _, err := conn.Write([]byte(line + "\r\n")); err != nil {
			t.Errorf("fakeSMTPServer: write error: %v", err)
		}
	}

	send("220 localhost fake SMTP")

	var inData bool
	var dataLines []string

	for {
		line, err := r.ReadString('\n')
		if err != nil {
			return
		}
		line = strings.TrimRight(line, "\r\n")

		if inData {
			if line == "." {
				s.received <- strings.Join(dataLines, "\n")
				send("250 OK: queued")
				inData = false
				continue
			}
			dataLines = append(dataLines, line)
			continue
		}

		upper := strings.ToUpper(line)
		switch {
		case strings.HasPrefix(upper, "EHLO"):
			send("250-localhost")
			send("250 AUTH PLAIN")
		case strings.HasPrefix(upper, "AUTH PLAIN"):
			send("235 Authentication successful")
		case strings.HasPrefix(upper, "MAIL FROM"):
			send("250 OK")
		case strings.HasPrefix(upper, "RCPT TO"):
			send("250 OK")
		case upper == "DATA":
			send("354 Start mail input")
			inData = true
		case upper == "QUIT":
			send("221 Bye")
			return
		default:
			send("500 unrecognized command")
		}
	}
}

func TestSMTPSendDeliversMessage(t *testing.T) {
	addr, received := startFakeSMTPServer(t)
	host, portStr, err := net.SplitHostPort(addr)
	if err != nil {
		t.Fatalf("SplitHostPort(%q) error = %v", addr, err)
	}
	port, err := strconv.Atoi(portStr)
	if err != nil {
		t.Fatalf("Atoi(%q) error = %v", portStr, err)
	}

	m := SMTP{
		Host:     host,
		Port:     port,
		Username: "bot@example.com",
		Password: "hunter2",
		From:     "bot@example.com",
	}

	if err := m.Send("alice@example.com", "Your login link", "click here: http://example.test/login/verify?token=abc"); err != nil {
		t.Fatalf("Send() error = %v", err)
	}

	select {
	case msg := <-received:
		if !strings.Contains(msg, "click here: http://example.test/login/verify?token=abc") {
			t.Fatalf("server did not receive the expected body, got: %q", msg)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("server never received a DATA payload")
	}
}
