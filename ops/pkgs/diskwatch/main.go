// diskwatch watches the root filesystem of every fleet host (via their
// node-exporter /metrics endpoints) and posts level transitions to an IRC
// channel, staying joined to the channel continuously (systemd keeps it
// alive). The minimal persistent-IRC plumbing below is shared, copy-pasted,
// with weatherman/xkcdbot/tagesschau/dwdwarn: keep them in sync or factor
// properly one day.
package main

import (
	"bufio"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"os"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"
)

type target struct {
	Name string `json:"name"`
	URL  string `json:"url"`
}

// diskLevel is how bad a host's root filesystem utilization is. Levels only
// ESCALATE through the channel (an ok host says nothing), plus one recovery
// message when a host drops a level.
type diskLevel int

const (
	levelUnknown diskLevel = iota - 1
	levelOK
	levelWarn
	levelCrit
)

const (
	warnPct = 85.0
	critPct = 95.0
)

func classify(pct float64) diskLevel {
	switch {
	case pct >= critPct:
		return levelCrit
	case pct >= warnPct:
		return levelWarn
	default:
		return levelOK
	}
}

var (
	sizeRe  = prometheusMetricLine("node_filesystem_size_bytes")
	availRe = prometheusMetricLine("node_filesystem_avail_bytes")
)

// prometheusMetricLine matches a node's exporter line for the given metric
// restricted to mountpoint="/" (label order varies), e.g.
// node_filesystem_size_bytes{device="/dev/sda1",fstype="ext4",mountpoint="/"} 5.1e9
func prometheusMetricLine(metric string) *regexp.Regexp {
	return regexp.MustCompile(`^` + metric + `\{[^}]*mountpoint="/"[^}]*\}\s+([0-9.eE+-]+)`)
}

func fetchRootUsage(url string) (usedBytes, sizeBytes float64, err error) {
	c := &http.Client{Timeout: 10 * time.Second}
	resp, err := c.Get(url)
	if err != nil {
		return 0, 0, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return 0, 0, fmt.Errorf("metrics endpoint returned %s", resp.Status)
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, 32<<20))
	if err != nil {
		return 0, 0, err
	}
	var availBytes float64
	seenAvail := false
	for _, line := range strings.Split(string(body), "\n") {
		if m := sizeRe.FindStringSubmatch(line); m != nil {
			sizeBytes, _ = strconv.ParseFloat(m[1], 64)
		}
		if m := availRe.FindStringSubmatch(line); m != nil {
			availBytes, _ = strconv.ParseFloat(m[1], 64)
			seenAvail = true
		}
	}
	if sizeBytes <= 0 || !seenAvail {
		return 0, 0, fmt.Errorf("no root filesystem metrics found")
	}
	return sizeBytes - availBytes, sizeBytes, nil
}

func formatHost(name string, used, total float64, level diskLevel) string {
	icon := "⚠️"
	if level == levelCrit {
		icon = "🚨"
	}
	return fmt.Sprintf("%s diskwatch: %s / at %.0f%% (%.1f of %.1f GiB used)",
		icon, name, 100*used/total, used/(1<<30), total/(1<<30))
}

// ---------------------------------------------------------------------------
// persistent mini IRC client
// ---------------------------------------------------------------------------

// bot owns its single upstream connection. privmsg may be called from any
// goroutine; runForever owns the read loop.
type bot struct {
	server, nick, channel, realname string

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

// connect dials, registers and joins.
func (b *bot) connect() error {
	conn, err := net.DialTimeout("tcp", b.server, 10*time.Second)
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

type hostState struct {
	Level int     `json:"level"`
	Pct   float64 `json:"pct"`
}

func loadState(path string) (map[string]hostState, error) {
	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return map[string]hostState{}, nil
	}
	if err != nil {
		return nil, err
	}
	var state map[string]hostState
	if err := json.Unmarshal(data, &state); err != nil {
		return nil, err
	}
	return state, nil
}

func saveState(path string, state map[string]hostState) error {
	data, err := json.Marshal(state)
	if err != nil {
		return err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

func main() {
	server := flag.String("server", "127.0.0.1:6667", "IRC server address (host:port, plaintext)")
	nick := flag.String("nick", "diskwatch", "IRC nick to connect as")
	channel := flag.String("channel", "#emile", "channel to post disk alerts in")
	targetsPath := flag.String("targets", "/etc/diskwatch/targets.json", "path to the JSON target list (name+metrics URL)")
	statePath := flag.String("state", "/var/lib/diskwatch/state.json", "path tracking per-host disk level")
	interval := flag.Duration("interval", 5*time.Minute, "poll interval")
	dryRun := flag.Bool("dry-run", false, "print current levels instead of connecting")
	flag.Parse()

	raw, err := os.ReadFile(*targetsPath)
	if err != nil {
		log.Fatalf("reading targets: %v", err)
	}
	var targets []target
	if err := json.Unmarshal(raw, &targets); err != nil {
		log.Fatalf("parsing targets: %v", err)
	}

	state, err := loadState(*statePath)
	if err != nil {
		log.Printf("loading state: %v (starting fresh)", err)
		state = map[string]hostState{}
	}

	if *dryRun {
		for _, t := range targets {
			used, total, err := fetchRootUsage(t.URL)
			if err != nil {
				fmt.Printf("%s: %v\n", t.Name, err)
				continue
			}
			pct := 100 * used / total
			fmt.Printf("%s: %.1f%% (%.1f of %.1f GiB)\n", t.Name, pct, used/(1<<30), total/(1<<30))
		}
		return
	}

	b := &bot{
		server:   *server,
		nick:     *nick,
		channel:  *channel,
		realname: "Disk Watch Bot",
	}
	go b.runForever()

	check := func(t target) (used, total float64, ok bool) {
		used, total, err := fetchRootUsage(t.URL)
		if err != nil {
			log.Printf("%s: %v", t.Name, err)
			return 0, 0, false
		}
		return used, total, true
	}

	for range time.Tick(*interval) {
		var wg sync.WaitGroup
		var mu sync.Mutex
		usage := map[string][2]float64{}
		for _, t := range targets {
			t := t
			wg.Add(1)
			go func() {
				defer wg.Done()
				used, total, ok := check(t)
				if ok {
					mu.Lock()
					usage[t.Name] = [2]float64{used, total}
					mu.Unlock()
				}
			}()
		}
		wg.Wait()

		for _, t := range targets {
			u, ok := usage[t.Name]
			if !ok {
				continue // unreachable/broken metrics: uptimebot's job, not ours
			}
			used, total := u[0], u[1]
			pct := 100 * used / total
			level := classify(pct)
			prev, seen := state[t.Name]
			state[t.Name] = hostState{Level: int(level), Pct: pct}

			switch {
			case !seen:
				// First sighting is recorded silently; only a warning-level
				// first sighting is worth announcing.
				if level >= levelWarn {
					b.privmsg(formatHost(t.Name, used, total, level))
					time.Sleep(300 * time.Millisecond)
				}
			case level > diskLevel(prev.Level):
				b.privmsg(formatHost(t.Name, used, total, level))
				time.Sleep(300 * time.Millisecond)
			case level < diskLevel(prev.Level):
				b.privmsg(fmt.Sprintf("✅ diskwatch: %s / back to %.0f%% (was %.0f%%)",
					t.Name, pct, prev.Pct))
				time.Sleep(300 * time.Millisecond)
			}
		}

		if err := saveState(*statePath, state); err != nil {
			log.Printf("saving state: %v", err)
		}
	}
}
