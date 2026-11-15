package store

import (
	"database/sql"
	"errors"
	"fmt"
	"time"

	"git.emile.space/r2wars-web/internal/model"
)

// InsertUser creates a user. The underlying UNIQUE constraints on name and
// email are the real guard against duplicate registrations under
// concurrency; callers should check for a constraint violation
// (IsDuplicateUser) rather than racily checking existence beforehand.
func (s *Store) InsertUser(user model.User) (int, error) {
	// Named columns, not positional VALUES(NULL,?,?,?): a database that
	// predates magic-link login still has the (now-unused) passwordHash
	// column alongside the migrated-in email column, so the table has one
	// more column than a fresh install's does. Positional inserts break the
	// moment the column count/order differs from what the code assumes;
	// naming columns makes the insert correct regardless of what other
	// (ignored) columns the table happens to carry.
	res, err := s.db.Exec(
		"INSERT INTO users (created_at, name, email) VALUES (?, ?, ?);",
		time.Now(), user.Name, user.Email,
	)
	if err != nil {
		return 0, fmt.Errorf("store: inserting user: %w", err)
	}
	id, err := res.LastInsertId()
	if err != nil {
		return 0, fmt.Errorf("store: reading new user id: %w", err)
	}
	return int(id), nil
}

// IsDuplicateUser reports whether err was caused by the users.name or
// users.email UNIQUE constraints.
func IsDuplicateUser(err error) bool {
	return err != nil && sqliteConstraintError(err)
}

func (s *Store) UpdateUserName(id int, newName string) error {
	_, err := s.db.Exec("UPDATE users SET name=? WHERE id=?", newName, id)
	if err != nil {
		return fmt.Errorf("store: updating username: %w", err)
	}
	return nil
}

func (s *Store) UpdateUserEmail(id int, email string) error {
	_, err := s.db.Exec("UPDATE users SET email=? WHERE id=?", email, id)
	if err != nil {
		return fmt.Errorf("store: updating email: %w", err)
	}
	return nil
}

// LinkUserBot links botID to the user identified by username.
func (s *Store) LinkUserBot(username string, botID int) error {
	_, err := s.db.Exec(`
		INSERT INTO user_bot_rel
		VALUES ((SELECT id FROM users WHERE name=?), ?)`, username, botID)
	if err != nil {
		return fmt.Errorf("store: linking bot to user: %w", err)
	}
	return nil
}

func (s *Store) GetUserByID(id int) (model.User, error) {
	var u model.User
	var email sql.NullString
	err := s.db.QueryRow("SELECT id, name, email FROM users WHERE id=?", id).Scan(&u.ID, &u.Name, &email)
	if errors.Is(err, sql.ErrNoRows) {
		return model.User{}, ErrNotFound
	}
	if err != nil {
		return model.User{}, fmt.Errorf("store: getting user by id: %w", err)
	}
	u.Email = email.String
	return u, nil
}

func (s *Store) GetUserByUsername(username string) (model.User, error) {
	var u model.User
	var email sql.NullString
	err := s.db.QueryRow("SELECT id, name, email FROM users WHERE name=?", username).Scan(&u.ID, &u.Name, &email)
	if errors.Is(err, sql.ErrNoRows) {
		return model.User{}, ErrNotFound
	}
	if err != nil {
		return model.User{}, fmt.Errorf("store: getting user by username: %w", err)
	}
	u.Email = email.String
	return u, nil
}

// GetUserByEmail looks up a user by their login email. Lookups are
// case-insensitive since email addresses conventionally are.
func (s *Store) GetUserByEmail(email string) (model.User, error) {
	var u model.User
	var gotEmail sql.NullString
	err := s.db.QueryRow(
		"SELECT id, name, email FROM users WHERE email IS NOT NULL AND lower(email)=lower(?)", email,
	).Scan(&u.ID, &u.Name, &gotEmail)
	if errors.Is(err, sql.ErrNoRows) {
		return model.User{}, ErrNotFound
	}
	if err != nil {
		return model.User{}, fmt.Errorf("store: getting user by email: %w", err)
	}
	u.Email = gotEmail.String
	return u, nil
}

// GetUserBotsByUsername returns the bots belonging to the given user.
func (s *Store) GetUserBotsByUsername(username string) ([]model.Bot, error) {
	rows, err := s.db.Query(`
		SELECT b.id, b.name, b.source
		FROM bots b
		LEFT JOIN user_bot_rel ub ON ub.bot_id = b.id
		WHERE ub.user_id=(SELECT id FROM users WHERE name=?)`, username)
	if err != nil {
		return nil, fmt.Errorf("store: getting bots for user %q: %w", username, err)
	}
	defer rows.Close()
	return scanBots(rows)
}

// GetUserBotsByID returns the bots belonging to the given user.
func (s *Store) GetUserBotsByID(id int) ([]model.Bot, error) {
	rows, err := s.db.Query(`
		SELECT b.id, b.name, b.source
		FROM bots b
		LEFT JOIN user_bot_rel ub ON ub.bot_id = b.id
		WHERE ub.user_id=?`, id)
	if err != nil {
		return nil, fmt.Errorf("store: getting bots for user %d: %w", id, err)
	}
	defer rows.Close()
	return scanBots(rows)
}

func scanBots(rows *sql.Rows) ([]model.Bot, error) {
	bots := []model.Bot{}
	for rows.Next() {
		var b model.Bot
		if err := rows.Scan(&b.ID, &b.Name, &b.Source); err != nil {
			return nil, fmt.Errorf("store: scanning bot: %w", err)
		}
		bots = append(bots, b)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("store: iterating bots: %w", err)
	}
	return bots, nil
}

func (s *Store) GetAllUsers() ([]model.User, error) {
	rows, err := s.db.Query("SELECT id, name FROM users")
	if err != nil {
		return nil, fmt.Errorf("store: getting all users: %w", err)
	}
	defer rows.Close()

	users := []model.User{}
	for rows.Next() {
		var u model.User
		if err := rows.Scan(&u.ID, &u.Name); err != nil {
			return nil, fmt.Errorf("store: scanning user: %w", err)
		}
		users = append(users, u)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("store: iterating users: %w", err)
	}
	return users, nil
}
