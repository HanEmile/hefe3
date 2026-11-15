package web

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"git.emile.space/r2wars-web/internal/store"
)

// newTestServer builds a Server backed by temp SQLite databases and the real
// templates, with request logging disabled. It does not require radare2.
func newTestServer(t *testing.T) *Server {
	t.Helper()
	dir := t.TempDir()

	st, err := store.New(dir + "/main.db")
	if err != nil {
		t.Fatalf("store.New: %v", err)
	}
	t.Cleanup(func() { st.Close() })

	sess, err := store.NewSessionStore(dir+"/sessions.db", "sessions", "/", 3600, []byte("0123456789abcdef0123456789abcdef"))
	if err != nil {
		t.Fatalf("NewSessionStore: %v", err)
	}

	srv, err := NewServer(st, sess, "../../templates/*.html", "test-version", "test-salt")
	if err != nil {
		t.Fatalf("NewServer: %v", err)
	}
	return srv
}

// registerAndLogin registers a user and logs in, returning the session cookie.
func registerAndLogin(t *testing.T, router http.Handler, username, password string) *http.Cookie {
	t.Helper()

	// register
	form := url.Values{"username": {username}, "password1": {password}, "password2": {password}}
	req := httptest.NewRequest(http.MethodPost, "/register", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("register: got status %d, body %q", rec.Code, rec.Body.String())
	}

	// login
	loginForm := url.Values{"username": {username}, "password": {password}}
	req = httptest.NewRequest(http.MethodPost, "/login", strings.NewReader(loginForm.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rec = httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("login: got status %d", rec.Code)
	}

	for _, c := range rec.Result().Cookies() {
		if c.Name == "session" {
			return c
		}
	}
	t.Fatal("login did not set a session cookie")
	return nil
}

func TestIndex(t *testing.T) {
	srv := newTestServer(t)
	router := srv.Router()

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("GET /: got status %d", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "r2wa.rs") {
		t.Errorf("GET / body missing expected content; got %q", rec.Body.String())
	}
}

func TestRegisterThenLogin(t *testing.T) {
	srv := newTestServer(t)
	router := srv.Router()

	cookie := registerAndLogin(t, router, "alice", "hunter2")
	if cookie.Value == "" {
		t.Fatal("empty session cookie value")
	}

	// the user must now exist in the store
	if _, err := srv.Store.UserByName("alice"); err != nil {
		t.Fatalf("user alice not found after register: %v", err)
	}
}

func TestUnauthenticatedBotRedirects(t *testing.T) {
	srv := newTestServer(t)
	router := srv.Router()

	req := httptest.NewRequest(http.MethodGet, "/bot", nil)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusSeeOther {
		t.Fatalf("GET /bot unauthenticated: got status %d, want 303", rec.Code)
	}
	if loc := rec.Header().Get("Location"); loc != "/login" {
		t.Errorf("GET /bot redirect target = %q, want /login", loc)
	}
}

func TestBattleListPublic(t *testing.T) {
	srv := newTestServer(t)
	router := srv.Router()

	// /battle is not auth-gated at the router level, but its handler redirects
	// to /login when there is no session.
	req := httptest.NewRequest(http.MethodGet, "/battle", nil)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("GET /battle anonymous: got status %d, want 303", rec.Code)
	}

	// once logged in it returns 200
	cookie := registerAndLogin(t, router, "bob", "hunter2")
	req = httptest.NewRequest(http.MethodGet, "/battle", nil)
	req.AddCookie(cookie)
	rec = httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("GET /battle logged in: got status %d", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "All Battles") {
		t.Errorf("GET /battle body missing expected content")
	}
}

func TestCreateBattle(t *testing.T) {
	srv := newTestServer(t)
	router := srv.Router()

	cookie := registerAndLogin(t, router, "carol", "hunter2")

	form := url.Values{
		"name":       {"my-battle"},
		"arena-size": {"4096"},
		"max-rounds": {"100"},
		"public":     {"on"},
	}
	req := httptest.NewRequest(http.MethodPost, "/battle/new", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.AddCookie(cookie)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusSeeOther {
		t.Fatalf("POST /battle/new: got status %d, body %q", rec.Code, rec.Body.String())
	}
	if loc := rec.Header().Get("Location"); loc != "/battle" {
		t.Errorf("POST /battle/new redirect = %q, want /battle", loc)
	}

	battles, err := srv.Store.AllBattles()
	if err != nil {
		t.Fatalf("AllBattles: %v", err)
	}
	if len(battles) != 1 || battles[0].Name != "my-battle" {
		t.Fatalf("expected one battle named my-battle, got %+v", battles)
	}
}
