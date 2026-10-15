// dwdwarn posts active Deutscher-Wetterdienst warnings for Düsseldorf to an
// IRC channel as they are issued, staying joined to the channel continuously
// (systemd keeps it alive). The minimal persistent-IRC plumbing below is
// shared, copy-pasted, with weatherman/xkcdbot/tagesschau/diskwatch: keep
// them in sync or factor properly one day.
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
	"strings"
	"sync"
	"time"
)

const warningsURL = "https://www.dwd.de/DWD/warnungen/warnapp/json/warnings.json"

// DWD warn cell ids for Düsseldorf: the four city parts, plus the whole-city
// aggregate cell that city-wide warnings are keyed under.
var warnCellIDs = []string{
	"705111001", // Düsseldorf-Mitte/West
	"705111002", // Düsseldorf-Nord
	"705111003", // Düsseldorf-Süd
	"705111004", // Düsseldorf-Ost
	"105111000", // Stadt Düsseldorf (aggregate)
}

type warning struct {
	Event       string `json:"event"`
	Headline    string `json:"headline"`
	Description string `json:"description"`
	Level       int    `json:"level"`
	Start       int64  `json:"start"`
	End         int64  `json:"end"`
	RegionName  string `json:"regionName"`
}

// warnings.json is JSONP: warnWetter.loadWarnings({...json...})
func fetchWarnings() (map[string][]warning, error) {
	c := &http.Client{Timeout: 15 * time.Second}
	resp, err := c.Get(warningsURL)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("dwd returned %s", resp.Status)
	}
	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	s := strings.TrimSpace(string(raw))
	if i := strings.Index(s, "("); i >= 0 {
		s = s[i+1:]
	}
	if i := strings.LastIndex(s, ")"); i >= 0 {
		s = s[:i]
	}
	var out struct {
		Warnings map[string][]warning `json:"warnings"`
	}
	if err := json.Unmarshal([]byte(s), &out); err != nil {
		return nil, fmt.Errorf("parsing dwd warnings: %w", err)
	}
	return out.Warnings, nil
}

// key identifies one warning issuance; republished variants of the same
// event with a different end time re-announce (that's a real update).
func key(cellID string, w warning) string {
	return fmt.Sprintf("%s|%s|%d|%d", cellID, w.Event, w.Start, w.End)
}

var levelIcon = map[int]string{1: "🟡", 2: "🟠", 3: "🔴", 4: "🟣"}

func formatWarnings(cellID string, w warning, loc *time.Location) string {
	icon := levelIcon[w.Level]
	if icon == "" {
		icon = "⚠️"
	}
	until := time.UnixMilli(w.End).In(loc)
	return fmt.Sprintf("%s %s: %s (bis %s)",
		icon, w.RegionName, strings.TrimSpace(w.Headline), until.Format("Mon 15:04"))
}

// loadSeen/saveSeen persist the set of already-announced warning keys.
func loadSeen(path string) (map[string]bool, error) {
	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return map[string]bool{}, nil
	}
	if err != nil {
		return nil, err
	}
	var seen map[string]bool
	if err := json.Unmarshal(data, &seen); err != nil {
		return nil, err
	}
	return seen, nil
}

func saveSeen(path string, seen map[string]bool) error {
	data, err := json.Marshal(seen)
	if err != nil {
		return err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, path)
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

func main() {
	server := flag.String("server", "127.0.0.1:6667", "IRC server address (host:port, plaintext)")
	nick := flag.String("nick", "dwdwarn", "IRC nick to connect as")
	channel := flag.String("channel", "#emile", "channel to post warnings in")
	state := flag.String("state", "/var/lib/dwdwarn/seen.json", "path tracking announced warnings")
	interval := flag.Duration("interval", 10*time.Minute, "poll interval for the DWD feed")
	dryRun := flag.Bool("dry-run", false, "print currently active Düsseldorf warnings instead of connecting")
	flag.Parse()

	loc, err := time.LoadLocation("Europe/Berlin")
	if err != nil {
		log.Fatalf("loading timezone: %v", err)
	}

	if *dryRun {
		w, err := fetchWarnings()
		if err != nil {
			log.Fatalf("fetching warnings: %v", err)
		}
		for _, cellID := range warnCellIDs {
			for _, wn := range w[cellID] {
				fmt.Println(formatWarnings(cellID, wn, loc))
			}
		}
		return
	}

	b := &bot{
		server:   *server,
		nick:     *nick,
		channel:  *channel,
		realname: "DWD Warnings Bot",
	}
	go b.runForever()

	seen, err := loadSeen(*state)
	if err != nil {
		log.Printf("loading state: %v (starting fresh)", err)
		seen = map[string]bool{}
	}

	for range time.Tick(*interval) {
		w, err := fetchWarnings()
		if err != nil {
			log.Printf("fetching warnings: %v", err)
			continue
		}

		var active []string // cell IDs with at least one active warning
		var newMsgs []string
		for _, cellID := range warnCellIDs {
			warnings := w[cellID]
			if len(warnings) > 0 {
				active = append(active, cellID)
			}
			for _, wn := range warnings {
				k := key(cellID, wn)
				if seen[k] {
					continue
				}
				newMsgs = append(newMsgs, formatWarnings(cellID, wn, loc))
				seen[k] = true
			}
		}

		posted := 0
		for _, m := range newMsgs {
			if !b.privmsg(m) {
				log.Printf("not joined, warning not posted: %s", m)
				break
			}
			posted++
			time.Sleep(300 * time.Millisecond)
		}

		// Entwarnung: a previously-active warn cell went quiet.
		if posted > 0 {
			previouslyActive := map[string]bool{}
			for k := range seen {
				cellID := strings.SplitN(k, "|", 2)[0]
				previouslyActive[cellID] = true
			}
			activeSet := map[string]bool{}
			for _, c := range active {
				activeSet[c] = true
			}
			for _, cellID := range warnCellIDs {
				if previouslyActive[cellID] && !activeSet[cellID] {
					b.privmsg(fmt.Sprintf("✅ %s: keine aktiven Warnungen mehr", cellRegionName(cellID)))
					time.Sleep(300 * time.Millisecond)
				}
			}
		}

		// Garbage-collect keys whose warnings have expired so the file
		// doesn't grow forever.
		liveKeys := map[string]bool{}
		for _, cellID := range warnCellIDs {
			for _, wn := range w[cellID] {
				liveKeys[key(cellID, wn)] = true
			}
		}
		for k := range seen {
			if !liveKeys[k] {
				delete(seen, k)
			}
		}

		if err := saveSeen(*state, seen); err != nil {
			log.Printf("saving state: %v", err)
		}
	}
}

// cellRegionName returns a readable name when we never saw an actual warning
// for that cell (regionName then only exists inside the warning itself).
func cellRegionName(cellID string) string {
	switch cellID {
	case "705111001":
		return "Düsseldorf-Mitte/West"
	case "705111002":
		return "Düsseldorf-Nord"
	case "705111003":
		return "Düsseldorf-Süd"
	case "705111004":
		return "Düsseldorf-Ost"
	default:
		return "Düsseldorf"
	}
}
