// internal/protocol/message.go — Wire protocol for local and remote task
// execution. Every unit of work is a Msg that can travel over channels,
// HTTP, or a future WebSocket/gRPC transport to a remote analytics server.
//
// Part of github.com/hanemile/kscope
package protocol

import (
	"encoding/json"
	"fmt"
	"sync/atomic"
	"time"
)

type Kind string

const (
	KindIndexStart    Kind = "index.start"
	KindIndexProgress Kind = "index.progress"
	KindIndexDone     Kind = "index.done"
	KindIndexError    Kind = "index.error"
	KindAnalyzeFile   Kind = "analyze.file"
	KindAnalyzeXRef   Kind = "analyze.xref"
	KindAnalyzeGraph  Kind = "analyze.graph"
	KindAnalyzeDups   Kind = "analyze.dups"
	KindResultSymbols Kind = "result.symbols"
	KindResultXRefs   Kind = "result.xrefs"
	KindResultGraph   Kind = "result.graph"
	KindResultDups    Kind = "result.dups"
	KindResultFile    Kind = "result.file"
	KindJobQueued     Kind = "job.queued"
	KindJobRunning    Kind = "job.running"
	KindJobDone       Kind = "job.done"
	KindJobCancelled  Kind = "job.cancelled"
	KindJobError      Kind = "job.error"
	KindWorkerHello   Kind = "worker.hello"
	KindWorkerBye     Kind = "worker.bye"
	KindWorkerStatus  Kind = "worker.status"
	KindSSEProgress   Kind = "sse.progress"
	KindSSEJobUpdate  Kind = "sse.job_update"
	KindSSEAlert      Kind = "sse.alert"
)

type Priority int

const (
	PriorityLow    Priority = 0
	PriorityNormal Priority = 5
	PriorityHigh   Priority = 10
)

var msgCounter uint64

// Msg is the universal envelope for all inter-component communication.
type Msg struct {
	ID       string          `json:"id"`
	Kind     Kind            `json:"kind"`
	TS       time.Time       `json:"ts"`
	Priority Priority        `json:"priority,omitempty"`
	CorrelID string          `json:"correl_id,omitempty"`
	Source   string          `json:"source,omitempty"`
	Target   string          `json:"target,omitempty"`
	Payload  json.RawMessage `json:"payload,omitempty"`
	Error    string          `json:"error,omitempty"`
}

func newID() string {
	n := atomic.AddUint64(&msgCounter, 1)
	return fmt.Sprintf("%d-%d", time.Now().UnixNano(), n)
}

func New(kind Kind, payload interface{}) (*Msg, error) {
	b, err := json.Marshal(payload)
	if err != nil {
		return nil, err
	}
	return &Msg{ID: newID(), Kind: kind, TS: time.Now().UTC(), Payload: b}, nil
}

func MustNew(kind Kind, payload interface{}) *Msg {
	m, err := New(kind, payload)
	if err != nil { panic(err) }
	return m
}

func (m *Msg) Decode(dst interface{}) error { return json.Unmarshal(m.Payload, dst) }

func (m *Msg) Reply(kind Kind, payload interface{}) (*Msg, error) {
	r, err := New(kind, payload)
	if err != nil { return nil, err }
	r.CorrelID = m.ID; r.Target = m.Source
	return r, nil
}

func (m *Msg) ErrReply(e string) *Msg {
	return &Msg{ID: newID(), Kind: KindJobError, TS: time.Now().UTC(), CorrelID: m.ID, Target: m.Source, Error: e}
}

// ── Payload structs ────────────────────────────────────────────────────────────

type IndexStartPayload struct {
	Root     string   `json:"root"`
	Label    string   `json:"label"`
	GitTag   string   `json:"git_tag,omitempty"`
	Workers  int      `json:"workers,omitempty"`
	// SkipDirs lists top-level subdirectory names to exclude from indexing.
	// When empty the indexer applies its built-in default skip list which
	// includes "drivers" — scan those on demand via POST /api/index-dir.
	SkipDirs []string `json:"skip_dirs,omitempty"`
	// SubDir restricts indexing to one subdirectory of Root (e.g. "drivers").
	SubDir   string   `json:"sub_dir,omitempty"`
}

type IndexProgressPayload struct {
	Label   string  `json:"label"`
	Phase   string  `json:"phase"`
	Files   int64   `json:"files"`
	Total   int64   `json:"total"`
	Symbols int64   `json:"symbols"`
	XRefs   int64   `json:"xrefs"`
	Elapsed float64 `json:"elapsed_s"`
	Pct     float64 `json:"pct"`
	Message string  `json:"message,omitempty"`
}

type JobStatusPayload struct {
	JobID    string    `json:"job_id"`
	Kind     Kind      `json:"kind"`
	State    string    `json:"state"`
	Priority Priority  `json:"priority"`
	Created  time.Time `json:"created"`
	Started  time.Time `json:"started,omitempty"`
	Ended    time.Time `json:"ended,omitempty"`
	Error    string    `json:"error,omitempty"`
	Progress float64   `json:"progress,omitempty"`
	Label    string    `json:"label,omitempty"`
}

type WorkerHelloPayload struct {
	WorkerID   string   `json:"worker_id"`
	Hostname   string   `json:"hostname"`
	Goroutines int      `json:"goroutines"`
	Caps       []string `json:"caps"`
}

type AlertPayload struct {
	Level   string `json:"level"`
	Title   string `json:"title"`
	Message string `json:"message"`
}

type AnalyzeXRefPayload struct {
	Symbol  string `json:"symbol"`
	Version string `json:"version"`
	Depth   int    `json:"depth"`
}
