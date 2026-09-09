// nntpd.go — a minimal, single-file NNTP server.
//
// Implements just enough of RFC 977 / RFC 3977 for real newsreaders
// (slrn included) to connect, browse, read and post: CAPABILITIES,
// MODE READER, LIST [ACTIVE|NEWSGROUPS], GROUP, LISTGROUP, STAT, HEAD,
// BODY, ARTICLE, NEXT, LAST, XOVER, POST, DATE, HELP, QUIT.
//
// Articles are stored one-file-per-article under -spool/<group>/<num>,
// which doubles as the on-disk persistence: on startup the spool is
// walked to rebuild the in-memory index, so restarting the server
// doesn't lose anything already posted.
//
// This is intentionally small and un-hardened: no AUTHINFO, no TLS, no
// peering/feeds, no per-user ACLs. Fine for a handful of trusted
// friends on a private network / over Tailscale etc. Put it behind
// your own access control (firewall, VPN, reverse proxy) rather than
// exposing it directly to the internet.
//
// Build:   go build -o nntpd nntpd.go
// Run:     ./nntpd -addr :1119 -spool ./spool -hostname news.example
package main

import (
	"bufio"
	"flag"
	"fmt"
	"log"
	"net"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

// ---------- storage ----------

type article struct {
	number  int
	msgID   string
	headers []string // raw header lines, in original order
	body    []string // raw body lines
}

func (a *article) header(name string) string {
	name = strings.ToLower(name) + ":"
	for _, h := range a.headers {
		if strings.HasPrefix(strings.ToLower(h), name) {
			return strings.TrimSpace(h[len(name):])
		}
	}
	return ""
}

type group struct {
	name     string
	articles map[int]*article
	low      int
	high     int
}

func (g *group) count() int { return len(g.articles) }

// sortedNumbers returns article numbers present in the group, ascending.
func (g *group) sortedNumbers() []int {
	nums := make([]int, 0, len(g.articles))
	for n := range g.articles {
		nums = append(nums, n)
	}
	sort.Ints(nums)
	return nums
}

type store struct {
	mu      sync.RWMutex
	spool   string
	groups  map[string]*group
	byMsgID map[string]*article // message-id -> article (any group)
}

func newStore(spoolDir string) (*store, error) {
	s := &store{
		spool:   spoolDir,
		groups:  make(map[string]*group),
		byMsgID: make(map[string]*article),
	}
	if err := os.MkdirAll(spoolDir, 0o755); err != nil {
		return nil, err
	}
	if err := s.loadSpool(); err != nil {
		return nil, err
	}
	if len(s.groups) == 0 {
		s.seedWelcomeGroup()
	}
	return s, nil
}

func (s *store) loadSpool() error {
	entries, err := os.ReadDir(s.spool)
	if err != nil {
		return err
	}
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		gname := e.Name()
		g := &group{name: gname, articles: make(map[int]*article)}
		groupDir := filepath.Join(s.spool, gname)
		files, err := os.ReadDir(groupDir)
		if err != nil {
			return err
		}
		for _, f := range files {
			num, err := strconv.Atoi(f.Name())
			if err != nil {
				continue // not an article file
			}
			data, err := os.ReadFile(filepath.Join(groupDir, f.Name()))
			if err != nil {
				continue
			}
			a := parseArticle(num, string(data))
			g.articles[num] = a
			if a.msgID != "" {
				s.byMsgID[a.msgID] = a
			}
			if g.low == 0 || num < g.low {
				g.low = num
			}
			if num > g.high {
				g.high = num
			}
		}
		s.groups[gname] = g
	}
	return nil
}

func (s *store) seedWelcomeGroup() {
	g := &group{name: "local.test", articles: make(map[int]*article)}
	a := &article{
		number: 1,
		msgID:  "<welcome-1@local.test>",
		headers: []string{
			"Newsgroups: local.test",
			"From: nntpd <nntpd@local.test>",
			"Subject: Welcome",
			"Message-ID: <welcome-1@local.test>",
			"Date: " + time.Now().Format(time.RFC1123Z),
		},
		body: []string{"This is a minimal nntpd. Post something!"},
	}
	g.articles[1] = a
	g.low, g.high = 1, 1
	s.groups[g.name] = g
	s.byMsgID[a.msgID] = a
	s.writeArticle(g.name, a)
}

func parseArticle(num int, raw string) *article {
	lines := strings.Split(strings.ReplaceAll(raw, "\r\n", "\n"), "\n")
	a := &article{number: num}
	i := 0
	for ; i < len(lines); i++ {
		if lines[i] == "" {
			i++
			break
		}
		a.headers = append(a.headers, lines[i])
	}
	for ; i < len(lines); i++ {
		a.body = append(a.body, lines[i])
	}
	// trim a possible single trailing empty line from the split
	if n := len(a.body); n > 0 && a.body[n-1] == "" {
		a.body = a.body[:n-1]
	}
	a.msgID = a.header("Message-ID")
	return a
}

func (s *store) writeArticle(groupName string, a *article) {
	dir := filepath.Join(s.spool, groupName)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		log.Printf("spool: mkdir %s: %v", dir, err)
		return
	}
	var b strings.Builder
	for _, h := range a.headers {
		b.WriteString(h)
		b.WriteString("\r\n")
	}
	b.WriteString("\r\n")
	for _, l := range a.body {
		b.WriteString(l)
		b.WriteString("\r\n")
	}
	path := filepath.Join(dir, strconv.Itoa(a.number))
	if err := os.WriteFile(path, []byte(b.String()), 0o644); err != nil {
		log.Printf("spool: write %s: %v", path, err)
	}
}

// post assigns a number in each newsgroup named by the article's
// Newsgroups header (comma-separated) and stores/persists it.
func (s *store) post(a *article) error {
	ngHeader := a.header("Newsgroups")
	if ngHeader == "" {
		return fmt.Errorf("no Newsgroups header")
	}
	if a.msgID == "" {
		a.msgID = fmt.Sprintf("<%d.%d@nntpd>", time.Now().UnixNano(), os.Getpid())
		a.headers = append(a.headers, "Message-ID: "+a.msgID)
	}
	if a.header("Date") == "" {
		a.headers = append(a.headers, "Date: "+time.Now().Format(time.RFC1123Z))
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	groups := strings.Split(ngHeader, ",")
	for _, gname := range groups {
		gname = strings.TrimSpace(gname)
		if gname == "" {
			continue
		}
		g, ok := s.groups[gname]
		if !ok {
			g = &group{name: gname, articles: make(map[int]*article)}
			s.groups[gname] = g
		}
		g.high++
		if g.low == 0 {
			g.low = g.high
		}
		num := g.high
		// each group gets its own *article struct (same content, own number)
		clone := &article{number: num, msgID: a.msgID, headers: a.headers, body: a.body}
		g.articles[num] = clone
		s.writeArticle(gname, clone)
		s.byMsgID[a.msgID] = clone // last one wins for cross-posted lookup by msgid; fine for this scale
	}
	return nil
}

// ---------- session ----------

type session struct {
	conn      net.Conn
	rw        *bufio.ReadWriter
	s         *store
	hostname  string
	curGroup  *group
	curArtNum int
}

func (sess *session) writeLine(format string, args ...interface{}) {
	fmt.Fprintf(sess.rw, format+"\r\n", args...)
}

func (sess *session) writeDotBlock(lines []string) {
	for _, l := range lines {
		if strings.HasPrefix(l, ".") {
			sess.rw.WriteString(".")
		}
		sess.rw.WriteString(l)
		sess.rw.WriteString("\r\n")
	}
	sess.rw.WriteString(".\r\n")
}

func (sess *session) readDotBlock() ([]string, error) {
	var lines []string
	for {
		line, err := sess.rw.ReadString('\n')
		if err != nil {
			return nil, err
		}
		line = strings.TrimRight(line, "\r\n")
		if line == "." {
			return lines, nil
		}
		if strings.HasPrefix(line, "..") {
			line = line[1:]
		}
		lines = append(lines, line)
	}
}

func handleConn(conn net.Conn, s *store, hostname string) {
	defer conn.Close()
	sess := &session{
		conn:     conn,
		rw:       bufio.NewReadWriter(bufio.NewReader(conn), bufio.NewWriter(conn)),
		s:        s,
		hostname: hostname,
	}
	sess.writeLine("200 %s NNTP Service Ready, posting allowed", hostname)
	sess.rw.Flush()

	for {
		line, err := sess.rw.ReadString('\n')
		if err != nil {
			return
		}
		line = strings.TrimRight(line, "\r\n")
		if line == "" {
			continue
		}
		fields := strings.Fields(line)
		cmd := strings.ToUpper(fields[0])
		args := fields[1:]

		switch cmd {
		case "CAPABILITIES":
			sess.writeLine("101 Capability list follows")
			sess.writeDotBlock([]string{"VERSION 2", "READER", "POST", "OVER", "LIST ACTIVE NEWSGROUPS"})
		case "MODE":
			sess.writeLine("200 Posting allowed")
		case "LIST":
			sess.cmdList(args)
		case "GROUP":
			sess.cmdGroup(args)
		case "LISTGROUP":
			sess.cmdListGroup(args)
		case "STAT", "HEAD", "BODY", "ARTICLE":
			sess.cmdArticle(cmd, args)
		case "NEXT":
			sess.cmdNextLast(1)
		case "LAST":
			sess.cmdNextLast(-1)
		case "XOVER", "OVER":
			sess.cmdOver(args)
		case "POST":
			sess.cmdPost()
		case "DATE":
			sess.writeLine("111 %s", time.Now().UTC().Format("20060102150405"))
		case "HELP":
			sess.writeLine("100 Help text follows")
			sess.writeDotBlock([]string{"CAPABILITIES, MODE READER, LIST, GROUP, LISTGROUP,",
				"STAT, HEAD, BODY, ARTICLE, NEXT, LAST, XOVER, POST, DATE, QUIT"})
		case "QUIT":
			sess.writeLine("205 Bye")
			sess.rw.Flush()
			return
		default:
			sess.writeLine("500 Command not recognized")
		}
		sess.rw.Flush()
	}
}

func (sess *session) cmdList(args []string) {
	sub := "ACTIVE"
	if len(args) > 0 {
		sub = strings.ToUpper(args[0])
	}
	sess.s.mu.RLock()
	defer sess.s.mu.RUnlock()

	names := make([]string, 0, len(sess.s.groups))
	for n := range sess.s.groups {
		names = append(names, n)
	}
	sort.Strings(names)

	switch sub {
	case "NEWSGROUPS":
		sess.writeLine("215 List of newsgroups follows")
		lines := make([]string, 0, len(names))
		for _, n := range names {
			lines = append(lines, n+" -")
		}
		sess.writeDotBlock(lines)
	default: // ACTIVE
		sess.writeLine("215 List of newsgroups follows")
		lines := make([]string, 0, len(names))
		for _, n := range names {
			g := sess.s.groups[n]
			lines = append(lines, fmt.Sprintf("%s %d %d y", n, g.high, g.low))
		}
		sess.writeDotBlock(lines)
	}
}

func (sess *session) cmdGroup(args []string) {
	if len(args) != 1 {
		sess.writeLine("501 GROUP requires a newsgroup name")
		return
	}
	sess.s.mu.RLock()
	g, ok := sess.s.groups[args[0]]
	sess.s.mu.RUnlock()
	if !ok {
		sess.writeLine("411 No such newsgroup")
		return
	}
	sess.curGroup = g
	if g.count() > 0 {
		sess.curArtNum = g.sortedNumbers()[0]
	} else {
		sess.curArtNum = 0
	}
	sess.writeLine("211 %d %d %d %s", g.count(), g.low, g.high, g.name)
}

func (sess *session) cmdListGroup(args []string) {
	g := sess.curGroup
	if len(args) == 1 {
		sess.s.mu.RLock()
		gg, ok := sess.s.groups[args[0]]
		sess.s.mu.RUnlock()
		if !ok {
			sess.writeLine("411 No such newsgroup")
			return
		}
		g = gg
		sess.curGroup = g
	}
	if g == nil {
		sess.writeLine("412 No newsgroup selected")
		return
	}
	nums := g.sortedNumbers()
	if len(nums) > 0 {
		sess.curArtNum = nums[0]
	}
	sess.writeLine("211 %d %d %d %s", g.count(), g.low, g.high, g.name)
	lines := make([]string, 0, len(nums))
	for _, n := range nums {
		lines = append(lines, strconv.Itoa(n))
	}
	sess.writeDotBlock(lines)
}

// resolveArticle finds an article by message-id (<...>), by number, or
// (if arg == "") the current article in the current group.
func (sess *session) resolveArticle(arg string) (*article, error) {
	sess.s.mu.RLock()
	defer sess.s.mu.RUnlock()

	if strings.HasPrefix(arg, "<") {
		a, ok := sess.s.byMsgID[arg]
		if !ok {
			return nil, fmt.Errorf("430")
		}
		return a, nil
	}
	if sess.curGroup == nil {
		return nil, fmt.Errorf("412")
	}
	num := sess.curArtNum
	if arg != "" {
		n, err := strconv.Atoi(arg)
		if err != nil {
			return nil, fmt.Errorf("501")
		}
		num = n
	}
	a, ok := sess.curGroup.articles[num]
	if !ok {
		return nil, fmt.Errorf("423")
	}
	sess.curArtNum = num
	return a, nil
}

func (sess *session) cmdArticle(cmd string, args []string) {
	arg := ""
	if len(args) > 0 {
		arg = args[0]
	}
	a, err := sess.resolveArticle(arg)
	if err != nil {
		switch err.Error() {
		case "430":
			sess.writeLine("430 No such article")
		case "412":
			sess.writeLine("412 No newsgroup selected")
		case "423":
			sess.writeLine("423 No such article number in this group")
		default:
			sess.writeLine("501 Syntax error")
		}
		return
	}
	switch cmd {
	case "STAT":
		sess.writeLine("223 %d %s", a.number, a.msgID)
	case "HEAD":
		sess.writeLine("221 %d %s", a.number, a.msgID)
		sess.writeDotBlock(a.headers)
	case "BODY":
		sess.writeLine("222 %d %s", a.number, a.msgID)
		sess.writeDotBlock(a.body)
	case "ARTICLE":
		sess.writeLine("220 %d %s", a.number, a.msgID)
		all := make([]string, 0, len(a.headers)+len(a.body)+1)
		all = append(all, a.headers...)
		all = append(all, "")
		all = append(all, a.body...)
		sess.writeDotBlock(all)
	}
}

func (sess *session) cmdNextLast(dir int) {
	if sess.curGroup == nil {
		sess.writeLine("412 No newsgroup selected")
		return
	}
	sess.s.mu.RLock()
	nums := sess.curGroup.sortedNumbers()
	sess.s.mu.RUnlock()
	idx := -1
	for i, n := range nums {
		if n == sess.curArtNum {
			idx = i
			break
		}
	}
	idx += dir
	if idx < 0 || idx >= len(nums) {
		if dir > 0 {
			sess.writeLine("421 No next article in this group")
		} else {
			sess.writeLine("422 No previous article in this group")
		}
		return
	}
	sess.curArtNum = nums[idx]
	a := sess.curGroup.articles[sess.curArtNum]
	sess.writeLine("223 %d %s", a.number, a.msgID)
}

func (sess *session) cmdOver(args []string) {
	if sess.curGroup == nil {
		sess.writeLine("412 No newsgroup selected")
		return
	}
	g := sess.curGroup
	sess.s.mu.RLock()
	nums := g.sortedNumbers()
	sess.s.mu.RUnlock()

	lo, hi := g.low, g.high
	if len(args) == 1 {
		rng := args[0]
		if i := strings.Index(rng, "-"); i >= 0 {
			if v, err := strconv.Atoi(rng[:i]); err == nil {
				lo = v
			}
			if rng[i+1:] != "" {
				if v, err := strconv.Atoi(rng[i+1:]); err == nil {
					hi = v
				}
			}
		} else if v, err := strconv.Atoi(rng); err == nil {
			lo, hi = v, v
		}
	}

	sess.writeLine("224 Overview information follows")
	var lines []string
	for _, n := range nums {
		if n < lo || n > hi {
			continue
		}
		a := g.articles[n]
		bytes := 0
		for _, h := range a.headers {
			bytes += len(h) + 2
		}
		for _, b := range a.body {
			bytes += len(b) + 2
		}
		lines = append(lines, fmt.Sprintf("%d\t%s\t%s\t%s\t%s\t%s\t%d\t%d",
			n, a.header("Subject"), a.header("From"), a.header("Date"),
			a.msgID, a.header("References"), bytes, len(a.body)))
	}
	sess.writeDotBlock(lines)
}

func (sess *session) cmdPost() {
	sess.writeLine("340 Send article to be posted")
	sess.rw.Flush()
	lines, err := sess.readDotBlock()
	if err != nil {
		return
	}
	a := &article{}
	i := 0
	for ; i < len(lines); i++ {
		if lines[i] == "" {
			i++
			break
		}
		a.headers = append(a.headers, lines[i])
	}
	a.body = lines[i:]
	a.msgID = a.header("Message-ID")

	if err := sess.s.post(a); err != nil {
		sess.writeLine("441 Posting failed: %v", err)
		return
	}
	sess.writeLine("240 Article posted")
}

// ---------- main ----------

func main() {
	addr := flag.String("addr", ":1119", "listen address")
	spool := flag.String("spool", "./spool", "spool directory")
	hostname := flag.String("hostname", "localhost", "hostname reported in the greeting")
	flag.Parse()

	s, err := newStore(*spool)
	if err != nil {
		log.Fatalf("store: %v", err)
	}

	ln, err := net.Listen("tcp", *addr)
	if err != nil {
		log.Fatalf("listen: %v", err)
	}
	log.Printf("nntpd listening on %s, spool=%s", *addr, *spool)

	for {
		conn, err := ln.Accept()
		if err != nil {
			log.Printf("accept: %v", err)
			continue
		}
		go handleConn(conn, s, *hostname)
	}
}
