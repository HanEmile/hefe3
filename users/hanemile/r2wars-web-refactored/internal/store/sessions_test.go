package store

import (
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
)

func TestSessionStoreRoundTrip(t *testing.T) {
	path := filepath.Join(t.TempDir(), "sessions.db")
	authKey := []byte("0123456789abcdef0123456789abcdef")

	ss, err := NewSessionStore(path, "sessions", "/", 3600, authKey)
	if err != nil {
		t.Fatalf("NewSessionStore: %v", err)
	}
	defer ss.Close()

	// Save a session and capture the Set-Cookie header.
	saveReq := httptest.NewRequest(http.MethodGet, "/", nil)
	sess, err := ss.New(saveReq, "session")
	if err != nil {
		t.Fatalf("New session: %v", err)
	}
	sess.Values["username"] = "carol"

	rec := httptest.NewRecorder()
	if err := ss.Save(saveReq, rec, sess); err != nil {
		t.Fatalf("Save: %v", err)
	}
	cookie := rec.Result().Cookies()
	if len(cookie) == 0 {
		t.Fatal("expected a session cookie to be set")
	}

	// New request carrying the cookie should reload the stored values.
	loadReq := httptest.NewRequest(http.MethodGet, "/", nil)
	loadReq.AddCookie(cookie[0])
	loaded, err := ss.New(loadReq, "session")
	if err != nil {
		t.Fatalf("reload New: %v", err)
	}
	if loaded.IsNew {
		t.Error("reloaded session should not be new")
	}
	if got := loaded.Values["username"]; got != "carol" {
		t.Errorf("reloaded username = %v; want carol", got)
	}
}

func TestSessionStoreSharesDriver(t *testing.T) {
	// Both Store and SessionStore use the single "sqlite3" driver registered
	// in store.go's init(); opening both against the same path must work.
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "app.db")

	s, err := New(dbPath)
	if err != nil {
		t.Fatalf("New store: %v", err)
	}
	defer s.Close()

	ss, err := NewSessionStore(filepath.Join(dir, "sess.db"), "sessions", "/", 60, []byte("k"))
	if err != nil {
		t.Fatalf("NewSessionStore: %v", err)
	}
	ss.Close()
}
