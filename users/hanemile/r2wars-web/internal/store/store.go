// Package store is the persistence layer: schema management, backups and
// all SQL for users, bots, battles, archs and bits.
package store

import (
	"database/sql"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

// ErrNotFound is returned by Get* methods when no row matches.
var ErrNotFound = errors.New("store: not found")

const schema = `
CREATE TABLE IF NOT EXISTS users (
	id INTEGER NOT NULL PRIMARY KEY,
	created_at DATETIME NOT NULL,
	name TEXT,
	email TEXT,
	UNIQUE(name)
);
CREATE TABLE IF NOT EXISTS bots (
	id INTEGER NOT NULL PRIMARY KEY,
	created_at DATETIME NOT NULL,
	name TEXT,
	source TEXT
);
CREATE TABLE IF NOT EXISTS battles (
	id INTEGER NOT NULL PRIMARY KEY,
	created_at DATETIME NOT NULL,
	name TEXT,
	public BOOLEAN,
	raw_output TEXT,
	max_rounds INTEGER,
	arena_size INTEGER
);
CREATE TABLE IF NOT EXISTS archs (
	id INTEGER NOT NULL PRIMARY KEY,
	name TEXT,
    UNIQUE(name)
);
INSERT OR IGNORE INTO archs (name) VALUES
	("null"), ("6502"), ("6502.cs"), ("8051"), ("alpha"), ("amd29k"),
	("any.as"), ("any.vasm"), ("arm.nz"), ("arm"), ("avr"), ("bf"), ("bpf.mr"),
	("bpf"), ("chip8"), ("cr16"), ("cris"), ("dalvik"), ("dis"), ("ebc"),
	("evm"), ("fslsp"), ("gb"), ("h8300"), ("i4004"), ("i8080"), ("java"),
	("jdh8"), ("kvx"), ("lh5801"), ("lm32"), ("m680x"), ("m68k"), ("mcore"),
	("mcs96"), ("mips"), ("msp430"), ("nios2"), ("or1k"), ("pic"), ("ppc"),
	("propeller"), ("pyc"), ("riscv"), ("riscv.cs"), ("rsp"), ("s390"),
	("sh"), ("sh.cs"), ("snes"), ("sparc"), ("tms320"), ("tricore"),
	("tricore.cs"), ("v850"), ("vax"), ("wasm"), ("ws"), ("x86"), ("x86.nz"),
	("xap"), ("xcore"), ("arm.gnu"), ("lanai"), ("loongarch"), ("m68k.gnu"),
	("mips.gnu"), ("nds32"), ("pdp11"), ("ppc.gnu"), ("s390.gnu"),
	("sparc.gnu"), ("xtensa"), ("z80")
;
CREATE TABLE IF NOT EXISTS bits (
	id INTEGER NOT NULL PRIMARY KEY,
	name TEXT,
    UNIQUE(name)
);
INSERT OR IGNORE INTO bits (name) VALUES
	("8"), ("16"), ("32"), ("64")
;

CREATE TABLE IF NOT EXISTS user_bot_rel (
	user_id INTEGER,
	bot_id INTEGER,
	PRIMARY KEY(user_id, bot_id)
);
CREATE TABLE IF NOT EXISTS arch_bot_rel (
	arch_id INTEGER,
	bot_id INTEGER,
	PRIMARY KEY(arch_id, bot_id)
);
CREATE TABLE IF NOT EXISTS bit_bot_rel (
	bit_id INTEGER,
	bot_id INTEGER,
	PRIMARY KEY(bit_id, bot_id)
);

CREATE TABLE IF NOT EXISTS user_battle_rel (
	user_id INTEGER,
	battle_id INTEGER,
	PRIMARY KEY(user_id, battle_id)
);
CREATE TABLE IF NOT EXISTS owner_battle_rel (
	user_id INTEGER,
	battle_id INTEGER,
	PRIMARY KEY(user_id, battle_id)
);
CREATE TABLE IF NOT EXISTS bot_battle_rel (
	bot_id INTEGER,
	battle_id INTEGER,
	PRIMARY KEY(bot_id, battle_id)
);
CREATE TABLE IF NOT EXISTS arch_battle_rel (
	arch_id INTEGER,
	battle_id INTEGER,
	PRIMARY KEY(arch_id, battle_id)
);
CREATE TABLE IF NOT EXISTS bit_battle_rel (
	bit_id INTEGER,
	battle_id INTEGER,
	PRIMARY KEY(bit_id, battle_id)
);

-- Magic-link login tokens. token_hash is sha256(raw token); the raw token
-- only ever exists in the emailed URL, never at rest. Single-use: a
-- successful verify deletes the row (see ConsumeMagicLink).
CREATE TABLE IF NOT EXISTS magic_links (
	token_hash TEXT NOT NULL PRIMARY KEY,
	user_id INTEGER NOT NULL,
	created_at DATETIME NOT NULL,
	expires_at DATETIME NOT NULL
);
`

// addColumnIfMissing adds column to table for databases created before that
// column existed. CREATE TABLE IF NOT EXISTS above is a no-op against an
// already-existing table, so this is the only thing that actually migrates
// it. Safe to run every startup: SQLite has no "ADD COLUMN IF NOT EXISTS",
// so a second run's "duplicate column name" error is expected and ignored.
//
// This exists because real deployed databases have, more than once, turned
// out to predate a column the current code assumes is always there (users
// shipped without email; battles shipped without raw_output/max_rounds/
// arena_size) - CREATE TABLE IF NOT EXISTS silently does nothing about that
// gap, so every column added after a table's first release needs an entry
// here or the app breaks against real data the moment it touches that column.
func addColumnIfMissing(db *sql.DB, table, column, sqlType string) error {
	_, err := db.Exec(fmt.Sprintf("ALTER TABLE %s ADD COLUMN %s %s", table, column, sqlType))
	if err != nil && !strings.Contains(err.Error(), "duplicate column name") {
		return fmt.Errorf("store: adding %s.%s: %w", table, column, err)
	}
	return nil
}

// migrateSchema brings a database created by an older version of this schema
// up to date. See addColumnIfMissing's doc comment for why this is needed
// despite the CREATE TABLE IF NOT EXISTS statements above.
func migrateSchema(db *sql.DB) error {
	for _, m := range []struct{ table, column, sqlType string }{
		{"users", "email", "TEXT"},
		{"battles", "raw_output", "TEXT"},
		{"battles", "max_rounds", "INTEGER"},
		{"battles", "arena_size", "INTEGER"},
	} {
		if err := addColumnIfMissing(db, m.table, m.column, m.sqlType); err != nil {
			return err
		}
	}
	if _, err := db.Exec("CREATE UNIQUE INDEX IF NOT EXISTS idx_users_email ON users(email)"); err != nil {
		return fmt.Errorf("store: indexing users.email: %w", err)
	}
	return nil
}

// Store is the persistence layer, backed by a single SQLite database.
type Store struct {
	db *sql.DB
}

// Open opens (and, if needed, creates) the database at path and ensures the
// schema exists. The schema is pure `CREATE TABLE IF NOT EXISTS` /
// `INSERT OR IGNORE`, so this is safe to call every startup even against an
// existing, populated database - that's the whole recovery story if the
// file is ever deleted or replaced with an empty one.
func Open(path string) (*Store, error) {
	db, err := sql.Open("sqlite3", path)
	if err != nil {
		return nil, fmt.Errorf("store: opening %q: %w", path, err)
	}

	// SQLite allows exactly one writer at a time; WAL mode (set per-connection
	// in driver.go) lets reads continue concurrently with that writer, so a
	// small pool is safe and avoids serializing every single request through
	// one connection.
	db.SetMaxOpenConns(8)

	if _, err := db.Exec(schema); err != nil {
		db.Close()
		return nil, fmt.Errorf("store: creating schema: %w", err)
	}
	if err := migrateSchema(db); err != nil {
		db.Close()
		return nil, err
	}

	return &Store{db: db}, nil
}

// Close closes the underlying database connection pool.
func (s *Store) Close() error {
	return s.db.Close()
}

// Backup writes a consistent point-in-time snapshot of the database to
// destPath. It uses SQLite's VACUUM INTO, which is safe to run against a
// live database without blocking other readers/writers for long.
func (s *Store) Backup(destPath string) error {
	if err := os.MkdirAll(filepath.Dir(destPath), 0o755); err != nil {
		return fmt.Errorf("store: creating backup dir: %w", err)
	}
	// Remove a leftover file first: VACUUM INTO refuses to overwrite an
	// existing file.
	_ = os.Remove(destPath)
	if _, err := s.db.Exec("VACUUM INTO ?", destPath); err != nil {
		return fmt.Errorf("store: backing up to %q: %w", destPath, err)
	}
	return nil
}

// withTx runs fn inside a transaction, committing on success and rolling
// back on any error (including a panic, which is re-raised after rollback).
func (s *Store) withTx(fn func(*sql.Tx) error) (err error) {
	tx, err := s.db.Begin()
	if err != nil {
		return fmt.Errorf("store: beginning transaction: %w", err)
	}
	defer func() {
		if p := recover(); p != nil {
			_ = tx.Rollback()
			panic(p)
		}
	}()

	if err := fn(tx); err != nil {
		_ = tx.Rollback()
		return err
	}
	return tx.Commit()
}

// linkMany replaces the full set of hand-built, string-concatenated INSERTs
// that used to live next to every "link N ids to an owning row" query with a
// single parameterized multi-row INSERT.
func linkMany(tx *sql.Tx, table, col, ownerCol string, ownerID int, ids []int) error {
	if len(ids) == 0 {
		return nil
	}

	placeholders := make([]string, len(ids))
	args := make([]interface{}, 0, len(ids)*2)
	for i, id := range ids {
		placeholders[i] = "(?, ?)"
		args = append(args, id, ownerID)
	}

	query := fmt.Sprintf(
		"INSERT INTO %s (%s, %s) VALUES %s",
		table, col, ownerCol, strings.Join(placeholders, ", "),
	)
	_, err := tx.Exec(query, args...)
	return err
}

// splitIDList parses a SQLite group_concat()-produced CSV list of integers.
// An empty string (nothing concatenated, i.e. no related rows) returns a nil
// slice rather than an error.
func splitIDList(csv string) ([]int, error) {
	if csv == "" {
		return nil, nil
	}
	parts := strings.Split(csv, ",")
	ids := make([]int, len(parts))
	for i, p := range parts {
		id, err := strconv.Atoi(p)
		if err != nil {
			return nil, fmt.Errorf("store: parsing id list %q: %w", csv, err)
		}
		ids[i] = id
	}
	return ids, nil
}

func splitNameList(csv string) []string {
	if csv == "" {
		return nil
	}
	return strings.Split(csv, ",")
}
