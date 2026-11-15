package store

import (
	"database/sql"
	"database/sql/driver"

	"modernc.org/sqlite"
)

// sqliteDriver wraps modernc.org/sqlite so every new connection gets a
// consistent set of pragmas applied, instead of relying on callers to
// remember to set them.
type sqliteDriver struct {
	*sqlite.Driver
}

type sqliteConnExecer interface {
	Exec(string, []driver.Value) (driver.Result, error)
}

// connPragmas are applied to every new connection:
//   - foreign_keys: SQLite ignores FK constraints unless this is set per-connection.
//   - journal_mode=WAL: lets readers proceed while a write is in flight, instead
//     of every access blocking on a single file lock.
//   - synchronous=NORMAL: safe pairing with WAL, avoids fsync-per-commit cost.
//   - busy_timeout: makes a connection retry for a while instead of immediately
//     failing with "database is locked" under concurrent access.
var connPragmas = []string{
	"PRAGMA foreign_keys = ON;",
	"PRAGMA journal_mode = WAL;",
	"PRAGMA synchronous = NORMAL;",
	"PRAGMA busy_timeout = 5000;",
}

func (d sqliteDriver) Open(name string) (conn driver.Conn, err error) {
	conn, err = d.Driver.Open(name)
	if err != nil {
		return nil, err
	}

	execer, ok := conn.(sqliteConnExecer)
	if !ok {
		return conn, nil
	}

	for _, pragma := range connPragmas {
		if _, err = execer.Exec(pragma, nil); err != nil {
			_ = conn.Close()
			return nil, err
		}
	}

	return conn, nil
}

func init() {
	sql.Register("sqlite3", sqliteDriver{Driver: &sqlite.Driver{}})
}
