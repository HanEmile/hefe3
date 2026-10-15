// weatherman posts a daily Düsseldorf forecast to an IRC channel and stays
// joined to that channel continuously (systemd keeps it alive). The minimal
// persistent-IRC plumbing below is shared, copy-pasted, with xkcdbot and
// the tagesschau bot: keep the three in sync or factor properly one day.
package main

import (
	"bufio"
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"net"
	"net/http"
	"strings"
	"sync"
	"time"
)

// Düsseldorf, forecast_days=1 so the hourly arrays cover exactly today
// (local midnight to midnight in the given timezone).
const weatherURL = "https://api.open-meteo.com/v1/forecast?latitude=51.2232&longitude=6.7793&daily=sunrise,sunset&hourly=temperature_2m,precipitation,wind_speed_10m,wind_gusts_10m&timezone=Europe%2FBerlin&forecast_days=1&wind_speed_unit=kn"

// Daily post time, local to the location the forecast is for.
const postHour = 8
const postLocName = "Europe/Berlin"

type forecast struct {
	Hourly struct {
		Temperature   []float64 `json:"temperature_2m"`
		Precipitation []float64 `json:"precipitation"`
		WindSpeed     []float64 `json:"wind_speed_10m"`
		WindGusts     []float64 `json:"wind_gusts_10m"`
	} `json:"hourly"`
	Daily struct {
		Sunrise []string `json:"sunrise"`
		Sunset  []string `json:"sunset"`
	} `json:"daily"`
}

func fetchForecast() (forecast, error) {
	var f forecast
	c := &http.Client{Timeout: 15 * time.Second}
	resp, err := c.Get(weatherURL)
	if err != nil {
		return f, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return f, fmt.Errorf("open-meteo returned %s", resp.Status)
	}
	if err := json.NewDecoder(resp.Body).Decode(&f); err != nil {
		return f, err
	}
	return f, nil
}

func minMax(xs []float64) (float64, float64) {
	min, max := xs[0], xs[0]
	for _, x := range xs[1:] {
		if x < min {
			min = x
		}
		if x > max {
			max = x
		}
	}
	return min, max
}

func sum(xs []float64) float64 {
	var s float64
	for _, x := range xs {
		s += x
	}
	return s
}

// clock pulls "07:38" out of an ISO8601 timestamp like "2026-10-04T07:38".
func clock(iso string) string {
	parts := strings.SplitN(iso, "T", 2)
	if len(parts) == 2 {
		return parts[1]
	}
	return iso
}

func formatMessage(f forecast) (string, error) {
	if len(f.Hourly.Temperature) == 0 || len(f.Daily.Sunrise) == 0 || len(f.Daily.Sunset) == 0 {
		return "", fmt.Errorf("incomplete forecast data")
	}
	minT, maxT := minMax(f.Hourly.Temperature)
	precip := sum(f.Hourly.Precipitation)
	_, maxGust := minMax(f.Hourly.WindGusts)
	return fmt.Sprintf(
		"Düsseldorf: 🌡️%.0f–%.0f°C 🌧️%.1fmm 🌬️≤%.0fkn 🌅%s 🌇%s",
		minT, maxT, precip, maxGust,
		clock(f.Daily.Sunrise[0]), clock(f.Daily.Sunset[0]),
	), nil
}

// ---------------------------------------------------------------------------
// persistent mini IRC client
// ---------------------------------------------------------------------------

// bot owns its single upstream connection. say and post may be called from
// any goroutine; run() owns the read loop and only say/privmsg write.
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

// nextDelay returns how long to wait until the next daily post time in loc.
func nextDelay(loc *time.Location, hour int) time.Duration {
	now := time.Now().In(loc)
	next := time.Date(now.Year(), now.Month(), now.Day(), hour, 0, 0, 0, loc)
	if !next.After(now) {
		next = next.AddDate(0, 0, 1)
	}
	return time.Until(next)
}

func main() {
	server := flag.String("server", "127.0.0.1:6667", "IRC server address (host:port, plaintext)")
	nick := flag.String("nick", "weatherman", "IRC nick to connect as")
	channel := flag.String("channel", "#emile", "channel to post the forecast in")
	dryRun := flag.Bool("dry-run", false, "print the forecast message instead of connecting")
	flag.Parse()

	if *dryRun {
		f, err := fetchForecast()
		if err != nil {
			log.Fatalf("fetching forecast: %v", err)
		}
		message, err := formatMessage(f)
		if err != nil {
			log.Fatalf("formatting forecast: %v", err)
		}
		fmt.Println(message)
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
		realname: "Weatherman Bot",
	}
	go b.runForever()

	for {
		time.Sleep(nextDelay(loc, postHour))
		f, err := fetchForecast()
		if err != nil {
			log.Printf("fetching forecast: %v", err)
			continue
		}
		message, err := formatMessage(f)
		if err != nil {
			log.Printf("formatting forecast: %v", err)
			continue
		}
		if !b.privmsg(message) {
			log.Printf("not joined, forecast not posted")
		}
	}
}
