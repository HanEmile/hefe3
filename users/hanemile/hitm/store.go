package main

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"log"
	"time"

	_ "modernc.org/sqlite"
)

// SchemaVersion is the version of the database schema this binary expects.
// Bump this whenever the schema changes and add a migration in migrations.
const SchemaVersion = 3

// AppVersion is the human-facing version of the application. Recorded in the
// database so we can tell which binary last wrote it.
const AppVersion = "0.1.0"

// Store wraps the SQLite database and provides persistence for flows, jobs,
// job results and listeners.
type Store struct {
	db *sql.DB
}

// PersistedFlow is the on-disk representation of a Flow. Unlike Flow it does
// not hold live *http.Request/*http.Response; it stores the rendered dumps and
// bodies that the API actually serves, so it can be reconstructed verbatim.
type PersistedFlow struct {
	id           uint64
	source       string
	method       string
	url          string
	status       int
	requestDump  []byte
	responseDump []byte
	requestBody  []byte
	responseBody []byte
	requestTime  string
	responseTime string
	hasResponse  int

	// fingerprint is the precomputed /map state identity, packed as
	// "fp|kind|err" (see packFingerprint/unpackFingerprint in map.go). It is
	// computed at persist time so the /map endpoint never has to re-parse the
	// (large) response dump/body. Empty for pre-v3 rows or flows without a
	// response yet; BuildGraph falls back to computing it on demand in that case.
	fingerprint string
}

// JSON renders a persisted flow into the same shape Flow.JSON produces, so the
// /flow, /history and SSE consumers don't need to know whether a flow came from
// memory or disk.
func (p *PersistedFlow) JSON(body bool) string {
	respDump := ""
	if p.hasResponse == 1 {
		respDump = string(p.responseDump)
	}
	payload := map[string]any{
		"id":           p.id,
		"source":       p.source,
		"request":      string(p.requestDump),
		"response":     respDump,
		"requestTime":  p.requestTime,
		"responseTime": p.responseTime,
		"status":       p.status,
		"method":       p.method,
		"url":          p.url,
	}
	if body {
		payload["requestBody"] = string(p.requestBody)
		payload["responseBody"] = string(p.responseBody)
	}
	b, _ := json.Marshal(payload)
	return string(b)
}

// Summary renders the lightweight shape used by /history.
func (p *PersistedFlow) Summary() map[string]any {
	return map[string]any{
		"id":           p.id,
		"source":       p.source,
		"method":       p.method,
		"url":          p.url,
		"status":       p.status,
		"requestTime":  p.requestTime,
		"responseTime": p.responseTime,
	}
}

// migration is a single forward schema change. index 0 migrates an empty
// database (version 0) to version 1, index 1 migrates 1 -> 2, and so on.
var migrations = []string{
	// v0 -> v1: initial schema
	`
	CREATE TABLE flows (
		id            INTEGER PRIMARY KEY,
		source        TEXT,
		method        TEXT,
		url           TEXT,
		status        INTEGER,
		request_dump  BLOB,
		response_dump BLOB,
		request_body  BLOB,
		response_body BLOB,
		request_time  TEXT,
		response_time TEXT,
		has_response  INTEGER DEFAULT 0
	);
	CREATE INDEX idx_flows_source ON flows(source);
	CREATE INDEX idx_flows_status ON flows(status);

	CREATE TABLE jobs (
		id         INTEGER PRIMARY KEY,
		type       TEXT,
		status     TEXT,
		created_at TEXT,
		total      INTEGER,
		done       INTEGER,
		config     TEXT
	);

	CREATE TABLE job_results (
		job_id      INTEGER,
		seq         INTEGER,
		payload     TEXT,
		status_code INTEGER,
		length      INTEGER,
		duration_ms INTEGER,
		error       TEXT,
		PRIMARY KEY (job_id, seq)
	);

	CREATE TABLE listeners (
		key      TEXT PRIMARY KEY,
		address  TEXT,
		name     TEXT,
		is_proxy INTEGER
	);
	`,
	// v1 -> v2: richer fuzz results (word/line counts, matched flag)
	`
	ALTER TABLE job_results ADD COLUMN words INTEGER DEFAULT 0;
	ALTER TABLE job_results ADD COLUMN lines INTEGER DEFAULT 0;
	ALTER TABLE job_results ADD COLUMN matched INTEGER DEFAULT 0;
	`,
	// v2 -> v3: precomputed /map state fingerprint (packed "fp|kind|err")
	`
	ALTER TABLE flows ADD COLUMN fingerprint TEXT DEFAULT '';
	`,
}

// OpenStore opens (or creates) the SQLite database at path, applies any
// pending migrations and records the current app version. It returns an error
// if the database is newer than this binary understands.
func OpenStore(path string) (*Store, error) {
	db, err := sql.Open("sqlite", path+"?_pragma=journal_mode(WAL)&_pragma=busy_timeout(5000)&_pragma=foreign_keys(1)")
	if err != nil {
		return nil, fmt.Errorf("open db: %w", err)
	}
	db.SetMaxOpenConns(1) // SQLite: serialize writes to avoid "database is locked"

	s := &Store{db: db}
	if err := s.init(); err != nil {
		db.Close()
		return nil, err
	}
	return s, nil
}

func (s *Store) init() error {
	if _, err := s.db.Exec(`CREATE TABLE IF NOT EXISTS meta (key TEXT PRIMARY KEY, value TEXT)`); err != nil {
		return fmt.Errorf("create meta table: %w", err)
	}

	current := s.metaInt("schema_version", 0)

	if current > SchemaVersion {
		return fmt.Errorf("database schema version %d is newer than this binary supports (%d); upgrade the application",
			current, SchemaVersion)
	}

	if current < SchemaVersion {
		log.Printf("Migrating database schema from version %d to %d", current, SchemaVersion)
		for v := current; v < SchemaVersion; v++ {
			tx, err := s.db.Begin()
			if err != nil {
				return err
			}
			if _, err := tx.Exec(migrations[v]); err != nil {
				tx.Rollback()
				return fmt.Errorf("migration %d -> %d failed: %w", v, v+1, err)
			}
			if err := tx.Commit(); err != nil {
				return err
			}
			log.Printf("Applied migration %d -> %d", v, v+1)
		}
		s.setMeta("schema_version", fmt.Sprintf("%d", SchemaVersion))
	}

	prevApp := s.metaStr("app_version", "")
	if prevApp != "" && prevApp != AppVersion {
		log.Printf("Database last written by app version %s, now running %s", prevApp, AppVersion)
	}
	s.setMeta("app_version", AppVersion)
	s.setMeta("last_opened", time.Now().Format(time.RFC3339))

	log.Printf("Database ready (schema v%d, app v%s)", SchemaVersion, AppVersion)
	return nil
}

func (s *Store) metaStr(key, def string) string {
	var v string
	err := s.db.QueryRow(`SELECT value FROM meta WHERE key = ?`, key).Scan(&v)
	if err != nil {
		return def
	}
	return v
}

func (s *Store) metaInt(key string, def int) int {
	v := s.metaStr(key, "")
	if v == "" {
		return def
	}
	var n int
	if _, err := fmt.Sscanf(v, "%d", &n); err != nil {
		return def
	}
	return n
}

func (s *Store) setMeta(key, value string) {
	if _, err := s.db.Exec(`INSERT INTO meta(key, value) VALUES(?, ?)
		ON CONFLICT(key) DO UPDATE SET value = excluded.value`, key, value); err != nil {
		log.Printf("setMeta(%s) failed: %v", key, err)
	}
}

// Version reports the schema and app versions for the /version endpoint.
func (s *Store) Version() map[string]any {
	return map[string]any{
		"schemaVersion":    SchemaVersion,
		"appVersion":       AppVersion,
		"dbSchemaVersion":  s.metaInt("schema_version", 0),
		"dbAppVersion":     s.metaStr("app_version", ""),
		"lastOpened":       s.metaStr("last_opened", ""),
		"upgradeAvailable": s.metaInt("schema_version", 0) < SchemaVersion,
	}
}

func (s *Store) Close() error {
	return s.db.Close()
}

// --- Flow persistence ---

// SaveFlow upserts a flow. It is called from the central OutFlow drain so both
// the initial request and the later response update are captured.
func (s *Store) SaveFlow(f *Flow) {
	if s == nil || f == nil || f.Request == nil {
		return
	}
	pf := f.persist()
	// Precompute the /map state fingerprint (packed "fp|kind|err") now, while we
	// still have the full bodies/dumps in hand. The /map endpoint then never has
	// to re-parse the response. Only meaningful once a response exists.
	if pf.hasResponse == 1 {
		pf.fingerprint = packFingerprint(pf)
	}
	_, err := s.db.Exec(`
		INSERT INTO flows(id, source, method, url, status, request_dump, response_dump,
			request_body, response_body, request_time, response_time, has_response, fingerprint)
		VALUES(?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(id) DO UPDATE SET
			status = excluded.status,
			response_dump = excluded.response_dump,
			response_body = excluded.response_body,
			response_time = excluded.response_time,
			has_response = excluded.has_response,
			fingerprint = excluded.fingerprint`,
		pf.id, pf.source, pf.method, pf.url, pf.status, pf.requestDump, pf.responseDump,
		pf.requestBody, pf.responseBody, pf.requestTime, pf.responseTime, pf.hasResponse, pf.fingerprint)
	if err != nil {
		log.Printf("SaveFlow(%d) failed: %v", f.ID, err)
	}
}

// LoadFlows restores all persisted flows into the given listener's Flows map
// and returns the highest flow id seen (so the in-memory counter can resume).
func (s *Store) LoadFlows() (map[uint64]*PersistedFlow, uint64, error) {
	rows, err := s.db.Query(`SELECT id, source, method, url, status, request_dump, response_dump,
		request_body, response_body, request_time, response_time, has_response, fingerprint FROM flows ORDER BY id`)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()

	out := make(map[uint64]*PersistedFlow)
	var maxID uint64
	for rows.Next() {
		var p PersistedFlow
		if err := rows.Scan(&p.id, &p.source, &p.method, &p.url, &p.status,
			&p.requestDump, &p.responseDump, &p.requestBody, &p.responseBody,
			&p.requestTime, &p.responseTime, &p.hasResponse, &p.fingerprint); err != nil {
			return nil, 0, err
		}
		out[p.id] = &p
		if p.id > maxID {
			maxID = p.id
		}
	}
	return out, maxID, rows.Err()
}

// LoadFlowsMeta loads the lightweight columns the /map endpoint needs: id,
// source, method, url, status, fingerprint and request_dump. It deliberately
// omits response_dump and response_body (the large columns), which are no
// longer needed once the fingerprint/kind/error are precomputed at persist
// time. request_dump is still needed for sessionization (Cookie/Referer) and
// for edge action labels (form field names). This is the heavy-flow path: it
// avoids pulling ~20k full response BLOBs into memory on every /map request.
func (s *Store) LoadFlowsMeta() ([]*PersistedFlow, error) {
	rows, err := s.db.Query(`SELECT id, source, method, url, status, fingerprint, request_dump
		FROM flows ORDER BY id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []*PersistedFlow
	for rows.Next() {
		p := &PersistedFlow{}
		if err := rows.Scan(&p.id, &p.source, &p.method, &p.url, &p.status,
			&p.fingerprint, &p.requestDump); err != nil {
			return nil, err
		}
		// A flow has a usable precomputed fingerprint iff it was persisted with a
		// response. Mark hasResponse so request-meta parsing still works while the
		// BuildGraph fast path uses p.fingerprint directly.
		if p.fingerprint != "" {
			p.hasResponse = 1
		}
		out = append(out, p)
	}
	return out, rows.Err()
}

// --- Job persistence ---

func (s *Store) SaveJob(j *Job) {
	if s == nil {
		return
	}
	cfgBytes, _ := json.Marshal(j.Config)
	_, err := s.db.Exec(`
		INSERT INTO jobs(id, type, status, created_at, total, done, config)
		VALUES(?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(id) DO UPDATE SET status = excluded.status, done = excluded.done`,
		j.ID, j.Type, j.Status, j.CreatedAt.Format(time.RFC3339), j.Total, j.Done.Load(), string(cfgBytes))
	if err != nil {
		log.Printf("SaveJob(%d) failed: %v", j.ID, err)
	}
}

func (s *Store) SaveJobResult(jobID uint64, seq int, r FuzzResult) {
	if s == nil {
		return
	}
	matched := 0
	if r.Matched {
		matched = 1
	}
	_, err := s.db.Exec(`
		INSERT INTO job_results(job_id, seq, payload, status_code, length, duration_ms, error, words, lines, matched)
		VALUES(?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(job_id, seq) DO NOTHING`,
		jobID, seq, r.Payload, r.StatusCode, r.Length, r.Duration, r.Error, r.Words, r.Lines, matched)
	if err != nil {
		log.Printf("SaveJobResult(%d,%d) failed: %v", jobID, seq, err)
	}
}

// SaveJobResultsBatch persists a contiguous run of fuzz results in a single
// transaction. rs[i] is stored at seq startSeq+i. Wrapping the inserts in one
// transaction amortizes the per-statement overhead of the single SQLite
// connection, lifting the throughput cap of one INSERT-per-result.
func (s *Store) SaveJobResultsBatch(jobID uint64, startSeq int, rs []FuzzResult) {
	if s == nil || len(rs) == 0 {
		return
	}
	tx, err := s.db.Begin()
	if err != nil {
		log.Printf("SaveJobResultsBatch(%d) begin failed: %v", jobID, err)
		return
	}
	stmt, err := tx.Prepare(`
		INSERT INTO job_results(job_id, seq, payload, status_code, length, duration_ms, error, words, lines, matched)
		VALUES(?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(job_id, seq) DO NOTHING`)
	if err != nil {
		tx.Rollback()
		log.Printf("SaveJobResultsBatch(%d) prepare failed: %v", jobID, err)
		return
	}
	for i, r := range rs {
		matched := 0
		if r.Matched {
			matched = 1
		}
		if _, err := stmt.Exec(jobID, startSeq+i, r.Payload, r.StatusCode, r.Length,
			r.Duration, r.Error, r.Words, r.Lines, matched); err != nil {
			log.Printf("SaveJobResultsBatch(%d,%d) exec failed: %v", jobID, startSeq+i, err)
		}
	}
	stmt.Close()
	if err := tx.Commit(); err != nil {
		log.Printf("SaveJobResultsBatch(%d) commit failed: %v", jobID, err)
	}
}

func (s *Store) LoadJobs() ([]*Job, uint64, error) {
	rows, err := s.db.Query(`SELECT id, type, status, created_at, total, done, config FROM jobs ORDER BY id`)
	if err != nil {
		return nil, 0, err
	}

	var jobs []*Job
	var maxID uint64
	// IMPORTANT: fully drain and close this query BEFORE issuing the per-job
	// result queries. The store caps the pool at one connection (SQLite write
	// serialization), so running loadJobResults while these rows are still open
	// would deadlock waiting for a connection that this query holds.
	for rows.Next() {
		j := &Job{}
		var createdAt, cfg string
		var done int64
		if err := rows.Scan(&j.ID, &j.Type, &j.Status, &createdAt, &j.Total, &done, &cfg); err != nil {
			rows.Close()
			return nil, 0, err
		}
		j.CreatedAt, _ = time.Parse(time.RFC3339, createdAt)
		j.Done.Store(done)
		json.Unmarshal([]byte(cfg), &j.Config)
		// Jobs that were running when the process exited are marked interrupted.
		if j.Status == "running" {
			j.Status = "interrupted"
		}
		jobs = append(jobs, j)
		if j.ID > maxID {
			maxID = j.ID
		}
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return nil, 0, err
	}
	rows.Close()

	// Now the connection is free; load each job's results.
	for _, j := range jobs {
		j.Results = s.loadJobResults(j.ID)
	}
	return jobs, maxID, nil
}

func (s *Store) loadJobResults(jobID uint64) []FuzzResult {
	rows, err := s.db.Query(`SELECT payload, status_code, length, duration_ms, error, words, lines, matched
		FROM job_results WHERE job_id = ? ORDER BY seq`, jobID)
	if err != nil {
		log.Printf("loadJobResults(%d) failed: %v", jobID, err)
		return nil
	}
	defer rows.Close()
	var results []FuzzResult
	for rows.Next() {
		var r FuzzResult
		var matched int
		if err := rows.Scan(&r.Payload, &r.StatusCode, &r.Length, &r.Duration, &r.Error, &r.Words, &r.Lines, &matched); err != nil {
			continue
		}
		r.Matched = matched == 1
		results = append(results, r)
	}
	return results
}

// --- Listener persistence ---

func (s *Store) SaveListener(key, address, name string, isProxy bool) {
	if s == nil {
		return
	}
	proxy := 0
	if isProxy {
		proxy = 1
	}
	_, err := s.db.Exec(`INSERT INTO listeners(key, address, name, is_proxy) VALUES(?, ?, ?, ?)
		ON CONFLICT(key) DO UPDATE SET address = excluded.address, name = excluded.name, is_proxy = excluded.is_proxy`,
		key, address, name, proxy)
	if err != nil {
		log.Printf("SaveListener(%s) failed: %v", key, err)
	}
}

type PersistedListener struct {
	Key     string
	Address string
	Name    string
	IsProxy bool
}

func (s *Store) LoadListeners() ([]PersistedListener, error) {
	rows, err := s.db.Query(`SELECT key, address, name, is_proxy FROM listeners`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []PersistedListener
	for rows.Next() {
		var pl PersistedListener
		var proxy int
		if err := rows.Scan(&pl.Key, &pl.Address, &pl.Name, &proxy); err != nil {
			return nil, err
		}
		pl.IsProxy = proxy == 1
		out = append(out, pl)
	}
	return out, rows.Err()
}
