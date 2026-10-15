// xkcdbot posts new xkcd comics to an IRC channel and stays joined to that
// channel continuously (systemd keeps it alive). The minimal persistent-IRC
// plumbing below is shared, copy-pasted, with weatherman and the tagesschau
// bot: keep the three in sync or factor properly one day.
package main

import (
	"bufio"
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"net"
	"net/http"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"
)

const latestURL = "https://xkcd.com/info.0.json"

type comic struct {
	Num   int    `json:"num"`
	Title string `json:"title"`
	Img   string `json:"img"`
	Alt   string `json:"alt"`
}

func fetchLatest() (comic, error) {
	var c comic
	hc := &http.Client{Timeout: 15 * time.Second}
	resp, err := hc.Get(latestURL)
	if err != nil {
		return c, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return c, fmt.Errorf("xkcd returned %s", resp.Status)
	}
	if err := json.NewDecoder(resp.Body).Decode(&c); err != nil {
		return c, err
	}
	return c, nil
}

func readLastSeen(path string) (int, error) {
	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return 0, nil
	}
	if err != nil {
		return 0, err
	}
	return strconv.Atoi(strings.TrimSpace(string(data)))
}

func writeLastSeen(path string, num int) error {
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, []byte(strconv.Itoa(num)), 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

func formatMessage(c comic) string {
	return fmt.Sprintf("xkcd #%d: %s — https://xkcd.com/%d/ (%s)", c.Num, c.Title, c.Num, c.Img)
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

// connect dials, registers and joins. Failures are fatal: systemd restarts.
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
			b.write("PONG %s", strings.TrimPrefix(line, "PONG "))
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
	nick := flag.String("nick", "xkcdbot", "IRC nick to connect as")
	channel := flag.String("channel", "#emile", "channel to post new comics in")
	state := flag.String("state", "/var/lib/xkcdbot/last-seen", "path tracking the last comic number seen")
	interval := flag.Duration("interval", 30*time.Minute, "poll interval for new comics")
	dryRun := flag.Bool("dry-run", false, "print the latest comic without connecting or touching state")
	flag.Parse()

	if *dryRun {
		c, err := fetchLatest()
		if err != nil {
			log.Fatalf("fetching latest comic: %v", err)
		}
		fmt.Println(formatMessage(c))
		return
	}

	b := &bot{
		server:   *server,
		nick:     *nick,
		channel:  *channel,
		realname: "xkcd Bot",
	}
	go b.runForever()

	for range time.Tick(*interval) {
		c, err := fetchLatest()
		if err != nil {
			log.Printf("fetching latest comic: %v", err)
			continue
		}
		lastSeen, err := readLastSeen(*state)
		if err != nil {
			log.Printf("reading state: %v", err)
			continue
		}
		// First run (no state file yet): establish the baseline silently, so
		// deploying the bot doesn't immediately announce whatever is already
		// the latest comic as "new".
		if lastSeen == 0 {
			if err := writeLastSeen(*state, c.Num); err != nil {
				log.Printf("writing initial state: %v", err)
			} else {
				log.Printf("initialized at #%d, no announcement", c.Num)
			}
			continue
		}
		if c.Num <= lastSeen {
			continue
		}
		if !b.privmsg(formatMessage(c)) {
			log.Printf("not joined, comic #%d not posted (will retry on next poll)", c.Num)
			continue
		}
		if err := writeLastSeen(*state, c.Num); err != nil {
			log.Printf("writing state: %v", err)
		}
	}
}
