package store

import (
	"errors"

	"modernc.org/sqlite"
	sqlite3 "modernc.org/sqlite/lib"
)

// sqliteConstraintError reports whether err is a SQLite UNIQUE/constraint
// violation, as opposed to some other kind of failure (connection, syntax,
// I/O, ...).
func sqliteConstraintError(err error) bool {
	var sqliteErr *sqlite.Error
	if !errors.As(err, &sqliteErr) {
		return false
	}
	code := sqliteErr.Code()
	return code == sqlite3.SQLITE_CONSTRAINT || code&0xff == sqlite3.SQLITE_CONSTRAINT
}
