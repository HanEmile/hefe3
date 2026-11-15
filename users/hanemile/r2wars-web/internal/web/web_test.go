package web

import (
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"testing"
	"time"

	"git.emile.space/r2wars-web/internal/sessionstore"
	"git.emile.space/r2wars-web/internal/store"
)

// fakeMailer records every send instead of talking to a real SMTP server,
// so tests can pull the magic-link token straight out of the "email" body.
type fakeMailer struct {
	mu   sync.Mutex
	sent []sentMail
}

type sentMail struct {
	To, Subject, Body string
}

func (f *fakeMailer) Send(to, subject, body string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.sent = append(f.sent, sentMail{To: to, Subject: subject, Body: body})
	return nil
}

func (f *fakeMailer) last(t *testing.T) sentMail {
	t.Helper()
	f.mu.Lock()
	defer f.mu.Unlock()
	if len(f.sent) == 0 {
		t.Fatal("fakeMailer: no mail was sent")
	}
	return f.sent[len(f.sent)-1]
}

var loginLinkRE = regexp.MustCompile(`token=([\w-]+)`)

// tokenFromMail extracts the raw magic-link token from an email body built
// by sendLoginLink.
func tokenFromMail(t *testing.T, body string) string {
	t.Helper()
	m := loginLinkRE.FindStringSubmatch(body)
	if m == nil {
		t.Fatalf("no login token found in mail body: %q", body)
	}
	return m[1]
}

func newTestServer(t *testing.T) (*Server, *fakeMailer) {
	t.Helper()

	st, err := store.Open(filepath.Join(t.TempDir(), "main.db"))
	if err != nil {
		t.Fatalf("store.Open() error = %v", err)
	}
	t.Cleanup(func() { st.Close() })

	sessions, err := sessionstore.New(filepath.Join(t.TempDir(), "sessions.db"), "sessions", "/", 3600, []byte("0123456789abcdef0123456789abcdef"))
	if err != nil {
		t.Fatalf("sessionstore.New() error = %v", err)
	}
	t.Cleanup(func() { sessions.Close() })

	mail := &fakeMailer{}
	srv, err := New(st, sessions, mail, "../../templates", "test", "http://example.test", 15*time.Minute)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	return srv, mail
}

func TestIndexHandlerAnonymous(t *testing.T) {
	srv, _ := newTestServer(t)
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	rec := httptest.NewRecorder()

	srv.Routes().ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("GET / status = %d, want %d", rec.Code, http.StatusOK)
	}
}

// TestAuthMiddlewareRedirectsAnonymous guards the fixed version of
// authMiddleware: it used to read session.Values["username"] and only
// checked it for nil, which was fine here, but downstream handlers did an
// unchecked `.( string)` that panicked given certain session states. The
// middleware itself should cleanly redirect rather than let anything panic.
func TestAuthMiddlewareRedirectsAnonymous(t *testing.T) {
	srv, _ := newTestServer(t)

	for _, path := range []string{"/bot", "/bot/new", "/user", "/user/1", "/user/1/profile"} {
		req := httptest.NewRequest(http.MethodGet, path, nil)
		rec := httptest.NewRecorder()

		srv.Routes().ServeHTTP(rec, req)

		if rec.Code != http.StatusSeeOther {
			t.Errorf("GET %s status = %d, want %d (redirect to /login)", path, rec.Code, http.StatusSeeOther)
			continue
		}
		if loc := rec.Header().Get("Location"); loc != "/login" {
			t.Errorf("GET %s Location = %q, want /login", path, loc)
		}
	}
}

// TestSessionUsernameMissingOrWrongType is a regression test for the
// original `session.Values["username"].(string)` panics: a session value
// that's absent, nil, or holds an unexpected type must resolve to "not
// logged in", never a panic.
func TestSessionUsernameMissingOrWrongType(t *testing.T) {
	srv, _ := newTestServer(t)

	cases := []struct {
		name string
		set  func(vals map[interface{}]interface{})
	}{
		{"absent", func(map[interface{}]interface{}) {}},
		{"nil", func(v map[interface{}]interface{}) { v["username"] = nil }},
		{"wrong type", func(v map[interface{}]interface{}) { v["username"] = 42 }},
		{"empty string", func(v map[interface{}]interface{}) { v["username"] = "" }},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, "/", nil)
			sess := srv.session(req)
			tc.set(sess.Values)

			if _, ok := srv.sessionUsername(req); ok {
				t.Errorf("sessionUsername() ok = true, want false")
			}
		})
	}
}

// TestRecoverMiddlewareReturns500 ensures a panicking handler results in a
// 500 response instead of taking down the server/connection.
func TestRecoverMiddlewareReturns500(t *testing.T) {
	srv, _ := newTestServer(t)
	panicking := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		panic("boom")
	})

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	rec := httptest.NewRecorder()

	srv.recoverMiddleware(panicking).ServeHTTP(rec, req)

	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusInternalServerError)
	}
}

func postForm(t *testing.T, handler http.Handler, path, form string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, path, strings.NewReader(form))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	return rec
}

// TestRegisterAndLoginFlow exercises the whole passwordless path end to end:
// register sends a link, clicking it (GET /login/verify?token=...) starts a
// session, and that session actually authorizes a protected page.
func TestRegisterAndLoginFlow(t *testing.T) {
	srv, mail := newTestServer(t)
	handler := srv.Routes()

	rec := postForm(t, handler, "/register", "username=alice&email=alice@example.com")
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("POST /register status = %d, want %d, body=%s", rec.Code, http.StatusSeeOther, rec.Body.String())
	}

	sentToRegister := mail.last(t)
	if sentToRegister.To != "alice@example.com" {
		t.Fatalf("registration email To = %q, want alice@example.com", sentToRegister.To)
	}
	regToken := tokenFromMail(t, sentToRegister.Body)

	// registering the same username/email again must fail cleanly, not
	// insert a second row.
	rec = postForm(t, handler, "/register", "username=alice&email=alice@example.com")
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("duplicate POST /register status = %d, want %d", rec.Code, http.StatusSeeOther)
	}

	// Request a fresh login link too (independent of the registration one)
	// to exercise the login endpoint itself.
	rec = postForm(t, handler, "/login", "email=alice@example.com")
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("POST /login status = %d, want %d, body=%s", rec.Code, http.StatusSeeOther, rec.Body.String())
	}
	loginToken := tokenFromMail(t, mail.last(t).Body)

	// The registration link and the login link are different tokens, and
	// both should still work (neither has been clicked yet).
	for _, tok := range []string{regToken, loginToken} {
		req := httptest.NewRequest(http.MethodGet, "/login/verify?token="+tok, nil)
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)
		if rec.Code != http.StatusSeeOther || rec.Header().Get("Location") != "/" {
			t.Fatalf("GET /login/verify?token=%s... = %d %q, want %d /", tok[:8], rec.Code, rec.Header().Get("Location"), http.StatusSeeOther)
		}

		// A used token must not work a second time.
		rec2 := httptest.NewRecorder()
		handler.ServeHTTP(rec2, req)
		if rec2.Header().Get("Location") == "/" {
			t.Fatalf("GET /login/verify?token=%s... succeeded twice, want single-use", tok[:8])
		}

		cookies := rec.Result().Cookies()
		if len(cookies) == 0 {
			t.Fatal("GET /login/verify did not set a session cookie")
		}

		// The session from verifying must actually authorize a protected page.
		authedReq := httptest.NewRequest(http.MethodGet, "/bot", nil)
		for _, c := range cookies {
			authedReq.AddCookie(c)
		}
		authedRec := httptest.NewRecorder()
		handler.ServeHTTP(authedRec, authedReq)
		if authedRec.Code != http.StatusOK {
			t.Fatalf("GET /bot with verified session = %d, want %d", authedRec.Code, http.StatusOK)
		}
	}
}

func TestLoginVerifyRejectsBadToken(t *testing.T) {
	srv, _ := newTestServer(t)
	handler := srv.Routes()

	req := httptest.NewRequest(http.MethodGet, "/login/verify?token=not-a-real-token", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusSeeOther || rec.Header().Get("Location") == "/" {
		t.Fatalf("GET /login/verify with bad token = %d %q, want redirect away from /", rec.Code, rec.Header().Get("Location"))
	}
}

// TestLoginDoesNotRevealWhetherEmailIsRegistered checks both a known and an
// unknown email get the identical response - a different one would let
// anyone enumerate who has an account.
func TestLoginDoesNotRevealWhetherEmailIsRegistered(t *testing.T) {
	srv, _ := newTestServer(t)
	handler := srv.Routes()
	postForm(t, handler, "/register", "username=alice&email=alice@example.com")

	known := postForm(t, handler, "/login", "email=alice@example.com")
	unknown := postForm(t, handler, "/login", "email=nobody@example.com")

	if known.Header().Get("Location") != unknown.Header().Get("Location") {
		t.Fatalf("response differs for known vs unknown email: %q vs %q",
			known.Header().Get("Location"), unknown.Header().Get("Location"))
	}
}
