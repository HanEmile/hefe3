// tagesschau posts the top three tagesschau headlines to an IRC channel at
// 08:00, 16:00 and 00:00 Europe/Berlin and stays joined to that channel
// continuously (systemd keeps it alive). Headlines are scraped from the ARD
// Text teletext front page (https://www.ard-text.de/mobil/101). The minimal
// persistent-IRC plumbing below is shared, copy-pasted, with weatherman and
// xkcdbot: keep the three in sync or factor properly one day.
package main

import (
	"bufio"
	"flag"
	"fmt"
	"html"
	"io"
	"log"
	"net"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"
)

// ARD Text (teletext): page 101 is the tagesschau news front page; the top
// headlines link from it to the individual story pages (/mobil/<page>).
const overviewURL = "https://www.ard-text.de/mobil/101"
const linkPrefix = "https://www.ard-text.de/mobil/"

// Posting times, local, and how many top stories to post.
const postLocName = "Europe/Berlin"

var postHours = []int{8, 16, 0}
const topN = 3

// Headline links look like:
//
//	<td class="text">
//		<a href="/mobil/104">
//			Kanzler Merz hält an Reformkurs seiner Regierung fest
//		</a>
//	</td>
//
// The bottom navigation table uses single-quoted hrefs, so requiring double
// quotes already filters it out; the page>=103 guard drops the remaining
// overview links (101/102 are the overviews themselves).
var headlineRe = regexp.MustCompile(`<td class="text">\s*<a href="/mobil/(\d+)">\s*([^<]+?)\s*</a>`)

// fetchTopStories returns the first topN headlines of teletext page 101.
func fetchTopStories() ([][2]string, error) {
	c := &http.Client{Timeout: 15 * time.Second}
	resp, err := c.Get(overviewURL)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("ard-text returned %s", resp.Status)
	}
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	var out [][2]string
	for _, m := range headlineRe.FindAllStringSubmatch(string(body), -1) {
		page, err := strconv.Atoi(m[1])
		if err != nil || page < 103 {
			continue
		}
		title := strings.TrimSpace(html.UnescapeString(m[2]))
		if title == "" {
			continue
		}
		out = append(out, [2]string{title, linkPrefix + m[1]})
		if len(out) >= topN {
			break
		}
	}
	if len(out) < topN {
		return nil, fmt.Errorf("only %d headlines on page 101, want %d", len(out), topN)
	}
	return out, nil
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
		// Watchdog: servers ping far more often than this; silence means the
		// connection is silently dead (e.g. NAT state lost) so reconnect.
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

// nextDelay returns how long to wait until the next posting time in loc.
func nextDelay(loc *time.Location) time.Duration {
	now := time.Now().In(loc)
	var best time.Time
	for day := 0; day <= 1; day++ {
		d := now.AddDate(0, 0, day)
		for _, h := range postHours {
			t := time.Date(d.Year(), d.Month(), d.Day(), h, 0, 0, 0, loc)
			if t.After(now) && (best.IsZero() || t.Before(best)) {
				best = t
			}
		}
	}
	return time.Until(best)
}

func postOnce(b *bot) {
	stories, err := fetchTopStories()
	if err != nil {
		log.Printf("fetching top stories: %v", err)
		return
	}
	if !b.joined {
		log.Printf("not joined yet, top stories not posted")
		return
	}
	// One message per story: just the headline title, no link.
	for i, s := range stories {
		if !b.privmsg(s[0]) {
			log.Printf("lost join mid-post on story %d", i+1)
			return
		}
		time.Sleep(300 * time.Millisecond) // keep server flood-limits happy
	}
	log.Printf("posted the top %d stories", len(stories))
}

func main() {
	server := flag.String("server", "127.0.0.1:6667", "IRC server address (host:port, plaintext)")
	nick := flag.String("nick", "tagesschau", "IRC nick to connect as")
	channel := flag.String("channel", "#emile", "channel to post stories in")
	dryRun := flag.Bool("dry-run", false, "print the current top stories instead of connecting")
	postNow := flag.Bool("post-now", false, "post immediately once joined (in addition to the schedule)")
	flag.Parse()

	if *dryRun {
		stories, err := fetchTopStories()
		if err != nil {
			log.Fatalf("fetching top stories: %v", err)
		}
		for _, s := range stories {
			fmt.Println(s[0])
		}
		return
	}

	loc, err := time.LoadLocation(postLocName)
	if err != nil {
		log.Fatalf("loading timezone %s: %v", postLocName, err)
	}

	b := &bot{
		server:   *server,
		nick:     *nick,
		channel:  *channel,
		realname: "Tagesschau Bot",
	}
	go b.runForever()

	if *postNow {
		// Give the join a moment to complete before posting.
		go func() {
			deadline := time.Now().Add(30 * time.Second)
			for time.Now().Before(deadline) {
				b.mu.Lock()
				joined := b.joined
				b.mu.Unlock()
				if joined {
					postOnce(b)
					return
				}
				time.Sleep(500 * time.Millisecond)
			}
			log.Printf("post-now: never joined within 30s")
		}()
	}

	for {
		d := nextDelay(loc)
		log.Printf("next top-stories post in %s", d.Round(time.Minute))
		time.Sleep(d)
		postOnce(b)
	}
}
