// internal/worker/pool.go — Generic priority worker pool with job tracking,
// cancellation, and a broadcast hook for SSE/remote forwarding.
// Paves the road for remote worker offloading via the protocol package.
//
// Part of github.com/hanemile/kscope
package worker

import (
	"context"
	"fmt"
	"log"
	"sort"
	"sync"
	"sync/atomic"
	"time"

	"github.com/hanemile/kscope/internal/protocol"
)

// HandlerFunc executes a job and may emit progress messages via emit.
type HandlerFunc func(ctx context.Context, job *Job, emit func(*protocol.Msg)) error

// Job is one unit of work.
type Job struct {
	ID       string
	Kind     protocol.Kind
	Priority protocol.Priority
	Payload  interface{}
	Created  time.Time
	Started  time.Time
	Ended    time.Time
	State    string // queued|running|done|cancelled|error
	Error    string
	Progress float64
	Label    string
	cancel   context.CancelFunc
	mu       sync.Mutex
}

func newJob(kind protocol.Kind, pri protocol.Priority, payload interface{}, label string) *Job {
	return &Job{
		ID:      fmt.Sprintf("%d", time.Now().UnixNano()),
		Kind:    kind, Priority: pri, Payload: payload,
		Created: time.Now(), State: "queued", Label: label,
	}
}

func (j *Job) StatusPayload() protocol.JobStatusPayload {
	j.mu.Lock(); defer j.mu.Unlock()
	return protocol.JobStatusPayload{
		JobID: j.ID, Kind: j.Kind, State: j.State, Priority: j.Priority,
		Created: j.Created, Started: j.Started, Ended: j.Ended,
		Error: j.Error, Progress: j.Progress, Label: j.Label,
	}
}

func (j *Job) SetProgress(p float64) { j.mu.Lock(); j.Progress = p; j.mu.Unlock() }

// Pool manages worker goroutines consuming a priority queue.
type Pool struct {
	workers  int
	queue    chan *Job
	jobs     map[string]*Job
	mu       sync.RWMutex
	handlers map[protocol.Kind]HandlerFunc
	emit     func(*protocol.Msg)
	wg       sync.WaitGroup
	active   int64
	done     uint64
	errors   uint64
}

// New creates a Pool with n workers.
func New(n int) *Pool {
	if n < 1 { n = 1 }
	return &Pool{
		workers:  n,
		queue:    make(chan *Job, 1024),
		jobs:     make(map[string]*Job),
		handlers: make(map[protocol.Kind]HandlerFunc),
	}
}

// SetEmit registers the broadcast hook for outgoing protocol messages.
func (p *Pool) SetEmit(fn func(*protocol.Msg)) { p.emit = fn }

// Register binds a HandlerFunc to a message kind.
func (p *Pool) Register(kind protocol.Kind, fn HandlerFunc) { p.handlers[kind] = fn }

// Start launches worker goroutines; ctx cancels all workers.
func (p *Pool) Start(ctx context.Context) {
	for i := 0; i < p.workers; i++ {
		p.wg.Add(1)
		go p.loop(ctx)
	}
	log.Printf("[pool] %d workers started", p.workers)
}

// Stop closes the queue and waits for all workers to exit.
func (p *Pool) Stop() { close(p.queue); p.wg.Wait() }

// Submit enqueues a job and returns its ID.
func (p *Pool) Submit(kind protocol.Kind, pri protocol.Priority, payload interface{}, label string) string {
	job := newJob(kind, pri, payload, label)
	p.mu.Lock(); p.jobs[job.ID] = job; p.mu.Unlock()
	p.queue <- job
	p.bcast(protocol.MustNew(protocol.KindJobQueued, job.StatusPayload()))
	return job.ID
}

// Cancel signals a job to stop.
func (p *Pool) Cancel(id string) bool {
	p.mu.RLock(); job, ok := p.jobs[id]; p.mu.RUnlock()
	if !ok { return false }
	job.mu.Lock()
	if job.cancel != nil { job.cancel() }
	if job.State == "queued" { job.State = "cancelled"; job.Ended = time.Now() }
	job.mu.Unlock()
	p.bcast(protocol.MustNew(protocol.KindJobCancelled, job.StatusPayload()))
	return true
}

// Jobs returns all job statuses sorted newest-first.
func (p *Pool) Jobs() []protocol.JobStatusPayload {
	p.mu.RLock(); defer p.mu.RUnlock()
	out := make([]protocol.JobStatusPayload, 0, len(p.jobs))
	for _, j := range p.jobs { out = append(out, j.StatusPayload()) }
	sort.Slice(out, func(i, k int) bool { return out[i].Created.After(out[k].Created) })
	return out
}

// Stats returns current pool counters.
func (p *Pool) Stats() map[string]interface{} {
	return map[string]interface{}{
		"workers": p.workers, "active": atomic.LoadInt64(&p.active),
		"queued": len(p.queue), "done": atomic.LoadUint64(&p.done),
		"errors": atomic.LoadUint64(&p.errors),
	}
}

func (p *Pool) loop(ctx context.Context) {
	defer p.wg.Done()
	for {
		select {
		case job, ok := <-p.queue:
			if !ok { return }
			p.run(ctx, job)
		case <-ctx.Done():
			return
		}
	}
}

func (p *Pool) run(ctx context.Context, job *Job) {
	job.mu.Lock()
	if job.State == "cancelled" { job.mu.Unlock(); return }
	jctx, cancel := context.WithCancel(ctx)
	job.cancel = cancel; job.State = "running"; job.Started = time.Now()
	job.mu.Unlock()

	atomic.AddInt64(&p.active, 1)
	p.bcast(protocol.MustNew(protocol.KindJobRunning, job.StatusPayload()))

	h, ok := p.handlers[job.Kind]
	var runErr error
	if !ok {
		runErr = fmt.Errorf("no handler for %q", job.Kind)
	} else {
		runErr = h(jctx, job, func(m *protocol.Msg) { p.bcast(m) })
	}
	cancel()
	atomic.AddInt64(&p.active, -1)

	job.mu.Lock()
	job.Ended = time.Now()
	if runErr != nil {
		job.State = "error"; job.Error = runErr.Error()
		atomic.AddUint64(&p.errors, 1)
	} else {
		job.State = "done"
		atomic.AddUint64(&p.done, 1)
	}
	job.mu.Unlock()

	k := protocol.KindJobDone
	if runErr != nil { k = protocol.KindJobError }
	p.bcast(protocol.MustNew(k, job.StatusPayload()))
}

func (p *Pool) bcast(m *protocol.Msg) { if p.emit != nil { p.emit(m) } }
