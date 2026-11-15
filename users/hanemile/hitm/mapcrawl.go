package main

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"io"
	"log"
	"math/rand"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"
)

// ============================================================================
// Active map exploration crawler ("Fuzz Map"-style, source-less).
//
// Where map.go builds a state graph PASSIVELY from already-observed flows, this
// crawler ACTIVELY drives a target: it issues GET navigations, fingerprints
// each response with the SAME StateFingerprint logic (via fingerprintResponse),
// and treats the set of distinct fingerprints as its "coverage" signal. Newly
// discovered states are expanded by extracting their links and enqueueing them
// as further actions.
//
// All flows it produces carry Source "mapcrawl", so BuildGraph folds them into
// the existing /map graph automatically -- the crawler discovers the
// states/edges and /map visualizes them.
//
// LIMITATION: this first version is GET-navigation only. Because plain HTTP
// navigation is stateless, "traveling" to a state's action is just issuing the
// GET for that action URL directly -- we do NOT replay the recorded shortest
// path. Path replay only matters for STATEFUL flows (e.g. POST/form
// submissions, multi-step wizards), which this version does not synthesize. The
// shortest-path map is still tracked (Fuzz Map's "seed pool") so a later
// version can replay it for stateful actions.
// ============================================================================

// MapCrawlConfig configures the active map exploration crawler.
type MapCrawlConfig struct {
	Sites      []string `json:"sites"`      // seed URLs
	Listener   string   `json:"listener"`   // listener to attach flows to
	MaxStates  int      `json:"maxStates"`  // stop after this many distinct states (default 200)
	MaxReqs    int      `json:"maxReqs"`    // hard cap on requests (default 2000)
	TimeoutSec int      `json:"timeoutSec"` // per-request timeout (default 10)
	Concurrent int      `json:"concurrent"` // worker concurrency (default 4)
	Headers    []string `json:"headers"`    // extra request headers ("K: V")
	Cookie     string   `json:"cookie"`     // Cookie header
	UserAgent  string   `json:"userAgent"`  // web|mobi|custom
}

func applyMapCrawlDefaults(cfg *MapCrawlConfig) {
	if cfg.MaxStates <= 0 {
		cfg.MaxStates = 200
	}
	if cfg.MaxReqs <= 0 {
		cfg.MaxReqs = 2000
	}
	if cfg.TimeoutSec <= 0 {
		cfg.TimeoutSec = 10
	}
	if cfg.Concurrent <= 0 {
		cfg.Concurrent = 4
	}
	if cfg.UserAgent == "" {
		cfg.UserAgent = "web"
	}
}

// crawlAction is a queued candidate: navigate to actionURL, which was found in
// the HTML of the state identified by fromState.
type crawlAction struct {
	fromState string // fingerprint of the state the link was discovered in
	actionURL string
}

// runMapCrawlJob actively explores the configured sites, growing a state graph.
func (s *HITM) runMapCrawlJob(ctx context.Context, cancel context.CancelFunc, job *Job, cfg MapCrawlConfig) {
	defer cancel()

	// Batch result/progress persistence on a ticker instead of per-result.
	persister := s.newJobPersister(job)

	// HTTP client, mirroring spider.go's construction (trust our CA so MITM'd
	// TLS to our own listeners validates).
	caCertPool, poolErr := x509.SystemCertPool()
	if poolErr != nil {
		caCertPool = x509.NewCertPool()
	}
	caCertPool.AddCert(s.CACert)
	transport := &http.Transport{TLSClientConfig: &tls.Config{RootCAs: caCertPool}}
	client := &http.Client{
		Transport: transport,
		Timeout:   time.Duration(cfg.TimeoutSec) * time.Second,
	}

	listener := s.getListener(cfg.Listener)

	// Shared exploration state, all guarded by mu.
	var (
		mu        sync.Mutex
		states    = map[string]bool{}     // fingerprints discovered (coverage substitute)
		paths     = map[string][]string{} // state fingerprint -> shortest URL path from a seed
		outgoing  = map[string]int{}            // state fingerprint -> explored outgoing actions (scheduling heuristic)
		pending   = map[string][]crawlAction{} // fromState -> queued actions (per-state buckets for O(1) same-state pop)
		pendingN  int                          // total queued actions across all buckets
		queued    = map[string]bool{}          // dedup key "fromState|actionURL" already enqueued
		reqs      int                          // requests issued
		lastState string                       // fingerprint of the most-recently-produced response
	)

	// Seed scope: only navigate to links sharing a seed host.
	seedHosts := map[string]bool{}
	for _, site := range cfg.Sites {
		site = strings.TrimSpace(site)
		if site == "" {
			continue
		}
		if !strings.HasPrefix(site, "http") {
			site = "https://" + site
		}
		if pu, err := url.Parse(site); err == nil {
			seedHosts[pu.Host] = true
		}
	}
	inScope := func(raw string) bool {
		pu, err := url.Parse(raw)
		if err != nil {
			return false
		}
		return seedHosts[pu.Host]
	}

	enqueue := func(from, actionURL string) {
		key := from + "|" + actionURL
		if queued[key] {
			return
		}
		queued[key] = true
		pending[from] = append(pending[from], crawlAction{fromState: from, actionURL: actionURL})
		pendingN++
	}

	// popFrom removes and returns the last queued action of a state's bucket,
	// dropping the bucket when it empties. O(1).
	popFrom := func(state string) crawlAction {
		b := pending[state]
		act := b[len(b)-1]
		b = b[:len(b)-1]
		if len(b) == 0 {
			delete(pending, state)
		} else {
			pending[state] = b
		}
		pendingN--
		return act
	}

	// Seed: GET each site, fingerprint, record the state with path [site].
	for _, site := range cfg.Sites {
		site = strings.TrimSpace(site)
		if site == "" {
			continue
		}
		if !strings.HasPrefix(site, "http") {
			site = "https://" + site
		}
		enqueue("", site)
		// The seed's own state is discovered when the action is processed; we
		// pre-seed paths under the empty-from sentinel so the first hop gets a
		// sensible shortest path.
		paths[""] = nil
	}

	// dequeue picks the next action using the scheduling heuristic:
	//   1. Prefer an action whose fromState == lastState (cheapest: no travel
	//      needed, the link came from the page we just rendered).
	//   2. Otherwise, with some probability, pick the action from the state
	//      with the FEWEST explored outgoing actions (broaden coverage / avoid
	//      getting stuck deep in one subtree).
	//   3. Otherwise pick a random queued item.
	// Implemented over per-state buckets (pending) so each selection avoids the
	// old O(n) slice splice and linear scans:
	//   1. Same-state: O(1) bucket lookup + pop.
	//   2. Least-explored: O(#states-with-pending) min-scan over bucket keys
	//      (far smaller than the total queued action count) + O(1) pop.
	//   3. Random: pick a bucket and pop O(1).
	dequeue := func() (crawlAction, bool) {
		if pendingN == 0 {
			return crawlAction{}, false
		}
		// 1. Same-state preference.
		if lastState != "" {
			if len(pending[lastState]) > 0 {
				return popFrom(lastState), true
			}
		}
		// 2. Occasionally target the least-explored state. pendingN>0 guarantees
		//    at least one bucket, so best is always set to a valid key.
		if rand.Intn(4) == 0 {
			best := ""
			bestOut := -1
			for state := range pending {
				if o := outgoing[state]; bestOut == -1 || o < bestOut {
					bestOut = o
					best = state
				}
			}
			return popFrom(best), true
		}
		// 3. Fallback: pop from an arbitrary bucket (map iteration order is
		//    effectively random, matching the old random-item behavior).
		for state := range pending {
			return popFrom(state), true
		}
		return crawlAction{}, false
	}

	// Worker pool bounded by cfg.Concurrent. We pull a batch of actions under
	// the lock, fetch them concurrently, then fold results back under the lock
	// and loop. This keeps the scheduling heuristic centralized while still
	// issuing requests in parallel.
	sem := make(chan struct{}, cfg.Concurrent)

	for {
		if ctx.Err() != nil {
			break
		}
		mu.Lock()
		stop := len(states) >= cfg.MaxStates || reqs >= cfg.MaxReqs
		// Grab a batch up to Concurrent so workers run in parallel.
		var batch []crawlAction
		for len(batch) < cfg.Concurrent && !stop {
			act, ok := dequeue()
			if !ok {
				break
			}
			reqs++
			batch = append(batch, act)
			if reqs >= cfg.MaxReqs {
				break
			}
		}
		mu.Unlock()
		if stop || len(batch) == 0 {
			break
		}

		type fetchResult struct {
			act    crawlAction
			fp     string
			status int
			ctype  string
			body   []byte
			isHTML bool
		}
		results := make([]fetchResult, len(batch))
		var wg sync.WaitGroup
		for i := range batch {
			wg.Add(1)
			go func(i int) {
				defer wg.Done()
				sem <- struct{}{}
				defer func() { <-sem }()
				act := batch[i]
				body, status, ctype := s.mapCrawlFetch(ctx, client, cfg, act.actionURL, listener)
				results[i] = fetchResult{
					act:    act,
					fp:     fingerprintResponse("GET", act.actionURL, status, ctype, body),
					status: status,
					ctype:  ctype,
					body:   body,
					isHTML: contentTypeClass(ctype) == "html",
				}
			}(i)
		}
		wg.Wait()

		// Fold results back into shared state under the lock.
		mu.Lock()
		for _, r := range results {
			outgoing[r.act.fromState]++
			isNew := !states[r.fp]
			if isNew {
				states[r.fp] = true
				// Shortest path = path to the state we acted from + this action.
				base := paths[r.act.fromState]
				newPath := make([]string, 0, len(base)+1)
				newPath = append(newPath, base...)
				newPath = append(newPath, r.act.actionURL)
				paths[r.fp] = newPath

				// Expand the new state: enqueue its in-scope links as actions.
				if r.isHTML {
					isJS := strings.Contains(r.ctype, "javascript") || strings.HasSuffix(r.act.actionURL, ".js")
					for _, link := range extractLinks(r.act.actionURL, string(r.body), isJS, false) {
						if inScope(link) {
							enqueue(r.fp, link)
						}
					}
				}
			}
			lastState = r.fp

			res := FuzzResult{
				Payload:    r.act.actionURL,
				StatusCode: r.status,
				Length:     len(r.body),
				Lines:      strings.Count(string(r.body), "\n"),
				Words:      len(strings.Fields(string(r.body))),
				Matched:    isNew, // "Matched" == discovered a new state
			}
			job.ResultsMu.Lock()
			job.Results = append(job.Results, res)
			// Total = number of distinct states discovered (reads best in the UI).
			job.Total = len(states)
			job.ResultsMu.Unlock()
			job.Done.Add(1)
			// Persistence handled in batches by the jobPersister ticker.
		}
		mu.Unlock()
	}

	if job.Status == "running" {
		if ctx.Err() != nil {
			job.Status = "cancelled"
		} else {
			job.Status = "completed"
		}
	}
	persister.Close() // flush remaining results + final job state
	log.Printf("mapcrawl job %d finished: status=%s states=%d reqs=%d", job.ID, job.Status, len(states), reqs)
}

// mapCrawlFetch issues a GET and records it as a "mapcrawl" flow (both the
// request and the completed response), mirroring spider.go's spiderFetch.
func (s *HITM) mapCrawlFetch(ctx context.Context, client *http.Client, cfg MapCrawlConfig, target string, listener *Listener) ([]byte, int, string) {
	req, err := http.NewRequestWithContext(ctx, "GET", target, nil)
	if err != nil {
		return nil, 0, ""
	}
	req.Header.Set("User-Agent", resolveUA(cfg.UserAgent))
	if cfg.Cookie != "" {
		req.Header.Set("Cookie", cfg.Cookie)
	}
	for _, h := range cfg.Headers {
		if parts := strings.SplitN(h, ":", 2); len(parts) == 2 {
			req.Header.Set(strings.TrimSpace(parts[0]), strings.TrimSpace(parts[1]))
		}
	}

	flowID := FlowCounter.Add(1)
	fl := &Flow{ID: flowID, Source: "mapcrawl", Request: req, RequestTime: time.Now()}
	if listener != nil && listener.Flows != nil {
		listener.FlowMutex.Lock()
		listener.Flows[flowID] = fl
		listener.evictOldFlows()
		listener.FlowMutex.Unlock()
	}
	s.OutFlow <- *fl

	resp, err := client.Do(req)
	if err != nil {
		return nil, 0, ""
	}
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 10*1024*1024))
	resp.Body.Close()
	resp.Body = io.NopCloser(strings.NewReader(string(body)))

	fl.Response = resp
	fl.ResponseBody = body
	fl.ResponseTime = time.Now()
	s.OutFlow <- *fl

	return body, resp.StatusCode, resp.Header.Get("Content-Type")
}

// mapCrawlHandler serves POST /mapcrawl: decode config, apply defaults, create
// and store a "mapcrawl" job, kick off the crawler, return {"jobId": id}.
// Modeled on the /spider handler in main.go.
func (s *HITM) mapCrawlHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	var cfg MapCrawlConfig
	if err := json.NewDecoder(r.Body).Decode(&cfg); err != nil {
		http.Error(w, "Invalid config: "+err.Error(), http.StatusBadRequest)
		return
	}
	applyMapCrawlDefaults(&cfg)
	if len(cfg.Sites) == 0 {
		http.Error(w, "No sites provided", http.StatusBadRequest)
		return
	}

	ctx, cancel := context.WithCancel(context.Background())
	id := JobCounter.Add(1)
	job := &Job{
		ID:        id,
		Type:      "mapcrawl",
		Status:    "running",
		CreatedAt: time.Now(),
		Total:     0,
		Config:    cfg,
		Cancel:    cancel,
	}
	s.JobsMu.Lock()
	s.Jobs[id] = job
	s.JobsMu.Unlock()
	s.Store.SaveJob(job)

	go s.runMapCrawlJob(ctx, cancel, job, cfg)

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]any{"jobId": id})
}
