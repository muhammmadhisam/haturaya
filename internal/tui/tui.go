// Package tui provides the Bubble Tea terminal user interface for Haturaya C2.
package tui

import (
	"fmt"
	"os/exec"
	"regexp"
	"runtime"
	"sort"
	"strconv"
	"strings"

	"github.com/charmbracelet/bubbles/textinput"
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"haturaya/internal/agent"
	hcrypto "haturaya/internal/crypto"
	"haturaya/internal/payload"
)

// ── Mode ────────────────────────────────────────────────────────────────────

// Mode controls what the input line does.
type Mode int

const (
	ModeConsole Mode = iota // main shell — "agents", "generate", etc.
	ModeAgent               // interacting with a specific agent
	ModePayload             // interactive payload builder menu
)

// payloadStep tracks which sub-step of the payload wizard we're in.
type payloadStep int

const (
	payloadStepSelect payloadStep = iota // pick shell type
	payloadStepHost                      // enter lhost
	payloadStepPort                      // enter lport
)

// ── Messages ─────────────────────────────────────────────────────────────────

// AgentConnectedMsg is sent by the handler goroutine when a new agent registers.
type AgentConnectedMsg struct{ Agent *agent.Agent }

// AgentDisconnectedMsg is sent when an agent's connection is lost.
type AgentDisconnectedMsg struct{ ID string }

// AgentOutputMsg carries output from an agent to be displayed in the viewport.
type AgentOutputMsg struct {
	ID   string
	Data string
}

// LogMsg appends a plain text line to the viewport log.
type LogMsg struct{ Text string }

// ── Styles ───────────────────────────────────────────────────────────────────

var (
	cyan   = lipgloss.Color("#00FFFF")
	green  = lipgloss.Color("#00FF88")
	yellow = lipgloss.Color("#FFD700")
	red    = lipgloss.Color("#FF4444")
	dim    = lipgloss.Color("#666666")
	white  = lipgloss.Color("#FFFFFF")
	navy   = lipgloss.Color("#1A1A2E")

	statusStyle = lipgloss.NewStyle().
			Background(navy).
			Foreground(cyan).
			Bold(true).
			Padding(0, 1)

	sidebarHeaderStyle = lipgloss.NewStyle().
				Foreground(cyan).
				Bold(true)

	cursorStyle = lipgloss.NewStyle().
			Foreground(cyan).
			Bold(true)

	encStyle = lipgloss.NewStyle().
			Foreground(green)

	rawStyle = lipgloss.NewStyle().
			Foreground(yellow)

	dimStyle = lipgloss.NewStyle().
			Foreground(dim)

	inputPrefixStyle = lipgloss.NewStyle().
				Foreground(green).
				Bold(true)

	errorStyle = lipgloss.NewStyle().
			Foreground(red)

	borderStyle = lipgloss.NewStyle().
			Foreground(dim)

	viewportStyle = lipgloss.NewStyle().
			Foreground(white)
)

const sidebarWidth = 22

// ── Model ────────────────────────────────────────────────────────────────────

// Model is the root Bubble Tea model.
type Model struct {
	// layout
	width, height int
	ready         bool

	// state
	mode        Mode
	cursor      int          // highlighted row in sidebar
	activeAgent *agent.Agent // non-nil when ModeAgent

	// payload wizard state
	payloadStep  payloadStep
	payloadShell string // selected shell type
	payloadLhost string // entered lhost
	payloadCursor int   // highlighted row in payload menu

	// UI components
	viewport  viewport.Model
	textInput textinput.Model

	// log buffer — all lines ever appended
	lines []string

	// last generated payloads (payload strings only, for copy <n>)
	lastPayloads []string

	// refs injected at construction
	registry *agent.Registry
	cipher   *hcrypto.Cipher
	host     string
	c2Port   int
	webPort  int
}

// New creates the initial Model.
func New(host string, c2Port, webPort int, ciph *hcrypto.Cipher, reg *agent.Registry) *Model {
	ti := textinput.New()
	ti.Focus()
	ti.CharLimit = 1024
	ti.Prompt = ""

	return &Model{
		registry:  reg,
		cipher:    ciph,
		host:      host,
		c2Port:    c2Port,
		webPort:   webPort,
		textInput: ti,
	}
}

// ── tea.Model interface ───────────────────────────────────────────────────────

func (m *Model) Init() tea.Cmd {
	return textinput.Blink
}

func (m *Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	var cmds []tea.Cmd

	switch msg := msg.(type) {

	// ── window resize ─────────────────────────────────────────────────────────
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		m.initViewport()
		m.ready = true

	// ── agent events ──────────────────────────────────────────────────────────
	case AgentConnectedMsg:
		a := msg.Agent
		modeLabel := encStyle.Render("enc")
		if a.Mode == agent.ModeRaw {
			modeLabel = rawStyle.Render("raw")
		}
		m.appendLine(fmt.Sprintf("[+] %s connected (%s) — %s  %s",
			a.IP, modeLabel, a.OS, a.Hostname))

	case AgentDisconnectedMsg:
		if m.activeAgent != nil && m.activeAgent.ID == msg.ID {
			m.activeAgent = nil
			m.mode = ModeConsole
			m.appendLine(errorStyle.Render("[-] active agent lost — returned to console"))
		} else {
			m.appendLine(dimStyle.Render(fmt.Sprintf("[-] agent %s disconnected", msg.ID)))
		}

	case AgentOutputMsg:
		if m.activeAgent != nil && m.activeAgent.ID == msg.ID {
			lines := strings.Split(strings.TrimRight(msg.Data, "\n"), "\n")
			for _, l := range lines {
				m.appendLine(l)
			}
		}

	case LogMsg:
		m.appendLine(msg.Text)

	// ── keyboard ──────────────────────────────────────────────────────────────
	case tea.KeyMsg:
		// Global: ctrl+c always quits.
		if msg.Type == tea.KeyCtrlC {
			return m, tea.Quit
		}

		switch m.mode {
		case ModeConsole:
			cmds = append(cmds, m.handleConsoleKey(msg))
		case ModeAgent:
			cmds = append(cmds, m.handleAgentKey(msg))
		case ModePayload:
			cmds = append(cmds, m.handlePayloadKey(msg))
		}
	}

	// Always update textInput (blink cursor etc.)
	var tiCmd tea.Cmd
	m.textInput, tiCmd = m.textInput.Update(msg)
	cmds = append(cmds, tiCmd)

	// Always update viewport (scroll etc.)
	var vpCmd tea.Cmd
	m.viewport, vpCmd = m.viewport.Update(msg)
	cmds = append(cmds, vpCmd)

	return m, tea.Batch(cmds...)
}

// handleConsoleKey processes keystrokes in ModeConsole.
func (m *Model) handleConsoleKey(msg tea.KeyMsg) tea.Cmd {
	agents := m.registry.List()

	switch msg.Type {
	case tea.KeyUp:
		if m.cursor > 0 {
			m.cursor--
		}
		return nil

	case tea.KeyDown:
		if m.cursor < len(agents)-1 {
			m.cursor++
		}
		return nil

	case tea.KeyEnter:
		// If cursor is on an agent and the input is empty, switch to that agent.
		line := strings.TrimSpace(m.textInput.Value())
		if line == "" {
			if len(agents) > 0 && m.cursor < len(agents) {
				m.switchToAgent(agents[m.cursor])
			}
			return nil
		}
		// Otherwise dispatch as a console command.
		m.textInput.SetValue("")
		return m.dispatchConsole(line)

	default:
		// textInput is updated unconditionally at the bottom of Update() — no-op here.
		return nil
	}
}

// handleAgentKey processes keystrokes in ModeAgent.
func (m *Model) handleAgentKey(msg tea.KeyMsg) tea.Cmd {
	switch msg.Type {
	case tea.KeyEsc:
		m.activeAgent = nil
		m.mode = ModeConsole
		m.appendLine(dimStyle.Render("[*] backgrounded agent — back to console"))
		return nil

	case tea.KeyEnter:
		line := strings.TrimSpace(m.textInput.Value())
		m.textInput.SetValue("")
		if line == "" {
			return nil
		}
		if line == "quit" || line == "exit" || line == "bg" {
			m.activeAgent = nil
			m.mode = ModeConsole
			m.appendLine(dimStyle.Render("[*] backgrounded agent — back to console"))
			return nil
		}
		// Show the command in the viewport.
		if m.activeAgent != nil {
			m.appendLine(inputPrefixStyle.Render(fmt.Sprintf("%s$ ", m.activeAgent.Hostname)) + line)
			// Non-blocking send to CmdChan.
			select {
			case m.activeAgent.CmdChan <- line:
			default:
				m.appendLine(errorStyle.Render("[!] command channel full — try again"))
			}
		}
		return nil

	default:
		// textInput is updated unconditionally at the bottom of Update() — no-op here.
		return nil
	}
}

// handlePayloadKey processes keystrokes in ModePayload.
func (m *Model) handlePayloadKey(msg tea.KeyMsg) tea.Cmd {
	shells := append(payload.ShellTypes(), "all")

	switch m.payloadStep {

	case payloadStepSelect:
		switch msg.Type {
		case tea.KeyUp:
			if m.payloadCursor > 0 {
				m.payloadCursor--
			}
		case tea.KeyDown:
			if m.payloadCursor < len(shells)-1 {
				m.payloadCursor++
			}
		case tea.KeyRunes:
			switch msg.Runes[0] {
			case 'k', 'K':
				if m.payloadCursor > 0 {
					m.payloadCursor--
				}
			case 'j', 'J':
				if m.payloadCursor < len(shells)-1 {
					m.payloadCursor++
				}
			}
		case tea.KeyEnter:
			m.payloadShell = shells[m.payloadCursor]
			m.payloadStep = payloadStepHost
			m.textInput.SetValue("")
			m.textInput.Placeholder = "e.g. 192.168.1.10"
		case tea.KeyEsc:
			m.enterConsole()
		}

	case payloadStepHost:
		switch msg.Type {
		case tea.KeyEnter:
			lhost := strings.TrimSpace(m.textInput.Value())
			if lhost == "" {
				return nil
			}
			m.payloadLhost = lhost
			m.payloadStep = payloadStepPort
			m.textInput.SetValue("9999")
			m.textInput.Placeholder = "e.g. 9999"
		case tea.KeyEsc:
			m.payloadStep = payloadStepSelect
			m.textInput.SetValue("")
			m.textInput.Placeholder = ""
		default:
			return nil
		}

	case payloadStepPort:
		switch msg.Type {
		case tea.KeyEnter:
			lport := strings.TrimSpace(m.textInput.Value())
			if lport == "" {
				lport = "9999"
			}
			m.textInput.SetValue("")
			m.textInput.Placeholder = ""
			m.enterConsole()
			m.showPayloads(m.payloadLhost, lport, m.payloadShell)
		case tea.KeyEsc:
			m.payloadStep = payloadStepHost
			m.textInput.SetValue(m.payloadLhost)
			m.textInput.Placeholder = "e.g. 192.168.1.10"
		default:
			return nil
		}
	}
	return nil
}

func (m *Model) enterPayloadMenu() {
	m.mode = ModePayload
	m.payloadStep = payloadStepSelect
	m.payloadCursor = 0
	m.payloadShell = ""
	m.payloadLhost = ""
	m.textInput.SetValue("")
	m.textInput.Placeholder = ""
}

func (m *Model) enterConsole() {
	m.mode = ModeConsole
	m.textInput.SetValue("")
	m.textInput.Placeholder = ""
}

// switchToAgent transitions to ModeAgent for the given agent.
func (m *Model) switchToAgent(a *agent.Agent) {
	m.activeAgent = a
	m.mode = ModeAgent
	modeLabel := "enc"
	if a.Mode == agent.ModeRaw {
		modeLabel = "raw"
	}
	m.appendLine(inputPrefixStyle.Render(fmt.Sprintf(
		"[*] interacting with agent %d (%s) — %s/%s  (ESC to background)",
		a.Index, modeLabel, a.IP, a.Hostname)))
}

// ── View ─────────────────────────────────────────────────────────────────────

func (m *Model) View() string {
	if !m.ready {
		return "Initializing…\n"
	}

	statusBar := m.renderStatusBar()

	var body string
	if m.mode == ModePayload {
		body = m.renderPayloadMenu()
	} else {
		body = m.renderBody()
	}

	inputLine := m.renderInputLine()

	return lipgloss.JoinVertical(lipgloss.Left, statusBar, body, inputLine)
}

func (m *Model) renderStatusBar() string {
	agentCount := m.registry.Count()
	text := fmt.Sprintf("  HATURAYA C2  │  %s:%d  │  Web: :%d  │  Agents: %d  ",
		m.host, m.c2Port, m.webPort, agentCount)
	return statusStyle.Width(m.width).Render(text)
}

func (m *Model) renderBody() string {
	// Available height: total minus status bar (1) minus input line (1).
	bodyHeight := m.height - 2
	if bodyHeight < 1 {
		bodyHeight = 1
	}

	// Sidebar.
	sidebar := m.renderSidebar(bodyHeight)

	// Divider.
	divLines := make([]string, bodyHeight)
	for i := range divLines {
		divLines[i] = borderStyle.Render("│")
	}
	divider := strings.Join(divLines, "\n")

	// Viewport.
	m.viewport.Width = m.width - sidebarWidth - 1
	m.viewport.Height = bodyHeight

	vpContent := viewportStyle.Render(m.viewport.View())

	return lipgloss.JoinHorizontal(lipgloss.Top, sidebar, divider, vpContent)
}

func (m *Model) renderSidebar(height int) string {
	agents := m.registry.List()

	header := sidebarHeaderStyle.Render("AGENTS") + "\n" +
		dimStyle.Render(strings.Repeat("─", sidebarWidth-1))

	rows := []string{header}
	for i, a := range agents {
		cursor := "  "
		if i == m.cursor {
			cursor = cursorStyle.Render("▶ ")
		}

		modeLabel := encStyle.Render("enc")
		if a.Mode == agent.ModeRaw {
			modeLabel = rawStyle.Render("raw")
		}

		active := ""
		if m.activeAgent != nil && m.activeAgent.ID == a.ID {
			active = cursorStyle.Render("*")
		}

		ip := a.IP
		if len(ip) > 13 {
			ip = ip[:13]
		}

		row := fmt.Sprintf("%s%s%d %s %s", cursor, active, a.Index, modeLabel, ip)
		rows = append(rows, row)
	}

	// Pad to fill sidebar height.
	content := strings.Join(rows, "\n")
	lines := strings.Split(content, "\n")
	for len(lines) < height {
		lines = append(lines, "")
	}
	if len(lines) > height {
		lines = lines[:height]
	}

	return lipgloss.NewStyle().Width(sidebarWidth).Render(strings.Join(lines, "\n"))
}

func (m *Model) renderPayloadMenu() string {
	bodyHeight := m.height - 2
	if bodyHeight < 1 {
		bodyHeight = 1
	}

	shells := append(payload.ShellTypes(), "all")

	// Left panel — shell type list.
	menuWidth := 22
	var rows []string
	rows = append(rows, sidebarHeaderStyle.Render("PAYLOAD TYPE"))
	rows = append(rows, dimStyle.Render(strings.Repeat("─", menuWidth-1)))
	for i, s := range shells {
		cur := "  "
		if i == m.payloadCursor {
			cur = cursorStyle.Render("▶ ")
		}
		label := strings.ToUpper(s)
		if i == m.payloadCursor {
			label = encStyle.Render(label)
		}
		rows = append(rows, cur+label)
	}
	for len(rows) < bodyHeight {
		rows = append(rows, "")
	}
	if len(rows) > bodyHeight {
		rows = rows[:bodyHeight]
	}
	leftPanel := lipgloss.NewStyle().Width(menuWidth).Render(strings.Join(rows, "\n"))

	// Divider.
	divLines := make([]string, bodyHeight)
	for i := range divLines {
		divLines[i] = borderStyle.Render("│")
	}
	divider := strings.Join(divLines, "\n")

	// Right panel — instructions based on current step.
	rightWidth := m.width - menuWidth - 1
	var info []string

	selected := ""
	if m.payloadCursor < len(shells) {
		selected = strings.ToUpper(shells[m.payloadCursor])
	}

	switch m.payloadStep {
	case payloadStepSelect:
		info = append(info,
			encStyle.Render("── Payload Builder ──"),
			"",
			fmt.Sprintf("  Selected : %s", rawStyle.Render(selected)),
			"",
			dimStyle.Render("  ↑ / ↓   navigate"),
			dimStyle.Render("  Enter   confirm selection"),
			dimStyle.Render("  Esc     back to console"),
		)
	case payloadStepHost:
		info = append(info,
			encStyle.Render("── Payload Builder ──"),
			"",
			fmt.Sprintf("  Type     : %s", rawStyle.Render(strings.ToUpper(m.payloadShell))),
			"",
			encStyle.Render("  Enter LHOST (attacker IP):"),
			"",
			dimStyle.Render("  Enter   confirm"),
			dimStyle.Render("  Esc     back"),
		)
	case payloadStepPort:
		info = append(info,
			encStyle.Render("── Payload Builder ──"),
			"",
			fmt.Sprintf("  Type     : %s", rawStyle.Render(strings.ToUpper(m.payloadShell))),
			fmt.Sprintf("  LHOST    : %s", rawStyle.Render(m.payloadLhost)),
			"",
			encStyle.Render("  Enter LPORT (default 9999):"),
			"",
			dimStyle.Render("  Enter   generate"),
			dimStyle.Render("  Esc     back"),
		)
	}

	for len(info) < bodyHeight {
		info = append(info, "")
	}
	if len(info) > bodyHeight {
		info = info[:bodyHeight]
	}
	rightPanel := lipgloss.NewStyle().Width(rightWidth).Render(strings.Join(info, "\n"))

	return lipgloss.JoinHorizontal(lipgloss.Top, leftPanel, divider, rightPanel)
}

func (m *Model) renderInputLine() string {
	var prefix string
	switch m.mode {
	case ModeAgent:
		if m.activeAgent != nil {
			prefix = inputPrefixStyle.Render(
				fmt.Sprintf("[AGENT %d] %s$ ", m.activeAgent.Index, m.activeAgent.Hostname))
		} else {
			prefix = inputPrefixStyle.Render("[AGENT] $ ")
		}
	case ModePayload:
		switch m.payloadStep {
		case payloadStepHost:
			prefix = inputPrefixStyle.Render("[LHOST] > ")
		case payloadStepPort:
			prefix = inputPrefixStyle.Render("[LPORT] > ")
		default:
			return dimStyle.Render("  ↑ / ↓  select   Enter  confirm   Esc  cancel")
		}
	default:
		prefix = inputPrefixStyle.Render("[CONSOLE] > ")
	}
	return prefix + m.textInput.View()
}

// ── Viewport helpers ──────────────────────────────────────────────────────────

func (m *Model) initViewport() {
	bodyHeight := m.height - 2
	if bodyHeight < 1 {
		bodyHeight = 1
	}
	vpWidth := m.width - sidebarWidth - 1
	if vpWidth < 1 {
		vpWidth = 1
	}
	m.viewport = viewport.New(vpWidth, bodyHeight)
	m.viewport.SetContent(strings.Join(m.lines, "\n"))
	m.viewport.GotoBottom()
}

func (m *Model) appendLine(text string) {
	m.lines = append(m.lines, text)
	m.viewport.SetContent(strings.Join(m.lines, "\n"))
	m.viewport.GotoBottom()
}

// ── Console command dispatcher ────────────────────────────────────────────────

var (
	reHost  = regexp.MustCompile(`lhost=(\S+)`)
	rePort  = regexp.MustCompile(`lport=(\d+)`)
	reShell = regexp.MustCompile(`shell=(\S+)`)
)

func (m *Model) dispatchConsole(line string) tea.Cmd {
	parts := strings.Fields(line)
	if len(parts) == 0 {
		return nil
	}
	cmd := strings.ToLower(parts[0])

	switch {
	case cmd == "exit" || cmd == "quit":
		return tea.Quit

	case cmd == "help" || cmd == "h":
		m.appendHelp()

	case cmd == "agents" || cmd == "list":
		m.appendAgentTable()

	case cmd == "payloads" || cmd == "payload":
		m.enterPayloadMenu()

	case cmd == "use" && len(parts) == 2:
		m.cmdUse(parts[1])

	case cmd == "kill" && len(parts) == 2:
		m.cmdKill(parts[1])

	case cmd == "copy" && len(parts) == 2:
		m.cmdCopy(parts[1])

	case cmd == "generate" && len(parts) >= 2 && strings.ToLower(parts[1]) == "payloads":
		m.cmdGenerate(line)

	case line == "server status":
		m.appendLine(fmt.Sprintf("[+] Web: http://%s:%d  C2: %s:%d",
			m.host, m.webPort, m.host, m.c2Port))

	default:
		m.appendLine(errorStyle.Render(fmt.Sprintf("[?] unknown command: %q  (type 'help')", line)))
	}
	return nil
}

func (m *Model) cmdUse(ref string) {
	var a *agent.Agent
	var ok bool
	if n, err := strconv.Atoi(ref); err == nil {
		a, ok = m.registry.GetByIndex(n)
	} else {
		a, ok = m.registry.Get(ref)
	}
	if !ok {
		m.appendLine(errorStyle.Render(fmt.Sprintf("[!] agent %q not found", ref)))
		return
	}
	m.switchToAgent(a)
}

func (m *Model) cmdKill(ref string) {
	var a *agent.Agent
	var ok bool
	if n, err := strconv.Atoi(ref); err == nil {
		a, ok = m.registry.GetByIndex(n)
	} else {
		a, ok = m.registry.Get(ref)
	}
	if !ok {
		m.appendLine(errorStyle.Render(fmt.Sprintf("[!] agent %q not found", ref)))
		return
	}
	if m.activeAgent != nil && m.activeAgent.ID == a.ID {
		m.activeAgent = nil
		m.mode = ModeConsole
	}
	a.Close()
	m.registry.Remove(a.ID)
	m.appendLine(encStyle.Render(fmt.Sprintf("[+] agent %s terminated", a.ID)))
}

func (m *Model) cmdGenerate(line string) {
	mHost := reHost.FindStringSubmatch(line)
	mPort := rePort.FindStringSubmatch(line)
	mShell := reShell.FindStringSubmatch(line)

	if mHost == nil {
		m.appendLine(errorStyle.Render("[!] lhost is required. Usage: generate payloads lhost=<ip> lport=<port> [shell=bash|all|...]"))
		return
	}
	if mPort == nil {
		m.appendLine(errorStyle.Render("[!] lport is required"))
		return
	}

	lhost := mHost[1]
	lport := mPort[1]
	shell := "all"
	if mShell != nil {
		shell = strings.ToLower(mShell[1])
	}

	validShells := append(payload.ShellTypes(), "all")
	valid := false
	for _, s := range validShells {
		if s == shell {
			valid = true
			break
		}
	}
	if !valid {
		m.appendLine(errorStyle.Render(fmt.Sprintf("[!] unknown shell type %q. Valid: %s",
			shell, strings.Join(validShells, ", "))))
		return
	}

	m.showPayloads(lhost, lport, shell)
}

// showPayloads generates and displays payloads, storing them in lastPayloads for copy <n>.
func (m *Model) showPayloads(lhost, lport, shell string) {
	webPort := strconv.Itoa(m.webPort)
	cats := payload.Build(lhost, lport, webPort)

	pDir := payload.PayloadsDir()
	if err := payload.WriteAgent(lhost, lport, m.cipher.KeyHex(), pDir); err != nil {
		m.appendLine(rawStyle.Render(fmt.Sprintf("[!] could not write agent.py: %v", err)))
	}
	if err := payload.WriteXLSM(lhost, lport, pDir); err != nil {
		m.appendLine(rawStyle.Render(fmt.Sprintf("[!] could not write payload.xlsm: %v", err)))
	}

	targetKey := strings.ToUpper(shell)
	var toShow map[string][]payload.Entry
	if targetKey == "ALL" {
		toShow = cats
	} else {
		toShow = map[string][]payload.Entry{targetKey: cats[targetKey]}
	}

	order := []string{"BASH", "SH", "PYTHON", "PERL", "RUBY", "PHP",
		"NETCAT", "SOCAT", "OPENSSL", "POWERSHELL", "OTHER", "EXCEL"}

	m.lastPayloads = nil
	idx := 1
	for _, cat := range order {
		entries, ok := toShow[cat]
		if !ok || len(entries) == 0 {
			continue
		}
		m.appendLine(encStyle.Render(fmt.Sprintf("══ %s payloads — %s:%s ══", strings.ToLower(cat), lhost, lport)))
		for _, e := range entries {
			m.appendLine(fmt.Sprintf("  [%d] %s", idx, rawStyle.Render(e.Label)))
			m.appendLine(fmt.Sprintf("      %s", e.Payload))
			m.appendLine("")
			m.lastPayloads = append(m.lastPayloads, e.Payload)
			idx++
		}
	}
	m.appendLine(encStyle.Render(fmt.Sprintf("[KEY] %s", m.cipher.KeyHex())))
	m.appendLine(dimStyle.Render(fmt.Sprintf("  tip: copy <n> — copy payload to clipboard")))
}

func (m *Model) cmdCopy(ref string) {
	n, err := strconv.Atoi(ref)
	if err != nil || n < 1 || n > len(m.lastPayloads) {
		if len(m.lastPayloads) == 0 {
			m.appendLine(errorStyle.Render("[!] no payloads generated yet — run 'payloads' first"))
		} else {
			m.appendLine(errorStyle.Render(fmt.Sprintf("[!] invalid index — pick 1..%d", len(m.lastPayloads))))
		}
		return
	}
	text := m.lastPayloads[n-1]
	if err := copyToClipboard(text); err != nil {
		m.appendLine(errorStyle.Render(fmt.Sprintf("[!] clipboard error: %v", err)))
		return
	}
	m.appendLine(encStyle.Render(fmt.Sprintf("[+] payload [%d] copied to clipboard", n)))
}

func copyToClipboard(text string) error {
	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "darwin":
		cmd = exec.Command("pbcopy")
	default:
		// Linux: try xclip first, then xsel
		if _, err := exec.LookPath("xclip"); err == nil {
			cmd = exec.Command("xclip", "-selection", "clipboard")
		} else {
			cmd = exec.Command("xsel", "--clipboard", "--input")
		}
	}
	cmd.Stdin = strings.NewReader(text)
	return cmd.Run()
}

func (m *Model) appendAgentTable() {
	agents := m.registry.List()
	if len(agents) == 0 {
		m.appendLine(errorStyle.Render("[!] no agents connected"))
		return
	}

	// Sort by index.
	sort.Slice(agents, func(i, j int) bool { return agents[i].Index < agents[j].Index })

	m.appendLine("┌─────┬──────────────────┬─────────────────┬──────────────┬──────────────┬───────┐")
	m.appendLine("│  #  │ UUID             │ IP              │ OS           │ Hostname     │ Mode  │")
	m.appendLine("├─────┼──────────────────┼─────────────────┼──────────────┼──────────────┼───────┤")
	for _, a := range agents {
		modeStr := "enc"
		if a.Mode == agent.ModeRaw {
			modeStr = "raw"
		}
		m.appendLine(fmt.Sprintf("│ %-3d │ %-16s │ %-15s │ %-12s │ %-12s │ %-5s │",
			a.Index, trunc(a.ID, 16), trunc(a.IP, 15),
			trunc(a.OS, 12), trunc(a.Hostname, 12), modeStr))
	}
	m.appendLine("└─────┴──────────────────┴─────────────────┴──────────────┴──────────────┴───────┘")
}

func (m *Model) appendHelp() {
	lines := []string{
		encStyle.Render("Haturaya C2 — Command Reference"),
		"",
		rawStyle.Render("Core commands:"),
		"  agents / list                     List all connected agents",
		"  use <n>                           Interact with agent by index",
		"  use <uuid>                        Interact with agent by UUID",
		"  kill <n|uuid>                     Terminate an agent connection",
		"  server status                     Show server addresses",
		"  exit / quit                       Shutdown the framework",
		"",
		rawStyle.Render("Payload generation:"),
		"  payloads                          Open interactive payload builder menu",
		"  generate payloads lhost=<ip> lport=<port> [shell=<type>]",
		"  Shell types: bash sh python perl ruby php netcat socat openssl powershell other excel all",
		"",
		rawStyle.Render("Agent session:"),
		"  <any shell command>               Execute on the active agent",
		"  quit / exit / bg  or  ESC        Return to main console",
		"",
		rawStyle.Render("Navigation:"),
		"  ↑ / ↓ (or k/j)                   Move sidebar cursor",
		"  Enter (empty input)               Switch to highlighted agent",
		"  Ctrl+C                            Quit",
	}
	for _, l := range lines {
		m.appendLine(l)
	}
}

// trunc truncates s to at most n bytes, appending "…" if truncated.
func trunc(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n-1] + "…"
}
