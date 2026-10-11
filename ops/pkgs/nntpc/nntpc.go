package main

import (
	"bufio"
	"flag"
	"fmt"
	"log"
	"net"
	"os"
	"os/exec"
	"os/signal"
	"strconv"
	"strings"
	"syscall"
	"unsafe"
)

// ---------- Terminal Management ----------

type termState struct {
	termios syscall.Termios
	isRaw   bool
}

func enableRawMode() (*termState, error) {
	fd := int(os.Stdin.Fd())
	state := &termState{}

	if err := getTermios(fd, &state.termios); err == nil {
		raw := state.termios
		raw.Iflag &^= (syscall.IGNBRK | syscall.BRKINT | syscall.PARMRK | syscall.ISTRIP | syscall.INLCR | syscall.IGNCR | syscall.ICRNL | syscall.IXON)
		raw.Oflag &^= syscall.OPOST
		raw.Lflag &^= (syscall.ECHO | syscall.ECHONL | syscall.ICANON | syscall.ISIG | syscall.IEXTEN)
		raw.Cflag &^= (syscall.CSIZE | syscall.PARENB)
		raw.Cflag |= syscall.CS8
		raw.Cc[syscall.VMIN] = 1
		raw.Cc[syscall.VTIME] = 0

		if err := setTermios(fd, &raw); err == nil {
			state.isRaw = true
			return state, nil
		}
	}

	cmd := exec.Command("stty", "-g")
	cmd.Stdin = os.Stdin
	out, err := cmd.Output()
	if err == nil {
		exec.Command("stty", "raw", "-echo").Run()
		state.isRaw = true
		_ = out
	}
	return state, nil
}

func restoreTerminal(state *termState) {
	if state == nil || !state.isRaw {
		return
	}
	fd := int(os.Stdin.Fd())
	_ = setTermios(fd, &state.termios)
	exec.Command("stty", "sane").Run()
	fmt.Print("\033[?25h\033[0m\033[2J\033[H")
	state.isRaw = false // Critical: reset state flag so multiple toggles don't conflict
}

func getTermios(fd int, t *syscall.Termios) error {
	var req uintptr = syscall.TIOCGETA
	if req == 0 {
		req = 0x5401
	}
	_, _, err := syscall.Syscall(syscall.SYS_IOCTL, uintptr(fd), req, uintptr(unsafe.Pointer(t)))
	if err != 0 {
		return err
	}
	return nil
}

func setTermios(fd int, t *syscall.Termios) error {
	var req uintptr = syscall.TIOCSETA
	if req == 0 {
		req = 0x5402
	}
	_, _, err := syscall.Syscall(syscall.SYS_IOCTL, uintptr(fd), req, uintptr(unsafe.Pointer(t)))
	if err != 0 {
		return err
	}
	return nil
}

type winsize struct {
	Row, Col, Xpixel, Ypixel uint16
}

func getTermSize() (int, int) {
	ws := &winsize{}
	var req uintptr = syscall.TIOCGWINSZ
	_, _, err := syscall.Syscall(syscall.SYS_IOCTL, uintptr(os.Stdout.Fd()), req, uintptr(unsafe.Pointer(ws)))
	if err != 0 || ws.Col == 0 || ws.Row == 0 {
		return 80, 24
	}
	return int(ws.Col), int(ws.Row)
}

func expandPath(path string) string {
	if strings.HasPrefix(path, "~/") {
		home, err := os.UserHomeDir()
		if err == nil {
			return strings.Replace(path, "~", home, 1)
		}
	}
	return path
}

// ---------- Data Types ----------

type GroupInfo struct {
	Name string
	High int
	Low  int
}

type Overview struct {
	Number     int
	Subject    string
	From       string
	Date       string
	MsgID      string
	References string
	Lines      int
}

type ThreadNode struct {
	Article  Overview
	Depth    int
	Children []*ThreadNode
}

type ViewState int

const (
	ViewGroups ViewState = iota
	ViewHeaders
	ViewArticle
)

// ---------- App Engine ----------

type App struct {
	conn        net.Conn
	rw          *bufio.ReadWriter
	groups      []GroupInfo
	overviews   []Overview
	flatThreads []*ThreadNode
	article     []string
	curGroup    string
	view        ViewState
	termState   *termState
	logger      *log.Logger

	minisignKey string
	minisignPub string
	autoSign    bool

	groupSel     int
	groupScroll  int
	headerSel    int
	headerScroll int
	artScroll    int

	statusMsg string
}

func main() {
	addr := flag.String("addr", "localhost:1119", "nntpd server address")
	minisignKey := flag.String("minisign-key", "", "path to minisign secret key (.key)")
	minisignPub := flag.String("minisign-pub", "", "path to minisign public key (.pub) for verification")
	autoSign := flag.Bool("auto-sign", true, "automatically sign posts with minisign if key is provided")
	logFile := flag.String("log", "", "path to a log file for debugging (optional)")
	flag.Parse()

	var logger *log.Logger
	if *logFile != "" {
		f, err := os.OpenFile(*logFile, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
		if err == nil {
			logger = log.New(f, "[NNTPC] ", log.LstdFlags|log.Ltime)
		} else {
			fmt.Fprintf(os.Stderr, "Warning: Failed to open log file %s: %v\n", *logFile, err)
		}
	}

	conn, err := net.Dial("tcp", *addr)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error connecting to %s: %v\n", *addr, err)
		os.Exit(1)
	}
	defer conn.Close()

	app := &App{
		conn:        conn,
		rw:          bufio.NewReadWriter(bufio.NewReader(conn), bufio.NewWriter(conn)),
		view:        ViewGroups,
		logger:      logger,
		minisignKey: *minisignKey,
		minisignPub: *minisignPub,
		autoSign:    *autoSign,
	}

	app.logf("Application started. Connecting to %s", *addr)

	greeting, err := app.rw.ReadString('\n')
	if err != nil || !strings.HasPrefix(greeting, "200") {
		fmt.Fprintf(os.Stderr, "Server connection error: %v\n", greeting)
		os.Exit(1)
	}
	app.logf("Server greeting: %s", strings.TrimSpace(greeting))

	app.termState, _ = enableRawMode()
	defer restoreTerminal(app.termState)

	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, os.Interrupt, syscall.SIGTERM)
	go func() {
		<-sigChan
		restoreTerminal(app.termState)
		if app.logger != nil {
			app.logf("Process interrupted by signal.")
		}
		os.Exit(0)
	}()

	app.fetchGroups()
	app.run()
}

func (a *App) logf(format string, args ...interface{}) {
	if a.logger != nil {
		a.logger.Printf(format, args...)
	}
}

func (a *App) sendCmd(format string, args ...interface{}) (string, error) {
	cmdStr := fmt.Sprintf(format, args...)
	a.logf(">>> %s", cmdStr)
	fmt.Fprintf(a.rw, "%s\r\n", cmdStr)
	a.rw.Flush()
	resp, err := a.rw.ReadString('\n')
	if err != nil {
		a.logf("<<< ERROR: %v", err)
		return "", err
	}
	resp = strings.TrimRight(resp, "\r\n")
	a.logf("<<< %s", resp)
	return resp, nil
}

func (a *App) readDotLines() ([]string, error) {
	var lines []string
	for {
		line, err := a.rw.ReadString('\n')
		if err != nil {
			return nil, err
		}
		line = strings.TrimRight(line, "\r\n")
		if line == "." {
			break
		}
		if strings.HasPrefix(line, "..") {
			line = line[1:]
		}
		lines = append(lines, line)
	}
	return lines, nil
}

func (a *App) fetchGroups() {
	resp, err := a.sendCmd("LIST")
	if err != nil || !strings.HasPrefix(resp, "215") {
		a.statusMsg = "Failed to fetch groups"
		return
	}
	lines, err := a.readDotLines()
	if err != nil {
		return
	}
	a.groups = nil
	for _, l := range lines {
		f := strings.Fields(l)
		if len(f) >= 3 {
			hi, _ := strconv.Atoi(f[1])
			lo, _ := strconv.Atoi(f[2])
			a.groups = append(a.groups, GroupInfo{Name: f[0], High: hi, Low: lo})
		}
	}
	a.statusMsg = fmt.Sprintf("Loaded %d group(s)", len(a.groups))
}

func (a *App) buildTree() {
	nodeMap := make(map[string]*ThreadNode)
	var roots []*ThreadNode

	for _, o := range a.overviews {
		nodeMap[o.MsgID] = &ThreadNode{Article: o}
	}

	for _, o := range a.overviews {
		node := nodeMap[o.MsgID]
		refList := strings.Fields(o.References)
		var parent *ThreadNode

		if len(refList) > 0 {
			parentID := refList[len(refList)-1]
			parent = nodeMap[parentID]
		}

		if parent != nil {
			parent.Children = append(parent.Children, node)
		} else {
			roots = append(roots, node)
		}
	}

	a.flatThreads = nil
	var flatten func(nodes []*ThreadNode, depth int)
	flatten = func(nodes []*ThreadNode, depth int) {
		for _, n := range nodes {
			n.Depth = depth
			a.flatThreads = append(a.flatThreads, n)
			flatten(n.Children, depth+1)
		}
	}
	flatten(roots, 0)
}

func (a *App) selectGroup(name string) {
	resp, err := a.sendCmd("GROUP %s", name)
	if err != nil || !strings.HasPrefix(resp, "211") {
		a.statusMsg = "Failed to select group"
		return
	}
	a.curGroup = name

	resp, err = a.sendCmd("XOVER")
	if err != nil || !strings.HasPrefix(resp, "224") {
		a.statusMsg = "Failed to fetch articles"
		return
	}
	lines, _ := a.readDotLines()
	a.overviews = nil
	for _, l := range lines {
		f := strings.Split(l, "\t")
		if len(f) >= 6 {
			num, _ := strconv.Atoi(f[0])
			refs := ""
			if len(f) >= 6 {
				refs = f[5]
			}
			linesCount := 0
			if len(f) >= 8 {
				linesCount, _ = strconv.Atoi(f[7])
			}
			a.overviews = append(a.overviews, Overview{
				Number:     num,
				Subject:    f[1],
				From:       f[2],
				Date:       f[3],
				MsgID:      f[4],
				References: refs,
				Lines:      linesCount,
			})
		}
	}
	a.buildTree()
	a.headerSel = 0
	a.headerScroll = 0
	a.view = ViewHeaders
	a.statusMsg = fmt.Sprintf("Group %s (%d articles)", name, len(a.flatThreads))
}

func (a *App) fetchArticle(num int) {
	resp, err := a.sendCmd("ARTICLE %d", num)
	if err != nil || !strings.HasPrefix(resp, "220") {
		a.statusMsg = "Failed to load article"
		return
	}
	lines, _ := a.readDotLines()
	a.article = lines
	a.artScroll = 0
	a.view = ViewArticle
}

// ---------- Interactive Minisign Signing & Verification ----------

func (a *App) signContent(body string) string {
	a.logf("--- Starting Minisign Phase ---")
	if a.minisignKey == "" {
		a.logf("Signing skipped: No key specified")
		return body
	}

	keyPath := expandPath(a.minisignKey)
	if _, err := os.Stat(keyPath); os.IsNotExist(err) {
		a.logf("Key file missing: %s", keyPath)
		a.statusMsg = fmt.Sprintf("Signing aborted: Key file missing (%s)", keyPath)
		return body
	}

	minisignBin, err := exec.LookPath("minisign")
	if err != nil {
		a.logf("Error: minisign not found in PATH")
		a.statusMsg = "Signing aborted: 'minisign' not found in PATH"
		return body
	}

	tmpMsg, err := os.CreateTemp("", "msg_*.txt")
	if err != nil {
		a.logf("Failed to create msg temp file: %v", err)
		a.statusMsg = "Sign error: temp message file failed"
		return body
	}
	tmpMsgPath := tmpMsg.Name()
	defer os.Remove(tmpMsgPath)

	bodyNormalized := strings.TrimSpace(strings.ReplaceAll(body, "\r\n", "\n"))
	tmpMsg.WriteString(bodyNormalized)
	tmpMsg.Close()
	a.logf("Created message file %s (len: %d bytes)", tmpMsgPath, len(bodyNormalized))

	tmpSig, err := os.CreateTemp("", "sig_*.txt")
	if err != nil {
		a.logf("Failed to create sig temp file: %v", err)
		a.statusMsg = "Sign error: temp signature file failed"
		return body
	}
	tmpSigPath := tmpSig.Name()
	tmpSig.Close()
	os.Remove(tmpSigPath) // Delete so minisign can create it cleanly
	defer os.Remove(tmpSigPath)
	a.logf("Reserved signature path: %s", tmpSigPath)

	// Temporarily restore standard terminal mode so user can type the passphrase
	restoreTerminal(a.termState)
	fmt.Print("\033[2J\033[H")
	fmt.Printf("\033[33;1m=== Minisign Signature Phase ===\033[0m\r\n\r\n")
	fmt.Printf("Using Key: %s\r\n\r\n", keyPath)

	cmd := exec.Command(minisignBin, "-S", "-s", keyPath, "-m", tmpMsgPath, "-x", tmpSigPath)
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	a.logf("Executing command: %v", cmd.Args)
	err = cmd.Run()

	// Re-enable raw input mode for TUI navigation immediately after completion
	a.termState, _ = enableRawMode()

	if err != nil {
		a.logf("Minisign cmd.Run() returned error: %v", err)
		a.statusMsg = fmt.Sprintf("Signing failed (Exit Code or Error): %v", err)
		return body
	}
	a.logf("Minisign cmd.Run() completed successfully.")

	sigBytes, err := os.ReadFile(tmpSigPath)
	if err != nil {
		a.logf("Failed to read signature file at %s: %v", tmpSigPath, err)
		a.statusMsg = fmt.Sprintf("Minisign failed: could not read output file (%v)", err)
		return body
	}
	if len(sigBytes) == 0 {
		a.logf("Signature file %s exists but is empty!", tmpSigPath)
		a.statusMsg = "Minisign failed: signature file was generated empty"
		return body
	}

	a.logf("Successfully extracted signature (%d bytes). Appending to body.", len(sigBytes))
	a.statusMsg = "Post successfully signed!"
	return bodyNormalized + "\n\n--- MINISIGN SIGNATURE ---\n" + string(sigBytes)
}

func (a *App) verifyMinisign() {
	if a.minisignPub == "" {
		a.statusMsg = "No public key set! Pass -minisign-pub"
		return
	}

	pubPath := expandPath(a.minisignPub)
	if _, err := os.Stat(pubPath); os.IsNotExist(err) {
		a.statusMsg = fmt.Sprintf("Public key missing: %s", pubPath)
		return
	}

	minisignBin, err := exec.LookPath("minisign")
	if err != nil {
		a.statusMsg = "CLI Error: 'minisign' not in PATH"
		return
	}

	fullArticle := strings.Join(a.article, "\n")
	sigMarker := "--- MINISIGN SIGNATURE ---"
	sigIdx := strings.Index(fullArticle, sigMarker)
	if sigIdx == -1 {
		a.statusMsg = "No Minisign signature block found in post"
		return
	}

	msgPart := strings.TrimSpace(fullArticle[:sigIdx])
	sigPart := strings.TrimSpace(fullArticle[sigIdx+len(sigMarker):])

	msgFile, err := os.CreateTemp("", "msg_*.txt")
	if err != nil {
		a.statusMsg = "Verification failed (temp file error)"
		return
	}
	defer os.Remove(msgFile.Name())
	msgFile.WriteString(msgPart)
	msgFile.Close()

	sigFile, err := os.CreateTemp("", "sig_*.txt")
	if err != nil {
		a.statusMsg = "Verification failed (temp sig error)"
		return
	}
	defer os.Remove(sigFile.Name())
	sigFile.WriteString(sigPart)
	sigFile.Close()

	cmd := exec.Command(minisignBin, "-V", "-p", pubPath, "-m", msgFile.Name(), "-x", sigFile.Name())
	out, err := cmd.CombinedOutput()

	if err == nil {
		a.statusMsg = "VALID SIGNATURE! Verified with Minisign."
	} else {
		a.statusMsg = "INVALID SIGNATURE: " + strings.TrimSpace(string(out))
	}
}

// ---------- Preview & Posting Engine ----------

func (a *App) confirmAndPost(headerBlock, bodyBlock string) bool {
	cols, rows := getTermSize()
	scroll := 0

	fullText := headerBlock + "\r\n\r\n" + bodyBlock
	lines := strings.Split(fullText, "\n")

	for {
		fmt.Print("\033[2J\033[H\033[?25l")

		title := " PREVIEW ARTICLE BEFORE POSTING "
		if len(title) < cols {
			title += strings.Repeat(" ", cols-len(title))
		}
		fmt.Printf("\033[43;30;1m%s\033[0m\r\n", title)

		// Leave room for header, footer separator, status text, and nav hints
		reservedRows := 5
		if a.statusMsg != "" {
			reservedRows = 6
		}

		contentRows := rows - reservedRows
		if contentRows < 1 {
			contentRows = 1
		}

		for i := 0; i < contentRows; i++ {
			idx := scroll + i
			if idx < len(lines) {
				line := strings.TrimRight(lines[idx], "\r")
				if len(line) > cols {
					line = line[:cols]
				}
				fmt.Printf("%s\r\n", line)
			} else {
				fmt.Print("\r\n")
			}
		}

		fmt.Printf("\033[90m%s\033[0m\r\n", strings.Repeat("-", cols))

		// If there is an active status message (like a signing error), display it in preview
		if a.statusMsg != "" {
			statusStr := fmt.Sprintf("Status: %s", a.statusMsg)
			if len(statusStr) > cols {
				statusStr = statusStr[:cols]
			}
			fmt.Printf("\033[32;1m%s\033[0m\r\n", statusStr)
		}

		nav := "Post message? [y] Yes / Post | [e] Re-edit | [j/k] Scroll | [c] Cancel"
		if len(nav) > cols {
			nav = nav[:cols]
		}
		fmt.Printf("\033[33;1m%s\033[0m", nav)

		buf := make([]byte, 8)
		n, err := os.Stdin.Read(buf)
		if err != nil || n == 0 {
			return false
		}

		key := buf[0]
		switch {
		case key == 'y' || key == 'Y':
			return true
		case key == 'c' || key == 'C' || key == 'q':
			a.statusMsg = "Post cancelled by user"
			return false
		case key == 'e' || key == 'E':
			return false
		case key == 'j' || (n == 3 && buf[2] == 'B'):
			if scroll < len(lines)-contentRows {
				scroll++
			}
		case key == 'k' || (n == 3 && buf[2] == 'A'):
			if scroll > 0 {
				scroll--
			}
		}
	}
}

func (a *App) editAndPostArticle(initialContent string) {
	currentContent := initialContent
	a.statusMsg = "" // Clear any lingering statuses before entering the edit loop

	for {
		restoreTerminal(a.termState)

		tmpFile, err := os.CreateTemp("", "nntpc_post_*.txt")
		if err != nil {
			a.statusMsg = "Failed to create temp file"
			a.termState, _ = enableRawMode()
			return
		}
		tmpPath := tmpFile.Name()
		defer os.Remove(tmpPath)

		if currentContent == "" {
			currentContent = fmt.Sprintf("Newsgroups: %s\nFrom: %s <user@nntpc>\nSubject: \n\n",
				a.curGroup, os.Getenv("USER"))
		}
		tmpFile.WriteString(currentContent)
		tmpFile.Close()

		editor := os.Getenv("EDITOR")
		if editor == "" {
			editor = "vim"
		}

		cmd := exec.Command(editor, tmpPath)
		cmd.Stdin = os.Stdin
		cmd.Stdout = os.Stdout
		cmd.Stderr = os.Stderr

		if err := cmd.Run(); err != nil {
			a.statusMsg = "Editing cancelled"
			a.termState, _ = enableRawMode()
			return
		}

		contentBytes, err := os.ReadFile(tmpPath)
		if err != nil || len(contentBytes) == 0 {
			a.statusMsg = "Post empty or unreadable"
			a.termState, _ = enableRawMode()
			return
		}

		a.termState, _ = enableRawMode()

		fullText := strings.ReplaceAll(string(contentBytes), "\r\n", "\n")
		parts := strings.SplitN(fullText, "\n\n", 2)
		headerBlock := parts[0]
		bodyBlock := ""
		if len(parts) > 1 {
			bodyBlock = parts[1]
		}

		// Interactive Minisign signing phase
		if a.minisignKey != "" && a.autoSign {
			bodyBlock = a.signContent(bodyBlock)
		}

		// Displays interactive preview window. If signing failed, the error will now be visible here.
		if !a.confirmAndPost(headerBlock, bodyBlock) {
			if strings.HasPrefix(a.statusMsg, "Post cancelled") {
				return
			}
			// User chose re-edit
			currentContent = string(contentBytes)
			continue
		}

		resp, err := a.sendCmd("POST")
		if err != nil || !strings.HasPrefix(resp, "340") {
			a.statusMsg = "Posting not allowed by server"
			return
		}

		// Send Headers
		for _, line := range strings.Split(headerBlock, "\n") {
			if strings.TrimSpace(line) != "" {
				a.rw.WriteString(line + "\r\n")
			}
		}
		a.rw.WriteString("\r\n")

		// Send Body with dot-stuffing
		for _, line := range strings.Split(bodyBlock, "\n") {
			if strings.HasPrefix(line, ".") {
				a.rw.WriteString(".")
			}
			a.rw.WriteString(line + "\r\n")
		}
		a.rw.WriteString(".\r\n")
		a.rw.Flush()

		postResp, _ := a.rw.ReadString('\n')
		if strings.HasPrefix(postResp, "240") {
			a.statusMsg = "Article posted successfully!"
			a.logf("Post confirmed: %s", strings.TrimSpace(postResp))
			a.selectGroup(a.curGroup)
		} else {
			a.statusMsg = "Post failed: " + postResp
			a.logf("Post rejected: %s", strings.TrimSpace(postResp))
		}
		return
	}
}

func (a *App) createNewsgroup() {
	restoreTerminal(a.termState)
	fmt.Print("\033[2J\033[H")
	reader := bufio.NewReader(os.Stdin)

	fmt.Print("Enter new newsgroup name (e.g. dev.golang): ")
	groupName, _ := reader.ReadString('\n')
	groupName = strings.TrimSpace(groupName)

	a.termState, _ = enableRawMode()

	if groupName == "" {
		a.statusMsg = "Cancelled group creation"
		return
	}

	a.curGroup = groupName
	a.editAndPostArticle(fmt.Sprintf("Newsgroups: %s\nFrom: %s <user@nntpc>\nSubject: Group Created\n\nWelcome to %s!", groupName, os.Getenv("USER"), groupName))
	a.fetchGroups()
}

func (a *App) replyArticle() {
	if len(a.flatThreads) == 0 || a.headerSel >= len(a.flatThreads) {
		return
	}
	target := a.flatThreads[a.headerSel].Article

	subj := target.Subject
	if !strings.HasPrefix(strings.ToLower(subj), "re:") {
		subj = "Re: " + subj
	}

	refs := target.References
	if refs != "" {
		refs += " " + target.MsgID
	} else {
		refs = target.MsgID
	}

	quote := ""
	for _, l := range a.article {
		quote += "> " + l + "\n"
	}

	template := fmt.Sprintf("Newsgroups: %s\nFrom: %s <user@nntpc>\nSubject: %s\nReferences: %s\n\n%s",
		a.curGroup, os.Getenv("USER"), subj, refs, quote)

	a.editAndPostArticle(template)
}

func (a *App) render() {
	cols, rows := getTermSize()

	fmt.Print("\033[2J\033[H\033[?25l")

	header := fmt.Sprintf(" NNTPC - Go Terminal Newsreader [%s]", a.curGroup)
	if len(header) < cols {
		header += strings.Repeat(" ", cols-len(header))
	} else {
		header = header[:cols]
	}
	fmt.Printf("\033[44;37;1m%s\033[0m\r\n", header)

	contentRows := rows - 4
	if contentRows < 1 {
		contentRows = 1
	}

	switch a.view {
	case ViewGroups:
		fmt.Print("\033[33;1mNEWSGROUPS:\033[0m\r\n")
		for i := 0; i < contentRows-1; i++ {
			idx := a.groupScroll + i
			if idx < len(a.groups) {
				g := a.groups[idx]
				cursor := "  "
				if idx == a.groupSel {
					cursor = "> "
					fmt.Print("\033[7m")
				}
				line := fmt.Sprintf("%s%-30s High: %-5d Low: %-5d", cursor, g.Name, g.High, g.Low)
				if len(line) > cols {
					line = line[:cols]
				}
				fmt.Printf("%s\033[0m\r\n", line)
			} else {
				fmt.Print("\r\n")
			}
		}

	case ViewHeaders:
		fmt.Printf("\033[33;1mTHREAD TREE IN %s:\033[0m\r\n", a.curGroup)
		for i := 0; i < contentRows-1; i++ {
			idx := a.headerScroll + i
			if idx < len(a.flatThreads) {
				node := a.flatThreads[idx]
				o := node.Article
				cursor := "  "
				if idx == a.headerSel {
					cursor = "> "
					fmt.Print("\033[7m")
				}

				indent := strings.Repeat("  ", node.Depth)
				treePrefix := ""
				if node.Depth > 0 {
					treePrefix = "└─ "
				}

				line := fmt.Sprintf("%s%s%s#%-4d %-25.25s [%s]", cursor, indent, treePrefix, o.Number, o.Subject, o.From)
				if len(line) > cols {
					line = line[:cols]
				}
				fmt.Printf("%s\033[0m\r\n", line)
			} else {
				fmt.Print("\r\n")
			}
		}

	case ViewArticle:
		artNum := 0
		if len(a.flatThreads) > a.headerSel {
			artNum = a.flatThreads[a.headerSel].Article.Number
		}
		fmt.Printf("\033[33;1mREADING ARTICLE (#%d):\033[0m\r\n", artNum)
		for i := 0; i < contentRows-1; i++ {
			idx := a.artScroll + i
			if idx < len(a.article) {
				line := a.article[idx]
				if len(line) > cols {
					line = line[:cols]
				}
				fmt.Printf("%s\r\n", line)
			} else {
				fmt.Print("\r\n")
			}
		}
	}

	fmt.Printf("\033[90m%s\033[0m\r\n", strings.Repeat("-", cols))

	status := fmt.Sprintf("Status: %s", a.statusMsg)
	if len(status) > cols {
		status = status[:cols]
	}
	fmt.Printf("\033[32m%s\033[0m\r\n", status)

	var nav string
	switch a.view {
	case ViewGroups:
		nav = "Keys: [j/k] Down/Up | [Enter] Select | [c] Create Group | [q] Quit"
	case ViewHeaders:
		nav = "Keys: [j/k] Down/Up | [Enter] Read | [p] Post | [r] Reply | [b] Back | [q] Quit"
	case ViewArticle:
		nav = "Keys: [j/k] Scroll | [r] Reply | [v] Verify Sig | [b] Back to List | [q] Quit"
	}
	if len(nav) > cols {
		nav = nav[:cols]
	}
	fmt.Printf("\033[36m%s\033[0m", nav)
}

func (a *App) run() {
	buf := make([]byte, 8)
	for {
		a.render()

		n, err := os.Stdin.Read(buf)
		if err != nil || n == 0 {
			break
		}

		key := buf[0]

		if key == 'q' {
			break
		}

		_, rows := getTermSize()
		contentRows := rows - 4
		if contentRows < 1 {
			contentRows = 1
		}

		switch a.view {
		case ViewGroups:
			switch {
			case key == 'j' || (n == 3 && buf[2] == 'B'):
				if a.groupSel < len(a.groups)-1 {
					a.groupSel++
					if a.groupSel >= a.groupScroll+contentRows-1 {
						a.groupScroll++
					}
				}
			case key == 'k' || (n == 3 && buf[2] == 'A'):
				if a.groupSel > 0 {
					a.groupSel--
					if a.groupSel < a.groupScroll {
						a.groupScroll = a.groupSel
					}
				}
			case key == '\r' || key == '\n':
				if len(a.groups) > 0 {
					a.selectGroup(a.groups[a.groupSel].Name)
				}
			case key == 'c':
				a.createNewsgroup()
			}

		case ViewHeaders:
			switch {
			case key == 'j' || (n == 3 && buf[2] == 'B'):
				if a.headerSel < len(a.flatThreads)-1 {
					a.headerSel++
					if a.headerSel >= a.headerScroll+contentRows-1 {
						a.headerScroll++
					}
				}
			case key == 'k' || (n == 3 && buf[2] == 'A'):
				if a.headerSel > 0 {
					a.headerSel--
					if a.headerSel < a.headerScroll {
						a.headerScroll = a.headerSel
					}
				}
			case key == '\r' || key == '\n':
				if len(a.flatThreads) > 0 {
					a.fetchArticle(a.flatThreads[a.headerSel].Article.Number)
				}
			case key == 'b':
				a.view = ViewGroups
			case key == 'p':
				a.editAndPostArticle("")
			case key == 'r':
				a.replyArticle()
			}

		case ViewArticle:
			switch {
			case key == 'j' || (n == 3 && buf[2] == 'B'):
				if a.artScroll < len(a.article)-1 {
					a.artScroll++
				}
			case key == 'k' || (n == 3 && buf[2] == 'A'):
				if a.artScroll > 0 {
					a.artScroll--
				}
			case key == 'b':
				a.view = ViewHeaders
			case key == 'r':
				a.replyArticle()
			case key == 'v':
				a.verifyMinisign()
			}
		}
	}
}
