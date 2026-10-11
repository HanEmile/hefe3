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
	"strconv"
	"strings"
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
	resp, err := http.Get(latestURL)
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

func readLine(r *bufio.Reader) (string, error) {
	line, err := r.ReadString('\n')
	return strings.TrimRight(line, "\r\n"), err
}

// postToIRC does a minimal raw-protocol handshake: connect, register, join,
// wait for the join to actually complete (RPL_ENDOFNAMES), post, quit.
func postToIRC(server, nick, channel, message string) error {
	conn, err := net.DialTimeout("tcp", server, 10*time.Second)
	if err != nil {
		return err
	}
	defer conn.Close()
	conn.SetDeadline(time.Now().Add(30 * time.Second))

	r := bufio.NewReader(conn)

	fmt.Fprintf(conn, "NICK %s\r\n", nick)
	fmt.Fprintf(conn, "USER %s 0 * :xkcd Bot\r\n", nick)

	joined := false
	for !joined {
		line, err := readLine(r)
		if err != nil {
			return fmt.Errorf("read: %w", err)
		}
		fields := strings.Fields(line)
		if len(fields) == 0 {
			continue
		}
		switch {
		case fields[0] == "PING":
			fmt.Fprintf(conn, "PONG %s\r\n", strings.TrimPrefix(line, "PING "))
		case len(fields) > 1 && fields[1] == "001":
			fmt.Fprintf(conn, "JOIN %s\r\n", channel)
		case len(fields) > 1 && fields[1] == "366":
			joined = true
		}
	}

	fmt.Fprintf(conn, "PRIVMSG %s :%s\r\n", channel, message)
	fmt.Fprintf(conn, "QUIT :new comic posted\r\n")
	io.Copy(io.Discard, r) // drain until the server closes, so QUIT flushes first
	return nil
}

func main() {
	server := flag.String("server", "127.0.0.1:6667", "IRC server address (host:port, plaintext)")
	nick := flag.String("nick", "xkcdbot", "IRC nick to connect as")
	channel := flag.String("channel", "#emile", "channel to post new comics in")
	state := flag.String("state", "/var/lib/xkcdbot/last-seen", "path tracking the last comic number seen")
	dryRun := flag.Bool("dry-run", false, "print the latest comic without posting or touching state")
	flag.Parse()

	c, err := fetchLatest()
	if err != nil {
		log.Fatalf("fetching latest comic: %v", err)
	}

	if *dryRun {
		fmt.Println(formatMessage(c))
		return
	}

	lastSeen, err := readLastSeen(*state)
	if err != nil {
		log.Fatalf("reading state: %v", err)
	}

	// First run (no state file yet): establish the baseline silently, so
	// deploying the bot doesn't immediately announce whatever is already
	// the latest comic as "new".
	if lastSeen == 0 {
		if err := writeLastSeen(*state, c.Num); err != nil {
			log.Fatalf("writing initial state: %v", err)
		}
		log.Printf("initialized at #%d, no announcement", c.Num)
		return
	}

	if c.Num <= lastSeen {
		return
	}

	if err := postToIRC(*server, *nick, *channel, formatMessage(c)); err != nil {
		log.Fatalf("posting to irc: %v", err)
	}
	if err := writeLastSeen(*state, c.Num); err != nil {
		log.Fatalf("writing state: %v", err)
	}
}
