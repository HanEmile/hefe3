package main

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"sync"
	"time"
)

// SpiderConfig holds gospider-style crawl options.
type SpiderConfig struct {
	Sites           []string `json:"sites"`           // -s / -S
	ProxyURL        string   `json:"proxyURL"`        // -p
	UserAgent       string   `json:"userAgent"`       // -u (web|mobi|custom string)
	Cookie          string   `json:"cookie"`          // --cookie
	Headers         []string `json:"headers"`         // -H
	Blacklist       string   `json:"blacklist"`       // --blacklist regex
	Whitelist       string   `json:"whitelist"`       // --whitelist regex
	WhitelistDomain string   `json:"whitelistDomain"` // --whitelist-domain
	Threads         int      `json:"threads"`         // -t (parallel sites)
	Concurrent      int      `json:"concurrent"`      // -c (per-domain concurrency)
	Depth           int      `json:"depth"`           // -d (0 = infinite)
	DelaySec        int      `json:"delaySec"`        // -k
	RandomDelaySec  int      `json:"randomDelaySec"`  // -K
	TimeoutSec      int      `json:"timeoutSec"`      // -m
	BaseOnly        bool     `json:"baseOnly"`        // -B (HTML only)
	JS              bool     `json:"js"`              // --js linkfinder
	Subs            bool     `json:"subs"`            // --subs
	Sitemap         bool     `json:"sitemap"`         // --sitemap
	Robots          bool     `json:"robots"`          // --robots
	NoRedirect      bool     `json:"noRedirect"`      // --no-redirect
	Listener        string   `json:"listener"`
	MaxURLs         int      `json:"maxUrls"` // safety cap
}

const (
	uaWeb  = "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/120.0 Safari/537.36"
	uaMobi = "Mozilla/5.0 (iPhone; CPU iPhone OS 17_0 like Mac OS X) AppleWebKit/605.1.15 (KHTML, like Gecko) Version/17.0 Mobile/15E148 Safari/604.1"
)

var (
	hrefRe = regexp.MustCompile(`(?i)(?:href|src|action)\s*=\s*["']([^"'<>]+)["']`)
	// linkfinder-style endpoints inside JS
	jsLinkRe = regexp.MustCompile(`(?i)["'\x60]((?:/|\.\./|\./)[^"'\x60\s]{2,}|https?://[^"'\x60\s]+)["'\x60]`)
)

func applySpiderDefaults(cfg *SpiderConfig) {
	if cfg.Threads <= 0 {
		cfg.Threads = 1
	}
	if cfg.Concurrent <= 0 {
		cfg.Concurrent = 5
	}
	if cfg.Depth == 0 {
		cfg.Depth = 1 // gospider default; 0 in UI maps to infinite via -1 sentinel below
	}
	if cfg.TimeoutSec <= 0 {
		cfg.TimeoutSec = 10
	}
	if cfg.UserAgent == "" {
		cfg.UserAgent = "web"
	}
	if cfg.MaxURLs <= 0 {
		cfg.MaxURLs = 5000
	}
}

func resolveUA(ua string) string {
	switch ua {
	case "web", "":
		return uaWeb
	case "mobi":
		return uaMobi
	default:
		return ua
	}
}

// runSpiderJob crawls the configured sites, emitting each fetched URL as a flow
// and recording it in the job results.
func (s *HITM) runSpiderJob(ctx context.Context, cancel context.CancelFunc, job *Job, cfg SpiderConfig) {
	defer cancel()

	// Batch result/progress persistence on a ticker instead of per-result.
	persister := s.newJobPersister(job)

	caCertPool, poolErr := x509.SystemCertPool()
	if poolErr != nil {
		caCertPool = x509.NewCertPool()
	}
	caCertPool.AddCert(s.CACert)

	transport := &http.Transport{TLSClientConfig: &tls.Config{RootCAs: caCertPool}}
	if cfg.ProxyURL != "" {
		if pu, err := url.Parse(cfg.ProxyURL); err == nil {
			transport.Proxy = http.ProxyURL(pu)
		}
	}
	client := &http.Client{
		Transport: transport,
		Timeout:   time.Duration(cfg.TimeoutSec) * time.Second,
	}
	if cfg.NoRedirect {
		client.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	}

	var blacklistRe, whitelistRe *regexp.Regexp
	if cfg.Blacklist != "" {
		blacklistRe, _ = regexp.Compile(cfg.Blacklist)
	}
	if cfg.Whitelist != "" {
		whitelistRe, _ = regexp.Compile(cfg.Whitelist)
	}

	listener := s.getListener(cfg.Listener)

	var (
		mu      sync.Mutex
		visited = map[string]bool{}
		count   int
	)

	type item struct {
		url   string
		depth int
	}
	queue := make(chan item, 1024)
	var wg sync.WaitGroup
	sem := make(chan struct{}, cfg.Concurrent)

	inFlight := &sync.WaitGroup{}

	enqueue := func(u string, depth int) {
		mu.Lock()
		defer mu.Unlock()
		if visited[u] || count >= cfg.MaxURLs {
			return
		}
		// scope checks
		if blacklistRe != nil && blacklistRe.MatchString(u) {
			return
		}
		if whitelistRe != nil && !whitelistRe.MatchString(u) {
			return
		}
		if cfg.WhitelistDomain != "" {
			if pu, err := url.Parse(u); err != nil || !strings.Contains(pu.Host, cfg.WhitelistDomain) {
				return
			}
		}
		visited[u] = true
		count++
		inFlight.Add(1)
		select {
		case queue <- item{u, depth}:
		default:
			// queue full; spin a goroutine to avoid deadlock
			go func() { queue <- item{u, depth} }()
		}
	}

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
		enqueue(site, 0)
	}

	inScope := func(raw string) bool {
		pu, err := url.Parse(raw)
		if err != nil {
			return false
		}
		if cfg.Subs {
			for h := range seedHosts {
				base := h
				if i := strings.Index(h, ":"); i != -1 {
					base = h[:i]
				}
				if strings.HasSuffix(pu.Hostname(), base) {
					return true
				}
			}
			return false
		}
		return seedHosts[pu.Host]
	}

	worker := func() {
		defer wg.Done()
		for it := range queue {
			func() {
				defer inFlight.Done()
				if ctx.Err() != nil {
					return
				}
				sem <- struct{}{}
				defer func() { <-sem }()

				if cfg.DelaySec > 0 {
					select {
					case <-time.After(time.Duration(cfg.DelaySec) * time.Second):
					case <-ctx.Done():
						return
					}
				}

				body, status, ctype, dur := s.spiderFetch(ctx, client, cfg, it.url, listener)

				res := FuzzResult{
					Payload:    it.url,
					StatusCode: status,
					Length:     len(body),
					Lines:      strings.Count(string(body), "\n"),
					Words:      len(strings.Fields(string(body))),
					Duration:   dur,
					Matched:    status >= 200 && status < 400,
				}
				job.ResultsMu.Lock()
				job.Results = append(job.Results, res)
				job.Total = len(job.Results)
				job.ResultsMu.Unlock()
				job.Done.Add(1)
				// Persistence handled in batches by the jobPersister ticker.

				// Recurse if within depth (depth<0 means infinite in UI mapping).
				maxDepth := cfg.Depth
				if maxDepth >= 0 && it.depth >= maxDepth {
					return
				}
				isJS := strings.Contains(ctype, "javascript") || strings.HasSuffix(it.url, ".js")
				if cfg.BaseOnly && isJS {
					return
				}
				for _, link := range extractLinks(it.url, string(body), isJS, cfg.JS) {
					if inScope(link) {
						enqueue(link, it.depth+1)
					}
				}
			}()
		}
	}

	for i := 0; i < cfg.Threads*cfg.Concurrent+1; i++ {
		wg.Add(1)
		go worker()
	}

	// Close the queue once all in-flight work drains.
	go func() {
		inFlight.Wait()
		close(queue)
	}()

	wg.Wait()

	if job.Status == "running" {
		if ctx.Err() != nil {
			job.Status = "cancelled"
		} else {
			job.Status = "completed"
		}
	}
	persister.Close() // flush remaining results + final job state
}

func (s *HITM) spiderFetch(ctx context.Context, client *http.Client, cfg SpiderConfig, target string, listener *Listener) ([]byte, int, string, int64) {
	req, err := http.NewRequestWithContext(ctx, "GET", target, nil)
	if err != nil {
		return nil, 0, "", 0
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
	fl := &Flow{ID: flowID, Source: "spider", Request: req, RequestTime: time.Now()}
	if listener != nil && listener.Flows != nil {
		listener.FlowMutex.Lock()
		listener.Flows[flowID] = fl
		listener.evictOldFlows()
		listener.FlowMutex.Unlock()
	}
	s.OutFlow <- *fl

	start := time.Now()
	resp, err := client.Do(req)
	dur := time.Since(start).Milliseconds()
	if err != nil {
		return nil, 0, "", dur
	}
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 10*1024*1024))
	resp.Body.Close()
	resp.Body = io.NopCloser(strings.NewReader(string(body)))

	fl.Response = resp
	fl.ResponseBody = body
	fl.ResponseTime = time.Now()
	s.OutFlow <- *fl

	return body, resp.StatusCode, resp.Header.Get("Content-Type"), dur
}

// extractLinks pulls links from HTML (href/src/action) and, when js is enabled
// and the document is JS, linkfinder-style endpoints. All links are resolved
// against base.
func extractLinks(base, body string, isJS, jsEnabled bool) []string {
	bu, err := url.Parse(base)
	if err != nil {
		return nil
	}
	var raw []string
	if isJS {
		if jsEnabled {
			for _, m := range jsLinkRe.FindAllStringSubmatch(body, -1) {
				raw = append(raw, m[1])
			}
		}
	} else {
		for _, m := range hrefRe.FindAllStringSubmatch(body, -1) {
			raw = append(raw, m[1])
		}
	}

	seen := map[string]bool{}
	var out []string
	for _, r := range raw {
		r = strings.TrimSpace(r)
		if r == "" || strings.HasPrefix(r, "javascript:") || strings.HasPrefix(r, "mailto:") || strings.HasPrefix(r, "#") || strings.HasPrefix(r, "data:") {
			continue
		}
		ru, err := url.Parse(r)
		if err != nil {
			continue
		}
		abs := bu.ResolveReference(ru)
		abs.Fragment = ""
		s := abs.String()
		if !seen[s] {
			seen[s] = true
			out = append(out, s)
		}
	}
	return out
}
