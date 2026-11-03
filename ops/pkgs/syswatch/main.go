// syswatch posts anomaly alerts for the host it runs on to an IRC channel,
// staying joined continuously (systemd keeps it alive): failed or stale
// restic-backups-*.service units, other failed systemd units, filesystems
// with little space remaining (cascading thresholds, 15/10/5/3/2/1% free by
// default), low available memory and high 15min load per core. Only state
// transitions are posted, plus recovery notices; state persists in a JSON
// file. The minimal persistent-IRC plumbing below is shared, copy-pasted,
// with weatherman/xkcdbot/tagesschau/dwdwarn/diskwatch: keep them in sync
// or factor properly one day. syswatch adds an optional TLS hop (--tls):
// the other bots run next to Ergo on the irc VM, but syswatch runs on
// lampadas, whose tailnet ACLs don't reach the irc VM - so it comes in
// through the public ircs ingress instead.
package main

import (
	"bufio"
	"crypto/tls"
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"net"
	"os"
	"os/exec"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"
)

// ---------------------------------------------------------------------------
// persistent mini IRC client
// ---------------------------------------------------------------------------

// bot owns its single upstream connection. privmsg may be called from any
// goroutine; runForever owns the read loop.
type bot struct {
	server, nick, channel, realname string
	useTLS                          bool

	mu     sync.Mutex
	conn   net.Conn
	joined bool
}

func readLine(r *bufio.Reader) (string, error) {
	line, err := r.ReadString('\n')
	return strings.TrimRight(line, "\r\n"), err
}

func (b *bot) write(format string, args ...any) {
	fmt.Fprintf(b.conn, format+"\r\n", args...)
}

// privmsg posts one channel message; returns false when not joined.
func (b *bot) privmsg(text string) bool {
	b.mu.Lock()
	defer b.mu.Unlock()
	if !b.joined {
		return false
	}
	text = strings.NewReplacer("\r", "", "\n", "").Replace(text)
	b.conn.SetWriteDeadline(time.Now().Add(10 * time.Second))
	if _, err := fmt.Fprintf(b.conn, "PRIVMSG %s :%s\r\n", b.channel, text); err != nil {
		b.joined = false
		return false
	}
	return true
}

func (b *bot) setConn(conn net.Conn, joined bool) {
	b.mu.Lock()
	b.conn = conn
	b.joined = joined
	b.mu.Unlock()
}

// connect dials (TLS when requested), registers and joins.
func (b *bot) connect() error {
	var conn net.Conn
	var err error
	d := &net.Dialer{Timeout: 10 * time.Second}
	if b.useTLS {
		host, _, serr := net.SplitHostPort(b.server)
		if serr != nil {
			return fmt.Errorf("server address: %w", serr)
		}
		conn, err = tls.DialWithDialer(d, "tcp", b.server, &tls.Config{
			ServerName: host,
			MinVersion: tls.VersionTLS12,
		})
	} else {
		conn, err = d.Dial("tcp", b.server)
	}
	if err != nil {
		return err
	}
	b.setConn(conn, false)
	conn.SetDeadline(time.Now().Add(30 * time.Second))
	r := bufio.NewReader(conn)

	b.write("NICK %s", b.nick)
	b.write("USER %s 0 * :%s", b.nick, b.realname)

	for {
		line, err := readLine(r)
		if err != nil {
			return fmt.Errorf("read during registration: %w", err)
		}
		fields := strings.Fields(line)
		if len(fields) == 0 {
			continue
		}
		switch {
		case fields[0] == "PING":
			b.write("PONG %s", strings.TrimPrefix(line, "PING "))
		case len(fields) > 1 && fields[1] == "001":
			b.write("JOIN %s", b.channel)
		case len(fields) > 1 && fields[1] == "366":
			// RPL_ENDOFNAMES: join complete. Drop the handshake deadline;
			// serve() takes over per-line deadlines from here on.
			conn.SetDeadline(time.Time{})
			b.setConn(conn, true)
			return nil
		case len(fields) > 1 && strings.Contains(" 403 471 473 474 475 ", " "+fields[1]+" "):
			return fmt.Errorf("joining %s failed: %s", b.channel, line)
		}
	}
}

// serve reads forever; PINGs are answered, KICKs trigger an immediate rejoin.
// Any read error (including the 4min silence watchdog) bubbles up.
func (b *bot) serve() error {
	b.mu.Lock()
	conn := b.conn
	b.mu.Unlock()
	r := bufio.NewReader(conn)
	for {
		conn.SetDeadline(time.Now().Add(4 * time.Minute))
		line, err := readLine(r)
		if err != nil {
			b.setConn(conn, false)
			return fmt.Errorf("read: %w", err)
		}
		fields := strings.Fields(line)
		switch {
		case len(fields) == 0:
			continue
		case fields[0] == "PING":
			b.write("PONG %s", strings.TrimPrefix(line, "PING "))
		case len(fields) > 3 && fields[1] == "KICK" && fields[3] == b.nick:
			log.Printf("kicked from %s, rejoining", fields[2])
			b.write("JOIN %s", b.channel)
		}
	}
}

func (b *bot) runForever() {
	for {
		if err := b.connect(); err != nil {
			log.Printf("connect/register/join: %v", err)
			time.Sleep(10 * time.Second)
			continue
		}
		log.Printf("joined %s on %s", b.channel, b.server)
		if err := b.serve(); err != nil {
			log.Printf("connection lost: %v; reconnecting", err)
		}
		time.Sleep(5 * time.Second)
	}
}

// ---------------------------------------------------------------------------
// persistent state
// ---------------------------------------------------------------------------

type state struct {
	// Disks maps mountpoint -> the remaining-% threshold it last alerted on
	// (0 = currently fine / not armed).
	Disks map[string]float64 `json:"disks"`
	// Backups maps unit name -> "ok" | "failed" | "stale".
	Backups map[string]string `json:"backups"`
	// Mem and Load: "ok" | "warn" | "crit".
	Mem  string `json:"mem"`
	Load string `json:"load"`
	// FailedUnits: sorted, comma-joined names of currently failed units that
	// are not covered by Backups ("" = none).
	FailedUnits string `json:"failed_units"`
}

func freshState() *state {
	return &state{
		Disks:   map[string]float64{},
		Backups: map[string]string{},
		Mem:     "ok",
		Load:    "ok",
	}
}

func loadState(path string) *state {
	data, err := os.ReadFile(path)
	if err != nil {
		if !os.IsNotExist(err) {
			log.Printf("loading state: %v (starting fresh)", err)
		}
		return freshState()
	}
	s := freshState()
	if err := json.Unmarshal(data, s); err != nil {
		log.Printf("parsing state: %v (starting fresh)", err)
		return freshState()
	}
	if s.Disks == nil {
		s.Disks = map[string]float64{}
	}
	if s.Backups == nil {
		s.Backups = map[string]string{}
	}
	if s.Mem == "" {
		s.Mem = "ok"
	}
	if s.Load == "" {
		s.Load = "ok"
	}
	return s
}

func (s *state) save(path string) {
	data, err := json.Marshal(s)
	if err != nil {
		log.Printf("marshaling state: %v", err)
		return
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		log.Printf("writing state: %v", err)
		return
	}
	if err := os.Rename(tmp, path); err != nil {
		log.Printf("renaming state: %v", err)
	}
}

// ---------------------------------------------------------------------------
// formatting helpers
// ---------------------------------------------------------------------------

// humanBytes auto-scales to GiB/TiB (a fleet NAS deals in both).
func humanBytes(v float64) string {
	switch {
	case v >= 1<<40:
		return fmt.Sprintf("%.1f TiB", v/(1<<40))
	default:
		return fmt.Sprintf("%.1f GiB", v/(1<<30))
	}
}

func humanDur(d time.Duration) string {
	if d < 0 {
		d = 0
	}
	h := int(d.Hours())
	switch {
	case h >= 48:
		return fmt.Sprintf("%dd%dh", h/24, h%24)
	case h >= 1:
		return fmt.Sprintf("%dh%dm", h, int(d.Minutes())%60)
	default:
		return fmt.Sprintf("%dm", int(d.Minutes()))
	}
}

func icon(level string) string {
	switch level {
	case "crit":
		return "🚨"
	case "warn":
		return "⚠️"
	default:
		return "✅"
	}
}

// ---------------------------------------------------------------------------
// probes (stdlib only: /proc + systemctl)
// ---------------------------------------------------------------------------

// Filesystems worth watching; everything else (pseudo fs, network mounts) is
// skipped. Notably cifs/smb3/nfs stay out: statfs on a hung network mount
// would block the whole poll loop.
var watchFSTypes = map[string]bool{
	"apfs": true, "btrfs": true, "exfat": true, "ext2": true, "ext3": true,
	"ext4": true, "f2fs": true, "hfs": true, "hfsplus": true, "ntfs": true,
	"ntfs3": true, "ufs": true, "vfat": true, "xfs": true, "zfs": true,
}

type mountInfo struct {
	dev        uint64
	mountpoint string
	fstype     string
}

// unescapeMount undoes the octal escapes of /proc/mounts fields.
func unescapeMount(s string) string {
	return strings.NewReplacer(`\040`, " ", `\011`, "\t", `\012`, "\n", `\134`, `\`).Replace(s)
}

// localMounts returns mountpoints to watch, deduped by device (btrfs
// subvolume mounts like /, /home, /nix share a device and identical statfs
// numbers - one entry suffices; first in sorted mountpoint order wins,
// typically "/").
func localMounts() ([]mountInfo, error) {
	data, err := os.ReadFile("/proc/mounts")
	if err != nil {
		return nil, err
	}
	var all []mountInfo
	for _, line := range strings.Split(string(data), "\n") {
		f := strings.Fields(line)
		if len(f) < 4 || !watchFSTypes[f[2]] {
			continue
		}
		// Note: do NOT filter out read-only mounts. Under systemd's
		// DynamicUser=yes (which implies ProtectSystem=strict) the unit sees
		// every mount as ro inside its mount namespace, and the disk check
		// would silently find nothing at all.
		m := mountInfo{mountpoint: unescapeMount(f[1]), fstype: f[2]}
		var st syscall.Stat_t
		if err := syscall.Stat(m.mountpoint, &st); err != nil {
			continue
		}
		m.dev = uint64(st.Dev)
		all = append(all, m)
	}
	sort.Slice(all, func(i, j int) bool { return all[i].mountpoint < all[j].mountpoint })
	seen := map[uint64]bool{}
	out := all[:0]
	for _, m := range all {
		if seen[m.dev] {
			continue
		}
		seen[m.dev] = true
		out = append(out, m)
	}
	return out, nil
}

// fsSample is one mountpoint's free space as of one poll.
type fsSample struct {
	mountpoint         string
	totalBytes, availB float64
	pctFree            float64
}

func sampleFS(m mountInfo) (fsSample, error) {
	var st syscall.Statfs_t
	if err := syscall.Statfs(m.mountpoint, &st); err != nil {
		return fsSample{}, err
	}
	if st.Blocks == 0 {
		return fsSample{}, fmt.Errorf("%s: zero blocks", m.mountpoint)
	}
	total := float64(st.Blocks) * float64(st.Bsize)
	avail := float64(st.Bavail) * float64(st.Bsize)
	return fsSample{
		mountpoint: m.mountpoint,
		totalBytes: total,
		availB:     avail,
		pctFree:    100 * avail / total,
	}, nil
}

// parseThresholds parses "15,10,5,3,2,1" into descending percents.
func parseThresholds(s string) ([]float64, error) {
	var out []float64
	for _, part := range strings.Split(s, ",") {
		v, err := strconv.ParseFloat(strings.TrimSpace(part), 64)
		if err != nil || v <= 0 || v >= 100 {
			return nil, fmt.Errorf("invalid threshold %q", part)
		}
		out = append(out, v)
	}
	sort.Sort(sort.Reverse(sort.Float64Slice(out)))
	if len(out) == 0 {
		return nil, fmt.Errorf("no thresholds given")
	}
	return out, nil
}

// worstCrossed returns the smallest threshold below pctFree (i.e. the most
// severe threshold whose boundary has been crossed), or 0 if none crossed.
func worstCrossed(pctFree float64, thresholds []float64) float64 {
	worst := 0.0
	for _, t := range thresholds {
		if pctFree < t && (worst == 0 || t < worst) {
			worst = t
		}
	}
	return worst
}

// memSample returns the percentage of MemAvailable relative to MemTotal.
func memSample() (pctAvail, availBytes, totalBytes float64, err error) {
	data, err := os.ReadFile("/proc/meminfo")
	if err != nil {
		return 0, 0, 0, err
	}
	var totalKB, availKB float64
	for _, line := range strings.Split(string(data), "\n") {
		f := strings.Fields(line)
		if len(f) < 2 {
			continue
		}
		v, perr := strconv.ParseFloat(f[1], 64)
		if perr != nil {
			continue
		}
		switch f[0] {
		case "MemTotal:":
			totalKB = v
		case "MemAvailable:":
			availKB = v
		}
	}
	if totalKB <= 0 {
		return 0, 0, 0, fmt.Errorf("MemTotal not found")
	}
	return 100 * availKB / totalKB, availKB * 1024, totalKB * 1024, nil
}

// loadSample returns the 15min loadavg. Fields: 1min 5min 15min ...
func loadSample() (float64, error) {
	data, err := os.ReadFile("/proc/loadavg")
	if err != nil {
		return 0, err
	}
	f := strings.Fields(string(data))
	if len(f) < 3 {
		return 0, fmt.Errorf("unexpected /proc/loadavg: %q", string(data))
	}
	return strconv.ParseFloat(f[2], 64)
}

func systemctl(args ...string) (string, error) {
	// no CombinedOutput: stderr of a systemctl failure is noise ("Warning:
	// ..."), the exit error is signal enough; callers log it.
	out, err := exec.Command("systemctl", args...).Output()
	return string(out), err
}

// listUnits runs systemctl list-units with the given args (already including
// --no-legend --no-pager --plain) and returns the unit names.
func listUnits(args ...string) ([]string, error) {
	out, err := systemctl(args...)
	if err != nil {
		return nil, err
	}
	var names []string
	for _, line := range strings.Split(out, "\n") {
		f := strings.Fields(line)
		if len(f) > 0 && strings.Contains(f[0], ".") {
			names = append(names, f[0])
		}
	}
	return names, nil
}

type unitProps struct {
	activeState   string
	subState      string
	result        string
	inactiveMonoS float64 // seconds since boot (0 = never became inactive)
}

func showUnit(unit string) (unitProps, error) {
	out, err := systemctl("show", unit,
		"--property=ActiveState,SubState,Result,InactiveEnterTimestampMonotonic")
	if err != nil {
		return unitProps{}, err
	}
	p := unitProps{}
	for _, line := range strings.Split(strings.TrimSpace(out), "\n") {
		k, v, ok := strings.Cut(line, "=")
		if !ok {
			continue
		}
		switch k {
		case "ActiveState":
			p.activeState = v
		case "SubState":
			p.subState = v
		case "Result":
			p.result = v
		case "InactiveEnterTimestampMonotonic":
			us, _ := strconv.ParseFloat(v, 64)
			p.inactiveMonoS = us / 1e6
		}
	}
	return p, nil
}

// uptimeSeconds from /proc/uptime, for comparisons against
// InactiveEnterTimestampMonotonic (both monotonic since boot).
func uptimeSeconds() (float64, error) {
	data, err := os.ReadFile("/proc/uptime")
	if err != nil {
		return 0, err
	}
	f := strings.Fields(string(data))
	if len(f) == 0 {
		return 0, fmt.Errorf("empty /proc/uptime")
	}
	return strconv.ParseFloat(f[0], 64)
}

// classifyBackupUnit decides the health of one restic-backups-* service.
// uptimeS is seconds since boot; maxStale is how long a unit may go without
// a successful run before it counts as stale.
func classifyBackupUnit(p unitProps, uptimeS float64, maxStale time.Duration) (status, detail string) {
	switch p.activeState {
	case "active", "activating", "deactivating", "reloading":
		return "ok", "currently running"
	case "failed":
		return "failed", "result=" + p.result
	}
	// inactive
	if p.result == "success" && p.inactiveMonoS > 0 {
		age := time.Duration(uptimeS-p.inactiveMonoS) * time.Second
		if age > maxStale {
			return "stale", fmt.Sprintf("no successful run in %s", humanDur(age))
		}
		return "ok", fmt.Sprintf("last success %s ago", humanDur(age))
	}
	if p.result == "" {
		// Never ran. Only complain once boot uptime exceeds the freshness
		// window (fresh boot right after a long outage would otherwise
		// false-positive).
		if time.Duration(uptimeS)*time.Second > maxStale {
			return "stale", "never completed a run since boot"
		}
		return "ok", "never ran, but boot is fresh"
	}
	// inactive with a failure-ish result (shouldn't normally persist;
	// systemctl --failed catches ActiveState=failed, but cover it anyway)
	return "failed", "result=" + p.result
}

// ---------------------------------------------------------------------------
// checks: compare probe results with persisted state, emit transitions
// ---------------------------------------------------------------------------

type config struct {
	hostname       string
	diskThresholds []float64
	memWarnPct     float64
	memCritPct     float64
	loadWarn       float64
	loadCrit       float64
	backupPrefix   string
	backupMaxStale time.Duration
	ownUnit        string
}

// levelRank orders ok < warn < crit for transition comparison.
var levelRank = map[string]int{"ok": 0, "warn": 1, "crit": 2}

type poster interface {
	privmsg(text string) bool
}

type nullPoster struct{}

func (nullPoster) privmsg(text string) bool {
	fmt.Println("POST:", text)
	return true
}

// check posts one message. level: "warn"/"crit" for alerts, "ok" for
// recovery notices.
func post(p poster, cfg *config, level, format string, args ...any) {
	msg := fmt.Sprintf("%s syswatch: %s: %s", icon(level), cfg.hostname, fmt.Sprintf(format, args...))
	p.privmsg(msg)
	time.Sleep(300 * time.Millisecond)
}

// checkDisks: alert on crossing to a more severe remaining-% threshold; one
// recovery message when climbing a threshold back up. Hysteresis for
// recovery: pctFree must exceed (alerted threshold + 1).
func checkDisks(p poster, cfg *config, st *state) {
	mounts, err := localMounts()
	if err != nil {
		log.Printf("disk check: listing mounts: %v", err)
		return
	}
	for _, m := range mounts {
		s, err := sampleFS(m)
		if err != nil {
			log.Printf("disk check: %v", err)
			continue
		}
		prev := st.Disks[m.mountpoint] // 0 = ok/unarmed
		cur := worstCrossed(s.pctFree, cfg.diskThresholds)
		switch {
		case cur != 0 && (prev == 0 || cur < prev):
			// Freshly crossed a more severe threshold (smaller number =
			// fuller disk).
			lvl := "warn"
			if cur == cfg.diskThresholds[len(cfg.diskThresholds)-1] {
				lvl = "crit"
			}
			post(p, cfg, lvl, "disk %s: %.1f%% free (%s of %s) - below %g%% remaining",
				m.mountpoint, s.pctFree, humanBytes(s.availB), humanBytes(s.totalBytes), cur)
			st.Disks[m.mountpoint] = cur
		case prev != 0 && s.pctFree >= prev+1:
			post(p, cfg, "ok", "disk %s: back to %.1f%% free (%s of %s)",
				m.mountpoint, s.pctFree, humanBytes(s.availB), humanBytes(s.totalBytes))
			// Stay armed at whatever severity still applies.
			st.Disks[m.mountpoint] = worstCrossed(s.pctFree, cfg.diskThresholds)
		}
	}
	// Drop state for mounts that vanished.
	valid := map[string]bool{}
	for _, m := range mounts {
		valid[m.mountpoint] = true
	}
	for mp := range st.Disks {
		if !valid[mp] {
			delete(st.Disks, mp)
		}
	}
}

func checkMem(p poster, cfg *config, st *state) {
	pct, avail, total, err := memSample()
	if err != nil {
		log.Printf("mem check: %v", err)
		return
	}
	cur := "ok"
	switch {
	case pct < cfg.memCritPct:
		cur = "crit"
	case pct < cfg.memWarnPct:
		cur = "warn"
	}
	if levelRank[cur] > levelRank[st.Mem] {
		post(p, cfg, cur, "memory: %.1f%% available (%s of %s)", pct, humanBytes(avail), humanBytes(total))
	} else if levelRank[cur] < levelRank[st.Mem] {
		post(p, cfg, "ok", "memory: back to %.1f%% available (%s of %s)", pct, humanBytes(avail), humanBytes(total))
	}
	st.Mem = cur
}

func checkLoad(p poster, cfg *config, st *state) {
	load, err := loadSample()
	if err != nil {
		log.Printf("load check: %v", err)
		return
	}
	cores := runtime.NumCPU()
	if cores < 1 {
		cores = 1
	}
	ratio := load / float64(cores)
	cur := "ok"
	switch {
	case ratio >= cfg.loadCrit:
		cur = "crit"
	case ratio >= cfg.loadWarn:
		cur = "warn"
	}
	if levelRank[cur] > levelRank[st.Load] {
		post(p, cfg, cur, "load: 15min avg %.1f on %d cores (%.2f/core)", load, cores, ratio)
	} else if levelRank[cur] < levelRank[st.Load] {
		post(p, cfg, "ok", "load: back to 15min avg %.1f on %d cores (%.2f/core)", load, cores, ratio)
	}
	st.Load = cur
}

// checkBackups watches restic-backups-*.service units for ActiveState=failed
// and for missing successful runs (staleness) - the two failure modes that
// kept the immich-data + homeassistant repos unprotected for ~3 months in
// 2026 while timers kept "firing" into a stranded repo lock.
func checkBackups(p poster, cfg *config, st *state) {
	units, err := listUnits("list-units", "--all", "--no-legend", "--no-pager", "--plain",
		cfg.backupPrefix+"*.service")
	if err != nil {
		log.Printf("backup check: listing units: %v", err)
		return
	}
	uptimeS, err := uptimeSeconds()
	if err != nil {
		log.Printf("backup check: uptime: %v", err)
		return
	}
	seen := map[string]bool{}
	for _, unit := range units {
		seen[unit] = true
		name := strings.TrimSuffix(strings.TrimPrefix(unit, cfg.backupPrefix), ".service")
		props, err := showUnit(unit)
		if err != nil {
			log.Printf("backup check: show %s: %v", unit, err)
			continue
		}
		status, detail := classifyBackupUnit(props, uptimeS, cfg.backupMaxStale)
		prev, known := st.Backups[unit]
		switch {
		case status != "ok" && (!known || prev != status):
			lvl := "crit"
			post(p, cfg, lvl, "backup %s: %s", name, detail)
		case status == "ok" && known && prev != "ok" && prev != "":
			post(p, cfg, "ok", "backup %s: healthy again (%s)", name, detail)
		}
		st.Backups[unit] = status
	}
	for unit := range st.Backups {
		if !seen[unit] {
			delete(st.Backups, unit)
		}
	}
}

// checkFailedUnits summarizes failed units not covered by checkBackups (and
// not syswatch itself) - the generic "something out of the ordinary" signal.
func checkFailedUnits(p poster, cfg *config, st *state) {
	failed, err := listUnits("list-units", "--failed", "--no-legend", "--no-pager", "--plain")
	if err != nil {
		log.Printf("failed-units check: %v", err)
		return
	}
	var names []string
	for _, u := range failed {
		if u == cfg.ownUnit || strings.HasPrefix(u, cfg.backupPrefix) {
			continue
		}
		names = append(names, u)
	}
	sort.Strings(names)
	joined := strings.Join(names, ",")
	if joined == st.FailedUnits {
		return
	}
	if len(names) == 0 {
		post(p, cfg, "ok", "no failed systemd units anymore")
	} else {
		shown := names
		more := ""
		if len(shown) > 5 {
			more = fmt.Sprintf(" (+%d more)", len(shown)-5)
			shown = shown[:5]
		}
		post(p, cfg, "warn", "%d failed systemd unit(s): %s%s", len(names), strings.Join(shown, ", "), more)
	}
	st.FailedUnits = joined
}

func runAllChecks(p poster, cfg *config, st *state, statePath string, persist bool) {
	checkBackups(p, cfg, st)
	checkDisks(p, cfg, st)
	checkMem(p, cfg, st)
	checkLoad(p, cfg, st)
	checkFailedUnits(p, cfg, st)
	if persist {
		st.save(statePath)
	}
}

// ---------------------------------------------------------------------------
// main
// ---------------------------------------------------------------------------

func main() {
	server := flag.String("server", "irc.emile.space:6697", "IRC server address (host:port)")
	useTLS := flag.Bool("tls", false, "connect with TLS (ircs) instead of plaintext")
	nick := flag.String("nick", "syswatch", "IRC nick to connect as")
	channel := flag.String("channel", "#emile", "channel to post alerts in")
	statePath := flag.String("state", "/var/lib/syswatch/state.json", "path tracking posted alert states")
	interval := flag.Duration("interval", 5*time.Minute, "poll interval")
	diskLevels := flag.String("disk-levels", "15,10,5,3,2,1", "descending % free thresholds at which to alert")
	memWarn := flag.Float64("mem-warn-pct", 10, "alert (warn) below this % of MemAvailable/MemTotal")
	memCrit := flag.Float64("mem-crit-pct", 5, "alert (crit) below this % of MemAvailable/MemTotal")
	loadWarn := flag.Float64("load-warn", 2, "alert (warn) above this 15min loadavg/cores ratio")
	loadCrit := flag.Float64("load-crit", 4, "alert (crit) above this 15min loadavg/cores ratio")
	backupPrefix := flag.String("backup-prefix", "restic-backups-", "systemd unit prefix identifying backup services")
	backupMaxStale := flag.Duration("backup-max-stale", 26*time.Hour, "max age of the last successful backup run before alerting")
	ownUnit := flag.String("own-unit", "syswatch.service", "this bot's own unit name (excluded from failed-unit alerts)")
	hostnameOverride := flag.String("hostname", "", "name used in messages (default: os.Hostname, short form)")
	dryRun := flag.Bool("dry-run", false, "run one poll, print what would be posted, no connect, no state write")
	say := flag.Bool("say", false, "one-shot: connect, post each remaining argument as a channel line, quit (for report scripts)")
	flag.Parse()

	if *say {
		if flag.NArg() == 0 {
			log.Fatal("--say requires at least one message argument")
		}
		b := &bot{
			server:   *server,
			useTLS:   *useTLS,
			nick:     *nick,
			channel:  *channel,
			realname: "System Watch Bot",
		}
		if err := b.connect(); err != nil {
			log.Fatalf("connect: %v", err)
		}
		for _, msg := range flag.Args() {
			if !b.privmsg(msg) {
				log.Fatal("posting failed")
			}
		}
		b.mu.Lock()
		b.write("QUIT :report delivered")
		b.mu.Unlock()
		// give the socket a moment to flush before process exit
		time.Sleep(500 * time.Millisecond)
		return
	}

	thresholds, err := parseThresholds(*diskLevels)
	if err != nil {
		log.Fatalf("disk-levels: %v", err)
	}

	host := *hostnameOverride
	if host == "" {
		h, err := os.Hostname()
		if err != nil {
			log.Fatalf("hostname: %v", err)
		}
		host = h
	}
	// labels in messages use the short hostname
	if i := strings.IndexByte(host, '.'); i > 0 {
		host = host[:i]
	}

	cfg := &config{
		hostname:       host,
		diskThresholds: thresholds,
		memWarnPct:     *memWarn,
		memCritPct:     *memCrit,
		loadWarn:       *loadWarn,
		loadCrit:       *loadCrit,
		backupPrefix:   *backupPrefix,
		backupMaxStale: *backupMaxStale,
		ownUnit:        *ownUnit,
	}

	st := loadState(*statePath)

	if *dryRun {
		runAllChecks(nullPoster{}, cfg, st, *statePath, false)
		return
	}

	b := &bot{
		server:   *server,
		useTLS:   *useTLS,
		nick:     *nick,
		channel:  *channel,
		realname: "System Watch Bot",
	}
	go b.runForever()

	runAllChecks(b, cfg, st, *statePath, true)
	ticker := time.NewTicker(*interval)
	defer ticker.Stop()
	for range ticker.C {
		runAllChecks(b, cfg, st, *statePath, true)
	}
}
