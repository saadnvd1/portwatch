package main

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

type Port struct {
	Number    int
	Process   string
	PID       int
	Container string
	Service   string
	Label     string
}

func (p Port) URL() string {
	if p.Number < 10000 {
		return fmt.Sprintf("http://localhost:%d", p.Number)
	}
	return ""
}

// Pre-compiled regexes
var (
	listenRe = regexp.MustCompile(`:(\d+)\s+\(LISTEN\)`)
	ansiRe   = regexp.MustCompile(`\x1b\[[0-9;]*m`)
	smPidRe  = regexp.MustCompile(`[●○]\s+(\S+)\s+\(PID\s+(\d+)\)`)
	dockerRe = regexp.MustCompile(`(?:0\.0\.0\.0|:::)(?::)?(\d+)->`)
)

var configDir = filepath.Join(os.Getenv("HOME"), ".config", "portwatch")

type tickMsg time.Time
type scanResultMsg []Port

func tickCmd() tea.Cmd {
	return tea.Tick(2*time.Second, func(t time.Time) tea.Msg { return tickMsg(t) })
}

func scanCmd(labels map[int]string) tea.Cmd {
	return func() tea.Msg {
		ports := scanPorts()
		applyLabels(ports, labels)
		return scanResultMsg(ports)
	}
}

type model struct {
	ports     []Port
	cursor    int
	filter    textinput.Model
	filtering bool
	width     int
	height    int
	labels    map[int]string
	message   string
	msgTimer  int
	scanning  bool
}

func initialModel() model {
	ti := textinput.New()
	ti.Placeholder = "filter..."
	ti.CharLimit = 40

	labels := loadLabels()
	ports := scanPorts()
	applyLabels(ports, labels)

	return model{ports: ports, filter: ti, labels: labels}
}

func (m model) Init() tea.Cmd { return tickCmd() }

func (m *model) setMessage(msg string) {
	m.message = msg
	m.msgTimer = 3
}

func (m *model) selected() *Port {
	f := m.filtered()
	if len(f) == 0 || m.cursor >= len(f) {
		return nil
	}
	return &f[m.cursor]
}

func (m model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height

	case tickMsg:
		if m.msgTimer > 0 {
			m.msgTimer--
			if m.msgTimer == 0 {
				m.message = ""
			}
		}
		if m.scanning {
			return m, tickCmd()
		}
		m.scanning = true
		return m, tea.Batch(tickCmd(), scanCmd(m.labels))

	case scanResultMsg:
		m.scanning = false
		m.ports = []Port(msg)
		filtered := m.filtered()
		if m.cursor >= len(filtered) {
			m.cursor = max(0, len(filtered)-1)
		}
		return m, nil

	case tea.KeyMsg:
		if m.filtering {
			switch msg.String() {
			case "esc":
				m.filtering = false
				m.filter.SetValue("")
				m.filter.Blur()
				m.cursor = 0
			case "enter":
				m.filtering = false
				m.filter.Blur()
				m.cursor = 0
			default:
				var cmd tea.Cmd
				m.filter, cmd = m.filter.Update(msg)
				m.cursor = 0
				return m, cmd
			}
			return m, nil
		}

		filtered := m.filtered()

		switch msg.String() {
		case "q", "ctrl+c":
			return m, tea.Quit
		case "j", "down":
			if m.cursor < len(filtered)-1 {
				m.cursor++
			}
		case "k", "up":
			if m.cursor > 0 {
				m.cursor--
			}
		case "g":
			m.cursor = 0
		case "G":
			m.cursor = max(0, len(filtered)-1)
		case "/":
			m.filtering = true
			m.filter.Focus()
			return m, textinput.Blink
		case "o":
			if p := m.selected(); p != nil && p.URL() != "" {
				exec.Command("open", p.URL()).Start()
				m.setMessage(fmt.Sprintf("opened %s", p.URL()))
			}
		case "K":
			if p := m.selected(); p != nil && p.PID > 0 {
				if err := exec.Command("kill", strconv.Itoa(p.PID)).Run(); err != nil {
					m.setMessage(fmt.Sprintf("failed to kill PID %d", p.PID))
				} else {
					m.setMessage(fmt.Sprintf("killed %s (PID %d)", p.Process, p.PID))
				}
			}
		case "c":
			if p := m.selected(); p != nil {
				curl := fmt.Sprintf("curl http://localhost:%d", p.Number)
				cmd := exec.Command("pbcopy")
				cmd.Stdin = strings.NewReader(curl)
				cmd.Run()
				m.setMessage(fmt.Sprintf("copied: %s", curl))
			}
		case "l":
			if p := m.selected(); p != nil {
				if p.Label != "" {
					delete(m.labels, p.Number)
					m.setMessage(fmt.Sprintf("removed label from :%d", p.Number))
				} else {
					m.labels[p.Number] = p.Process
					m.setMessage(fmt.Sprintf("labeled :%d as \"%s\"", p.Number, p.Process))
				}
				saveLabels(m.labels)
				applyLabels(m.ports, m.labels)
			}
		}
	}

	return m, nil
}

func (m model) filtered() []Port {
	q := strings.ToLower(m.filter.Value())
	if q == "" {
		return m.ports
	}
	var result []Port
	for _, p := range m.ports {
		haystack := strings.ToLower(fmt.Sprintf("%d %s %s %s", p.Number, p.Process, p.Container, p.Label))
		if strings.Contains(haystack, q) {
			result = append(result, p)
		}
	}
	return result
}

// --- Styles (Nord palette) ---

var (
	purple      = lipgloss.Color("#b48ead")
	cyan        = lipgloss.Color("#88c0d0")
	green       = lipgloss.Color("#a3be8c")
	yellow      = lipgloss.Color("#ebcb8b")
	blue        = lipgloss.Color("#81a1c1")
	dimWhite    = lipgloss.Color("#4c566a")
	white       = lipgloss.Color("#d8dee9")
	brightWhite = lipgloss.Color("#eceff4")
	highlight   = lipgloss.Color("#3b4252")

	titleStyle  = lipgloss.NewStyle().Bold(true).Foreground(purple).PaddingLeft(1)
	countStyle  = lipgloss.NewStyle().Foreground(dimWhite)
	headerStyle = lipgloss.NewStyle().Foreground(dimWhite).Bold(true).PaddingLeft(1)
	helpStyle   = lipgloss.NewStyle().Foreground(dimWhite).PaddingLeft(1)
	msgStyle    = lipgloss.NewStyle().Foreground(green).PaddingLeft(1)
	helpKey     = lipgloss.NewStyle().Foreground(blue).Bold(true)
	helpSep     = lipgloss.NewStyle().Foreground(dimWhite)
	detailBox   = lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).BorderForeground(dimWhite).Padding(0, 1).MarginTop(1).MarginLeft(1)
	detailKey   = lipgloss.NewStyle().Foreground(dimWhite)
	detailVal   = lipgloss.NewStyle().Foreground(white)
)

// stylePair returns the appropriate style based on selection state
func stylePair(fg lipgloss.Color, sel bool) lipgloss.Style {
	s := lipgloss.NewStyle().Foreground(fg)
	if sel {
		s = s.Background(highlight).Bold(true)
	}
	return s
}

// dimCol renders a column value or a dim placeholder
func dimCol(val string, width int, fg lipgloss.Color, sel bool) string {
	if val != "" {
		return stylePair(fg, sel).Render(fmt.Sprintf("%-*s", width, truncate(val, width)))
	}
	return stylePair(dimWhite, sel).Render(fmt.Sprintf("%-*s", width, "·"))
}

func (m model) View() string {
	var b strings.Builder

	filtered := m.filtered()

	hasDocker := false
	hasLabels := false
	dockerCount := 0
	for _, p := range filtered {
		if p.Container != "" {
			hasDocker = true
			dockerCount++
		}
		if p.Label != "" {
			hasLabels = true
		}
	}

	// Title
	b.WriteString("\n" + titleStyle.Render("portwatch"))
	count := fmt.Sprintf("  %d ports", len(filtered))
	if dockerCount > 0 {
		count += fmt.Sprintf(" / %d docker", dockerCount)
	}
	b.WriteString(countStyle.Render(count) + "\n")

	if m.filtering {
		b.WriteString("  " + m.filter.View() + "\n")
	} else if m.filter.Value() != "" {
		b.WriteString(helpStyle.Render("/ "+m.filter.Value()) + "\n")
	}

	b.WriteString("\n")

	// Dynamic header
	hdr := fmt.Sprintf(" %-7s  %-20s", "PORT", "PROCESS")
	sepLen := 32
	if hasDocker {
		hdr += fmt.Sprintf("  %-20s", "CONTAINER")
		sepLen += 22
	}
	if hasLabels {
		hdr += fmt.Sprintf("  %-12s", "LABEL")
		sepLen += 14
	}
	b.WriteString(headerStyle.Render(hdr) + "\n")
	b.WriteString(lipgloss.NewStyle().Foreground(dimWhite).PaddingLeft(1).Render(strings.Repeat("─", sepLen)) + "\n")

	// Rows
	maxRows := m.height - 10
	if maxRows < 5 {
		maxRows = 20
	}
	start := 0
	if m.cursor >= maxRows {
		start = m.cursor - maxRows + 1
	}

	for i := start; i < len(filtered) && i < start+maxRows; i++ {
		p := filtered[i]
		sel := i == m.cursor

		line := " " + stylePair(cyan, sel).Render(fmt.Sprintf("%-7d", p.Number)) +
			"  " + stylePair(white, sel).Render(fmt.Sprintf("%-20s", truncate(p.Process, 20)))

		if hasDocker {
			line += "  " + dimCol(p.Container, 20, blue, sel)
		}
		if hasLabels {
			line += "  " + dimCol(p.Label, 12, yellow, sel)
		}

		if sel {
			if pad := m.width - lipgloss.Width(line); pad > 0 {
				line += lipgloss.NewStyle().Background(highlight).Render(strings.Repeat(" ", pad))
			}
		}

		b.WriteString(line + "\n")
	}

	if len(filtered) == 0 {
		b.WriteString(helpStyle.Render("\n  no ports listening\n"))
	}

	// Detail panel
	if p := m.selected(); p != nil {
		var details []string
		if url := p.URL(); url != "" {
			details = append(details, detailKey.Render("url ")+detailVal.Render(url))
		}
		details = append(details, detailKey.Render("pid ")+detailVal.Render(strconv.Itoa(p.PID)))
		if p.Service != "" {
			details = append(details, detailKey.Render("service ")+lipgloss.NewStyle().Foreground(green).Render(p.Service))
		}
		if p.Container != "" {
			details = append(details, detailKey.Render("container ")+detailVal.Render(p.Container))
		}
		if p.Label != "" {
			details = append(details, detailKey.Render("label ")+lipgloss.NewStyle().Foreground(yellow).Render(p.Label))
		}
		b.WriteString(detailBox.Render(strings.Join(details, "   ")) + "\n")
	}

	if m.message != "" {
		b.WriteString(msgStyle.Render(m.message) + "\n")
	}

	b.WriteString("\n")
	b.WriteString(lipgloss.NewStyle().PaddingLeft(1).Render(
		helpKey.Render("o") + helpSep.Render(" open  ") +
			helpKey.Render("K") + helpSep.Render(" kill  ") +
			helpKey.Render("c") + helpSep.Render(" curl  ") +
			helpKey.Render("l") + helpSep.Render(" label  ") +
			helpKey.Render("/") + helpSep.Render(" filter  ") +
			helpKey.Render("q") + helpSep.Render(" quit")))

	return b.String()
}

func truncate(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n-1]) + "…"
}

// --- Process name resolution ---

func getCwd(pid int) string {
	out, err := exec.Command("lsof", "-p", strconv.Itoa(pid), "-Fn", "-a", "-d", "cwd").Output()
	if err != nil {
		return ""
	}
	for _, line := range strings.Split(string(out), "\n") {
		if strings.HasPrefix(line, "n") && len(line) > 1 {
			return line[1:]
		}
	}
	return ""
}

var genericNames = map[string]bool{
	"server": true, "app": true, "index": true, "main": true,
	"start": true, "dev": true, "run": true,
}

func getProcessInfo(pid int) string {
	out, err := exec.Command("ps", "-p", strconv.Itoa(pid), "-o", "args=").Output()
	if err != nil {
		return ""
	}
	args := strings.TrimSpace(string(out))
	if args == "" {
		return ""
	}
	name := friendlyName(args)

	if genericNames[name] {
		if cwd := getCwd(pid); cwd != "" {
			dir := filepath.Base(cwd)
			if dir != "" && dir != "/" {
				return dir + "/" + name
			}
		}
	}

	return name
}

func isPython(bin string) bool {
	return bin == "python3" || bin == "Python" || bin == "python" ||
		strings.HasPrefix(bin, "python3.")
}

func friendlyName(cmdline string) string {
	parts := strings.Fields(cmdline)
	if len(parts) == 0 {
		return ""
	}

	binary := filepath.Base(parts[0])

	if isPython(binary) {
		for _, p := range parts[1:] {
			if strings.HasPrefix(p, "-") {
				continue
			}
			// Keep last 2 path segments for context
			dir, file := filepath.Split(p)
			if dir != "" {
				parent := filepath.Base(strings.TrimSuffix(dir, "/"))
				if parent != "." && parent != "/" {
					return parent + "/" + strings.TrimSuffix(file, ".py")
				}
			}
			return strings.TrimSuffix(file, ".py")
		}
		return "python"
	}

	if binary == "node" {
		rest := strings.Join(parts[1:], " ")

		if strings.Contains(rest, "npx ") {
			if idx := strings.Index(rest, "npx "); idx >= 0 {
				name := strings.TrimRight(strings.TrimSpace(rest[idx+4:]), ")")
				return strings.TrimSpace(name)
			}
		}

		if strings.Contains(rest, "node_modules/.bin/") {
			for _, p := range parts[1:] {
				if strings.Contains(p, "node_modules/.bin/") {
					name := filepath.Base(p)
					for _, a := range parts[2:] {
						if !strings.HasPrefix(a, "-") && !strings.Contains(a, "/") {
							return name + " " + a
						}
					}
					return name
				}
			}
		}

		for _, p := range parts[1:] {
			if strings.HasPrefix(p, "-") || strings.HasPrefix(p, "(") {
				continue
			}
			name := filepath.Base(p)
			name = strings.TrimSuffix(name, ".js")
			name = strings.TrimSuffix(name, ".mjs")
			return name
		}
		return "node"
	}

	return binary
}

// --- Port scanning ---

func scanPorts() []Port {
	out, err := exec.Command("lsof", "-iTCP", "-sTCP:LISTEN", "-nP").Output()
	if err != nil {
		return nil
	}

	portMap := make(map[int]*Port)
	pidNames := make(map[int]string)

	for _, line := range strings.Split(string(out), "\n") {
		fields := strings.Fields(line)
		if len(fields) < 9 {
			continue
		}

		pid, _ := strconv.Atoi(fields[1])
		matches := listenRe.FindStringSubmatch(line)
		if len(matches) < 2 {
			continue
		}
		port, _ := strconv.Atoi(matches[1])
		if port == 0 {
			continue
		}

		if _, exists := portMap[port]; !exists {
			process, ok := pidNames[pid]
			if !ok {
				process = getProcessInfo(pid)
				if process == "" {
					process = fields[0]
				}
				pidNames[pid] = process
			}
			portMap[port] = &Port{
				Number:  port,
				Process: process,
				PID:     pid,
			}
		}
	}

	enrichServiceman(portMap)
	enrichDocker(portMap)

	ports := make([]Port, 0, len(portMap))
	for _, p := range portMap {
		ports = append(ports, *p)
	}
	sort.Slice(ports, func(i, j int) bool {
		return ports[i].Number < ports[j].Number
	})
	return ports
}

func enrichServiceman(portMap map[int]*Port) {
	// sm list --json not supported yet, use text output
	out, err := exec.Command("sm", "list").Output()
	if err != nil {
		return
	}

	cleaned := ansiRe.ReplaceAllString(string(out), "")

	// Build PID -> service name map (direct + children)
	type svc struct {
		name string
		pid  int
	}
	var services []svc

	for _, line := range strings.Split(cleaned, "\n") {
		matches := smPidRe.FindStringSubmatch(line)
		if len(matches) < 3 {
			continue
		}
		pid, _ := strconv.Atoi(matches[2])
		services = append(services, svc{name: matches[1], pid: pid})
	}

	// Map direct PIDs
	for _, s := range services {
		for _, p := range portMap {
			if p.PID == s.pid {
				p.Service = s.name
				p.Process = s.name
			}
		}
	}

	// Map child PIDs
	for _, s := range services {
		children, err := exec.Command("pgrep", "-P", strconv.Itoa(s.pid)).Output()
		if err != nil {
			continue
		}
		for _, line := range strings.Split(strings.TrimSpace(string(children)), "\n") {
			childPID, _ := strconv.Atoi(strings.TrimSpace(line))
			if childPID == 0 {
				continue
			}
			for _, p := range portMap {
				if p.PID == childPID && p.Service == "" {
					p.Service = s.name
					p.Process = s.name
				}
			}
		}
	}
}

func enrichDocker(portMap map[int]*Port) {
	out, err := exec.Command("docker", "ps", "--format", "{{.Names}}\t{{.Ports}}").Output()
	if err != nil {
		return
	}

	for _, line := range strings.Split(strings.TrimSpace(string(out)), "\n") {
		if line == "" {
			continue
		}
		parts := strings.SplitN(line, "\t", 2)
		if len(parts) < 2 {
			continue
		}
		name := parts[0]
		for _, m := range dockerRe.FindAllStringSubmatch(parts[1], -1) {
			port, _ := strconv.Atoi(m[1])
			if p, exists := portMap[port]; exists {
				p.Container = name
			}
		}
	}
}

// --- Labels (JSON persistence) ---

func labelsPath() string {
	return filepath.Join(configDir, "labels.json")
}

func loadLabels() map[int]string {
	labels := make(map[int]string)
	data, err := os.ReadFile(labelsPath())
	if err != nil {
		return labels
	}
	// JSON keys are strings, so unmarshal to map[string]string then convert
	var raw map[string]string
	if err := json.Unmarshal(data, &raw); err != nil {
		return labels
	}
	for k, v := range raw {
		if port, err := strconv.Atoi(k); err == nil {
			labels[port] = v
		}
	}
	return labels
}

func saveLabels(labels map[int]string) {
	os.MkdirAll(configDir, 0755)
	raw := make(map[string]string, len(labels))
	for port, label := range labels {
		raw[strconv.Itoa(port)] = label
	}
	data, err := json.MarshalIndent(raw, "", "  ")
	if err != nil {
		return
	}
	os.WriteFile(labelsPath(), append(data, '\n'), 0644)
}

func applyLabels(ports []Port, labels map[int]string) {
	for i := range ports {
		if label, ok := labels[ports[i].Number]; ok {
			ports[i].Label = label
		}
	}
}

// --- Main ---

func main() {
	if len(os.Args) > 1 && os.Args[1] == "list" {
		ports := scanPorts()
		labels := loadLabels()
		applyLabels(ports, labels)
		fmt.Printf("%-7s %-25s %-8s %-25s %-15s %s\n", "PORT", "PROCESS", "PID", "CONTAINER", "LABEL", "URL")
		for _, p := range ports {
			container, label := "-", "-"
			if p.Container != "" {
				container = p.Container
			}
			if p.Label != "" {
				label = p.Label
			}
			fmt.Printf("%-7d %-25s %-8d %-25s %-15s %s\n", p.Number, p.Process, p.PID, container, label, p.URL())
		}
		fmt.Printf("\n%d ports\n", len(ports))
		return
	}

	p := tea.NewProgram(initialModel(), tea.WithAltScreen())
	if _, err := p.Run(); err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
}
