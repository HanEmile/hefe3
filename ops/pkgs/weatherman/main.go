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
	"strings"
	"time"
)

// Düsseldorf, forecast_days=1 so the hourly arrays cover exactly today
// (local midnight to midnight in the given timezone).
const weatherURL = "https://api.open-meteo.com/v1/forecast?latitude=51.2232&longitude=6.7793&daily=sunrise,sunset&hourly=temperature_2m,precipitation,wind_speed_10m,wind_gusts_10m&timezone=Europe%2FBerlin&forecast_days=1&wind_speed_unit=kn"

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
	resp, err := http.Get(weatherURL)
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
	fmt.Fprintf(conn, "USER %s 0 * :Weatherman Bot\r\n", nick)

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
	fmt.Fprintf(conn, "QUIT :forecast posted\r\n")
	io.Copy(io.Discard, r) // drain until the server closes, so QUIT flushes first
	return nil
}

func main() {
	server := flag.String("server", "127.0.0.1:6667", "IRC server address (host:port, plaintext)")
	nick := flag.String("nick", "weatherman", "IRC nick to connect as")
	channel := flag.String("channel", "#emile", "channel to post the forecast in")
	dryRun := flag.Bool("dry-run", false, "print the forecast message instead of posting it")
	flag.Parse()

	f, err := fetchForecast()
	if err != nil {
		log.Fatalf("fetching forecast: %v", err)
	}
	message, err := formatMessage(f)
	if err != nil {
		log.Fatalf("formatting forecast: %v", err)
	}
	if *dryRun {
		fmt.Println(message)
		return
	}
	if err := postToIRC(*server, *nick, *channel, message); err != nil {
		log.Fatalf("posting to irc: %v", err)
	}
}
