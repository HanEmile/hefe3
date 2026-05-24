// status-board: tiny dashboard for the medano fleet.
//
// Polls each VM's node-exporter, asks virsh for libvirt state, walks the
// storagebox restic dirs for backup freshness, renders one HTML page.
//
// Env:
//   STATUS_BOARD_INVENTORY -- path to JSON file with [{name, ip, bridge}]
//   STATUS_BOARD_LISTEN    -- bind address:port (default 127.0.0.1:8090)
//   STATUS_BOARD_STORAGEBOX -- base path of restic repos (default /mnt/storagebox-bx11/backup)
package main

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"html"
	"html/template"
	"io"
	"log"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

type VM struct {
	Name   string `json:"name"`
	IP     string `json:"ip"`
	Bridge string `json:"bridge"`
}

type Probe struct {
	Name     string
	URL      string
	Up       bool
	HTTPCode int
}

type Restic struct {
	Configured  bool     // is vmBackups.enable true in nix for this VM?
	Exists      string
	Snapshots   int
	AgeHours    float64  // hours since most recent snapshot
	OldestHours float64  // hours since OLDEST snapshot — shows retention depth
	Size        string   // du -shx on data/
	RepoPath    string   // where the restic repo lives on the storagebox
	// Bucketed counts derived from snapshot mtimes — no extra IO needed.
	Last24h   int
	Last7d    int
	Last30d   int
	Older     int
}

// BackupTarget is one place a path is backed up to (typically a restic repo).
// Each VM-path-target tuple corresponds to one physical repo today; modelled
// as a list to allow adding cold archive / second-site targets later without
// changing this code.
type BackupTarget struct {
	Kind     string // "restic"
	Label    string // human label e.g. "storagebox-bx11"
	Repo     string // filesystem path of the repo

	// Snapshot stats for this target repo (counts across all paths in the
	// repo - restic keeps them in one snapshots/ dir per repo).
	Exists      string // yes | no | no-snaps
	Snapshots   int
	AgeHours    float64
	OldestHours float64
	Size        string
	Last24h     int
	Last7d      int
	Last30d     int
	Older       int
}

// BackupPath is one backed-up directory inside a VM, with one or more targets.
type BackupPath struct {
	Path    string
	Targets []BackupTarget
}

// BackupView is the per-VM rollup used by the Backups tab.
type BackupView struct {
	Configured bool
	Paths      []BackupPath
}

type Storagebox struct {
	Reachable bool
	Total     int64 // bytes
	Used      int64
	Avail     int64
	UsedPct   float64
}

// Zpool holds one ZFS pool's capacity stats (read via `zpool list -Hp`).
type Zpool struct {
	Name    string
	Total   int64
	Alloc   int64
	Free    int64
	UsedPct float64
}

type Capacity struct {
	// disk
	FsTotal int64
	FsAvail int64
	FsUsed  int64
	FsPct   float64
	// memory
	MemTotal int64
	MemAvail int64
	MemUsed  int64
	MemPct   float64
	// trend (filled in if we have enough samples; else zero)
	DaysUntilFsFull  float64 // -1 = unknown
	DaysUntilMemFull float64 // -1 = unknown
}

type VirshInfo struct {
	State      string
	MaxMemKiB  int64
	UsedMemKiB int64
	VCPUs      string
}

type VMStat struct {
	VM
	Reachable bool
	Probes    []Probe
	MemTotal  int64
	MemUsed   int64
	Virsh     VirshInfo
	Restic    Restic
	Backups   BackupView
	Capacity  Capacity

	// Extra metrics for the Overview tab. Zero when not reachable.
	Load1     float64
	CPUCount  int
	CPUBusy   float64 // 0..1 over last sample (fraction of CPU not idle)
	NetRxBps  float64 // bytes/sec since last scrape on primary iface
	NetTxBps  float64
	NetRxTot  int64
	NetTxTot  int64
	TSUp      bool    // tailscale interface present & up
}

// fleetOverview is aggregate state computed from the scrape pass.
// It is recomputed on every collectAll() and used by the Overview tab.
type fleetOverview struct {
	VMsTotal        int
	VMsReachable    int
	TSUp            int
	TSDown          int
	LoadSum         float64
	CPUCountSum     int
	CPUBusyAvg      float64 // mean across reachable
	MemUsedSum      int64
	MemTotalSum     int64
	FsUsedSum       int64
	FsTotalSum      int64
	NetRxBps        float64
	NetTxBps        float64
	BackupsOK       int
	BackupsWarn     int
	BackupsBad      int
	BackupsOff      int
}

var (
	inventory     []VM
	backupsEnabled = map[string]bool{}
	backupPaths    = map[string][]string{}
	backupTargets  = map[string][]BackupTarget{}
	zfsPoolNames   []string
	listenAddr    = "127.0.0.1:8090"
	storageboxDir = "/mnt/storagebox-bx11"
	storageboxBackupDir = "/mnt/storagebox-bx11/backup"
	samplesPath   = "/var/lib/status-board/samples.jsonl"
	metricLine    = regexp.MustCompile(`^([a-zA-Z_:][a-zA-Z0-9_:]*)(\{[^}]*\})?\s+([-0-9.eE+]+|NaN|\+Inf|-Inf)\s*$`)
	labelKV       = regexp.MustCompile(`([a-zA-Z_][a-zA-Z0-9_]*)="((?:[^"\\]|\\.)*)"`)
	httpClient    = &http.Client{Timeout: 3 * time.Second}

	// Previous per-VM network counters, used to compute deltas across scrapes.
	netPrevMu sync.Mutex
	netPrev   = map[string]struct {
		Rx, Tx int64
		Ts     time.Time
	}{}

	// Cache the last scrape so the SSE handler doesn't fan out collectAll()
	// on every connected client tick. collectAllLocked serializes refreshes.
	collectMu      sync.Mutex
	cachedVMs      []VMStat
	cachedSB       Storagebox
	cachedZPools   []Zpool
	cachedFleet    fleetOverview
	cachedAt       time.Time
)

// One sample per (vm, kind) per scrape — kept on disk so we have history
// across restarts and can fit trend lines for "days until full".
type sample struct {
	Ts    int64  `json:"ts"`
	Vm    string `json:"vm"`
	Kind  string `json:"kind"`  // "fs" | "mem"
	Used  int64  `json:"used"`
	Total int64  `json:"total"`
}

// Append a sample line to samplesPath (creating the dir if needed). Best-effort.
func appendSample(s sample) {
	_ = os.MkdirAll(filepath.Dir(samplesPath), 0o755)
	f, err := os.OpenFile(samplesPath, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return
	}
	defer f.Close()
	b, _ := json.Marshal(s)
	f.Write(append(b, '\n'))
}

// Read recent samples for a given (vm, kind). Returns chronological slice.
func readSamples(vm, kind string, sinceHours float64) []sample {
	f, err := os.Open(samplesPath)
	if err != nil {
		return nil
	}
	defer f.Close()
	cutoff := time.Now().Add(-time.Duration(sinceHours) * time.Hour).Unix()
	scan := bufio.NewScanner(f)
	scan.Buffer(make([]byte, 0, 4096), 1024*1024)
	var out []sample
	for scan.Scan() {
		var s sample
		if err := json.Unmarshal(scan.Bytes(), &s); err != nil {
			continue
		}
		if s.Vm != vm || s.Kind != kind || s.Ts < cutoff {
			continue
		}
		out = append(out, s)
	}
	return out
}

// forecastDaysToFull fits a least-squares line through recent samples and
// extrapolates to 100% used. Returns -1 if we don't have enough data
// (need at least 4 samples spanning at least 6h) or if usage is shrinking.
func forecastDaysToFull(vm, kind string, currentUsed, total int64) float64 {
	if total <= 0 {
		return -1
	}
	hist := readSamples(vm, kind, 7*24)
	if len(hist) < 4 {
		return -1
	}
	if hist[len(hist)-1].Ts-hist[0].Ts < 6*3600 {
		return -1
	}
	// fit Used vs Ts (seconds): linear regression, then solve for ts where Used == total.
	var sumX, sumY, sumXY, sumXX float64
	n := float64(len(hist))
	t0 := float64(hist[0].Ts)
	for _, h := range hist {
		x := float64(h.Ts) - t0
		y := float64(h.Used)
		sumX += x
		sumY += y
		sumXY += x * y
		sumXX += x * x
	}
	denom := n*sumXX - sumX*sumX
	if denom == 0 {
		return -1
	}
	slope := (n*sumXY - sumX*sumY) / denom
	intercept := (sumY - slope*sumX) / n
	if slope <= 0 {
		return -1 // usage stable or shrinking
	}
	tFull := (float64(total) - intercept) / slope
	tNow := float64(time.Now().Unix()) - t0
	if tFull <= tNow {
		return 0 // already full at current trend (shouldn't happen if currentUsed < total)
	}
	return (tFull - tNow) / 86400
}

func loadInventory() error {
	path := os.Getenv("STATUS_BOARD_INVENTORY")
	if path == "" {
		return fmt.Errorf("STATUS_BOARD_INVENTORY not set")
	}
	body, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	return json.Unmarshal(body, &inventory)
}

type metricRow struct {
	Labels map[string]string
	Value  float64
}

func fetchMetrics(ip string) (map[string][]metricRow, error) {
	url := fmt.Sprintf("http://%s:9100/metrics", ip)
	resp, err := httpClient.Get(url)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	out := map[string][]metricRow{}
	for _, line := range strings.Split(string(body), "\n") {
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		m := metricLine.FindStringSubmatch(line)
		if m == nil {
			continue
		}
		name, rawLabels, value := m[1], m[2], m[3]
		v, err := strconv.ParseFloat(value, 64)
		if err != nil {
			continue
		}
		labels := map[string]string{}
		if rawLabels != "" {
			for _, lm := range labelKV.FindAllStringSubmatch(rawLabels, -1) {
				labels[lm[1]] = lm[2]
			}
		}
		out[name] = append(out[name], metricRow{Labels: labels, Value: v})
	}
	return out, nil
}

func virshState() map[string]VirshInfo {
	state := map[string]VirshInfo{}
	out, err := exec.Command("virsh", "list", "--all", "--name").Output()
	if err != nil {
		return state
	}
	for _, name := range strings.Split(strings.TrimSpace(string(out)), "\n") {
		name = strings.TrimSpace(name)
		if name == "" {
			continue
		}
		info, err := exec.Command("virsh", "dominfo", name).Output()
		if err != nil {
			continue
		}
		vi := VirshInfo{}
		for _, line := range strings.Split(string(info), "\n") {
			parts := strings.SplitN(line, ":", 2)
			if len(parts) != 2 {
				continue
			}
			k := strings.ToLower(strings.TrimSpace(parts[0]))
			v := strings.TrimSpace(parts[1])
			switch k {
			case "state":
				vi.State = v
			case "max memory":
				fields := strings.Fields(v)
				if len(fields) > 0 {
					if n, err := strconv.ParseInt(fields[0], 10, 64); err == nil {
						vi.MaxMemKiB = n
					}
				}
			case "used memory":
				fields := strings.Fields(v)
				if len(fields) > 0 {
					if n, err := strconv.ParseInt(fields[0], 10, 64); err == nil {
						vi.UsedMemKiB = n
					}
				}
			case "cpu(s)":
				vi.VCPUs = v
			}
		}
		state[name] = vi
	}
	return state
}

func resticFor(vm string) Restic {
	repo := filepath.Join(storageboxBackupDir, vm)
	r := Restic{Configured: backupsEnabled[vm], Exists: "no", RepoPath: repo}
	// Skip the stat+du path entirely for VMs without configured backups -
	// the storagebox is CIFS-over-autofs and 20 parallel stats can wedge
	// it for tens of seconds.
	if !r.Configured {
		return r
	}
	if _, err := os.Stat(repo); err != nil {
		return r
	}
	snapsDir := filepath.Join(repo, "snapshots")
	entries, err := os.ReadDir(snapsDir)
	if err != nil {
		r.Exists = "no-snaps"
		return r
	}
	var latest, oldest time.Time
	count := 0
	now := time.Now()
	for _, e := range entries {
		info, err := e.Info()
		if err != nil {
			continue
		}
		count++
		mt := info.ModTime()
		if mt.After(latest) {
			latest = mt
		}
		if oldest.IsZero() || mt.Before(oldest) {
			oldest = mt
		}
		ageH := now.Sub(mt).Hours()
		switch {
		case ageH < 24:
			r.Last24h++
		case ageH < 24*7:
			r.Last7d++
		case ageH < 24*30:
			r.Last30d++
		default:
			r.Older++
		}
	}
	r.Exists = "yes"
	r.Snapshots = count
	if count > 0 {
		r.AgeHours = time.Since(latest).Hours()
		r.OldestHours = time.Since(oldest).Hours()
	}
	// 'du -shx' over the storagebox CIFS mount stalls under concurrent load
	// (one du per VM in parallel during a page render = wedged mount). Wrap
	// in a short context so a slow scan doesn't hold the entire dashboard
	// hostage.
	ctx, cancel := context.WithTimeout(context.Background(), 1500*time.Millisecond)
	defer cancel()
	if out, err := exec.CommandContext(ctx, "du", "-shx", filepath.Join(repo, "data")).Output(); err == nil {
		fields := strings.Fields(string(out))
		if len(fields) > 0 {
			r.Size = fields[0]
		}
	}
	return r
}

// scanTargetRepo populates snapshot stats for one BackupTarget by walking
// its repo on disk. Mirrors resticFor's per-repo logic so the two stay in
// sync. Returns target with stats filled in (or Exists="no" / "no-snaps").
func scanTargetRepo(t BackupTarget) BackupTarget {
	t.Exists = "no"
	if t.Repo == "" {
		return t
	}
	if _, err := os.Stat(t.Repo); err != nil {
		return t
	}
	snapsDir := filepath.Join(t.Repo, "snapshots")
	entries, err := os.ReadDir(snapsDir)
	if err != nil {
		t.Exists = "no-snaps"
		return t
	}
	var latest, oldest time.Time
	count := 0
	now := time.Now()
	for _, e := range entries {
		info, err := e.Info()
		if err != nil {
			continue
		}
		count++
		mt := info.ModTime()
		if mt.After(latest) {
			latest = mt
		}
		if oldest.IsZero() || mt.Before(oldest) {
			oldest = mt
		}
		ageH := now.Sub(mt).Hours()
		switch {
		case ageH < 24:
			t.Last24h++
		case ageH < 24*7:
			t.Last7d++
		case ageH < 24*30:
			t.Last30d++
		default:
			t.Older++
		}
	}
	t.Exists = "yes"
	t.Snapshots = count
	if count > 0 {
		t.AgeHours = time.Since(latest).Hours()
		t.OldestHours = time.Since(oldest).Hours()
	}
	ctx, cancel := context.WithTimeout(context.Background(), 1500*time.Millisecond)
	defer cancel()
	if out, err := exec.CommandContext(ctx, "du", "-shx", filepath.Join(t.Repo, "data")).Output(); err == nil {
		fields := strings.Fields(string(out))
		if len(fields) > 0 {
			t.Size = fields[0]
		}
	}
	return t
}

// backupViewFor builds the nested per-VM Backups view (paths -> targets).
// Today every VM has one target (storagebox) and the same target repo holds
// snapshots for every path within that VM, so we scan each target once and
// reuse its stats across the VM's paths. When a future config adds a
// per-path or per-target repo, scanTargetRepo will read it independently.
func backupViewFor(vm string) BackupView {
	bv := BackupView{Configured: backupsEnabled[vm]}
	if !bv.Configured {
		return bv
	}
	paths := backupPaths[vm]
	rawTargets := backupTargets[vm]
	scanned := make(map[string]BackupTarget, len(rawTargets))
	for _, t := range rawTargets {
		if _, ok := scanned[t.Repo]; ok {
			continue
		}
		scanned[t.Repo] = scanTargetRepo(t)
	}
	if len(paths) == 0 {
		paths = []string{"(no paths declared)"}
	}
	for _, path := range paths {
		bp := BackupPath{Path: path}
		for _, t := range rawTargets {
			bp.Targets = append(bp.Targets, scanned[t.Repo])
		}
		bv.Paths = append(bv.Paths, bp)
	}
	return bv
}

// zpoolsStatus reads `zpool list -Hp` for the configured zpools and
// returns capacity/usage. Pools listed in zfsPoolNames but missing on
// the host are silently skipped (no-op).
func zpoolsStatus() []Zpool {
	if len(zfsPoolNames) == 0 {
		return nil
	}
	args := append([]string{"list", "-Hp", "-o", "name,size,alloc,free"}, zfsPoolNames...)
	out, err := exec.Command("zpool", args...).Output()
	if err != nil {
		return nil
	}
	var pools []Zpool
	for _, line := range strings.Split(strings.TrimSpace(string(out)), "\n") {
		fields := strings.Fields(line)
		if len(fields) < 4 {
			continue
		}
		p := Zpool{Name: fields[0]}
		p.Total, _ = strconv.ParseInt(fields[1], 10, 64)
		p.Alloc, _ = strconv.ParseInt(fields[2], 10, 64)
		p.Free, _ = strconv.ParseInt(fields[3], 10, 64)
		if p.Total > 0 {
			p.UsedPct = float64(p.Alloc) / float64(p.Total) * 100
		}
		pools = append(pools, p)
	}
	return pools
}

// Storagebox-wide free space probe (df on the CIFS mount).
// The mount is autofs; if nothing has touched the path recently the
// kernel hasn't actually mounted it yet, so df returns the parent fs.
// A simple readdir forces autofs to mount before we measure.
func storageboxStatus() Storagebox {
	sb := Storagebox{}
	_, _ = os.ReadDir(storageboxDir) // wake autofs
	out, err := exec.Command("df", "-B1", "--output=size,used,avail,pcent", storageboxDir).Output()
	if err != nil {
		return sb
	}
	// Two lines: header + values. Avail is value[2], expressed in bytes.
	lines := strings.Split(strings.TrimSpace(string(out)), "\n")
	if len(lines) < 2 {
		return sb
	}
	fields := strings.Fields(lines[1])
	if len(fields) < 4 {
		return sb
	}
	sb.Reachable = true
	sb.Total, _ = strconv.ParseInt(fields[0], 10, 64)
	sb.Used, _ = strconv.ParseInt(fields[1], 10, 64)
	sb.Avail, _ = strconv.ParseInt(fields[2], 10, 64)
	pct := strings.TrimSuffix(fields[3], "%")
	sb.UsedPct, _ = strconv.ParseFloat(pct, 64)
	return sb
}

// Capacity computes filesystem + memory utilisation for a VM from its
// node-exporter metrics. The DaysUntilFull fields are -1 unless we have
// enough samples in /var/lib/status-board/samples.jsonl to fit a trend.
func capacityFor(vm string, metrics map[string][]metricRow) Capacity {
	cap := Capacity{DaysUntilFsFull: -1, DaysUntilMemFull: -1}
	// root fs (mountpoint="/", fstype != tmpfs)
	for _, row := range metrics["node_filesystem_size_bytes"] {
		if row.Labels["mountpoint"] == "/" && row.Labels["fstype"] != "tmpfs" {
			cap.FsTotal = int64(row.Value)
			break
		}
	}
	for _, row := range metrics["node_filesystem_avail_bytes"] {
		if row.Labels["mountpoint"] == "/" && row.Labels["fstype"] != "tmpfs" {
			cap.FsAvail = int64(row.Value)
			break
		}
	}
	if cap.FsTotal > 0 {
		cap.FsUsed = cap.FsTotal - cap.FsAvail
		cap.FsPct = float64(cap.FsUsed) / float64(cap.FsTotal) * 100
	}
	// memory
	if rs := metrics["node_memory_MemTotal_bytes"]; len(rs) > 0 {
		cap.MemTotal = int64(rs[0].Value)
	}
	if rs := metrics["node_memory_MemAvailable_bytes"]; len(rs) > 0 {
		cap.MemAvail = int64(rs[0].Value)
	}
	if cap.MemTotal > 0 {
		cap.MemUsed = cap.MemTotal - cap.MemAvail
		cap.MemPct = float64(cap.MemUsed) / float64(cap.MemTotal) * 100
	}
	// Fit a linear trend from samples (set in trends.go logic below).
	cap.DaysUntilFsFull = forecastDaysToFull(vm, "fs", cap.FsUsed, cap.FsTotal)
	cap.DaysUntilMemFull = forecastDaysToFull(vm, "mem", cap.MemUsed, cap.MemTotal)
	return cap
}

func collectVM(vm VM) VMStat {
	st := VMStat{VM: vm}
	metrics, err := fetchMetrics(vm.IP)
	if err == nil {
		st.Reachable = true
		// probes
		codes := map[string]int{}
		for _, row := range metrics["health_probe_http_code"] {
			key := row.Labels["name"] + "|" + row.Labels["url"]
			codes[key] = int(row.Value)
		}
		for _, row := range metrics["health_probe_up"] {
			key := row.Labels["name"] + "|" + row.Labels["url"]
			st.Probes = append(st.Probes, Probe{
				Name:     row.Labels["name"],
				URL:      row.Labels["url"],
				Up:       row.Value == 1,
				HTTPCode: codes[key],
			})
		}
		sort.Slice(st.Probes, func(i, j int) bool { return st.Probes[i].Name < st.Probes[j].Name })
		if rs := metrics["node_memory_MemTotal_bytes"]; len(rs) > 0 {
			st.MemTotal = int64(rs[0].Value)
		}
		if rs := metrics["node_memory_MemAvailable_bytes"]; len(rs) > 0 {
			st.MemUsed = st.MemTotal - int64(rs[0].Value)
		}

		// load1 + cpu count
		if rs := metrics["node_load1"]; len(rs) > 0 {
			st.Load1 = rs[0].Value
		}
		// node_cpu_seconds_total has one row per (cpu,mode); count distinct cpus.
		cpus := map[string]bool{}
		var idle, total float64
		for _, row := range metrics["node_cpu_seconds_total"] {
			cpus[row.Labels["cpu"]] = true
			total += row.Value
			if row.Labels["mode"] == "idle" {
				idle += row.Value
			}
		}
		st.CPUCount = len(cpus)
		if total > 0 {
			st.CPUBusy = 1.0 - idle/total
		}

		// Primary iface bytes: prefer enp1s0 (libvirt VM main iface), else eth0, else first non-lo.
		var rx, tx int64
		findIface := func() string {
			pref := []string{"enp1s0", "eth0", "eno1", "ens3"}
			for _, p := range pref {
				for _, row := range metrics["node_network_receive_bytes_total"] {
					if row.Labels["device"] == p {
						return p
					}
				}
			}
			for _, row := range metrics["node_network_receive_bytes_total"] {
				if d := row.Labels["device"]; d != "" && d != "lo" {
					return d
				}
			}
			return ""
		}
		iface := findIface()
		if iface != "" {
			for _, row := range metrics["node_network_receive_bytes_total"] {
				if row.Labels["device"] == iface {
					rx = int64(row.Value)
				}
			}
			for _, row := range metrics["node_network_transmit_bytes_total"] {
				if row.Labels["device"] == iface {
					tx = int64(row.Value)
				}
			}
		}
		st.NetRxTot = rx
		st.NetTxTot = tx
		// Compute delta vs previous scrape for this VM.
		now := time.Now()
		netPrevMu.Lock()
		prev, ok := netPrev[vm.Name]
		if ok {
			dt := now.Sub(prev.Ts).Seconds()
			if dt > 0 && dt < 600 {
				if rx >= prev.Rx {
					st.NetRxBps = float64(rx-prev.Rx) / dt
				}
				if tx >= prev.Tx {
					st.NetTxBps = float64(tx-prev.Tx) / dt
				}
			}
		}
		netPrev[vm.Name] = struct {
			Rx, Tx int64
			Ts     time.Time
		}{rx, tx, now}
		netPrevMu.Unlock()

		// Tailscale interface up?
		for _, row := range metrics["node_network_up"] {
			if row.Labels["device"] == "tailscale0" && row.Value == 1 {
				st.TSUp = true
			}
		}
	}
	st.Capacity = capacityFor(vm.Name, metrics)
	st.Restic = resticFor(vm.Name)
	st.Backups = backupViewFor(vm.Name)
	// best-effort: record this scrape for trend fitting (only if VM is reachable
	// and we got real numbers).
	now := time.Now().Unix()
	if st.Capacity.FsTotal > 0 {
		appendSample(sample{Ts: now, Vm: vm.Name, Kind: "fs", Used: st.Capacity.FsUsed, Total: st.Capacity.FsTotal})
	}
	if st.Capacity.MemTotal > 0 {
		appendSample(sample{Ts: now, Vm: vm.Name, Kind: "mem", Used: st.Capacity.MemUsed, Total: st.Capacity.MemTotal})
	}
	return st
}

func collectAll() ([]VMStat, map[string]VirshInfo) {
	virsh := virshState()
	var wg sync.WaitGroup
	results := make([]VMStat, len(inventory))
	for i, vm := range inventory {
		wg.Add(1)
		go func(i int, vm VM) {
			defer wg.Done()
			results[i] = collectVM(vm)
			results[i].Virsh = virsh[vm.Name]
		}(i, vm)
	}
	wg.Wait()
	sort.Slice(results, func(i, j int) bool { return results[i].Name < results[j].Name })
	return results, virsh
}

// computeFleet aggregates per-VM metrics into a fleet overview.
func computeFleet(vms []VMStat) fleetOverview {
	f := fleetOverview{VMsTotal: len(vms)}
	var busyN int
	for _, v := range vms {
		if v.Reachable {
			f.VMsReachable++
			f.LoadSum += v.Load1
			f.CPUCountSum += v.CPUCount
			if v.CPUCount > 0 {
				f.CPUBusyAvg += v.CPUBusy
				busyN++
			}
			f.MemUsedSum += v.Capacity.MemUsed
			f.MemTotalSum += v.Capacity.MemTotal
			f.FsUsedSum += v.Capacity.FsUsed
			f.FsTotalSum += v.Capacity.FsTotal
			f.NetRxBps += v.NetRxBps
			f.NetTxBps += v.NetTxBps
		}
		if v.TSUp {
			f.TSUp++
		} else {
			f.TSDown++
		}
		switch resticBucket(v.Restic) {
		case "ok":
			f.BackupsOK++
		case "warn":
			f.BackupsWarn++
		case "bad":
			f.BackupsBad++
		default:
			f.BackupsOff++
		}
	}
	if busyN > 0 {
		f.CPUBusyAvg /= float64(busyN)
	}
	return f
}

// resticBucket categorises a Restic struct identically to the template's resticCls.
func resticBucket(r Restic) string {
	if !r.Configured {
		return "off"
	}
	if r.Exists == "no" || r.Snapshots == 0 {
		if r.Exists == "no" {
			return "bad"
		}
		return "warn"
	}
	if r.AgeHours > 48 {
		return "bad"
	}
	if r.AgeHours > 26 {
		return "warn"
	}
	return "ok"
}

// refresh runs collectAll() and stashes the result in the package-level cache.
// Serialized by collectMu so we never run two scrapes concurrently.
func refresh() ([]VMStat, Storagebox, []Zpool, fleetOverview) {
	collectMu.Lock()
	defer collectMu.Unlock()
	vms, _ := collectAll()
	sb := storageboxStatus()
	zp := zpoolsStatus()
	fl := computeFleet(vms)
	cachedVMs = vms
	cachedSB = sb
	cachedZPools = zp
	cachedFleet = fl
	cachedAt = time.Now()
	return vms, sb, zp, fl
}

// cachedOrRefresh returns the cached scrape if it's <maxAge old, else triggers a refresh.
func cachedOrRefresh(maxAge time.Duration) ([]VMStat, Storagebox, []Zpool, fleetOverview) {
	collectMu.Lock()
	if !cachedAt.IsZero() && time.Since(cachedAt) < maxAge {
		v, s, z, f := cachedVMs, cachedSB, cachedZPools, cachedFleet
		collectMu.Unlock()
		return v, s, z, f
	}
	collectMu.Unlock()
	return refresh()
}

// --------- SVG network flow ----------

// flowEntry describes one DNS-fronted (or tailscale-fronted, or VM-only)
// path through the medano network. Hard-coded because the mappings live in
// medano's nginx vhost configs and would otherwise need scraping.
type flowEntry struct {
	DNS     string // public DNS name, "" for VM-only
	PubIP   string // public IP / tailscale IP, "" if N/A
	Iface   string // eno1, tailscale0, internal
	TLS     bool   // https terminated at medano nginx?
	Nginx   bool   // does this path traverse the medano nginx?
	Bridge  string // virbr0/virbr1/virbr2, or "" for host-local
	VM      string // VM name (must match a VMStat.Name) or "" if host-local
	Service string // backend service name
	Port    string // backend port (with /proto if useful)
	Notes   string // small annotation, optional
}

func buildSVG(vms []VMStat) template.HTML {
	// ---- hard-coded medano flow table ----
	flows := []flowEntry{
		{DNS: "medano.emile.space", PubIP: "95.217.35.60", Iface: "eno1", TLS: true, Nginx: true, Bridge: "virbr0", VM: "naraj", Service: "nginx", Port: "80", Notes: "static"},
		{DNS: "tmp.medano.emile.space", PubIP: "95.217.35.60", Iface: "eno1", TLS: true, Nginx: true, Bridge: "virbr0", VM: "tmp", Service: "nginx", Port: "80", Notes: "autoindex"},
		{DNS: "md.medano.emile.space", PubIP: "95.217.35.60", Iface: "eno1", TLS: true, Nginx: true, Bridge: "virbr0", VM: "md", Service: "hedgedoc", Port: "9091"},
		{DNS: "auth.medano.emile.space", PubIP: "95.217.35.60", Iface: "eno1", TLS: true, Nginx: true, Bridge: "virbr0", VM: "auth", Service: "authelia", Port: "9091"},
		{DNS: "photo.medano.emile.space", PubIP: "95.217.35.60", Iface: "eno1", TLS: true, Nginx: true, Bridge: "virbr0", VM: "photo", Service: "immich", Port: "9091"},
		{DNS: "amaltheea.medano.emile.space", PubIP: "95.217.35.60", Iface: "eno1", TLS: true, Nginx: true, Bridge: "virbr0", VM: "amalthea", Service: "backend", Port: "8080"},
		{DNS: "status.medano.emile.space", PubIP: "95.217.35.60", Iface: "eno1", TLS: true, Nginx: true, Bridge: "", VM: "", Service: "status-board", Port: "8090", Notes: "127.0.0.1"},
		{DNS: "rss.pinto-pike.ts.net", PubIP: "100.x", Iface: "tailscale0", TLS: false, Nginx: false, Bridge: "virbr0", VM: "rss", Service: "miniflux", Port: "8080"},
		{DNS: "data.pinto-pike.ts.net", PubIP: "100.x", Iface: "tailscale0", TLS: false, Nginx: false, Bridge: "virbr0", VM: "data", Service: "sftpgo", Port: "8080/22"},
		{DNS: "arr (no DNS)", PubIP: "wg0", Iface: "wg0", TLS: false, Nginx: false, Bridge: "virbr1", VM: "arr", Service: "jellyfin", Port: "8096", Notes: "via rou"},
	}

	// VM-only entries (no DNS hostname). These appear as VM-layer boxes
	// with a small "internal" tag, dangling off their bridge.
	internalOnly := map[string]bool{
		"miki": true, "late": true, "social": true,
		"demo01": true, "sb1": true, "sb2": true, "sb3": true,
	}

	// Index VMStat by name so we can colour the VM boxes.
	vmByName := map[string]VMStat{}
	bridgeOf := map[string]string{}
	for _, v := range vms {
		vmByName[v.Name] = v
		if v.Bridge != "" {
			bridgeOf[v.Name] = "virbr" + v.Bridge
		}
	}

	vmCls := func(name string) string {
		v, ok := vmByName[name]
		if !ok {
			return "box-vm-warn"
		}
		if len(v.Probes) > 0 {
			allUp := true
			for _, p := range v.Probes {
				if !p.Up {
					allUp = false
					break
				}
			}
			if allUp {
				return "box-vm-ok"
			}
			return "box-vm-bad"
		}
		if v.Reachable {
			return "box-vm-warn"
		}
		return "box-vm-bad"
	}

	// ---- collect layer items ----
	// Layer 1: DNS hostnames (from flows where DNS != "")
	// Layer 2: public IPs / tailscale IPs (distinct)
	// Layer 3: interfaces (eno1, tailscale0, wg0, internal)
	// Layer 4: nginx vhost markers (one per flow that goes through nginx) + bypass marker
	// Layer 5: bridges (virbr0, virbr1, virbr2) + "host" pseudo-bridge
	// Layer 6: VMs (from flows + internal-only)
	// Layer 7 (tucked next to VM): service:port

	// We'll collapse layer 7 into the VM box (two lines: name/IP + service:port)
	// so we still have six logical layers as requested.

	// Distinct values per layer in stable order:
	type strset struct {
		order []string
		seen  map[string]bool
	}
	add := func(s *strset, v string) {
		if v == "" || s.seen[v] {
			return
		}
		if s.seen == nil {
			s.seen = map[string]bool{}
		}
		s.seen[v] = true
		s.order = append(s.order, v)
	}

	var dnsSet, pubIPSet, ifaceSet, vhostSet, bridgeSet, vmSet strset
	for _, f := range flows {
		add(&dnsSet, f.DNS)
		add(&pubIPSet, f.PubIP)
		add(&ifaceSet, f.Iface)
		if f.Nginx {
			add(&vhostSet, f.DNS) // one nginx vhost node per flow
		}
		if f.Bridge != "" {
			add(&bridgeSet, f.Bridge)
		} else {
			add(&bridgeSet, "host")
		}
		if f.VM != "" {
			add(&vmSet, f.VM)
		} else {
			add(&vmSet, "(host) "+f.Service)
		}
	}
	// add internal-only VMs to the VM layer
	for _, v := range vms {
		if internalOnly[v.Name] {
			add(&vmSet, v.Name)
			b := "virbr" + v.Bridge
			if v.Bridge == "" {
				b = "host"
			}
			add(&bridgeSet, b)
		}
	}
	// also include "bypass" marker in vhost layer for non-nginx flows so the
	// layer isn't empty for tailscale paths.
	hasBypass := false
	for _, f := range flows {
		if !f.Nginx {
			hasBypass = true
			break
		}
	}
	if hasBypass {
		add(&vhostSet, "(bypass nginx)")
	}

	// ---- layout ----
	const (
		layerCount = 6
		colW       = 220 // width of each layer column
		colGap     = 30  // gap between columns
		boxW       = 190
		boxH       = 38
		vboxH      = 50 // taller for VM boxes (two lines + service)
		rowH       = 18 // vertical spacing between rows within a layer
		topPad     = 70
		leftPad    = 30
		bottomPad  = 30
	)
	layerNames := []string{"DNS hostname", "public IP", "interface", "vhost / bypass", "bridge", "VM + service"}
	layerSets := []*strset{&dnsSet, &pubIPSet, &ifaceSet, &vhostSet, &bridgeSet, &vmSet}

	// compute row heights so each layer is vertically distributed
	maxRows := 1
	for _, s := range layerSets {
		if len(s.order) > maxRows {
			maxRows = len(s.order)
		}
	}
	// Use a fixed slot height; each layer centers its items vertically.
	const slotH = 56
	innerH := maxRows * slotH
	if innerH < 240 {
		innerH = 240
	}
	if innerH > 900 {
		innerH = 900
	}
	totalH := topPad + innerH + bottomPad
	totalW := leftPad + layerCount*colW + (layerCount-1)*colGap + leftPad

	// position helpers
	colX := func(layer int) int {
		return leftPad + layer*(colW+colGap)
	}
	rowY := func(layer, idx int) int {
		n := len(layerSets[layer].order)
		if n == 0 {
			return topPad + innerH/2
		}
		// distribute n items across innerH
		step := innerH / (n + 1)
		return topPad + (idx+1)*step
	}

	// box renderer
	esc := html.EscapeString

	var sb strings.Builder
	fmt.Fprintf(&sb, `<svg viewBox="0 0 %d %d" width="%d" height="%d" xmlns="http://www.w3.org/2000/svg" style="min-width:%dpx">`, totalW, totalH, totalW, totalH, totalW)
	sb.WriteString(`<defs><style>
text { font-family: -apple-system, system-ui, sans-serif; font-size: 12px; fill: #eee; }
text.mono { font-family: ui-monospace, Menlo, monospace; }
.dim { fill: #888; font-size: 10px; }
.layer-title { fill: #6aa9ff; font-size: 11px; letter-spacing: .5px; text-transform: uppercase; }
.box-host { fill: #1f3d5a; stroke: #4d9fff; stroke-width: 1; }
.box-iface { fill: #1a2c45; stroke: #6aa9ff; stroke-width: 1; }
.box-dns { fill: #20283a; stroke: #88a; stroke-width: 1; }
.box-vhost { fill: #2a2138; stroke: #b07cff; stroke-width: 1; }
.box-vhost-bypass { fill: #2a2a2a; stroke: #888; stroke-dasharray: 3 2; stroke-width: 1; }
.box-bridge { fill: #2a2a2a; stroke: #888; stroke-width: 1; }
.box-vm-ok { fill: #182b1c; stroke: #2ea043; stroke-width: 1; }
.box-vm-bad { fill: #2b1818; stroke: #f85149; stroke-width: 1; }
.box-vm-warn { fill: #2a2418; stroke: #d29922; stroke-width: 1; }
.box-host-svc { fill: #182233; stroke: #4d9fff; stroke-dasharray: 3 2; stroke-width: 1; }
.edge { stroke: #444; stroke-width: 1; fill: none; }
.edge-tls { stroke: #6aa9ff; stroke-width: 1.2; fill: none; }
.edge-bypass { stroke: #b07cff; stroke-width: 1.2; stroke-dasharray: 4 2; fill: none; }
.tag { font-size: 9px; fill: #b07cff; }
.tag-tls { font-size: 9px; fill: #2ea043; }
.tag-int { font-size: 9px; fill: #d29922; }
.legend-bg { fill: #181818; stroke: #333; }
</style>
<marker id="arrow" viewBox="0 0 10 10" refX="9" refY="5" markerWidth="6" markerHeight="6" orient="auto">
  <path d="M0,0 L10,5 L0,10 z" fill="#444"/>
</marker>
<marker id="arrow-tls" viewBox="0 0 10 10" refX="9" refY="5" markerWidth="6" markerHeight="6" orient="auto">
  <path d="M0,0 L10,5 L0,10 z" fill="#6aa9ff"/>
</marker>
<marker id="arrow-bypass" viewBox="0 0 10 10" refX="9" refY="5" markerWidth="6" markerHeight="6" orient="auto">
  <path d="M0,0 L10,5 L0,10 z" fill="#b07cff"/>
</marker>
</defs>`)

	// background
	fmt.Fprintf(&sb, `<rect x="0" y="0" width="%d" height="%d" fill="#141414"/>`, totalW, totalH)

	// layer headers
	for i, name := range layerNames {
		x := colX(i) + boxW/2
		fmt.Fprintf(&sb, `<text class="layer-title" x="%d" y="%d" text-anchor="middle">%d. %s</text>`, x, 30, i+1, esc(name))
		// faint column divider
		fmt.Fprintf(&sb, `<line x1="%d" y1="40" x2="%d" y2="%d" stroke="#1f1f1f" stroke-width="1"/>`, x, x, totalH-bottomPad+10)
	}

	// position lookups
	pos := make([]map[string]struct{ x, y int }, layerCount)
	for li := 0; li < layerCount; li++ {
		pos[li] = map[string]struct{ x, y int }{}
		for i, item := range layerSets[li].order {
			pos[li][item] = struct{ x, y int }{colX(li), rowY(li, i)}
		}
	}

	// draw boxes
	drawBox := func(li int, item string) {
		p := pos[li][item]
		x, y := p.x, p.y-boxH/2
		h := boxH
		switch li {
		case 0: // DNS
			fmt.Fprintf(&sb, `<rect x="%d" y="%d" width="%d" height="%d" rx="4" class="box-dns"/>`, x, y, boxW, h)
			fmt.Fprintf(&sb, `<text class="mono" x="%d" y="%d" text-anchor="middle">%s</text>`, x+boxW/2, y+22, esc(item))
		case 1: // pub IP
			fmt.Fprintf(&sb, `<rect x="%d" y="%d" width="%d" height="%d" rx="4" class="box-host"/>`, x, y, boxW, h)
			fmt.Fprintf(&sb, `<text class="mono" x="%d" y="%d" text-anchor="middle">%s</text>`, x+boxW/2, y+22, esc(item))
		case 2: // iface
			fmt.Fprintf(&sb, `<rect x="%d" y="%d" width="%d" height="%d" rx="4" class="box-iface"/>`, x, y, boxW, h)
			fmt.Fprintf(&sb, `<text x="%d" y="%d" text-anchor="middle">%s</text>`, x+boxW/2, y+22, esc(item))
		case 3: // vhost
			cls := "box-vhost"
			if item == "(bypass nginx)" {
				cls = "box-vhost-bypass"
			}
			fmt.Fprintf(&sb, `<rect x="%d" y="%d" width="%d" height="%d" rx="4" class="%s"/>`, x, y, boxW, h, cls)
			label := item
			if item != "(bypass nginx)" {
				label = "nginx: " + item
			}
			fmt.Fprintf(&sb, `<text class="mono" x="%d" y="%d" text-anchor="middle">%s</text>`, x+boxW/2, y+16, esc(label))
			if item != "(bypass nginx)" {
				fmt.Fprintf(&sb, `<text class="tag-tls" x="%d" y="%d" text-anchor="middle">TLS</text>`, x+boxW/2, y+30)
			}
		case 4: // bridge
			fmt.Fprintf(&sb, `<rect x="%d" y="%d" width="%d" height="%d" rx="4" class="box-bridge"/>`, x, y, boxW, h)
			fmt.Fprintf(&sb, `<text x="%d" y="%d" text-anchor="middle">%s</text>`, x+boxW/2, y+22, esc(item))
		case 5: // VM + service
			// look up the flow(s) for this VM to pull service:port
			var svc, port, ip string
			isHost := strings.HasPrefix(item, "(host) ")
			for _, f := range flows {
				if f.VM == item || (isHost && f.VM == "" && "(host) "+f.Service == item) {
					svc = f.Service
					port = f.Port
				}
			}
			if v, ok := vmByName[item]; ok {
				ip = v.IP
			}
			cls := vmCls(item)
			if isHost {
				cls = "box-host-svc"
			}
			h = vboxH
			y = p.y - h/2
			fmt.Fprintf(&sb, `<rect x="%d" y="%d" width="%d" height="%d" rx="4" class="%s"/>`, x, y, boxW, h, cls)
			fmt.Fprintf(&sb, `<text x="%d" y="%d" text-anchor="middle">%s</text>`, x+boxW/2, y+14, esc(strings.TrimPrefix(item, "(host) ")))
			if ip != "" {
				fmt.Fprintf(&sb, `<text class="mono dim" x="%d" y="%d" text-anchor="middle">%s</text>`, x+boxW/2, y+28, esc(ip))
			}
			if svc != "" {
				fmt.Fprintf(&sb, `<text class="mono" x="%d" y="%d" text-anchor="middle" style="font-size:10px;fill:#aac">%s:%s</text>`, x+boxW/2, y+42, esc(svc), esc(port))
			} else if internalOnly[item] {
				fmt.Fprintf(&sb, `<text class="tag-int" x="%d" y="%d" text-anchor="middle">internal</text>`, x+boxW/2, y+42)
			}
		}
	}

	for li := 0; li < layerCount; li++ {
		for _, item := range layerSets[li].order {
			drawBox(li, item)
		}
	}

	// draw edges by walking each flow through the six layers
	edge := func(li int, from, to string, style string) {
		pf, ok1 := pos[li][from]
		pt, ok2 := pos[li+1][to]
		if !ok1 || !ok2 {
			return
		}
		x1 := pf.x + boxW
		y1 := pf.y
		x2 := pt.x
		y2 := pt.y
		// cubic bezier between layers for a clean fan-out
		c1x := x1 + (x2-x1)/2
		c2x := x2 - (x2-x1)/2
		cls := "edge"
		marker := "arrow"
		switch style {
		case "tls":
			cls = "edge-tls"
			marker = "arrow-tls"
		case "bypass":
			cls = "edge-bypass"
			marker = "arrow-bypass"
		}
		fmt.Fprintf(&sb, `<path d="M%d,%d C%d,%d %d,%d %d,%d" class="%s" marker-end="url(#%s)"/>`, x1, y1, c1x, y1, c2x, y2, x2, y2, cls, marker)
	}

	for _, f := range flows {
		style := "default"
		if f.TLS {
			style = "tls"
		}
		if !f.Nginx {
			style = "bypass"
		}
		// layer 0->1
		edge(0, f.DNS, f.PubIP, style)
		// layer 1->2
		edge(1, f.PubIP, f.Iface, style)
		// layer 2->3
		var vh string
		if f.Nginx {
			vh = f.DNS
		} else {
			vh = "(bypass nginx)"
		}
		edge(2, f.Iface, vh, style)
		// layer 3->4
		br := f.Bridge
		if br == "" {
			br = "host"
		}
		edge(3, vh, br, style)
		// layer 4->5
		vm := f.VM
		if vm == "" {
			vm = "(host) " + f.Service
		}
		edge(4, br, vm, style)
	}
	// internal-only VMs: draw a single edge from their bridge so they don't float
	for _, v := range vms {
		if !internalOnly[v.Name] {
			continue
		}
		br := "virbr" + v.Bridge
		if v.Bridge == "" {
			br = "host"
		}
		edge(4, br, v.Name, "default")
	}

	sb.WriteString(`</svg>`)
	return template.HTML(sb.String())
}

// --------- template ----------

// sortBackups returns VMs ordered by backup health: bad → warn → ok → off.
// Used by the Backups tab.
func sortBackups(vms []VMStat) []VMStat {
	out := make([]VMStat, len(vms))
	copy(out, vms)
	rank := func(v VMStat) int {
		switch resticBucket(v.Restic) {
		case "bad":
			return 0
		case "warn":
			return 1
		case "ok":
			return 2
		default:
			return 3
		}
	}
	sort.SliceStable(out, func(i, j int) bool {
		ri, rj := rank(out[i]), rank(out[j])
		if ri != rj {
			return ri < rj
		}
		return out[i].Name < out[j].Name
	})
	return out
}

func humanRate(bps float64) string {
	if bps < 1024 {
		return fmt.Sprintf("%.0f B/s", bps)
	}
	if bps < 1024*1024 {
		return fmt.Sprintf("%.1f KiB/s", bps/1024)
	}
	if bps < 1024*1024*1024 {
		return fmt.Sprintf("%.1f MiB/s", bps/1024/1024)
	}
	return fmt.Sprintf("%.2f GiB/s", bps/1024/1024/1024)
}

var page = template.Must(template.New("page").Funcs(template.FuncMap{
	"div": func(a, b float64) float64 { return a / b },
	"mb":  func(b int64) float64 { return float64(b) / 1024 / 1024 },
	"gb":  func(b int64) float64 { return float64(b) / 1024 / 1024 / 1024 },
	"tb":  func(b int64) float64 { return float64(b) / 1024 / 1024 / 1024 / 1024 },
	"kib2gb": func(k int64) float64 { return float64(k) / 1024 / 1024 },
	"pct":     func(a, b int64) float64 {
		if b == 0 { return 0 }
		return float64(a) / float64(b) * 100
	},
	"capCls": func(p float64) string {
		switch {
		case p >= 90: return "bad"
		case p >= 75: return "warn"
		default:      return "ok"
		}
	},
	"barPct": func(p float64) int {
		if p < 0 { return 0 }
		if p > 100 { return 100 }
		return int(p)
	},
	"daysCls": func(d float64) string {
		switch {
		case d < 0: return "unknown"
		case d < 14: return "bad"
		case d < 60: return "warn"
		default: return "ok"
		}
	},
	"daysText": func(d float64) string {
		if d < 0 { return "—" }
		if d < 1 {
			return fmt.Sprintf("%.0fh", d*24)
		}
		if d < 365 {
			return fmt.Sprintf("%.0fd", d)
		}
		return fmt.Sprintf("%.1fy", d/365)
	},
	"resticCls": func(r Restic) string { return resticBucket(r) },
	"ageText": func(h float64) string {
		if h < 1 {
			return fmt.Sprintf("%.0fm", h*60)
		}
		if h < 48 {
			return fmt.Sprintf("%.1fh", h)
		}
		if h < 24*60 {
			return fmt.Sprintf("%.1fd", h/24)
		}
		return fmt.Sprintf("%.0fd", h/24)
	},
	"probeCls": func(p Probe) string {
		if p.Up {
			return "ok"
		}
		return "bad"
	},
	"reachCls": func(b bool) string {
		if b {
			return "ok"
		}
		return "bad"
	},
	"rate":        humanRate,
	"sortBackups": sortBackups,
	"mulFloat":    func(a, b float64) float64 { return a * b },
	"loadCls": func(load float64, cpu int) string {
		if cpu == 0 { return "unknown" }
		r := load / float64(cpu)
		switch {
		case r >= 1.0: return "bad"
		case r >= 0.7: return "warn"
		default:       return "ok"
		}
	},
}).Parse(`<!doctype html>
<html><head><meta charset="utf-8"><title>medano fleet</title>
<style>
  /*
   * Palette tuned for WCAG AA. Verified by tools/status-board/check-contrast.sh
   * (runs on every nix build). Status badges have explicit fg/bg pairs because
   * the previous design rendered text in the same colour as its background,
   * which gave 1:1 contrast (illegible).
   *
   * Light-mode palette lives in the @media (prefers-color-scheme: light) block
   * below; the OS picks one — there is intentionally no in-page toggle.
   */
  :root {
    --bg: #111;
    --fg: #eeeeee;
    --dim: #bdbdbd;
    --dimmer: #9a9a9a;
    --border: #444444;
    --card: #181818;
    --card2: #1f1f1f;
    --link: #88aaff;
    --ok:      #4ec97b;
    --warn:    #e2b04a;
    --bad:     #ff7a7a;
    --unknown: #9a9a9a;
    /* Status badge fg/bg pairs (text on tinted background). */
    --ok-bg:      #1b3322;
    --ok-fg:      #a8e8b8;
    --warn-bg:    #3a2e15;
    --warn-fg:    #ffd784;
    --bad-bg:     #3a1d1d;
    --bad-fg:     #ffc0c0;
    --unknown-bg: #2a2a2a;
    --unknown-fg: #d0d0d0;
    /* Generic chip background (e.g. .probe). */
    --chip-bg: #2a2a2a;
  }
  @media (prefers-color-scheme: light) {
    :root {
      --bg: #f7f7f7;
      --fg: #1a1a1a;
      --dim: #444444;
      --dimmer: #5d5d5d;
      --border: #cccccc;
      --card: #ffffff;
      --card2: #ececec;
      --link: #1a4ea6;
      --ok:      #0e5d28;
      --warn:    #7a4a00;
      --bad:     #8c1818;
      --unknown: #5d5d5d;
      --ok-bg:      #d6f5dd;
      --ok-fg:      #0e5d28;
      --warn-bg:    #fbe7b8;
      --warn-fg:    #6b4100;
      --bad-bg:     #fbd8d8;
      --bad-fg:     #8c1818;
      --unknown-bg: #e0e0e0;
      --unknown-fg: #3a3a3a;
      --chip-bg: #e6e6e6;
    }
  }
  body { font-family: -apple-system, sans-serif; background: var(--bg); color: var(--fg); padding: 0; margin: 0; }
  main { padding: 0 1.5em 2em; }
  h1 { margin: 0 0 .25em; font-weight: 400; }
  h2 { margin: 1.5em 0 .5em; font-weight: 400; color: var(--dim); font-size: 1.1em; }
  table { border-collapse: collapse; width: 100%; font-size: 13px; }
  th, td { text-align: left; padding: 6px 10px; border-bottom: 1px solid var(--border); vertical-align: top; }
  th { color: var(--dim); font-weight: 500; }
  td.name { font-weight: 600; }
  td.ip { font-family: monospace; color: var(--link); }
  .dot { display: inline-block; width: 8px; height: 8px; border-radius: 50%; vertical-align: middle; margin-right: 4px; }
  /* Status badges: readable text on a tinted background, in a padded chip. */
  .ok      { background: var(--ok-bg);      color: var(--ok-fg)      !important; padding: 1px 6px; border-radius: 3px; }
  .warn    { background: var(--warn-bg);    color: var(--warn-fg)    !important; padding: 1px 6px; border-radius: 3px; }
  .bad     { background: var(--bad-bg);     color: var(--bad-fg)     !important; padding: 1px 6px; border-radius: 3px; }
  .unknown { background: var(--unknown-bg); color: var(--unknown-fg) !important; padding: 1px 6px; border-radius: 3px; }
  .probe { display: inline-block; padding: 2px 6px; margin: 1px 2px 1px 0; border-radius: 3px; font-size: 11px; background: var(--chip-bg); color: var(--dim); }
  .probe.ok   { color: var(--ok); }
  .probe.bad  { color: var(--bad); }
  td.restic { font-family: monospace; font-size: 11px; line-height: 1.5; }
  td.restic.ok   { border-left: 3px solid var(--ok);   padding-left: 8px; }
  td.restic.warn { border-left: 3px solid var(--warn); padding-left: 8px; }
  td.restic.bad  { border-left: 3px solid var(--bad);  padding-left: 8px; }
  td.restic.off  { border-left: 3px solid var(--border);padding-left: 8px; color: var(--dimmer); }
  td.restic .repo { color: var(--dimmer); font-size: 10px; }
  /* progress bar inside table cells (memory / disk) */
  .bar { position: relative; height: 6px; background: var(--chip-bg); border-radius: 3px; overflow: hidden; margin-top: 3px; width: 120px; }
  .bar > span { display: block; height: 100%; background: var(--ok); }
  .bar.warn > span { background: var(--warn); }
  .bar.bad > span  { background: var(--bad); }
  td.cap { font-family: monospace; font-size: 11px; }
  td.cap .forecast { color: var(--dim); font-size: 10px; margin-left: 4px; }
  td.cap .forecast.bad  { color: var(--bad); }
  td.cap .forecast.warn { color: var(--warn); }
  td.cap .forecast.unknown { color: var(--unknown); }
  .footer { color: var(--dimmer); font-size: 11px; margin-top: 1em; }
  svg { background: var(--card); border-radius: 4px; max-width: 100%; }
  /* legend lives outside the SVG to avoid overlap */
  .legend {
    display: flex; flex-wrap: wrap; gap: 1em;
    font-size: 12px; color: var(--dim);
    background: var(--card); border: 1px solid var(--border); border-radius: 4px;
    padding: 8px 12px; margin-top: 8px;
  }
  .legend > div { display: inline-flex; align-items: center; gap: 6px; }
  .legend .sw { display: inline-block; width: 14px; height: 10px; border-radius: 2px; }
  .legend .sw.ok   { background: #1b3322; border: 1px solid var(--ok); }
  .legend .sw.warn { background: #2a2418; border: 1px solid var(--warn); }
  .legend .sw.bad  { background: #3a1d1d; border: 1px solid var(--bad); }
  .legend .sw.tls  { background: #6aa9ff; }
  .legend .sw.byp  { background: #b07cff; }
  /* storagebox-bx11 banner */
  .sb-banner { background: var(--card); border: 1px solid var(--border); border-radius: 4px; padding: 10px 12px; margin: .5em 0 1em; font-size: 13px; display: flex; align-items: center; gap: 1.5em; }
  .sb-banner .label { color: var(--dim); }
  .sb-banner .bar { width: 320px; height: 10px; }

  /* ---- tab bar ---- */
  header.topbar {
    position: sticky; top: 0; z-index: 10;
    background: var(--card2); border-bottom: 1px solid var(--border);
    padding: 10px 1.5em 0;
  }
  header.topbar h1 { font-size: 1.1em; margin: 0 0 6px; color: var(--fg); }
  nav.tabs { display: flex; gap: 4px; }
  nav.tabs a {
    display: inline-block; padding: 8px 14px; font-size: 13px;
    color: var(--dim); text-decoration: none;
    border: 1px solid var(--border); border-bottom: none;
    border-radius: 4px 4px 0 0; background: var(--card);
  }
  nav.tabs a.active { color: var(--fg); background: var(--card2); border-color: var(--link); border-bottom: 1px solid var(--card2); position: relative; top: 1px; }
  nav.tabs a:hover { color: var(--fg); }
  section.tab { display: none; }
  section.tab.active { display: block; }
  .stamp { color: var(--dim); font-size: 12px; padding-left: 1em; }

  /* ---- Overview grid + cards ---- */
  .grid {
    display: grid;
    grid-template-columns: repeat(auto-fill, minmax(240px, 1fr));
    gap: 12px;
    margin: 12px 0;
  }
  .card {
    background: var(--card); border: 1px solid var(--border); border-radius: 6px;
    padding: 12px 14px; display: flex; flex-direction: column; gap: 6px;
  }
  .card .title { font-size: 11px; color: var(--dim); text-transform: uppercase; letter-spacing: .5px; }
  .card .big   { font-size: 22px; font-weight: 600; font-family: ui-monospace, Menlo, monospace; }
  .card .sub   { font-size: 11px; color: var(--dim); }
  .card.bad  { border-color: var(--bad); }
  .card.warn { border-color: var(--warn); }
  .card.ok   { border-color: var(--ok); }
  /* --- Backups tab: nested VM -> path -> target --- */
  .backups-list { display:flex; flex-direction:column; gap:6px; }
  .backup-vm { border:1px solid var(--border); border-radius:4px; background:var(--card); padding:6px 10px; }
  .backup-vm.bad  { border-left:3px solid var(--bad); }
  .backup-vm.warn { border-left:3px solid var(--warn); }
  .backup-vm.ok   { border-left:3px solid var(--ok); }
  .backup-vm.off  { border-left:3px solid var(--dimmer); color:var(--dim); display:flex; align-items:center; gap:.7em; }
  .backup-vm summary { cursor:pointer; display:flex; align-items:center; gap:.7em; list-style:none; }
  .backup-vm summary::-webkit-details-marker { display:none; }
  .backup-vm summary::before { content:"\25B8"; color:var(--dimmer); font-size:10px; width:1em; }
  .backup-vm[open] summary::before { content:"\25BE"; }
  .bvm-name { font-weight:600; font-family:ui-monospace, Menlo, monospace; }
  .bvm-meta { color:var(--dim); font-size:11px; margin-left:auto; font-family:ui-monospace, Menlo, monospace; }
  .badge { font-size:10px; padding:1px 6px; border-radius:8px; font-family:ui-monospace, Menlo, monospace; text-transform:uppercase; letter-spacing:.5px; }
  .badge.ok   { background:var(--ok-bg); color:var(--ok-fg); }
  .badge.warn { background:var(--warn-bg); color:var(--warn-fg); }
  .badge.bad  { background:var(--bad-bg); color:var(--bad-fg); }
  .badge.off  { background:var(--unknown-bg); color:var(--unknown-fg); }
  .bvm-paths { margin-top:8px; padding-left:14px; display:flex; flex-direction:column; gap:6px; }
  .bpath { border-left:2px solid var(--border); padding:2px 0 2px 10px; }
  .bpath-label { font-family:ui-monospace, Menlo, monospace; font-size:12px; color:var(--fg); }
  .bpath-icon { color:var(--dimmer); margin-right:1px; }
  .bpath-name { font-weight:500; }
  .btargets-tbl { width:100%; font-size:11px; font-family:ui-monospace, Menlo, monospace; margin-top:3px; border-collapse:collapse; }
  .btargets-tbl th { text-align:left; color:var(--dimmer); font-weight:normal; font-size:10px; text-transform:uppercase; letter-spacing:.5px; padding:2px 6px; }
  .btargets-tbl td { padding:2px 6px; color:var(--fg); }
  .btargets-tbl td.repo-path { color:var(--dimmer); font-size:10px; }
  .ttype { color:var(--dim); }
  .tlabel { font-weight:500; }
  /* --- Force-graph toggles --- */
  .fg-controls { display:flex; flex-wrap:wrap; gap:.6em 1em; align-items:center; padding:6px 8px; margin-bottom:6px; background:var(--card); border:1px solid var(--border); border-radius:4px; font-size:11px; }
  .fg-controls .node-toggle { display:inline-flex; align-items:center; gap:.25em; cursor:pointer; user-select:none; font-family:ui-monospace, Menlo, monospace; }
  .fg-controls .node-toggle input { accent-color:var(--ok); }
  .fg-controls .node-toggle .sw { display:inline-block; width:8px; height:8px; border-radius:50%; }
  .fg-controls .sep { width:1px; align-self:stretch; background:var(--border); }
  .fg-controls .explore { display:inline-flex; align-items:center; gap:.3em; padding:2px 8px; border:1px solid var(--border); border-radius:3px; cursor:pointer; }
  .fg-controls .explore.active { background:#2a3a26; color:#9fe6b5; border-color:#3a5a36; }
  .fg-hidden { display:none !important; }
  .spark { width: 100%; height: 26px; display: block; }
</style></head>
</style></head>
<body>
  <header class="topbar">
    <h1>medano fleet <span class="stamp" id="stamp">scraped at {{ .Now }}</span></h1>
    <nav class="tabs" id="tabs">
      <a href="#overview" data-tab="overview">overview</a>
      <a href="#nat" data-tab="nat">NAT</a>
      <a href="#flow" data-tab="flow">traffic flow</a>
      <a href="#inventory" data-tab="inventory">inventory</a>
      <a href="#backups" data-tab="backups">backups</a>
    </nav>
  </header>
  <main>

  <section class="tab" id="tab-overview" data-tab="overview">
    {{ with .SB }}
    {{- if .Reachable }}
    <div class="sb-banner" id="sb-banner">
      <span class="label">storagebox bx11:</span>
      <span id="sb-used">{{ printf "%.1f GiB used" (gb .Used) }} / {{ printf "%.1f GiB total" (gb .Total) }}</span>
      <span class="bar {{ capCls .UsedPct }}" id="sb-bar"><span style="width:{{ barPct .UsedPct }}%"></span></span>
      <span class="{{ capCls .UsedPct }}" id="sb-pct">{{ printf "%.1f%% used" .UsedPct }}</span>
      <span style="color:var(--dimmer);" id="sb-free">{{ printf "%.1f GiB free" (gb .Avail) }}</span>
    </div>
    {{- else }}
    <div class="sb-banner bad">storagebox bx11 unreachable (CIFS mount down?)</div>
    {{- end }}
    {{ end }}
    {{ range .ZPools }}
    <div class="sb-banner">
      <span class="label">zpool {{ .Name }}:</span>
      <span>{{ printf "%.1f GiB used" (gb .Alloc) }} / {{ printf "%.1f GiB total" (gb .Total) }}</span>
      <span class="bar {{ capCls .UsedPct }}"><span style="width:{{ barPct .UsedPct }}%"></span></span>
      <span class="{{ capCls .UsedPct }}">{{ printf "%.1f%% used" .UsedPct }}</span>
      <span style="color:var(--dimmer);">{{ printf "%.1f GiB free" (gb .Free) }}</span>
    </div>
    {{ end }}

    <h2>fleet bottlenecks</h2>
    <div class="grid">
      {{ with .Fleet }}
      <div class="card {{ if ge .CPUBusyAvg 0.8 }}bad{{ else if ge .CPUBusyAvg 0.5 }}warn{{ end }}">
        <div class="title">CPU</div>
        <div class="big" id="ov-cpu">{{ printf "%.0f%% busy" (mulFloat .CPUBusyAvg 100.0) }}</div>
        <div class="sub">load sum <span id="ov-load">{{ printf "%.2f" .LoadSum }}</span> across <span id="ov-cpucount">{{ .CPUCountSum }}</span> cpus</div>
      </div>
      <div class="card {{ if gt (pct .MemUsedSum .MemTotalSum) 90.0 }}bad{{ else if gt (pct .MemUsedSum .MemTotalSum) 75.0 }}warn{{ end }}">
        <div class="title">RAM</div>
        <div class="big" id="ov-mem">{{ printf "%.1f GiB" (gb .MemUsedSum) }} / {{ printf "%.1f GiB" (gb .MemTotalSum) }}</div>
        <div class="sub"><span id="ov-mempct">{{ printf "%.1f%%" (pct .MemUsedSum .MemTotalSum) }}</span> used across {{ .VMsReachable }}/{{ .VMsTotal }} reachable VMs</div>
      </div>
      <div class="card {{ if gt (pct .FsUsedSum .FsTotalSum) 90.0 }}bad{{ else if gt (pct .FsUsedSum .FsTotalSum) 75.0 }}warn{{ end }}">
        <div class="title">disk (fleet rollup)</div>
        <div class="big" id="ov-disk">{{ printf "%.1f GiB" (gb .FsUsedSum) }} / {{ printf "%.1f GiB" (gb .FsTotalSum) }}</div>
        <div class="sub"><span id="ov-diskpct">{{ printf "%.1f%%" (pct .FsUsedSum .FsTotalSum) }}</span> used across reachable VMs</div>
      </div>
      <div class="card">
        <div class="title">network throughput</div>
        <div class="big" id="ov-net">↓ {{ rate .NetRxBps }} · ↑ {{ rate .NetTxBps }}</div>
        <div class="sub">sum across reachable VM ifaces (delta over last scrape)</div>
      </div>
      <div class="card {{ if gt .TSDown 1 }}warn{{ end }}">
        <div class="title">tailscale</div>
        <div class="big" id="ov-ts">{{ .TSUp }} up · {{ .TSDown }} down</div>
        <div class="sub">node_network_up{device="tailscale0"}</div>
      </div>
      <div class="card {{ if gt .BackupsBad 0 }}bad{{ else if gt .BackupsWarn 0 }}warn{{ end }}">
        <div class="title">backups freshness</div>
        <div class="big" id="ov-backups">{{ .BackupsOK }} ok · {{ .BackupsWarn }} warn · {{ .BackupsBad }} bad</div>
        <div class="sub">{{ .BackupsOff }} repos not configured</div>
      </div>
      {{ end }}
      {{ range .ZPools }}
      <div class="card {{ capCls .UsedPct }}">
        <div class="title">zpool {{ .Name }}</div>
        <div class="big">{{ printf "%.1f%%" .UsedPct }}</div>
        <div class="sub">{{ printf "%.1f GiB used / %.1f GiB" (gb .Alloc) (gb .Total) }}</div>
      </div>
      {{ end }}
    </div>
  </section>

  <section class="tab" id="tab-nat" data-tab="nat">
    <h2>NAT topology</h2>
    {{ .NATSVG }}
  </section>

  <section class="tab" id="tab-flow" data-tab="flow">
    <h2>traffic flow</h2>
    {{ .ForceGraph }}
    <div class="legend">
      <div><span class="sw ok"></span> all probes up</div>
      <div><span class="sw warn"></span> reachable, no probes</div>
      <div><span class="sw bad"></span> unreachable / probe fail</div>
      <div><span class="sw tls"></span> TLS-terminating path via naraj</div>
      <div><span class="sw byp"></span> tailscale-only (bypasses naraj)</div>
    </div>
  </section>

  <section class="tab" id="tab-inventory" data-tab="inventory">
    <h2>vm inventory (<span id="vm-count">{{ len .VMs }}</span> VMs)</h2>
    <table>
      <thead><tr>
        <th>name</th><th>bridge</th><th>ip</th><th>libvirt</th>
        <th>memory</th><th>disk</th>
        <th>probes</th><th>backups</th>
      </tr></thead>
      <tbody id="vm-tbody">
      {{- range .VMs }}
        <tr data-vm="{{ .Name }}">
          <td class="name">{{ .Name }}</td>
          <td>{{ .Bridge }}</td>
          <td class="ip">{{ .IP }}</td>
          <td data-field="state">{{ .Virsh.State }}</td>
          <td class="cap" data-field="mem">
            {{ printf "%.1fG / %.1fG" (gb .Capacity.MemUsed) (gb .Capacity.MemTotal) }}
            <div class="bar {{ capCls .Capacity.MemPct }}"><span style="width:{{ barPct .Capacity.MemPct }}%"></span></div>
          </td>
          <td class="cap" data-field="disk">
            {{ if gt .Capacity.FsTotal 0 -}}
            {{ printf "%.1fG / %.1fG" (gb .Capacity.FsUsed) (gb .Capacity.FsTotal) }}
            <div class="bar {{ capCls .Capacity.FsPct }}"><span style="width:{{ barPct .Capacity.FsPct }}%"></span></div>
            <span class="forecast {{ daysCls .Capacity.DaysUntilFsFull }}">full in {{ daysText .Capacity.DaysUntilFsFull }}</span>
            {{- else -}}<span style="color:var(--dimmer);">—</span>{{- end }}
          </td>
          <td data-field="probes">
            <span class="dot {{ reachCls .Reachable }}"></span>
            {{- if .Probes -}}
              {{- range .Probes }}
                <span class="probe {{ probeCls . }}" title="{{ .URL }}">{{ .Name }} {{ .HTTPCode }}</span>
              {{- end -}}
            {{- else -}}
              <span class="probe">no probes</span>
            {{- end -}}
          </td>
          <td class="restic {{ resticCls .Restic }}" data-field="restic">
            {{- if not .Restic.Configured -}}
              backups not configured
            {{- else if eq .Restic.Exists "no" -}}
              no repo yet
              <div class="repo">{{ .Restic.RepoPath }}</div>
            {{- else if eq .Restic.Snapshots 0 -}}
              repo exists, no snapshots yet
              <div class="repo">{{ .Restic.RepoPath }}</div>
            {{- else -}}
              latest: {{ ageText .Restic.AgeHours }} ago<br>
              oldest: {{ ageText .Restic.OldestHours }} ago<br>
              {{ .Restic.Snapshots }} snapshots, {{ .Restic.Size }} on disk
              <div class="repo">{{ .Restic.RepoPath }}</div>
            {{- end }}
          </td>
        </tr>
      {{- end }}
      </tbody>
    </table>
    <div class="footer">
      data: virsh + each VM's node-exporter:9100 + storagebox restic dirs + samples at /var/lib/status-board/samples.jsonl.
      disk forecast "full in" is a least-squares fit over the last 7 days — "—" means &lt;6h of data or usage stable/shrinking.
    </div>
  </section>

  <section class="tab" id="tab-backups" data-tab="backups">
    <h2>backups</h2>
    <div class="backups-list">
    {{- range sortBackups .VMs }}
      {{- $vmCls := resticCls .Restic }}
      {{- if not .Restic.Configured }}
      <div class="backup-vm off" data-vm="{{ .Name }}" data-backup-paths="0">
        <span class="bvm-name">{{ .Name }}</span>
        <span class="badge off">no backups configured</span>
      </div>
      {{- else }}
      <details class="backup-vm {{ $vmCls }}" data-vm="{{ .Name }}" data-backup-paths="{{ len .Backups.Paths }}" {{ if or (eq $vmCls "bad") (eq $vmCls "warn") }}open{{ end }}>
        <summary>
          <span class="bvm-name">{{ .Name }}</span>
          <span class="badge {{ $vmCls }}">
            {{- if eq .Restic.Snapshots 0 -}}no snapshots
            {{- else -}}{{ ageText .Restic.AgeHours }} ago{{- end -}}
          </span>
          <span class="bvm-meta">{{ len .Backups.Paths }} path{{ if ne (len .Backups.Paths) 1 }}s{{ end }} · {{ .Restic.Snapshots }} snap{{ if ne .Restic.Snapshots 1 }}s{{ end }}{{ if .Restic.Size }} · {{ .Restic.Size }} on disk{{ end }}</span>
        </summary>
        <div class="bvm-paths">
          {{- range .Backups.Paths }}
          <div class="bpath">
            <div class="bpath-label"><span class="bpath-icon">/</span><span class="bpath-name">{{ .Path }}</span></div>
            <div class="btargets">
              <table class="btargets-tbl">
                <thead><tr>
                  <th>target</th><th>latest</th><th>snaps</th>
                  <th>24h</th><th>7d</th><th>30d</th><th>older</th><th>size</th><th>repo</th>
                </tr></thead>
                <tbody>
                {{- range .Targets }}
                  <tr>
                    <td><span class="ttype">{{ .Kind }}</span> <span class="tlabel">{{ .Label }}</span></td>
                    <td class="age">
                      {{- if eq .Exists "no" -}}<span class="badge bad">no repo</span>
                      {{- else if eq .Snapshots 0 -}}<span class="badge warn">no snaps</span>
                      {{- else -}}{{ ageText .AgeHours }} ago{{- end -}}
                    </td>
                    <td>{{ .Snapshots }}</td>
                    <td>{{ .Last24h }}</td>
                    <td>{{ .Last7d }}</td>
                    <td>{{ .Last30d }}</td>
                    <td>{{ .Older }}</td>
                    <td>{{ if .Size }}{{ .Size }}{{ else }}—{{ end }}</td>
                    <td class="repo-path">{{ .Repo }}</td>
                  </tr>
                {{- end }}
                </tbody>
              </table>
            </div>
          </div>
          {{- end }}
        </div>
      </details>
      {{- end }}
    {{- end }}
    </div>
    <div class="footer">
      Each VM groups its configured paths; each path lists every target it is backed up to.
      Buckets from snapshot mtimes; repo size from du -shx (best-effort, short timeout).
      VMs sorted bad → warn → ok → unconfigured. backupPaths exposed via /etc/status-board-graph.json.
    </div>
  </section>

  </main>

<script>
(function(){
  // ---- tab switching via URL hash ----
  const tabs = Array.from(document.querySelectorAll("nav.tabs a"));
  const sections = Array.from(document.querySelectorAll("section.tab"));
  function activate(name) {
    if (!name) name = "overview";
    let any = false;
    tabs.forEach(a => {
      const on = a.dataset.tab === name;
      a.classList.toggle("active", on);
      if (on) any = true;
    });
    sections.forEach(s => s.classList.toggle("active", s.dataset.tab === name));
    if (!any) activate("overview");
  }
  function fromHash(){ return (location.hash || "#overview").replace(/^#/, ""); }
  activate(fromHash());
  window.addEventListener("hashchange", () => activate(fromHash()));

  // ---- SSE: patch the DOM in place ----
  function pct(used, total){ return total > 0 ? (used/total*100) : 0; }
  function gb(b){ return (b/1024/1024/1024).toFixed(1); }
  function fmtRate(bps){
    if (bps < 1024) return bps.toFixed(0)+" B/s";
    if (bps < 1024*1024) return (bps/1024).toFixed(1)+" KiB/s";
    if (bps < 1024*1024*1024) return (bps/1024/1024).toFixed(1)+" MiB/s";
    return (bps/1024/1024/1024).toFixed(2)+" GiB/s";
  }
  function capCls(p){ if (p >= 90) return "bad"; if (p >= 75) return "warn"; return "ok"; }
  function ageHText(h){
    if (h < 1) return (h*60).toFixed(0)+"m";
    if (h < 48) return h.toFixed(1)+"h";
    if (h < 24*60) return (h/24).toFixed(1)+"d";
    return (h/24).toFixed(0)+"d";
  }
  function patchVMRow(tr, v){
    const stateCell = tr.querySelector('[data-field=state]');
    if (stateCell) stateCell.textContent = v.Virsh && v.Virsh.State ? v.Virsh.State : "";
    const memCell = tr.querySelector('[data-field=mem]');
    if (memCell) {
      const c = v.Capacity || {};
      const p = c.MemPct || 0;
      memCell.innerHTML =
        (gb(c.MemUsed||0)) + "G / " + (gb(c.MemTotal||0)) + "G" +
        '<div class="bar ' + capCls(p) + '"><span style="width:' + Math.max(0,Math.min(100,p|0)) + '%"></span></div>';
    }
    const diskCell = tr.querySelector('[data-field=disk]');
    if (diskCell) {
      const c = v.Capacity || {};
      if (c.FsTotal > 0) {
        const p = c.FsPct || 0;
        let forecast = "—";
        let forecastCls = "unknown";
        if (c.DaysUntilFsFull >= 0) {
          const d = c.DaysUntilFsFull;
          if (d < 1) forecast = (d*24).toFixed(0)+"h";
          else if (d < 365) forecast = d.toFixed(0)+"d";
          else forecast = (d/365).toFixed(1)+"y";
          forecastCls = d < 14 ? "bad" : (d < 60 ? "warn" : "ok");
        }
        diskCell.innerHTML =
          (gb(c.FsUsed||0)) + "G / " + (gb(c.FsTotal||0)) + "G" +
          '<div class="bar ' + capCls(p) + '"><span style="width:' + Math.max(0,Math.min(100,p|0)) + '%"></span></div>' +
          '<span class="forecast ' + forecastCls + '">full in ' + forecast + '</span>';
      } else {
        diskCell.innerHTML = '<span style="color:var(--dimmer);">—</span>';
      }
    }
    const probeCell = tr.querySelector('[data-field=probes]');
    if (probeCell) {
      let html = '<span class="dot ' + (v.Reachable ? "ok" : "bad") + '"></span>';
      if (v.Probes && v.Probes.length) {
        for (const p of v.Probes) {
          html += '<span class="probe ' + (p.Up ? "ok" : "bad") + '" title="'+ (p.URL||"") +'">'+ (p.Name||"") +' '+ (p.HTTPCode||"") +'</span>';
        }
      } else {
        html += '<span class="probe">no probes</span>';
      }
      probeCell.innerHTML = html;
    }
  }

  // For force-graph: update node stroke colour by class.
  // The forcegraph.go script creates one <g class="fg-node"> per node and
  // the colour mapping lives in STATUS_STROKE. We update the first <circle>'s
  // stroke based on per-VM probe state.
  const STATUS_STROKE = { ok:"#4ec97b", warn:"#e2b04a", bad:"#ff7a7a" };
  function patchForceGraph(vms) {
    const svg = document.getElementById("force-graph");
    if (!svg) return;
    // Build a map vm-name -> class from VMs[] (same logic as vmCls in Go).
    const cls = {};
    for (const v of vms) {
      let c = "warn";
      if (v.Probes && v.Probes.length) {
        c = v.Probes.every(p => p.Up) ? "ok" : "bad";
      } else if (v.Reachable) {
        c = "warn";
      } else {
        c = "bad";
      }
      cls["vm:"+v.Name] = c;
    }
    // The script stores nodes as Object[] inside its closure — we can't reach
    // those, but each <g class="fg-node"> contains a <text> with the label
    // we wrote. Instead we tag by ID in a side-channel: the data is embedded
    // verbatim in #force-graph-data and the nodes are appended in order.
    const data = JSON.parse(document.getElementById("force-graph-data").textContent);
    const nodeGroups = svg.querySelectorAll("g.fg-node");
    for (let i = 0; i < data.nodes.length && i < nodeGroups.length; i++) {
      const n = data.nodes[i];
      const c = cls[n.id];
      if (!c) continue;
      const circle = nodeGroups[i].querySelector("circle");
      if (!circle) continue;
      circle.setAttribute("stroke", STATUS_STROKE[c] || "#9a9a9a");
      circle.setAttribute("stroke-width", "2.5");
    }
  }

  let evt = null;
  function connect() {
    if (!window.EventSource) return;
    try { evt = new EventSource("/events"); } catch (e) { return; }
    evt.onmessage = (m) => {
      let d;
      try { d = JSON.parse(m.data); } catch (e) { return; }
      // stamp
      const s = document.getElementById("stamp");
      if (s && d.Now) s.textContent = "scraped at " + d.Now;
      // VM rows
      if (d.VMs) {
        for (const v of d.VMs) {
          const tr = document.querySelector('tr[data-vm="'+ v.Name +'"]');
          if (tr) patchVMRow(tr, v);
        }
        patchForceGraph(d.VMs);
      }
      // storagebox banner
      if (d.SB && d.SB.Reachable) {
        const used = document.getElementById("sb-used");
        const free = document.getElementById("sb-free");
        const pctEl = document.getElementById("sb-pct");
        const bar = document.getElementById("sb-bar");
        if (used) used.textContent = gb(d.SB.Used) + " GiB used / " + gb(d.SB.Total) + " GiB total";
        if (free) free.textContent = gb(d.SB.Avail) + " GiB free";
        if (pctEl) {
          pctEl.textContent = d.SB.UsedPct.toFixed(1) + "% used";
          pctEl.className = capCls(d.SB.UsedPct);
        }
        if (bar) {
          bar.className = "bar " + capCls(d.SB.UsedPct);
          const span = bar.querySelector("span");
          if (span) span.style.width = Math.max(0,Math.min(100,d.SB.UsedPct|0)) + "%";
        }
      }
      // Overview cards
      if (d.Fleet) {
        const f = d.Fleet;
        const setText = (id, txt) => { const el = document.getElementById(id); if (el) el.textContent = txt; };
        setText("ov-cpu", (f.CPUBusyAvg*100).toFixed(0)+"% busy");
        setText("ov-load", f.LoadSum.toFixed(2));
        setText("ov-cpucount", String(f.CPUCountSum));
        setText("ov-mem", gb(f.MemUsedSum)+" GiB / "+gb(f.MemTotalSum)+" GiB");
        setText("ov-mempct", pct(f.MemUsedSum,f.MemTotalSum).toFixed(1)+"%");
        setText("ov-disk", gb(f.FsUsedSum)+" GiB / "+gb(f.FsTotalSum)+" GiB");
        setText("ov-diskpct", pct(f.FsUsedSum,f.FsTotalSum).toFixed(1)+"%");
        setText("ov-net", "↓ "+fmtRate(f.NetRxBps)+" · ↑ "+fmtRate(f.NetTxBps));
        setText("ov-ts", f.TSUp+" up · "+f.TSDown+" down");
        setText("ov-backups", f.BackupsOK+" ok · "+f.BackupsWarn+" warn · "+f.BackupsBad+" bad");
      }
    };
    evt.onerror = () => {
      // Browser auto-reconnects; nothing to do.
    };
  }
  connect();
})();
</script>
</body></html>`))

// pageData is the struct passed to the template; also the JSON shape
// emitted on the /events stream (sans the rendered SVGs).
type pageData struct {
	Now        string
	NATSVG     template.HTML `json:"-"`
	ForceGraph template.HTML `json:"-"`
	VMs        []VMStat
	SB         Storagebox
	ZPools     []Zpool
	Fleet      fleetOverview
}

func handle(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/" && r.URL.Path != "/index.html" {
		http.NotFound(w, r)
		return
	}
	vms, sb, zp, fl := cachedOrRefresh(2 * time.Second)
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	page.Execute(w, pageData{
		Now:        time.Now().Format("2006-01-02 15:04:05"),
		NATSVG:     buildNATSVG(),
		ForceGraph: buildForceGraph(vms),
		VMs:        vms,
		SB:         sb,
		ZPools:     zp,
		Fleet:      fl,
	})
}

// events streams JSON snapshots of the fleet state every 5 seconds via SSE.
// Each connected client gets its own ticker; refresh() is serialized by
// collectMu so concurrent clients don't fan out parallel scrapes.
func eventsHandler(w http.ResponseWriter, r *http.Request) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "streaming unsupported", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Accel-Buffering", "no")

	send := func() bool {
		vms, sb, zp, fl := cachedOrRefresh(4 * time.Second)
		pd := pageData{
			Now:    time.Now().Format("2006-01-02 15:04:05"),
			VMs:    vms,
			SB:     sb,
			ZPools: zp,
			Fleet:  fl,
		}
		b, err := json.Marshal(pd)
		if err != nil {
			return false
		}
		if _, err := fmt.Fprintf(w, "data: %s\n\n", b); err != nil {
			return false
		}
		flusher.Flush()
		return true
	}
	if !send() {
		return
	}
	tick := time.NewTicker(5 * time.Second)
	defer tick.Stop()
	for {
		select {
		case <-r.Context().Done():
			return
		case <-tick.C:
			if !send() {
				return
			}
		}
	}
}

func main() {
	if env := os.Getenv("STATUS_BOARD_LISTEN"); env != "" {
		listenAddr = env
	}
	if env := os.Getenv("STATUS_BOARD_STORAGEBOX"); env != "" {
		storageboxDir = env
		storageboxBackupDir = env + "/backup"
	}
	if err := loadInventory(); err != nil {
		log.Fatalf("inventory: %v", err)
	}
	if g, err := loadGraphData(); err == nil {
		for _, v := range g.VMs {
			backupsEnabled[v.Name] = v.BackupsEnabled
			backupPaths[v.Name] = v.BackupPaths
			tgts := make([]BackupTarget, 0, len(v.BackupTargets))
			for _, t := range v.BackupTargets {
				tgts = append(tgts, BackupTarget{Kind: t.Kind, Label: t.Label, Repo: t.Repo})
			}
			backupTargets[v.Name] = tgts
		}
		zfsPoolNames = g.Zpools
	} else {
		log.Printf("graph data not loaded: %v (backups/zfs gauges will be limited)", err)
	}
	log.Printf("status-board listening on %s (inventory: %d VMs, %d zpools, %d backups configured)", listenAddr, len(inventory), len(zfsPoolNames), len(backupsEnabled))
	http.HandleFunc("/events", eventsHandler)
	http.HandleFunc("/", handle)
	log.Fatal(http.ListenAndServe(listenAddr, nil))
}
