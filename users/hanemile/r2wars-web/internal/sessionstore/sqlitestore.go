// Package sessionstore is a gorilla/sessions backend for SQLite, originally
// vendored in-tree as sqlitestore.go (MIT, (c) 2013 Contributors).
package sessionstore

import (
	"database/sql"
	"encoding/gob"
	"errors"
	"fmt"
	"log"
	"net/http"
	"strings"
	"time"

	"github.com/gorilla/securecookie"
	"github.com/gorilla/sessions"
)

type Store struct {
	db         DB
	stmtInsert *sql.Stmt
	stmtDelete *sql.Stmt
	stmtUpdate *sql.Stmt
	stmtSelect *sql.Stmt

	Codecs  []securecookie.Codec
	Options *sessions.Options
	table   string
}

type sessionRow struct {
	id         string
	data       string
	createdOn  time.Time
	modifiedOn time.Time
	expiresOn  time.Time
}

type DB interface {
	Exec(query string, args ...interface{}) (sql.Result, error)
	Prepare(query string) (*sql.Stmt, error)
	Close() error
}

func init() {
	gob.Register(time.Time{})
}

// New opens its own SQLite connection at endpoint, in addition to whatever
// connection the main application store uses. Sessions are high-frequency,
// low-value writes (every login/logout), so keeping them in a separate file
// means they can't contend with or corrupt battle/user data.
func New(endpoint, tableName, path string, maxAge int, keyPairs ...[]byte) (*Store, error) {
	db, err := sql.Open("sqlite3", endpoint)
	if err != nil {
		return nil, fmt.Errorf("sessionstore: opening %q: %w", endpoint, err)
	}
	return NewFromConnection(db, tableName, path, maxAge, keyPairs...)
}

func NewFromConnection(db DB, tableName string, path string, maxAge int, keyPairs ...[]byte) (*Store, error) {
	// Make sure table name is enclosed.
	tableName = "`" + strings.Trim(tableName, "`") + "`"

	cTableQ := "CREATE TABLE IF NOT EXISTS " +
		tableName + " (id INTEGER PRIMARY KEY, " +
		"session_data LONGBLOB, " +
		"created_on TIMESTAMP DEFAULT 0, " +
		"modified_on TIMESTAMP DEFAULT CURRENT_TIMESTAMP, " +
		"expires_on TIMESTAMP DEFAULT 0);"
	if _, err := db.Exec(cTableQ); err != nil {
		return nil, fmt.Errorf("sessionstore: creating table: %w", err)
	}

	insQ := "INSERT INTO " + tableName +
		"(id, session_data, created_on, modified_on, expires_on) VALUES (NULL, ?, ?, ?, ?)"
	stmtInsert, err := db.Prepare(insQ)
	if err != nil {
		return nil, fmt.Errorf("sessionstore: preparing insert: %w", err)
	}

	delQ := "DELETE FROM " + tableName + " WHERE id = ?"
	stmtDelete, err := db.Prepare(delQ)
	if err != nil {
		return nil, fmt.Errorf("sessionstore: preparing delete: %w", err)
	}

	updQ := "UPDATE " + tableName + " SET session_data = ?, created_on = ?, expires_on = ? " +
		"WHERE id = ?"
	stmtUpdate, err := db.Prepare(updQ)
	if err != nil {
		return nil, fmt.Errorf("sessionstore: preparing update: %w", err)
	}

	selQ := "SELECT id, session_data, created_on, modified_on, expires_on from " +
		tableName + " WHERE id = ?"
	stmtSelect, err := db.Prepare(selQ)
	if err != nil {
		return nil, fmt.Errorf("sessionstore: preparing select: %w", err)
	}

	return &Store{
		db:         db,
		stmtInsert: stmtInsert,
		stmtDelete: stmtDelete,
		stmtUpdate: stmtUpdate,
		stmtSelect: stmtSelect,
		Codecs:     securecookie.CodecsFromPairs(keyPairs...),
		Options: &sessions.Options{
			Path:   path,
			MaxAge: maxAge,
		},
		table: tableName,
	}, nil
}

func (m *Store) Close() error {
	m.stmtSelect.Close()
	m.stmtUpdate.Close()
	m.stmtDelete.Close()
	m.stmtInsert.Close()
	return m.db.Close()
}

func (m *Store) Get(r *http.Request, name string) (*sessions.Session, error) {
	return sessions.GetRegistry(r).Get(m, name)
}

func (m *Store) New(r *http.Request, name string) (*sessions.Session, error) {
	session := sessions.NewSession(m, name)
	session.Options = &sessions.Options{
		Path:   m.Options.Path,
		MaxAge: m.Options.MaxAge,
	}
	session.IsNew = true
	var err error
	if cook, errCookie := r.Cookie(name); errCookie == nil {
		err = securecookie.DecodeMulti(name, cook.Value, &session.ID, m.Codecs...)
		if err == nil {
			err = m.load(session)
			if err == nil {
				session.IsNew = false
			} else {
				err = nil
			}
		}
	}
	return session, err
}

func (m *Store) Save(r *http.Request, w http.ResponseWriter, session *sessions.Session) error {
	var err error
	if session.ID == "" {
		if err = m.insert(session); err != nil {
			return err
		}
	} else if err = m.save(session); err != nil {
		return err
	}
	encoded, err := securecookie.EncodeMulti(session.Name(), session.ID, m.Codecs...)
	if err != nil {
		return err
	}
	http.SetCookie(w, sessions.NewCookie(session.Name(), encoded, session.Options))
	return nil
}

// timeValue safely reads a time.Time out of a session value that may be
// nil, or - if a stale/foreign cookie ever gets decoded here - some other
// type entirely. A bad assertion here used to be able to panic a request
// instead of just falling back to "now".
func timeValue(v interface{}, fallback time.Time) time.Time {
	if v == nil {
		return fallback
	}
	t, ok := v.(time.Time)
	if !ok {
		return fallback
	}
	return t
}

func (m *Store) insert(session *sessions.Session) error {
	createdOn := timeValue(session.Values["created_on"], time.Now())
	modifiedOn := createdOn
	expiresOn := timeValue(session.Values["expires_on"], time.Now().Add(time.Second*time.Duration(session.Options.MaxAge)))

	delete(session.Values, "created_on")
	delete(session.Values, "expires_on")
	delete(session.Values, "modified_on")

	encoded, encErr := securecookie.EncodeMulti(session.Name(), session.Values, m.Codecs...)
	if encErr != nil {
		return encErr
	}
	res, insErr := m.stmtInsert.Exec(encoded, createdOn, modifiedOn, expiresOn)
	if insErr != nil {
		return insErr
	}
	lastInserted, lInsErr := res.LastInsertId()
	if lInsErr != nil {
		return lInsErr
	}
	session.ID = fmt.Sprintf("%d", lastInserted)
	return nil
}

func (m *Store) Delete(r *http.Request, w http.ResponseWriter, session *sessions.Session) error {
	// Set cookie to expire.
	options := *session.Options
	options.MaxAge = -1
	http.SetCookie(w, sessions.NewCookie(session.Name(), "", &options))
	// Clear session values.
	for k := range session.Values {
		delete(session.Values, k)
	}

	_, delErr := m.stmtDelete.Exec(session.ID)
	if delErr != nil {
		return delErr
	}
	return nil
}

func (m *Store) save(session *sessions.Session) error {
	if session.IsNew {
		return m.insert(session)
	}

	createdOn := timeValue(session.Values["created_on"], time.Now())

	maxAge := time.Second * time.Duration(session.Options.MaxAge)
	expiresOn, hadExpiry := session.Values["expires_on"].(time.Time)
	if !hadExpiry {
		expiresOn = time.Now().Add(maxAge)
		log.Print("sessionstore: no expires_on on existing session, resetting")
	} else if expiresOn.Before(time.Now().Add(maxAge)) {
		expiresOn = time.Now().Add(maxAge)
	}

	delete(session.Values, "created_on")
	delete(session.Values, "expires_on")
	delete(session.Values, "modified_on")
	encoded, encErr := securecookie.EncodeMulti(session.Name(), session.Values, m.Codecs...)
	if encErr != nil {
		return encErr
	}
	_, updErr := m.stmtUpdate.Exec(encoded, createdOn, expiresOn, session.ID)
	if updErr != nil {
		return updErr
	}
	return nil
}

func (m *Store) load(session *sessions.Session) error {
	row := m.stmtSelect.QueryRow(session.ID)
	sess := sessionRow{}
	scanErr := row.Scan(&sess.id, &sess.data, &sess.createdOn, &sess.modifiedOn, &sess.expiresOn)
	if scanErr != nil {
		return scanErr
	}
	if sess.expiresOn.Before(time.Now()) {
		return errors.New("sessionstore: session expired")
	}
	err := securecookie.DecodeMulti(session.Name(), sess.data, &session.Values, m.Codecs...)
	if err != nil {
		return err
	}
	session.Values["created_on"] = sess.createdOn
	session.Values["modified_on"] = sess.modifiedOn
	session.Values["expires_on"] = sess.expiresOn
	return nil
}
