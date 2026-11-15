// Package store is the data-access layer for r2wars-web. It wraps an SQLite
// database and exposes typed methods returning model types. It owns the schema
// and the registration of the custom "sqlite3" driver (which enables foreign
// key enforcement on every connection).
package store

import (
	"database/sql"
	"database/sql/driver"
	"fmt"
	"strings"

	"git.emile.space/r2wars-web/internal/model"
	"modernc.org/sqlite"
)

// sqlite3Driver wraps modernc's sqlite driver so we can run a PRAGMA on every
// new connection. Without this, foreign-key enforcement would be off by
// default (SQLite's historical default).
type sqlite3Driver struct {
	*sqlite.Driver
}

type sqlite3DriverConn interface {
	Exec(string, []driver.Value) (driver.Result, error)
}

func (d sqlite3Driver) Open(name string) (conn driver.Conn, err error) {
	conn, err = d.Driver.Open(name)
	if err != nil {
		return
	}
	_, err = conn.(sqlite3DriverConn).Exec("PRAGMA foreign_keys = ON;", nil)
	if err != nil {
		_ = conn.Close()
	}
	return
}

// init registers the custom driver exactly once for the whole package. Both
// Store and SessionStore open connections via the "sqlite3" driver name.
func init() {
	sql.Register("sqlite3", sqlite3Driver{Driver: &sqlite.Driver{}})
}

// Store is the handle to the application database.
type Store struct {
	db *sql.DB
}

// New opens (creating if necessary) the database at dbPath, applies the schema
// and seeds the archs/bits reference tables, then tunes the connection for
// concurrent use via WAL mode and a busy timeout.
func New(dbPath string) (*Store, error) {
	db, err := sql.Open("sqlite3", dbPath)
	if err != nil {
		return nil, fmt.Errorf("opening db: %w", err)
	}

	// WAL gives us concurrent readers alongside a writer; the busy timeout
	// makes writers wait briefly instead of failing immediately on lock
	// contention.
	if _, err := db.Exec("PRAGMA journal_mode=WAL;"); err != nil {
		db.Close()
		return nil, fmt.Errorf("enabling WAL: %w", err)
	}
	if _, err := db.Exec("PRAGMA busy_timeout=5000;"); err != nil {
		db.Close()
		return nil, fmt.Errorf("setting busy_timeout: %w", err)
	}

	if _, err := db.Exec(schema); err != nil {
		db.Close()
		return nil, fmt.Errorf("applying schema: %w", err)
	}

	return &Store{db: db}, nil
}

// Close closes the underlying database.
func (s *Store) Close() error {
	return s.db.Close()
}

//////////////////////////////////////////////////////////////////////////////
// USERS

// InsertUser inserts a new user and returns its id.
func (s *Store) InsertUser(name string, passwordHash []byte) (int, error) {
	res, err := s.db.Exec("INSERT INTO users VALUES(NULL,datetime('now'),?,?);", name, passwordHash)
	if err != nil {
		return 0, err
	}
	id, err := res.LastInsertId()
	if err != nil {
		return 0, err
	}
	return int(id), nil
}

// CheckUserPassword returns true if the given name/hash pair matches a row.
func (s *Store) CheckUserPassword(name string, passwordHash []byte) bool {
	var id int
	err := s.db.QueryRow("SELECT id FROM users WHERE name=? AND passwordHash=?", name, passwordHash).Scan(&id)
	return err == nil
}

// UpdateUserPasswordHash sets a new password hash for the user with the given name.
func (s *Store) UpdateUserPasswordHash(name string, passwordHash []byte) error {
	_, err := s.db.Exec("UPDATE users SET passwordHash=? WHERE name=?", passwordHash, name)
	return err
}

// UpdateUsername renames the user with the given id.
func (s *Store) UpdateUsername(id int, newName string) error {
	_, err := s.db.Exec("UPDATE users SET name=? WHERE id=?", newName, id)
	return err
}

// UserByID returns the user with the given id (without password hash).
func (s *Store) UserByID(id int) (model.User, error) {
	var u model.User
	err := s.db.QueryRow("SELECT id, name FROM users WHERE id=?", id).Scan(&u.ID, &u.Name)
	if err != nil {
		return model.User{}, err
	}
	return u, nil
}

// UserByName returns the user with the given name (without password hash).
func (s *Store) UserByName(name string) (model.User, error) {
	var u model.User
	err := s.db.QueryRow("SELECT id, name FROM users WHERE name=?", name).Scan(&u.ID, &u.Name)
	if err != nil {
		return model.User{}, err
	}
	return u, nil
}

// AllUsers returns every user (id+name only).
func (s *Store) AllUsers() ([]model.User, error) {
	rows, err := s.db.Query("SELECT id, name FROM users")
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var users []model.User
	for rows.Next() {
		var u model.User
		if err := rows.Scan(&u.ID, &u.Name); err != nil {
			return users, err
		}
		users = append(users, u)
	}
	return users, rows.Err()
}

// UsernameCount returns how many users carry the given name.
func (s *Store) UsernameCount(name string) (int, error) {
	var count int
	err := s.db.QueryRow("SELECT COUNT(*) FROM users WHERE name=?", name).Scan(&count)
	if err != nil {
		return -1, err
	}
	return count, nil
}

// LinkUserBot links a bot to a user identified by username.
func (s *Store) LinkUserBot(username string, botID int) error {
	_, err := s.db.Exec(`
		INSERT INTO user_bot_rel
		VALUES ((SELECT id FROM users WHERE name=?), ?)`, username, botID)
	return err
}

// BotsForUsername returns the bots owned by the named user (id+name+source).
func (s *Store) BotsForUsername(username string) ([]model.Bot, error) {
	rows, err := s.db.Query(`
		SELECT b.id, b.name, b.source
		FROM bots b
		LEFT JOIN user_bot_rel ub ON ub.bot_id = b.id
		WHERE ub.user_id=(SELECT id FROM users WHERE name=?)`, username)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanBots(rows)
}

// BotsForUserID returns the bots owned by the user with the given id.
func (s *Store) BotsForUserID(userID int) ([]model.Bot, error) {
	rows, err := s.db.Query(`
		SELECT b.id, b.name, b.source
		FROM bots b
		LEFT JOIN user_bot_rel ub ON ub.bot_id = b.id
		WHERE ub.user_id=?`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanBots(rows)
}

// scanBots scans rows of (id, name, source) into a bot slice.
func scanBots(rows *sql.Rows) ([]model.Bot, error) {
	var bots []model.Bot
	for rows.Next() {
		var b model.Bot
		if err := rows.Scan(&b.ID, &b.Name, &b.Source); err != nil {
			return bots, err
		}
		bots = append(bots, b)
	}
	return bots, rows.Err()
}

//////////////////////////////////////////////////////////////////////////////
// BOTS

// InsertBot inserts a new bot and returns its id.
func (s *Store) InsertBot(name, source string) (int, error) {
	res, err := s.db.Exec("INSERT INTO bots VALUES(NULL,datetime('now'),?,?);", name, source)
	if err != nil {
		return 0, err
	}
	id, err := res.LastInsertId()
	if err != nil {
		return 0, err
	}
	return int(id), nil
}

// UpdateBot updates a bot's name and source.
func (s *Store) UpdateBot(id int, name, source string) error {
	_, err := s.db.Exec("UPDATE bots SET name=?, source=? WHERE id=?", name, source, id)
	return err
}

// BotByID returns a bot with its Users, Archs and Bits populated. Unlike the
// original (which abused group_concat + Split), this issues separate, simple
// JOIN queries and scans each relation row-by-row.
func (s *Store) BotByID(id int) (model.Bot, error) {
	var b model.Bot
	err := s.db.QueryRow("SELECT id, name, source FROM bots WHERE id=?", id).
		Scan(&b.ID, &b.Name, &b.Source)
	if err != nil {
		return model.Bot{}, err
	}

	// owners
	b.Users, err = s.botUsers(id)
	if err != nil {
		return model.Bot{}, err
	}

	// archs
	archRows, err := s.db.Query(`
		SELECT a.id, a.name
		FROM archs a
		JOIN arch_bot_rel ab ON ab.arch_id = a.id
		WHERE ab.bot_id=?`, id)
	if err != nil {
		return model.Bot{}, err
	}
	b.Archs, err = scanArchs(archRows)
	archRows.Close()
	if err != nil {
		return model.Bot{}, err
	}

	// bits
	bitRows, err := s.db.Query(`
		SELECT bi.id, bi.name
		FROM bits bi
		JOIN bit_bot_rel bb ON bb.bit_id = bi.id
		WHERE bb.bot_id=?`, id)
	if err != nil {
		return model.Bot{}, err
	}
	b.Bits, err = scanBits(bitRows)
	bitRows.Close()
	if err != nil {
		return model.Bot{}, err
	}

	return b, nil
}

// botUsers returns the owners linked to a bot.
func (s *Store) botUsers(botID int) ([]model.User, error) {
	rows, err := s.db.Query(`
		SELECT u.id, u.name
		FROM users u
		JOIN user_bot_rel ub ON ub.user_id = u.id
		WHERE ub.bot_id=?`, botID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanUsers(rows)
}

// scanUsers scans rows of (id, name) into a user slice.
func scanUsers(rows *sql.Rows) ([]model.User, error) {
	var users []model.User
	for rows.Next() {
		var u model.User
		if err := rows.Scan(&u.ID, &u.Name); err != nil {
			return users, err
		}
		users = append(users, u)
	}
	return users, rows.Err()
}

// scanArchs scans rows of (id, name) into an arch slice (Enabled=true).
func scanArchs(rows *sql.Rows) ([]model.Arch, error) {
	var archs []model.Arch
	for rows.Next() {
		var a model.Arch
		if err := rows.Scan(&a.ID, &a.Name); err != nil {
			return archs, err
		}
		a.Enabled = true
		archs = append(archs, a)
	}
	return archs, rows.Err()
}

// scanBits scans rows of (id, name) into a bit slice (Enabled=true).
func scanBits(rows *sql.Rows) ([]model.Bit, error) {
	var bits []model.Bit
	for rows.Next() {
		var bi model.Bit
		if err := rows.Scan(&bi.ID, &bi.Name); err != nil {
			return bits, err
		}
		bi.Enabled = true
		bits = append(bits, bi)
	}
	return bits, rows.Err()
}

// AllBots returns every bot (id+name only).
func (s *Store) AllBots() ([]model.Bot, error) {
	rows, err := s.db.Query("SELECT id, name FROM bots;")
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var bots []model.Bot
	for rows.Next() {
		var b model.Bot
		if err := rows.Scan(&b.ID, &b.Name); err != nil {
			return bots, err
		}
		bots = append(bots, b)
	}
	return bots, rows.Err()
}

// AllBotsWithUsers returns every bot with its owners populated. It scans the
// joined rows directly into per-bot user slices instead of group_concat+Split.
func (s *Store) AllBotsWithUsers() ([]model.Bot, error) {
	rows, err := s.db.Query(`
		SELECT b.id, b.name, b.source, u.id, u.name
		FROM bots b
		LEFT JOIN user_bot_rel ub ON ub.bot_id = b.id
		LEFT JOIN users u ON u.id = ub.user_id
		ORDER BY b.id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	// Preserve first-seen ordering of bots while accumulating their users.
	var order []int
	byID := map[int]*model.Bot{}
	for rows.Next() {
		var (
			botID           int
			botName, botSrc string
			userID          sql.NullInt64
			userName        sql.NullString
		)
		if err := rows.Scan(&botID, &botName, &botSrc, &userID, &userName); err != nil {
			return nil, err
		}
		b, ok := byID[botID]
		if !ok {
			b = &model.Bot{ID: botID, Name: botName, Source: botSrc}
			byID[botID] = b
			order = append(order, botID)
		}
		if userID.Valid {
			b.Users = append(b.Users, model.User{ID: int(userID.Int64), Name: userName.String})
		}
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	bots := make([]model.Bot, 0, len(order))
	for _, id := range order {
		bots = append(bots, *byID[id])
	}
	return bots, nil
}

// LinkBotArchs replaces a bot's arch links with the given set, in one transaction.
func (s *Store) LinkBotArchs(botID int, archIDs []int) error {
	return s.replaceLinks("arch_bot_rel", "arch_id", "bot_id", botID, archIDs)
}

// LinkBotBits replaces a bot's bit links with the given set, in one transaction.
func (s *Store) LinkBotBits(botID int, bitIDs []int) error {
	return s.replaceLinks("bit_bot_rel", "bit_id", "bot_id", botID, bitIDs)
}

// replaceLinks deletes all rows in `table` for ownerCol=ownerID, then inserts
// (refID, ownerID) pairs for every refID, all within a single transaction.
// Uses parameter binding throughout (no string-concatenated VALUES lists).
func (s *Store) replaceLinks(table, refCol, ownerCol string, ownerID int, refIDs []int) error {
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	if _, err := tx.Exec(fmt.Sprintf("DELETE FROM %s WHERE %s=?", table, ownerCol), ownerID); err != nil {
		return err
	}

	if len(refIDs) > 0 {
		query, args := buildInsertPairs(table, refCol, ownerCol, ownerID, refIDs)
		if _, err := tx.Exec(query, args...); err != nil {
			return err
		}
	}

	return tx.Commit()
}

// buildInsertPairs builds a parametrised multi-row INSERT of (refID, ownerID)
// pairs: "INSERT INTO t (refCol, ownerCol) VALUES (?,?),(?,?)..." plus its args.
// Table/column identifiers are caller-controlled constants (never user input).
func buildInsertPairs(table, refCol, ownerCol string, ownerID int, refIDs []int) (string, []any) {
	placeholders := make([]string, len(refIDs))
	args := make([]any, 0, len(refIDs)*2)
	for i, id := range refIDs {
		placeholders[i] = "(?,?)"
		args = append(args, id, ownerID)
	}
	query := fmt.Sprintf("INSERT INTO %s (%s, %s) VALUES %s",
		table, refCol, ownerCol, strings.Join(placeholders, ","))
	return query, args
}

//////////////////////////////////////////////////////////////////////////////
// ARCHS & BITS

// AllArchs returns every architecture.
func (s *Store) AllArchs() ([]model.Arch, error) {
	rows, err := s.db.Query("SELECT id, name FROM archs")
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var archs []model.Arch
	for rows.Next() {
		var a model.Arch
		if err := rows.Scan(&a.ID, &a.Name); err != nil {
			return archs, err
		}
		archs = append(archs, a)
	}
	return archs, rows.Err()
}

// AllBits returns every word-size option.
func (s *Store) AllBits() ([]model.Bit, error) {
	rows, err := s.db.Query("SELECT id, name FROM bits")
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var bits []model.Bit
	for rows.Next() {
		var bi model.Bit
		if err := rows.Scan(&bi.ID, &bi.Name); err != nil {
			return bits, err
		}
		bits = append(bits, bi)
	}
	return bits, rows.Err()
}
