package main

import (
	"flag"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/textinput"
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/hanemile/xilebo/pkg/client"
)

// ── Styles ───────────────────────────────────────────────

var (
	accentColor = lipgloss.Color("#5865f2")
	dimColor    = lipgloss.Color("#6d6f78")
	dangerColor = lipgloss.Color("#ed4245")
	successColor = lipgloss.Color("#3ba55d")

	titleStyle = lipgloss.NewStyle().Bold(true).Foreground(accentColor)
	dimStyle   = lipgloss.NewStyle().Foreground(dimColor)
	errStyle   = lipgloss.NewStyle().Foreground(dangerColor)
	okStyle    = lipgloss.NewStyle().Foreground(successColor)

	sidebarStyle = lipgloss.NewStyle().
			Width(28).
			BorderRight(true).
			BorderStyle(lipgloss.NormalBorder()).
			BorderForeground(dimColor).
			Padding(0, 1)

	roomItemStyle = lipgloss.NewStyle().Padding(0, 1)
	activeRoom    = lipgloss.NewStyle().Bold(true).Foreground(accentColor).Padding(0, 1)

	msgSender = lipgloss.NewStyle().Bold(true)
	msgTime   = lipgloss.NewStyle().Foreground(dimColor)

	statusBar = lipgloss.NewStyle().
			Background(lipgloss.Color("#25262b")).
			Foreground(lipgloss.Color("#e4e5e7")).
			Padding(0, 1)
)

// ── Model ────────────────────────────────────────────────

type view int

const (
	viewLogin view = iota
	viewChat
)

type model struct {
	client     *client.Client
	view       view
	width      int
	height     int

	// Login
	loginField int // 0=server, 1=handle, 2=password
	inputs     [3]textinput.Model
	loginErr   string
	registering bool

	// Chat
	rooms      []client.Room
	activeRoom int
	messages   []client.Message
	members    []client.Member
	input      textinput.Model
	viewport   viewport.Model
	chatErr    string
}

func initialModel(serverURL string) model {
	// Login fields
	serverInput := textinput.New()
	serverInput.Placeholder = "http://localhost:8080"
	serverInput.Focus()
	if serverURL != "" {
		serverInput.SetValue(serverURL)
	}

	handleInput := textinput.New()
	handleInput.Placeholder = "@alice@localhost"

	pwInput := textinput.New()
	pwInput.Placeholder = "password"
	pwInput.EchoMode = textinput.EchoPassword

	// Chat input
	chatInput := textinput.New()
	chatInput.Placeholder = "Type a message..."

	return model{
		client:     nil,
		view:       viewLogin,
		inputs:     [3]textinput.Model{serverInput, handleInput, pwInput},
		input:      chatInput,
		activeRoom: -1,
	}
}

// ── Messages ─────────────────────────────────────────────

type loginDoneMsg struct{ err error }
type roomsLoadedMsg struct {
	rooms []client.Room
	err   error
}
type msgsLoadedMsg struct {
	msgs []client.Message
	err  error
}
type membersLoadedMsg struct {
	members []client.Member
	err     error
}
type msgSentMsg struct {
	msg *client.Message
	err error
}
type syncMsg struct {
	data []byte
	err  error
}
type tickMsg struct{}

// ── Init ─────────────────────────────────────────────────

func (m model) Init() tea.Cmd {
	return textinput.Blink
}

// ── Update ───────────────────────────────────────────────

func (m model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		switch msg.String() {
		case "ctrl+c", "ctrl+q":
			return m, tea.Quit
		}
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		m.viewport.Width = msg.Width - 30
		m.viewport.Height = msg.Height - 5 // header + input + status
	}

	switch m.view {
	case viewLogin:
		return m.updateLogin(msg)
	case viewChat:
		return m.updateChat(msg)
	}
	return m, nil
}

func (m model) updateLogin(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		switch msg.String() {
		case "tab", "down":
			m.inputs[m.loginField].Blur()
			m.loginField = (m.loginField + 1) % 3
			m.inputs[m.loginField].Focus()
			return m, nil
		case "shift+tab", "up":
			m.inputs[m.loginField].Blur()
			m.loginField = (m.loginField + 2) % 3
			m.inputs[m.loginField].Focus()
			return m, nil
		case "enter":
			if m.loginField < 2 {
				m.inputs[m.loginField].Blur()
				m.loginField++
				m.inputs[m.loginField].Focus()
				return m, nil
			}
			return m, m.doLogin()
		case "ctrl+r":
			m.registering = !m.registering
			return m, nil
		}
	case loginDoneMsg:
		if msg.err != nil {
			m.loginErr = msg.err.Error()
			return m, nil
		}
		m.view = viewChat
		m.input.Focus()
		m.viewport = viewport.New(m.width-30, m.height-5)
		return m, m.loadRooms()
	}

	var cmd tea.Cmd
	m.inputs[m.loginField], cmd = m.inputs[m.loginField].Update(msg)
	return m, cmd
}

func (m model) updateChat(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		switch msg.String() {
		case "enter":
			return m, m.doSend()
		case "ctrl+n":
			if m.activeRoom < len(m.rooms)-1 {
				m.activeRoom++
				return m, m.loadMessages()
			}
		case "ctrl+p":
			if m.activeRoom > 0 {
				m.activeRoom--
				return m, m.loadMessages()
			}
		case "ctrl+l":
			return m, m.loadRooms()
		}
	case roomsLoadedMsg:
		if msg.err != nil {
			m.chatErr = msg.err.Error()
			return m, nil
		}
		m.rooms = msg.rooms
		if len(m.rooms) > 0 && m.activeRoom < 0 {
			m.activeRoom = 0
			return m, m.loadMessages()
		}
		return m, nil
	case msgsLoadedMsg:
		if msg.err != nil {
			m.chatErr = msg.err.Error()
			return m, nil
		}
		m.messages = msg.msgs
		m.viewport.SetContent(m.renderMessages())
		m.viewport.GotoBottom()
		return m, m.loadMembers()
	case membersLoadedMsg:
		if msg.err == nil {
			m.members = msg.members
		}
		return m, nil
	case msgSentMsg:
		if msg.err != nil {
			m.chatErr = msg.err.Error()
			return m, nil
		}
		m.chatErr = ""
		m.messages = append(m.messages, *msg.msg)
		m.viewport.SetContent(m.renderMessages())
		m.viewport.GotoBottom()
		return m, nil
	}

	var cmds []tea.Cmd
	var cmd tea.Cmd
	m.input, cmd = m.input.Update(msg)
	cmds = append(cmds, cmd)
	m.viewport, cmd = m.viewport.Update(msg)
	cmds = append(cmds, cmd)
	return m, tea.Batch(cmds...)
}

// ── Commands ─────────────────────────────────────────────

func (m *model) doLogin() tea.Cmd {
	return func() tea.Msg {
		serverURL := m.inputs[0].Value()
		handle := m.inputs[1].Value()
		password := m.inputs[2].Value()

		if serverURL == "" {
			serverURL = "http://localhost:8080"
		}

		c := client.New(serverURL)
		m.client = c

		var err error
		if m.registering {
			// Extract username from handle or use raw value
			username := handle
			if strings.HasPrefix(username, "@") {
				parts := strings.SplitN(username[1:], "@", 2)
				username = parts[0]
			}
			err = c.Register(username, password)
		} else {
			err = c.Login(handle, password)
		}

		return loginDoneMsg{err: err}
	}
}

func (m *model) loadRooms() tea.Cmd {
	return func() tea.Msg {
		rooms, err := m.client.ListRooms()
		return roomsLoadedMsg{rooms: rooms, err: err}
	}
}

func (m *model) loadMessages() tea.Cmd {
	return func() tea.Msg {
		if m.activeRoom < 0 || m.activeRoom >= len(m.rooms) {
			return msgsLoadedMsg{}
		}
		msgs, err := m.client.GetMessages(m.rooms[m.activeRoom].ID, 50)
		// Reverse: API returns newest first
		for i, j := 0, len(msgs)-1; i < j; i, j = i+1, j-1 {
			msgs[i], msgs[j] = msgs[j], msgs[i]
		}
		return msgsLoadedMsg{msgs: msgs, err: err}
	}
}

func (m *model) loadMembers() tea.Cmd {
	return func() tea.Msg {
		if m.activeRoom < 0 || m.activeRoom >= len(m.rooms) {
			return membersLoadedMsg{}
		}
		members, err := m.client.GetMembers(m.rooms[m.activeRoom].ID)
		return membersLoadedMsg{members: members, err: err}
	}
}

func (m *model) doSend() tea.Cmd {
	content := strings.TrimSpace(m.input.Value())
	if content == "" {
		return nil
	}
	m.input.Reset()

	return func() tea.Msg {
		if m.activeRoom < 0 || m.activeRoom >= len(m.rooms) {
			return msgSentMsg{err: fmt.Errorf("no room selected")}
		}
		msg, err := m.client.SendMessage(m.rooms[m.activeRoom].ID, content)
		return msgSentMsg{msg: msg, err: err}
	}
}

// ── View ─────────────────────────────────────────────────

func (m model) View() string {
	switch m.view {
	case viewLogin:
		return m.viewLogin()
	case viewChat:
		return m.viewChat()
	}
	return ""
}

func (m model) viewLogin() string {
	var b strings.Builder
	b.WriteString("\n")
	b.WriteString(titleStyle.Render("  xilebo"))
	b.WriteString("\n\n")

	mode := "Log In"
	if m.registering {
		mode = "Register"
	}
	b.WriteString(fmt.Sprintf("  %s  %s\n\n", mode, dimStyle.Render("(Ctrl+R to toggle)")))

	labels := []string{"Server", "Handle", "Password"}
	for i, label := range labels {
		prefix := "  "
		if i == m.loginField {
			prefix = "> "
		}
		b.WriteString(fmt.Sprintf("%s%s\n", prefix, dimStyle.Render(label)))
		b.WriteString(fmt.Sprintf("  %s\n\n", m.inputs[i].View()))
	}

	if m.loginErr != "" {
		b.WriteString(errStyle.Render("  "+m.loginErr) + "\n\n")
	}

	b.WriteString(dimStyle.Render("  Enter=submit  Tab=next field  Ctrl+C=quit"))
	return b.String()
}

func (m model) viewChat() string {
	sidebar := m.renderSidebar()
	main := m.renderMain()

	// Join sidebar and main horizontally
	return lipgloss.JoinHorizontal(lipgloss.Top, sidebar, main)
}

func (m model) renderSidebar() string {
	var b strings.Builder
	b.WriteString(titleStyle.Render("xilebo") + "\n\n")

	if len(m.rooms) == 0 {
		b.WriteString(dimStyle.Render("No channels\n"))
	}
	for i, r := range m.rooms {
		name := r.Name
		if name == "" {
			name = "unnamed"
		}
		if i == m.activeRoom {
			b.WriteString(activeRoom.Render("# "+name) + "\n")
		} else {
			b.WriteString(roomItemStyle.Render(dimStyle.Render("# ")+name) + "\n")
		}
	}

	b.WriteString("\n")
	if m.client != nil {
		b.WriteString(dimStyle.Render(nameFromHandle(m.client.Handle)))
	}

	return sidebarStyle.Height(m.height - 1).Render(b.String())
}

func (m model) renderMain() string {
	if m.activeRoom < 0 || m.activeRoom >= len(m.rooms) {
		return "\n" + dimStyle.Render("  Select a channel (Ctrl+N/Ctrl+P)")
	}

	room := m.rooms[m.activeRoom]
	header := titleStyle.Render("# "+room.Name)
	if room.Topic != "" {
		header += "  " + dimStyle.Render(room.Topic)
	}

	input := "  " + m.input.View()

	status := statusBar.Render(fmt.Sprintf(
		" %s | %d members | Ctrl+N/P=switch rooms | Ctrl+C=quit",
		room.Name, len(m.members),
	))

	if m.chatErr != "" {
		status = errStyle.Render(" "+m.chatErr)
	}

	return fmt.Sprintf(
		" %s\n%s\n%s\n%s",
		header,
		m.viewport.View(),
		input,
		status,
	)
}

func (m model) renderMessages() string {
	var b strings.Builder
	for _, msg := range m.messages {
		name := nameFromHandle(msg.Sender)
		ts := time.UnixMilli(msg.ServerTS).Format("15:04")
		b.WriteString(fmt.Sprintf("  %s %s\n", msgSender.Render(name), msgTime.Render(ts)))
		b.WriteString(fmt.Sprintf("  %s\n\n", msg.Content))
	}
	if len(m.messages) == 0 {
		b.WriteString(dimStyle.Render("  No messages yet. Say something!\n"))
	}
	return b.String()
}

// ── Helpers ──────────────────────────────────────────────

func nameFromHandle(handle string) string {
	if strings.HasPrefix(handle, "@") {
		parts := strings.SplitN(handle[1:], "@", 2)
		return parts[0]
	}
	return handle
}

// ── Main ─────────────────────────────────────────────────

func main() {
	serverURL := flag.String("server", "", "Server URL (e.g. http://localhost:8080)")
	flag.Parse()

	p := tea.NewProgram(initialModel(*serverURL), tea.WithAltScreen())
	if _, err := p.Run(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
