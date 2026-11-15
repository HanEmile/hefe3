package store

import (
	"database/sql"
	"path/filepath"
	"testing"

	"git.emile.space/r2wars-web/internal/model"
)

// TestOpenMigratesPreEmailDatabase guards the real upgrade path: production
// databases created before magic-link login existed have a `users` table
// with no `email` column at all (and a now-unused `passwordHash` one).
// Open() must add the column and keep existing rows intact rather than
// erroring or silently losing data.
func TestOpenMigratesPreEmailDatabase(t *testing.T) {
	path := filepath.Join(t.TempDir(), "legacy.db")

	raw, err := sql.Open("sqlite3", path)
	if err != nil {
		t.Fatalf("sql.Open() error = %v", err)
	}
	_, err = raw.Exec(`
		CREATE TABLE users (
			id INTEGER NOT NULL PRIMARY KEY,
			created_at DATETIME NOT NULL,
			name TEXT,
			passwordHash TEXT,
			UNIQUE(name)
		);
		INSERT INTO users (id, created_at, name, passwordHash) VALUES (1, '2024-01-01', 'legacyuser', 'somehash');
	`)
	if err != nil {
		t.Fatalf("seeding legacy schema: %v", err)
	}
	if err := raw.Close(); err != nil {
		t.Fatalf("closing seed connection: %v", err)
	}

	st, err := Open(path)
	if err != nil {
		t.Fatalf("Open() on legacy db error = %v", err)
	}
	defer st.Close()

	user, err := st.GetUserByUsername("legacyuser")
	if err != nil {
		t.Fatalf("GetUserByUsername() after migration error = %v", err)
	}
	if user.ID != 1 {
		t.Fatalf("migrated user.ID = %d, want 1", user.ID)
	}
	if user.Email != "" {
		t.Fatalf("migrated user.Email = %q, want empty (not yet set)", user.Email)
	}

	// The migrated column must be usable going forward, including its
	// uniqueness guarantee.
	if err := st.UpdateUserEmail(user.ID, "legacy@example.com"); err != nil {
		t.Fatalf("UpdateUserEmail() on migrated row error = %v", err)
	}
	got, err := st.GetUserByEmail("legacy@example.com")
	if err != nil {
		t.Fatalf("GetUserByEmail() after setting migrated email error = %v", err)
	}
	if got.ID != 1 {
		t.Fatalf("GetUserByEmail().ID = %d, want 1", got.ID)
	}

	// Regression test: a migrated table has one more column (the unused
	// passwordHash) than a freshly-created one, which broke a positional
	// `INSERT INTO users VALUES (NULL, ?, ?, ?)` (4 values into a 5-column
	// table) the moment someone tried to register against a real,
	// previously-deployed database.
	newID, err := st.InsertUser(model.User{Name: "newuser", Email: "newuser@example.com"})
	if err != nil {
		t.Fatalf("InsertUser() against migrated schema error = %v", err)
	}
	newUser, err := st.GetUserByID(newID)
	if err != nil {
		t.Fatalf("GetUserByID() for newly-inserted user error = %v", err)
	}
	if newUser.Name != "newuser" || newUser.Email != "newuser@example.com" {
		t.Fatalf("newly-inserted user = %+v, want name=newuser email=newuser@example.com", newUser)
	}
}

// TestOpenMigratesPreColumnsBattlesTable is a direct regression test for a
// production incident: the real, previously-deployed battles table had only
// (id, created_at, name, public) - raw_output/max_rounds/arena_size were
// added to the Go schema later, but CREATE TABLE IF NOT EXISTS never adds
// columns to an existing table, so GetBattleByIDDeep failed outright with
// "no such column: raw_output" against real data.
func TestOpenMigratesPreColumnsBattlesTable(t *testing.T) {
	path := filepath.Join(t.TempDir(), "legacy-battles.db")

	raw, err := sql.Open("sqlite3", path)
	if err != nil {
		t.Fatalf("sql.Open() error = %v", err)
	}
	_, err = raw.Exec(`
		CREATE TABLE battles (
			id INTEGER NOT NULL PRIMARY KEY,
			created_at DATETIME NOT NULL,
			name TEXT,
			public BOOLEAN
		);
		CREATE TABLE bots (
			id INTEGER NOT NULL PRIMARY KEY,
			created_at DATETIME NOT NULL,
			name TEXT,
			source TEXT
		);
		CREATE TABLE bot_battle_rel (
			bot_id INTEGER,
			battle_id INTEGER,
			PRIMARY KEY(bot_id, battle_id)
		);
		INSERT INTO battles (id, created_at, name, public) VALUES (1, '2024-01-01', 'testbattle 1', 0);
		INSERT INTO bots (id, created_at, name, source) VALUES (4, '2024-01-01', 'somebot', 'nop');
		INSERT INTO bot_battle_rel (bot_id, battle_id) VALUES (4, 1);
	`)
	if err != nil {
		t.Fatalf("seeding legacy battles schema: %v", err)
	}
	if err := raw.Close(); err != nil {
		t.Fatalf("closing seed connection: %v", err)
	}

	st, err := Open(path)
	if err != nil {
		t.Fatalf("Open() on legacy battles db error = %v", err)
	}
	defer st.Close()

	battle, err := st.GetBattleByIDDeep(1)
	if err != nil {
		t.Fatalf("GetBattleByIDDeep() against pre-columns battles table error = %v", err)
	}
	if battle.Name != "testbattle 1" {
		t.Fatalf("battle.Name = %q, want %q", battle.Name, "testbattle 1")
	}
	if battle.ArenaSize != 4096 {
		t.Fatalf("battle.ArenaSize = %d, want the COALESCE default 4096", battle.ArenaSize)
	}
	if battle.MaxRounds != 100 {
		t.Fatalf("battle.MaxRounds = %d, want the COALESCE default 100", battle.MaxRounds)
	}
	if len(battle.Bots) != 1 || battle.Bots[0].ID != 4 {
		t.Fatalf("battle.Bots = %+v, want [{ID: 4}]", battle.Bots)
	}
}
