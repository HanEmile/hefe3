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
	"os/exec"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"
)

// A target describes one thing to check. "kind" selects how:
//
//   - "metrics": GET a node-exporter URL. The GET succeeding (200) is itself
//     a liveness signal ("<name>/node"); the body is additionally scanned
//     for health_probe_up{name="X",url="..."} lines (as published by the
//     healthProbes NixOS module), each becoming its own "<name>/X" result.
//   - "ping": ICMP ping a host. Result key is "<name>/reachable".
//   - "tcp": raw TCP connect to host:port. Result key is "<name>/<label>".
type target struct {
	Kind  string `json:"kind"`
	Name  string `json:"name"`
	URL   string `json:"url,omitempty"`
	Host  string `json:"host,omitempty"`
	Port  int    `json:"port,omitempty"`
	Label string `json:"label,omitempty"`
}

var healthProbeLine = regexp.MustCompile(`^health_probe_up\{name="([^"]*)",url="[^"]*"\}\s+(\d+(?:\.\d+)?)`)

const checkTimeout = 5 * time.Second

func runMetrics(t target, results map[string]bool, mu *sync.Mutex) {
	key := t.Name + "/node"
	client := http.Client{Timeout: checkTimeout}
	resp, err := client.Get(t.URL)
	if err != nil || resp.StatusCode != http.StatusOK {
		mu.Lock()
		results[key] = false
		mu.Unlock()
		return
	}
	defer resp.Body.Close()

	mu.Lock()
	results[key] = true
	mu.Unlock()

	scanner := bufio.NewScanner(resp.Body)
	for scanner.Scan() {
		m := healthProbeLine.FindStringSubmatch(scanner.Text())
		if m == nil {
			continue
		}
		up := m[2] == "1"
		mu.Lock()
		results[t.Name+"/"+m[1]] = up
		mu.Unlock()
	}
}

func runPing(t target, results map[string]bool, mu *sync.Mutex) {
	err := exec.Command("ping", "-c", "1", "-W", "2", t.Host).Run()
	mu.Lock()
	results[t.Name+"/reachable"] = err == nil
	mu.Unlock()
}

func runTCP(t target, results map[string]bool, mu *sync.Mutex) {
	addr := net.JoinHostPort(t.Host, strconv.Itoa(t.Port))
	conn, err := net.DialTimeout("tcp", addr, checkTimeout)
	if err == nil {
		conn.Close()
	}
	mu.Lock()
	results[t.Name+"/"+t.Label] = err == nil
	mu.Unlock()
}

func runChecks(targets []target) map[string]bool {
	results := make(map[string]bool)
	var mu sync.Mutex
	var wg sync.WaitGroup

	for _, t := range targets {
		t := t
		wg.Add(1)
		go func() {
			defer wg.Done()
			switch t.Kind {
			case "metrics":
				runMetrics(t, results, &mu)
			case "ping":
				runPing(t, results, &mu)
			case "tcp":
				runTCP(t, results, &mu)
			default:
				log.Printf("unknown target kind %q for %q, skipping", t.Kind, t.Name)
			}
		}()
	}
	wg.Wait()
	return results
}

func loadState(path string) (map[string]bool, error) {
	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return map[string]bool{}, nil
	}
	if err != nil {
		return nil, err
	}
	var state map[string]bool
	if err := json.Unmarshal(data, &state); err != nil {
		return nil, err
	}
	return state, nil
}

func saveState(path string, state map[string]bool) error {
	data, err := json.MarshalIndent(state, "", "  ")
	if err != nil {
		return err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

// transitions returns, for every check present in `current`, a message iff
// its value differs from a PREVIOUSLY KNOWN value in `previous`. A check
// seen for the first time is recorded silently (no announcement), so adding
// a new target doesn't spam the channel with its baseline state.
func transitions(previous, current map[string]bool) []string {
	var msgs []string
	for key, up := range current {
		was, known := previous[key]
		if !known || was == up {
			continue
		}
		if up {
			msgs = append(msgs, fmt.Sprintf("\x033UP\x03 %s", key))
		} else {
			msgs = append(msgs, fmt.Sprintf("\x034DOWN\x03 %s", key))
		}
	}
	return msgs
}

func readLine(r *bufio.Reader) (string, error) {
	line, err := r.ReadString('\n')
	return strings.TrimRight(line, "\r\n"), err
}

// postToIRC does a minimal raw-protocol handshake: connect, register, join,
// wait for the join to actually complete (RPL_ENDOFNAMES), post each
// message, quit.
func postToIRC(server, nick, channel string, messages []string) error {
	conn, err := net.DialTimeout("tcp", server, 10*time.Second)
	if err != nil {
		return err
	}
	defer conn.Close()
	conn.SetDeadline(time.Now().Add(30 * time.Second))

	r := bufio.NewReader(conn)

	fmt.Fprintf(conn, "NICK %s\r\n", nick)
	fmt.Fprintf(conn, "USER %s 0 * :Uptime Bot\r\n", nick)

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

	for _, msg := range messages {
		fmt.Fprintf(conn, "PRIVMSG %s :%s\r\n", channel, msg)
	}
	fmt.Fprintf(conn, "QUIT :state change posted\r\n")
	io.Copy(io.Discard, r) // drain until the server closes, so QUIT flushes first
	return nil
}

func main() {
	server := flag.String("server", "127.0.0.1:6667", "IRC server address (host:port, plaintext)")
	nick := flag.String("nick", "uptimebot", "IRC nick to connect as")
	channel := flag.String("channel", "#emile", "channel to post status changes in")
	targetsPath := flag.String("targets", "/etc/uptimebot/targets.json", "path to the JSON target list")
	statePath := flag.String("state", "/var/lib/uptimebot/state.json", "path tracking last-known up/down per check")
	flag.Parse()

	rawTargets, err := os.ReadFile(*targetsPath)
	if err != nil {
		log.Fatalf("reading targets: %v", err)
	}
	var targets []target
	if err := json.Unmarshal(rawTargets, &targets); err != nil {
		log.Fatalf("parsing targets: %v", err)
	}

	previous, err := loadState(*statePath)
	if err != nil {
		log.Fatalf("loading state: %v", err)
	}

	current := runChecks(targets)

	msgs := transitions(previous, current)
	if len(msgs) > 0 {
		if err := postToIRC(*server, *nick, *channel, msgs); err != nil {
			log.Fatalf("posting to irc: %v", err)
		}
	}

	if err := saveState(*statePath, current); err != nil {
		log.Fatalf("saving state: %v", err)
	}
}
