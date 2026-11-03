package main

// kernelorg.go - tracks the latest upstream kernel releases so the dashboard
// can show how far each host's booted kernel is behind its own branch.
//
// Data: https://www.kernel.org/releases.json, refreshed hourly, cached on
// disk (STATUS_BOARD_KERNELCACHE, default /var/lib/status-board/kernelorg.json)
// so restarts and kernel.org outages degrade to "n/a" cells instead of errors.

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"sync"
	"time"
)

// KernelRow is the per-host freshness line rendered in the System section.
type KernelRow struct {
	Name    string `json:"name"`
	Kernel  string `json:"kernel"`  // booted release, e.g. "6.12.108"
	Latest  string `json:"latest"`  // latest upstream release of that branch
	Behind  int    `json:"behind"`  // patch levels behind; -1 = unknown
	EOL     bool   `json:"eol"`     // branch no longer maintained upstream
	Level   string `json:"level"`   // ok | warn | bad | unknown
	FleetUp string `json:"fleetup"` // last nightly-upgrade outcome for this host, "" if none
}

// FleetStatus is written by the fleet-upgrade service (on lampadas) and
// shipped to medano after every nightly run; status-board merely renders it.
type FleetStatus struct {
	Pin        string            `json:"pin"`         // nixos-26.05 release used, e.g. "26.05.9199.abc123"
	StartedAt  string            `json:"started_at"`  // RFC3339
	FinishedAt string            `json:"finished_at"` // RFC3339
	Hosts      []FleetHostResult `json:"hosts"`
}

// FleetHostResult is one host's outcome of the last nightly run.
type FleetHostResult struct {
	Name     string `json:"name"`
	Deployed string `json:"deployed"` // ok | fail | skip
	Rebooted string `json:"rebooted"` // yes | no | fail
	BackUp   string `json:"back_up"`  // ok | fail | skip ("" when unrebooted)
	Error    string `json:"error,omitempty"`
}

var (
	kernelCachePath = "/var/lib/status-board/kernelorg.json"
	fleetStatusPath = "/var/lib/status-board/fleet.json"

	kernelMu       sync.RWMutex
	kernelReleases = map[string]kernelRelease{}

	// booted kernels + upstream versions: branch is the first two components,
	// patch the (optional, "7.3" mainline releases) third.
	bootedRe = regexp.MustCompile(`^(\d+\.\d+)(?:\.(\d+))?`)
)

type kernelRelease struct {
	Version string `json:"version"`
	EOL     bool   `json:"eol"`
}

// kernelorgJSON mirrors the parts of www.kernel.org/releases.json we use.
type kernelorgJSON struct {
	Releases []struct {
		Moniker string `json:"moniker"`
		Version string `json:"version"`
		ISEOL   bool   `json:"iseol"`
	} `json:"releases"`
}

func init() {
	if env := os.Getenv("STATUS_BOARD_KERNELCACHE"); env != "" {
		kernelCachePath = env
	}
	if env := os.Getenv("STATUS_BOARD_FLEET"); env != "" {
		fleetStatusPath = env
	}
}

// startKernelFetcher seeds from disk, then refreshes hourly.
func startKernelFetcher() {
	if b, err := os.ReadFile(kernelCachePath); err == nil {
		if parseKernelorg(b) {
			log.Printf("kernel releases: seeded %d branches from cache", len(kernelReleases))
		}
	}
	go func() {
		for {
			if err := fetchKernelorg(); err != nil {
				log.Printf("kernel.org fetch: %v (keeping cache)", err)
			}
			time.Sleep(time.Hour)
		}
	}()
}

func fetchKernelorg() error {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, "https://www.kernel.org/releases.json", nil)
	if err != nil {
		return err
	}
	resp, err := httpClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return &httpError{resp.StatusCode}
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return err
	}
	if !parseKernelorg(body) {
		return errParse
	}
	if err := os.MkdirAll(filepath.Dir(kernelCachePath), 0o755); err != nil {
		return err
	}
	return os.WriteFile(kernelCachePath, body, 0o644)
}

type httpError struct{ code int }

func (e *httpError) Error() string { return "kernel.org: HTTP " + strconv.Itoa(e.code) }

var errParse = errors.New("kernel.org: unparseable payload")

// parseKernelorg replaces kernelReleases with the latest release of every
// numeric branch ("6.12", "7.3", ...) listed upstream. releases.json groups
// by CATEGORY (moniker mainline/stable/longterm), so the branch is derived
// from the version itself. The list is newest-first overall, so the first
// occurrence of a branch is its newest release.
func parseKernelorg(body []byte) bool {
	var kj kernelorgJSON
	if err := json.Unmarshal(body, &kj); err != nil || len(kj.Releases) == 0 {
		return false
	}
	m := make(map[string]kernelRelease, len(kj.Releases))
	for _, r := range kj.Releases {
		v := bootedRe.FindStringSubmatch(r.Version)
		if v == nil {
			continue
		}
		branch := v[1]
		if _, exists := m[branch]; !exists {
			m[branch] = kernelRelease{Version: r.Version, EOL: r.ISEOL}
		}
	}
	if len(m) == 0 {
		return false
	}
	kernelMu.Lock()
	kernelReleases = m
	kernelMu.Unlock()
	return true
}

// freshnessFor turns a booted kernel release ("6.12.108") into a KernelRow.
func freshnessFor(name, booted string, fleetUp string) KernelRow {
	row := KernelRow{Name: name, Kernel: booted, Behind: -1, Level: "unknown", FleetUp: fleetUp}
	m := bootedRe.FindStringSubmatch(booted)
	if m == nil {
		return row
	}
	branch := m[1]
	patch := 0
	if m[2] != "" {
		var err error
		patch, err = strconv.Atoi(m[2])
		if err != nil {
			return row
		}
	}
	kernelMu.RLock()
	rel, ok := kernelReleases[branch]
	kernelMu.RUnlock()
	if !ok {
		// branch not listed upstream at all - almost certainly EOL'd out of the JSON
		row.EOL = true
		row.Level = "bad"
		return row
	}
	row.Latest = rel.Version
	row.EOL = rel.EOL
	latestPatch := 0
	if lm := bootedRe.FindStringSubmatch(rel.Version); lm != nil && lm[2] != "" {
		p, err := strconv.Atoi(lm[2])
		if err != nil {
			return row
		}
		latestPatch = p
	}
	row.Behind = latestPatch - patch
	if row.Behind < 0 {
		row.Behind = 0 // ahead of upstream listing (rc/next) - fine
	}
	switch {
	case row.EOL || row.Behind > 10:
		row.Level = "bad"
	case row.Behind > 3:
		row.Level = "warn"
	default:
		row.Level = "ok"
	}
	return row
}

// loadFleetStatus reads the last nightly-upgrade result, or nil when absent.
func loadFleetStatus() *FleetStatus {
	b, err := os.ReadFile(fleetStatusPath)
	if err != nil {
		return nil
	}
	var fs FleetStatus
	if err := json.Unmarshal(b, &fs); err != nil || len(fs.Hosts) == 0 {
		return nil
	}
	return &fs
}
