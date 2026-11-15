package main

import (
	"bufio"
	"bytes"
	"crypto/tls"
	"crypto/x509"
	"io"
	"log"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"sync/atomic"
	"time"

	"context"
	"sync"
)

// jobPersister batches DB writes for a long-running job. Workers append results
// to job.Results in memory (the UI reads that); the persister flushes any
// not-yet-persisted tail of job.Results to the DB in ONE transaction on a
// ~1s ticker (or when the tail reaches batchSize), and also persists job
// progress on the same cadence. This replaces the old one-INSERT-per-result +
// SaveJob-every-25 pattern, which capped throughput on the single SQLite
// connection. Results are contiguous in job.Results, so the persisted cursor
// (persisted) doubles as the next startSeq.
type jobPersister struct {
	hitm      *HITM
	job       *Job
	batchSize int
	stop      chan struct{}
	done      chan struct{}
	once      sync.Once
}

func (s *HITM) newJobPersister(job *Job) *jobPersister {
	p := &jobPersister{
		hitm:      s,
		job:       job,
		batchSize: 100,
		stop:      make(chan struct{}),
		done:      make(chan struct{}),
	}
	go p.run()
	return p
}

// flush persists any new results (and job progress) once. Safe to call from the
// ticker loop and the final flush.
func (p *jobPersister) flush(persisted *int) {
	p.job.ResultsMu.Lock()
	n := len(p.job.Results)
	var batch []FuzzResult
	if n > *persisted {
		batch = make([]FuzzResult, n-*persisted)
		copy(batch, p.job.Results[*persisted:])
	}
	p.job.ResultsMu.Unlock()

	if len(batch) > 0 {
		p.hitm.Store.SaveJobResultsBatch(p.job.ID, *persisted, batch)
		*persisted += len(batch)
	}
	p.hitm.Store.SaveJob(p.job)
}

func (p *jobPersister) run() {
	defer close(p.done)
	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()
	persisted := 0
	for {
		select {
		case <-p.stop:
			p.flush(&persisted) // final flush captures everything
			return
		case <-ticker.C:
			p.flush(&persisted)
		}
	}
}

// Close stops the persister and waits for the final flush to complete.
func (p *jobPersister) Close() {
	p.once.Do(func() { close(p.stop) })
	<-p.done
}

// applyFuzzDefaults fills in ffuf-equivalent defaults for unset fields.
func applyFuzzDefaults(cfg *FuzzConfig) {
	if cfg.Marker == "" {
		cfg.Marker = "FUZZ"
	}
	if cfg.Scheme == "" {
		cfg.Scheme = "https"
	}
	if cfg.Method == "" {
		cfg.Method = "GET"
	}
	if cfg.Threads <= 0 {
		cfg.Threads = 40
	}
	if cfg.TimeoutSec <= 0 {
		cfg.TimeoutSec = 10
	}
	if cfg.MatchCodes == "" {
		cfg.MatchCodes = "200-299,301,302,307,401,403,405,500"
	}
	if cfg.MatchMode == "" {
		cfg.MatchMode = "or"
	}
	if cfg.FilterMode == "" {
		cfg.FilterMode = "or"
	}
	if cfg.Mode == "" {
		cfg.Mode = "sniper"
	}
}

// expandPayloads applies -e extensions: for every payload, also emit payload+ext.
func expandPayloads(payloads []string, extensions string) []string {
	exts := splitCSV(extensions)
	if len(exts) == 0 {
		return payloads
	}
	out := make([]string, 0, len(payloads)*(len(exts)+1))
	for _, p := range payloads {
		out = append(out, p)
		for _, e := range exts {
			out = append(out, p+e)
		}
	}
	return out
}

func splitCSV(s string) []string {
	if strings.TrimSpace(s) == "" {
		return nil
	}
	parts := strings.Split(s, ",")
	var out []string
	for _, p := range parts {
		if t := strings.TrimSpace(p); t != "" {
			out = append(out, t)
		}
	}
	return out
}

// numRange represents a comma-separated list of numbers and ranges, e.g.
// "200-299,301,302". It's used for status/size/word/line matchers and filters.
type numRange struct {
	all    bool
	ranges [][2]int
	single map[int]bool
}

func parseNumRange(s string) *numRange {
	s = strings.TrimSpace(s)
	if s == "" {
		return nil
	}
	if s == "all" {
		return &numRange{all: true}
	}
	nr := &numRange{single: map[int]bool{}}
	for _, part := range strings.Split(s, ",") {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		if strings.Contains(part, "-") {
			bounds := strings.SplitN(part, "-", 2)
			lo, err1 := strconv.Atoi(strings.TrimSpace(bounds[0]))
			hi, err2 := strconv.Atoi(strings.TrimSpace(bounds[1]))
			if err1 == nil && err2 == nil {
				nr.ranges = append(nr.ranges, [2]int{lo, hi})
			}
		} else if n, err := strconv.Atoi(part); err == nil {
			nr.single[n] = true
		}
	}
	return nr
}

func (nr *numRange) contains(n int) bool {
	if nr == nil {
		return false
	}
	if nr.all {
		return true
	}
	if nr.single[n] {
		return true
	}
	for _, r := range nr.ranges {
		if n >= r[0] && n <= r[1] {
			return true
		}
	}
	return false
}

// ttfbMatch parses ">100"/"<100" style millisecond comparisons.
func ttfbMatch(spec string, ms int64) bool {
	spec = strings.TrimSpace(spec)
	if spec == "" {
		return false
	}
	if strings.HasPrefix(spec, ">") {
		if n, err := strconv.ParseInt(strings.TrimSpace(spec[1:]), 10, 64); err == nil {
			return ms > n
		}
	}
	if strings.HasPrefix(spec, "<") {
		if n, err := strconv.ParseInt(strings.TrimSpace(spec[1:]), 10, 64); err == nil {
			return ms < n
		}
	}
	return false
}

// specSet holds the precompiled matcher OR filter parameters: the parsed
// numRanges and compiled regex. Built once per job rather than re-parsed/
// re-compiled per result.
type specSet struct {
	codes, lines, words, size *numRange
	regex                     *regexp.Regexp
	mode                      string
}

func newSpecSet(codes, lines, words, size, regex, mode string) specSet {
	ss := specSet{
		codes: parseNumRange(codes),
		lines: parseNumRange(lines),
		words: parseNumRange(words),
		size:  parseNumRange(size),
		mode:  mode,
	}
	if strings.TrimSpace(regex) != "" {
		ss.regex, _ = regexp.Compile(regex)
	}
	return ss
}

// tests returns the boolean outcomes for each configured spec against r.
func (ss specSet) tests(r *FuzzResult) []bool {
	var tests []bool
	if ss.codes != nil {
		tests = append(tests, ss.codes.contains(r.StatusCode))
	}
	if ss.lines != nil {
		tests = append(tests, ss.lines.contains(r.Lines))
	}
	if ss.words != nil {
		tests = append(tests, ss.words.contains(r.Words))
	}
	if ss.size != nil {
		tests = append(tests, ss.size.contains(r.Length))
	}
	if ss.regex != nil {
		tests = append(tests, ss.regex.MatchString(r.bodyForRegex))
	}
	return tests
}

// fuzzMatcher is the precompiled matcher+filter for a job, built once before
// the worker loop in runFuzzJob and reused for every result.
type fuzzMatcher struct {
	match  specSet
	filter specSet
}

func newFuzzMatcher(cfg FuzzConfig) fuzzMatcher {
	return fuzzMatcher{
		match:  newSpecSet(cfg.MatchCodes, cfg.MatchLines, cfg.MatchWords, cfg.MatchSize, cfg.MatchRegex, cfg.MatchMode),
		filter: newSpecSet(cfg.FilterCodes, cfg.FilterLines, cfg.FilterWords, cfg.FilterSize, cfg.FilterRegex, cfg.FilterMode),
	}
}

// evaluate reports whether r counts as a match. Semantics are identical to the
// old evaluateMatch: matchers select (empty => everything before filtering),
// filters reject.
func (m fuzzMatcher) evaluate(r *FuzzResult) bool {
	matchTests := m.match.tests(r)
	matched := combine(matchTests, m.match.mode)
	if len(matchTests) == 0 {
		matched = true // no matchers => everything matches before filtering
	}
	if !matched {
		return false
	}
	filterTests := m.filter.tests(r)
	if len(filterTests) > 0 && combine(filterTests, m.filter.mode) {
		return false
	}
	return true
}

func combine(tests []bool, mode string) bool {
	if len(tests) == 0 {
		return false
	}
	if mode == "and" {
		for _, t := range tests {
			if !t {
				return false
			}
		}
		return true
	}
	for _, t := range tests {
		if t {
			return true
		}
	}
	return false
}

// runFuzzJob executes a fuzzing job using a worker pool of cfg.Threads workers.
func (s *HITM) runFuzzJob(ctx context.Context, cancel context.CancelFunc, job *Job, cfg FuzzConfig, payloads []string) {
	defer cancel()

	// Compile matchers/filters ONCE, not per result.
	matcher := newFuzzMatcher(cfg)

	// Batch result/progress persistence on a ticker instead of per-result.
	persister := s.newJobPersister(job)

	caCertPool, poolErr := x509.SystemCertPool()
	if poolErr != nil {
		caCertPool = x509.NewCertPool()
	}
	caCertPool.AddCert(s.CACert)

	tlsConf := &tls.Config{RootCAs: caCertPool}
	if cfg.SNI != "" {
		tlsConf.ServerName = cfg.SNI
	}
	transport := &http.Transport{
		TLSClientConfig:   tlsConf,
		ForceAttemptHTTP2: cfg.HTTP2,
	}
	if cfg.ProxyURL != "" {
		if pu, err := url.Parse(cfg.ProxyURL); err == nil {
			transport.Proxy = http.ProxyURL(pu)
		}
	}
	client := &http.Client{
		Transport: transport,
		Timeout:   time.Duration(cfg.TimeoutSec) * time.Second,
	}
	if !cfg.FollowRedir {
		client.CheckRedirect = func(*http.Request, []*http.Request) error {
			return http.ErrUseLastResponse
		}
	}

	listener := s.getListener(cfg.Listener)

	// Optional overall time limit.
	if cfg.MaxTimeSec > 0 {
		var tctx context.Context
		tctx, cancel2 := context.WithTimeout(ctx, time.Duration(cfg.MaxTimeSec)*time.Second)
		defer cancel2()
		ctx = tctx
	}

	// Optional rate limiter (req/s, shared across workers).
	var ticker *time.Ticker
	if cfg.Rate > 0 {
		ticker = time.NewTicker(time.Second / time.Duration(cfg.Rate))
		defer ticker.Stop()
	}

	jobs := make(chan int)
	var forbidden, total atomic.Int64

	// Worker pool.
	done := make(chan struct{})
	worker := func() {
		for idx := range jobs {
			if ctx.Err() != nil {
				continue
			}
			payload := payloads[idx]

			if ticker != nil {
				select {
				case <-ticker.C:
				case <-ctx.Done():
					continue
				}
			}
			if d := delayDuration(cfg, idx); d > 0 {
				select {
				case <-time.After(d):
				case <-ctx.Done():
					continue
				}
			}

			result := s.fuzzOne(ctx, client, cfg, matcher, payload, listener)

			job.ResultsMu.Lock()
			job.Results = append(job.Results, result)
			job.ResultsMu.Unlock()
			job.Done.Add(1)
			// Persistence (results + progress) is handled in batches by the
			// jobPersister ticker goroutine, not per-result.

			// Stop-on conditions.
			if cfg.StopOnErr && result.Error != "" {
				cancel()
			}
			if cfg.StopOn403 {
				total.Add(1)
				if result.StatusCode == 403 {
					forbidden.Add(1)
				}
				if total.Load() > 50 && float64(forbidden.Load())/float64(total.Load()) > 0.95 {
					log.Printf("Fuzz job %d: >95%% 403, stopping", job.ID)
					cancel()
				}
			}
		}
		done <- struct{}{}
	}

	for i := 0; i < cfg.Threads; i++ {
		go worker()
	}

	go func() {
		for i := range payloads {
			select {
			case jobs <- i:
			case <-ctx.Done():
				close(jobs)
				return
			}
		}
		close(jobs)
	}()

	for i := 0; i < cfg.Threads; i++ {
		<-done
	}

	if job.Status == "running" {
		if ctx.Err() != nil {
			job.Status = "cancelled"
		} else {
			job.Status = "completed"
		}
	}
	// Stop the persister (flushes any remaining results + final job state).
	persister.Close()
}

func delayDuration(cfg FuzzConfig, idx int) time.Duration {
	if cfg.DelayMin <= 0 && cfg.DelayMax <= 0 {
		return 0
	}
	if cfg.DelayMax <= cfg.DelayMin {
		return time.Duration(cfg.DelayMin * float64(time.Second))
	}
	// Deterministic pseudo-jitter (Math.random unavailable in some contexts; this
	// is fine for spacing requests). Vary by index.
	span := cfg.DelayMax - cfg.DelayMin
	frac := float64(idx%100) / 100.0
	return time.Duration((cfg.DelayMin + span*frac) * float64(time.Second))
}

// fuzzOne builds and sends a single fuzzed request, records it as a flow, and
// returns the evaluated result.
func (s *HITM) fuzzOne(ctx context.Context, client *http.Client, cfg FuzzConfig, matcher fuzzMatcher, payload string, listener *Listener) FuzzResult {
	result := FuzzResult{Payload: payload}

	req, err := buildFuzzRequest(cfg, payload)
	if err != nil {
		result.Error = err.Error()
		return result
	}
	req = req.WithContext(ctx)

	// Record request flow. req.Body is nil for bodyless requests (e.g. GET) —
	// io.ReadAll(nil) panics, so guard it.
	var reqBody []byte
	if req.Body != nil {
		reqBody, _ = io.ReadAll(req.Body)
		req.Body = io.NopCloser(bytes.NewBuffer(reqBody))
	}
	flowID := FlowCounter.Add(1)
	fl := &Flow{
		ID:          flowID,
		Source:      "fuzz",
		Request:     req,
		RequestBody: reqBody,
		RequestTime: time.Now(),
	}
	if listener != nil && listener.Flows != nil {
		listener.FlowMutex.Lock()
		listener.Flows[flowID] = fl
		listener.evictOldFlows()
		listener.FlowMutex.Unlock()
	}
	s.OutFlow <- *fl

	start := time.Now()
	resp, err := client.Do(req)
	result.Duration = time.Since(start).Milliseconds()
	if err != nil {
		result.Error = err.Error()
		return result
	}

	var body []byte
	if !cfg.IgnoreBody {
		body, _ = io.ReadAll(resp.Body)
	}
	resp.Body.Close()
	resp.Body = io.NopCloser(bytes.NewBuffer(body))

	result.StatusCode = resp.StatusCode
	result.Length = len(body)
	result.Lines = bytes.Count(body, []byte("\n"))
	result.Words = len(bytes.Fields(body))
	result.bodyForRegex = string(body)
	result.Matched = matcher.evaluate(&result)

	fl.Response = resp
	fl.ResponseBody = body
	fl.ResponseTime = time.Now()
	s.OutFlow <- *fl

	return result
}

// buildFuzzRequest constructs the *http.Request for one payload. If a raw
// request template is provided it is used (with marker substitution); otherwise
// the request is assembled from -u/-X/-H/-d/-b.
func buildFuzzRequest(cfg FuzzConfig, payload string) (*http.Request, error) {
	if strings.TrimSpace(cfg.RawRequest) != "" {
		raw := normalizeRawRequest(strings.ReplaceAll(cfg.RawRequest, cfg.Marker, payload))
		req, err := http.ReadRequest(bufio.NewReader(strings.NewReader(raw)))
		if err != nil {
			return nil, err
		}
		req.RequestURI = ""
		if req.URL.Scheme == "" {
			req.URL.Scheme = cfg.Scheme
		}
		if req.URL.Host == "" {
			req.URL.Host = req.Host
		}
		if req.URL.Host == "" {
			req.URL.Host = cfg.Host
		}
		applyExtraHTTPOptions(req, cfg, payload)
		return req, nil
	}

	// Build from -u/-X/-d.
	target := strings.ReplaceAll(cfg.URL, cfg.Marker, payload)
	var bodyReader io.Reader
	if cfg.Data != "" {
		bodyReader = strings.NewReader(strings.ReplaceAll(cfg.Data, cfg.Marker, payload))
	}
	req, err := http.NewRequest(cfg.Method, target, bodyReader)
	if err != nil {
		return nil, err
	}
	applyExtraHTTPOptions(req, cfg, payload)
	return req, nil
}

func applyExtraHTTPOptions(req *http.Request, cfg FuzzConfig, payload string) {
	for _, h := range cfg.Headers {
		parts := strings.SplitN(h, ":", 2)
		if len(parts) == 2 {
			name := strings.TrimSpace(parts[0])
			val := strings.ReplaceAll(strings.TrimSpace(parts[1]), cfg.Marker, payload)
			if strings.EqualFold(name, "Host") {
				req.Host = val
			} else {
				req.Header.Set(name, val)
			}
		}
	}
	if cfg.Cookies != "" {
		req.Header.Set("Cookie", strings.ReplaceAll(cfg.Cookies, cfg.Marker, payload))
	}
}
