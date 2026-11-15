package store

import (
	"path/filepath"
	"testing"

	"git.emile.space/r2wars-web/internal/model"
)

// newTestStore opens a fresh on-disk SQLite database in t.TempDir(). A real
// file (rather than ":memory:") exercises the same WAL/pragma path
// production uses, which is exactly the part most likely to regress.
func newTestStore(t *testing.T) *Store {
	t.Helper()
	path := filepath.Join(t.TempDir(), "test.db")
	st, err := Open(path)
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	t.Cleanup(func() { st.Close() })
	return st
}

func mustCreateUser(t *testing.T, st *Store, name string) model.User {
	t.Helper()
	email := name + "@example.com"
	id, err := st.InsertUser(model.User{Name: name, Email: email})
	if err != nil {
		t.Fatalf("InsertUser(%q) error = %v", name, err)
	}
	return model.User{ID: id, Name: name, Email: email}
}

func TestOpenIsIdempotent(t *testing.T) {
	path := filepath.Join(t.TempDir(), "test.db")

	st1, err := Open(path)
	if err != nil {
		t.Fatalf("first Open() error = %v", err)
	}
	if _, err := st1.InsertUser(model.User{Name: "alice", Email: "alice@example.com"}); err != nil {
		t.Fatalf("InsertUser() error = %v", err)
	}
	if err := st1.Close(); err != nil {
		t.Fatalf("Close() error = %v", err)
	}

	// Re-opening (as happens on every process restart) must not error and
	// must preserve existing data - this is the whole "comes back up on its
	// own" story for the schema.
	st2, err := Open(path)
	if err != nil {
		t.Fatalf("second Open() error = %v", err)
	}
	defer st2.Close()

	users, err := st2.GetAllUsers()
	if err != nil {
		t.Fatalf("GetAllUsers() error = %v", err)
	}
	if len(users) != 1 || users[0].Name != "alice" {
		t.Fatalf("GetAllUsers() = %+v, want [alice]", users)
	}
}

func TestBackupRestoresData(t *testing.T) {
	st := newTestStore(t)
	mustCreateUser(t, st, "alice")

	backupPath := filepath.Join(t.TempDir(), "backup.db")
	if err := st.Backup(backupPath); err != nil {
		t.Fatalf("Backup() error = %v", err)
	}

	restored, err := Open(backupPath)
	if err != nil {
		t.Fatalf("opening backup: %v", err)
	}
	defer restored.Close()

	users, err := restored.GetAllUsers()
	if err != nil {
		t.Fatalf("GetAllUsers() on restored backup: %v", err)
	}
	if len(users) != 1 || users[0].Name != "alice" {
		t.Fatalf("restored users = %+v, want [alice]", users)
	}
}
