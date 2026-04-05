package tui

import (
	"fmt"
	"os"
	"os/exec"
	"sort"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/moosecode/mori/agent"
	"github.com/moosecode/mori/config"
)

const (
	insightsWidth    = 50
	tickFast         = 2 * time.Second
	tickSlow         = 10 * time.Second
	sideByMinWidth   = 120
	listOnlyMinWidth = 80
)

var (
	headerStyle   = lipgloss.NewStyle().Foreground(lipgloss.Color("205")).Bold(true)
	activeStyle   = lipgloss.NewStyle().Foreground(lipgloss.Color("76"))
	noneStyle     = lipgloss.NewStyle().Foreground(lipgloss.Color("240"))
	selectedStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("212")).Bold(true)
	footerStyle   = lipgloss.NewStyle().Foreground(lipgloss.Color("243"))
	dimStyle      = lipgloss.NewStyle().Foreground(lipgloss.Color("240"))
	workingStyle  = lipgloss.NewStyle().Foreground(lipgloss.Color("214")).Bold(true)
	waitingStyle  = lipgloss.NewStyle().Foreground(lipgloss.Color("226")).Bold(true)
	boldStyle     = lipgloss.NewStyle().Bold(true)
	errorStyle    = lipgloss.NewStyle().Foreground(lipgloss.Color("196"))
	successStyle  = lipgloss.NewStyle().Foreground(lipgloss.Color("76"))
)

type inputMode int

const (
	modeNormal inputMode = iota
	modeSearch
	modeCreate
	modeConfirmDelete
)

type sortMode int

const (
	sortDefault sortMode = iota
	sortStatus
	sortActivity
	sortName
)

func (s sortMode) String() string {
	switch s {
	case sortStatus:
		return "status"
	case sortActivity:
		return "activity"
	case sortName:
		return "name"
	default:
		return ""
	}
}

type statusMsg struct {
	text    string
	isError bool
	expires time.Time
}

// worktreeCreatedMsg is sent when a worktree creation completes
type worktreeCreatedMsg struct {
	err error
}

// worktreeRemovedMsg is sent when a worktree removal completes
type worktreeRemovedMsg struct {
	err error
}

type model struct {
	worktrees     []Worktree // all worktrees (unfiltered)
	filtered      []int      // indices into worktrees matching current filter
	cursor        int
	selected      string
	currentBranch string
	width         int
	height        int
	showInsights  bool
	showHelp      bool
	tick          time.Time

	// Input modes
	mode      inputMode
	textInput textinput.Model
	statusMsg *statusMsg

	// Sort
	sortMode sortMode

	// Delete confirmation
	deleteTarget int // index in filtered list
}

func Run(worktrees []Worktree) {
	currentBranch := ""
	if out, err := exec.Command("git", "branch", "--show-current").Output(); err == nil {
		currentBranch = strings.TrimSpace(string(out))
	}

	ti := textinput.New()
	ti.CharLimit = 60

	filtered := make([]int, len(worktrees))
	for i := range worktrees {
		filtered[i] = i
	}

	p := tea.NewProgram(model{
		worktrees:     worktrees,
		filtered:      filtered,
		currentBranch: currentBranch,
		showInsights:  false,
		tick:          time.Now(),
		textInput:     ti,
		mode:          modeNormal,
		sortMode:      sortDefault,
	})

	m, err := p.Run()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error running TUI: %v\n", err)
		os.Exit(1)
	}

	if finalModel, ok := m.(model); ok && finalModel.selected != "" {
		fmt.Print(finalModel.selected)
	}
}

func (m model) hasActiveAgent() bool {
	for _, wt := range m.worktrees {
		if wt.Insights.Status == agent.StatusWorking {
			return true
		}
	}
	return false
}

func (m model) tickInterval() time.Duration {
	if m.hasActiveAgent() {
		return tickFast
	}
	return tickSlow
}

func (m model) Init() tea.Cmd {
	return tea.Tick(m.tickInterval(), func(t time.Time) tea.Msg {
		return t
	})
}

func (m *model) refreshInsights() {
	for i := range m.worktrees {
		m.worktrees[i].Insights = agent.GetInsights(m.worktrees[i].Path)
	}
	m.tick = time.Now()
}

func (m *model) applyFilter() {
	query := strings.ToLower(m.textInput.Value())
	if m.mode != modeSearch || query == "" {
		m.filtered = make([]int, len(m.worktrees))
		for i := range m.worktrees {
			m.filtered[i] = i
		}
	} else {
		m.filtered = nil
		for i, wt := range m.worktrees {
			if strings.Contains(strings.ToLower(wt.Branch), query) ||
				strings.Contains(strings.ToLower(wt.RelativePath), query) {
				m.filtered = append(m.filtered, i)
			}
		}
	}
	m.applySort()
	if m.cursor >= len(m.filtered) {
		m.cursor = max(0, len(m.filtered)-1)
	}
}

func (m *model) applySort() {
	if m.sortMode == sortDefault {
		return
	}
	wts := m.worktrees
	sort.SliceStable(m.filtered, func(a, b int) bool {
		ia, ib := m.filtered[a], m.filtered[b]
		switch m.sortMode {
		case sortStatus:
			return statusRank(wts[ia].Insights.Status) < statusRank(wts[ib].Insights.Status)
		case sortActivity:
			return wts[ia].Insights.LastActivity.After(wts[ib].Insights.LastActivity)
		case sortName:
			return strings.ToLower(wts[ia].Branch) < strings.ToLower(wts[ib].Branch)
		}
		return false
	})
}

func statusRank(s agent.AgentStatusType) int {
	switch s {
	case agent.StatusWorking:
		return 0
	case agent.StatusWait:
		return 1
	case agent.StatusIdle:
		return 2
	default:
		return 3
	}
}

func (m model) selectedWorktree() *Worktree {
	if m.cursor < len(m.filtered) {
		return &m.worktrees[m.filtered[m.cursor]]
	}
	return nil
}

func (m model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		return m, nil

	case time.Time:
		m.refreshInsights()
		m.applyFilter()
		// Clear expired status messages
		if m.statusMsg != nil && time.Now().After(m.statusMsg.expires) {
			m.statusMsg = nil
		}
		return m, tea.Tick(m.tickInterval(), func(t time.Time) tea.Msg {
			return t
		})

	case worktreeCreatedMsg:
		if msg.err != nil {
			m.statusMsg = &statusMsg{text: "Create failed: " + msg.err.Error(), isError: true, expires: time.Now().Add(5 * time.Second)}
		} else {
			m.statusMsg = &statusMsg{text: "Worktree created", expires: time.Now().Add(3 * time.Second)}
			m.refreshWorktreeList()
		}
		return m, nil

	case worktreeRemovedMsg:
		if msg.err != nil {
			m.statusMsg = &statusMsg{text: "Remove failed: " + msg.err.Error(), isError: true, expires: time.Now().Add(5 * time.Second)}
		} else {
			m.statusMsg = &statusMsg{text: "Worktree removed", expires: time.Now().Add(3 * time.Second)}
			m.refreshWorktreeList()
		}
		return m, nil

	case tea.KeyMsg:
		return m.handleKey(msg)
	}
	return m, nil
}

func (m model) handleKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	key := msg.String()

	switch m.mode {
	case modeSearch:
		return m.handleSearchKey(msg, key)
	case modeCreate:
		return m.handleCreateKey(msg, key)
	case modeConfirmDelete:
		return m.handleDeleteKey(key)
	default:
		return m.handleNormalKey(key)
	}
}

func (m model) handleNormalKey(key string) (tea.Model, tea.Cmd) {
	switch key {
	case "ctrl+c", "q":
		return m, tea.Quit
	case "up", "k":
		if m.cursor > 0 {
			m.cursor--
		}
	case "down", "j":
		if m.cursor < len(m.filtered)-1 {
			m.cursor++
		}
	case "enter":
		if wt := m.selectedWorktree(); wt != nil {
			m.selected = wt.Path
		}
		return m, tea.Quit
	case "i":
		m.showInsights = !m.showInsights
	case "r":
		m.refreshInsights()
		m.applyFilter()
		return m, tea.Tick(m.tickInterval(), func(t time.Time) tea.Msg {
			return t
		})
	case "?":
		m.showHelp = !m.showHelp
	case "/":
		m.mode = modeSearch
		m.textInput.Placeholder = "filter by branch or path..."
		m.textInput.SetValue("")
		m.textInput.Focus()
		return m, m.textInput.Cursor.BlinkCmd()
	case "n":
		m.mode = modeCreate
		m.textInput.Placeholder = "branch name (empty for random)"
		m.textInput.SetValue("")
		m.textInput.Focus()
		return m, m.textInput.Cursor.BlinkCmd()
	case "d":
		if wt := m.selectedWorktree(); wt != nil {
			if wt.IsMain {
				m.statusMsg = &statusMsg{text: "Cannot delete main worktree", isError: true, expires: time.Now().Add(3 * time.Second)}
			} else if wt.Insights.Status == agent.StatusWorking || wt.Insights.Status == agent.StatusWait {
				m.statusMsg = &statusMsg{text: "Worktree has active session — use D to force", isError: true, expires: time.Now().Add(3 * time.Second)}
			} else {
				m.mode = modeConfirmDelete
				m.deleteTarget = m.cursor
			}
		}
	case "D":
		if wt := m.selectedWorktree(); wt != nil {
			if wt.IsMain {
				m.statusMsg = &statusMsg{text: "Cannot delete main worktree", isError: true, expires: time.Now().Add(3 * time.Second)}
			} else {
				m.mode = modeConfirmDelete
				m.deleteTarget = m.cursor
			}
		}
	case "s":
		m.sortMode = (m.sortMode + 1) % 4
		m.applyFilter()
	}
	return m, nil
}

func (m model) handleSearchKey(msg tea.KeyMsg, key string) (tea.Model, tea.Cmd) {
	switch key {
	case "ctrl+c":
		return m, tea.Quit
	case "esc":
		m.mode = modeNormal
		m.textInput.SetValue("")
		m.textInput.Blur()
		m.applyFilter()
		return m, nil
	case "enter":
		m.mode = modeNormal
		m.textInput.Blur()
		// Keep filter active
		return m, nil
	case "up", "down":
		if key == "up" && m.cursor > 0 {
			m.cursor--
		} else if key == "down" && m.cursor < len(m.filtered)-1 {
			m.cursor++
		}
		return m, nil
	}

	var cmd tea.Cmd
	m.textInput, cmd = m.textInput.Update(msg)
	m.applyFilter()
	return m, cmd
}

func (m model) handleCreateKey(msg tea.KeyMsg, key string) (tea.Model, tea.Cmd) {
	switch key {
	case "ctrl+c":
		return m, tea.Quit
	case "esc":
		m.mode = modeNormal
		m.textInput.Blur()
		return m, nil
	case "enter":
		branch := strings.TrimSpace(m.textInput.Value())
		m.mode = modeNormal
		m.textInput.Blur()
		m.statusMsg = &statusMsg{text: "Creating worktree...", expires: time.Now().Add(30 * time.Second)}
		return m, m.createWorktreeCmd(branch)
	}

	var cmd tea.Cmd
	m.textInput, cmd = m.textInput.Update(msg)
	return m, cmd
}

func (m model) handleDeleteKey(key string) (tea.Model, tea.Cmd) {
	switch key {
	case "y", "Y":
		if m.deleteTarget < len(m.filtered) {
			wt := m.worktrees[m.filtered[m.deleteTarget]]
			m.mode = modeNormal
			m.statusMsg = &statusMsg{text: "Removing worktree...", expires: time.Now().Add(30 * time.Second)}
			return m, m.removeWorktreeCmd(wt.Path)
		}
		m.mode = modeNormal
	case "n", "N", "esc", "ctrl+c":
		m.mode = modeNormal
	}
	return m, nil
}

func (m model) createWorktreeCmd(branch string) tea.Cmd {
	return func() tea.Msg {
		repoRoot := m.findRepoRoot()
		args := []string{"-C", repoRoot, "worktree", "add"}
		if branch == "" {
			branch = "wt-" + randomSuffix()
		}

		mainBranch := m.currentBranch
		if mainBranch == "" {
			mainBranch = "main"
		}

		worktreeDir := repoRoot + "/.claude/worktrees/" + branch
		args = append(args, worktreeDir, "-b", branch, mainBranch)

		if err := exec.Command("git", args...).Run(); err != nil {
			return worktreeCreatedMsg{err: fmt.Errorf("git worktree add failed: %w", err)}
		}

		cfg := config.Load(repoRoot)
		for _, step := range cfg.PostCreate {
			cmd := exec.Command("sh", "-c", step.Cmd)
			cmd.Dir = worktreeDir
			cmd.Run()
		}

		return worktreeCreatedMsg{}
	}
}

func (m model) removeWorktreeCmd(path string) tea.Cmd {
	return func() tea.Msg {
		if err := exec.Command("git", "worktree", "remove", path).Run(); err != nil {
			// Try force
			if err2 := exec.Command("git", "worktree", "remove", "--force", path).Run(); err2 != nil {
				return worktreeRemovedMsg{err: err2}
			}
		}
		return worktreeRemovedMsg{}
	}
}

func (m model) findRepoRoot() string {
	out, err := exec.Command("git", "rev-parse", "--show-toplevel").Output()
	if err != nil {
		return "."
	}
	return strings.TrimSpace(string(out))
}

func (m *model) refreshWorktreeList() {
	if out, err := exec.Command("git", "worktree", "list", "--porcelain").Output(); err == nil {
		// Re-parse worktrees
		var wts []Worktree
		var current Worktree

		for _, line := range strings.Split(string(out), "\n") {
			if strings.HasPrefix(line, "worktree ") {
				if current.Path != "" {
					wts = append(wts, current)
				}
				current = Worktree{Path: strings.TrimPrefix(line, "worktree ")}
			} else if strings.HasPrefix(line, "branch ") {
				parts := strings.Split(line, "/")
				current.Branch = parts[len(parts)-1]
				if current.Branch == m.currentBranch {
					current.IsMain = true
				}
			}
		}
		if current.Path != "" {
			wts = append(wts, current)
		}

		home, _ := os.UserHomeDir()
		gitDir, _ := exec.Command("git", "rev-parse", "--git-dir").Output()
		mainPath := ""
		if len(gitDir) > 0 {
			mainPath = strings.TrimSpace(string(gitDir))
			if idx := strings.LastIndex(mainPath, "/"); idx >= 0 {
				mainPath = mainPath[:idx]
			}
		}

		for i := range wts {
			wts[i].RelativePath = makeRelativePath(wts[i].Path, mainPath, home)
			wts[i].Insights = agent.GetInsights(wts[i].Path)
		}

		m.worktrees = wts
		m.applyFilter()
	}
}

func makeRelativePath(path, mainPath, home string) string {
	rel := path
	if home != "" && strings.HasPrefix(rel, home) {
		rel = "~" + rel[len(home):]
	}

	parts := strings.Split(rel, "/")
	if len(parts) > 0 {
		name := parts[len(parts)-1]
		if mainPath != "" && name == baseName(mainPath) {
			return "./main"
		}
		return "./" + name
	}
	return rel
}

func baseName(path string) string {
	parts := strings.Split(path, "/")
	if len(parts) > 0 {
		return parts[len(parts)-1]
	}
	return path
}

func randomSuffix() string {
	out, err := exec.Command("sh", "-c", "LC_ALL=C tr -dc 'a-z0-9' </dev/urandom | head -c5").Output()
	if err != nil || len(string(out)) == 0 {
		return fmt.Sprintf("%d", os.Getpid())
	}
	return string(out)
}

// --- View ---

func (m model) View() string {
	totalWidth := m.width
	if totalWidth == 0 {
		totalWidth = 140
	}

	if m.showHelp {
		return m.viewHelp(totalWidth)
	}

	if m.showInsights {
		if totalWidth >= sideByMinWidth {
			return m.viewSideBySide(totalWidth)
		}
		if totalWidth >= listOnlyMinWidth {
			return m.viewStacked(totalWidth)
		}
	}
	return m.viewListOnly(totalWidth)
}

func (m model) renderHeader() string {
	header := " " + headerStyle.Render("MORI") + dimStyle.Render(" | Current: ") + selectedStyle.Render(m.currentBranch)
	if m.sortMode != sortDefault {
		header += dimStyle.Render(" | Sort: ") + footerStyle.Render(m.sortMode.String())
	}
	return header
}

func (m model) viewListOnly(width int) string {
	var s strings.Builder

	s.WriteString("\n")
	s.WriteString(m.renderHeader() + "\n")
	s.WriteString("\n")
	s.WriteString(m.renderWorktreeList(width) + "\n")
	s.WriteString(m.renderInputLine() + "\n")
	s.WriteString(m.renderFooter() + "\n")

	return s.String()
}

func (m model) viewStacked(width int) string {
	var s strings.Builder

	s.WriteString("\n")
	s.WriteString(m.renderHeader() + "\n")
	s.WriteString("\n")
	s.WriteString(m.renderWorktreeList(width) + "\n")
	s.WriteString("\n")
	s.WriteString(renderInsightsPanel(m, width-4) + "\n")
	s.WriteString(m.renderInputLine() + "\n")
	s.WriteString(m.renderFooter() + "\n")

	return s.String()
}

func (m model) viewSideBySide(width int) string {
	var s strings.Builder

	s.WriteString("\n")
	s.WriteString(m.renderHeader() + "\n")
	s.WriteString("\n")

	listWidth := width - insightsWidth - 3
	leftPane := m.renderWorktreeList(listWidth)
	rightPane := renderInsightsPanel(m, insightsWidth)

	leftLines := strings.Split(leftPane, "\n")
	rightLines := strings.Split(rightPane, "\n")

	maxLines := len(leftLines)
	if len(rightLines) > maxLines {
		maxLines = len(rightLines)
	}

	for len(leftLines) < maxLines {
		leftLines = append(leftLines, "")
	}
	for len(rightLines) < maxLines {
		rightLines = append(rightLines, "")
	}

	divider := dimStyle.Render("│")

	for i := 0; i < maxLines; i++ {
		left := lipgloss.NewStyle().Width(listWidth).Render(leftLines[i])
		right := rightLines[i]
		s.WriteString(left + " " + divider + " " + right + "\n")
	}

	s.WriteString("\n")
	s.WriteString(m.renderInputLine() + "\n")
	s.WriteString(m.renderFooter() + "\n")

	return s.String()
}

func (m model) viewHelp(width int) string {
	var s strings.Builder

	s.WriteString("\n")
	s.WriteString(" " + headerStyle.Render("MORI") + dimStyle.Render(" | Keybindings") + "\n")
	s.WriteString("\n")

	w := 50
	if width < w {
		w = width - 4
	}

	s.WriteString(dimStyle.Render(strings.Repeat("─", w)) + "\n")

	bindings := []struct {
		section string
		keys    []struct{ key, desc string }
	}{
		{"Navigation", []struct{ key, desc string }{
			{"j/k, ↑/↓", "Move cursor up/down"},
			{"Enter", "Select worktree and switch"},
			{"q, Ctrl+C", "Quit"},
		}},
		{"Views", []struct{ key, desc string }{
			{"i", "Toggle insights panel"},
			{"?", "Toggle this help"},
		}},
		{"Actions", []struct{ key, desc string }{
			{"n", "Create new worktree"},
			{"d", "Delete worktree"},
			{"D", "Force delete (skip active session check)"},
			{"r", "Refresh insights now"},
		}},
		{"Search & Sort", []struct{ key, desc string }{
			{"/", "Filter by branch/path"},
			{"s", "Cycle sort mode"},
			{"Esc", "Clear filter / cancel"},
		}},
	}

	for _, section := range bindings {
		s.WriteString("\n " + boldStyle.Render(section.section) + "\n")
		for _, b := range section.keys {
			s.WriteString(fmt.Sprintf("  %-14s %s\n", selectedStyle.Render(b.key), dimStyle.Render(b.desc)))
		}
	}

	s.WriteString("\n" + dimStyle.Render(strings.Repeat("─", w)) + "\n")
	s.WriteString(footerStyle.Render("[?] Close help  [q] Quit") + "\n")

	return s.String()
}

func (m model) renderInputLine() string {
	switch m.mode {
	case modeSearch:
		return " " + dimStyle.Render("/") + " " + m.textInput.View()
	case modeCreate:
		return " " + headerStyle.Render("New worktree: ") + m.textInput.View()
	case modeConfirmDelete:
		if m.deleteTarget < len(m.filtered) {
			wt := m.worktrees[m.filtered[m.deleteTarget]]
			return " " + errorStyle.Render("Delete "+wt.Branch+"? ") + dimStyle.Render("[y/N]")
		}
		return ""
	default:
		if m.statusMsg != nil && time.Now().Before(m.statusMsg.expires) {
			if m.statusMsg.isError {
				return " " + errorStyle.Render(m.statusMsg.text)
			}
			return " " + successStyle.Render(m.statusMsg.text)
		}
		return ""
	}
}

func (m model) renderFooter() string {
	if m.mode == modeSearch {
		return footerStyle.Render("[Enter] Apply  [Esc] Clear  [↑/↓] Navigate")
	}
	if m.showInsights {
		return footerStyle.Render("[?] Help  [i] Hide Insights  [Enter] Switch  [q] Quit")
	}
	return footerStyle.Render("[?] Help  [i] Insights  [Enter] Switch  [q] Quit")
}

func colWidths(width int) (int, int, int, int) {
	pathW := 30
	branchW := 25
	sessionW := 12
	costW := 8
	if width > 100 {
		pathW = 40
		branchW = 30
	}
	return pathW, branchW, sessionW, costW
}

func (m model) renderWorktreeList(width int) string {
	var s strings.Builder

	pathW, branchW, sessionW, costW := colWidths(width)
	tableW := 4 + pathW + branchW + sessionW + costW

	s.WriteString(dimStyle.Render(strings.Repeat("─", tableW)) + "\n")
	s.WriteString(lipgloss.JoinHorizontal(0,
		dimStyle.Width(4).Render(""),
		boldStyle.Width(pathW).Render("PATH"),
		boldStyle.Width(branchW).Render("BRANCH"),
		boldStyle.Width(sessionW).Render("SESSION"),
		dimStyle.Width(costW).Render("COST"),
	) + "\n")
	s.WriteString(dimStyle.Render(strings.Repeat("─", tableW)) + "\n")

	for i, wtIdx := range m.filtered {
		s.WriteString(renderWorktreeRow(m, i, wtIdx, width) + "\n")
	}

	if len(m.filtered) == 0 {
		s.WriteString(dimStyle.Render("  No matching worktrees") + "\n")
	}

	return s.String()
}

func statusIcon(status agent.AgentStatusType) string {
	switch status {
	case agent.StatusWorking:
		return "●"
	case agent.StatusWait:
		return "◐"
	case agent.StatusIdle:
		return "○"
	default:
		return "·"
	}
}

func statusStyle(status agent.AgentStatusType) lipgloss.Style {
	switch status {
	case agent.StatusWorking:
		return workingStyle
	case agent.StatusWait:
		return waitingStyle
	case agent.StatusIdle:
		return activeStyle
	default:
		return noneStyle
	}
}

func renderWorktreeRow(m model, cursorIdx, wtIdx int, width int) string {
	wt := m.worktrees[wtIdx]
	pathW, branchW, sessionW, costW := colWidths(width)

	trunc := func(s string, w int) string {
		if len(s) > w-3 {
			return s[:w-3] + "..."
		}
		return s
	}

	checkbox := "[ ] "
	pathVal := trunc(wt.RelativePath, pathW)
	branchVal := trunc(wt.Branch, branchW)

	icon := statusIcon(wt.Insights.Status)
	sessionVal := icon + " " + string(wt.Insights.Status)
	stStyle := statusStyle(wt.Insights.Status)

	costVal := ""
	if wt.Insights.CostUSD > 0 {
		costVal = fmt.Sprintf("$%.2f", wt.Insights.CostUSD)
	}

	if m.cursor == cursorIdx {
		checkbox = selectedStyle.Render("[*] ")
		pathVal = selectedStyle.Width(pathW).Render(pathVal)
		branchVal = selectedStyle.Width(branchW).Render(branchVal)
		sessionVal = stStyle.Width(sessionW).Render(sessionVal)
		costVal = dimStyle.Width(costW).Render(costVal)
	} else {
		checkbox = dimStyle.Render(checkbox)
		pathVal = dimStyle.Width(pathW).Render(pathVal)
		branchVal = dimStyle.Width(branchW).Render(branchVal)
		sessionVal = stStyle.Width(sessionW).Render(sessionVal)
		costVal = dimStyle.Width(costW).Render(costVal)
	}

	return lipgloss.JoinHorizontal(0, checkbox, pathVal, branchVal, sessionVal, costVal)
}

func renderInsightsPanel(m model, width int) string {
	var s strings.Builder

	s.WriteString(boldStyle.Render(" AGENT INSIGHTS") + "\n")
	s.WriteString(dimStyle.Render(strings.Repeat("─", width)) + "\n")

	wt := m.selectedWorktree()
	if wt == nil {
		s.WriteString(dimStyle.Render("No worktree selected"))
		return s.String()
	}

	statusStr := string(wt.Insights.Status)
	stStyle := statusStyle(wt.Insights.Status)

	// Status + last tool + error badge + last activity
	icon := statusIcon(wt.Insights.Status)
	statusLine := icon + " [" + statusStr + "]"
	if wt.Insights.HasError {
		statusLine += " " + errorStyle.Render("ERROR")
	}
	if wt.Insights.LastTool != "" && wt.Insights.Status == agent.StatusWorking {
		statusLine += " " + dimStyle.Render(wt.Insights.LastTool)
	}
	if wt.Insights.Status == agent.StatusIdle && !wt.Insights.LastActivity.IsZero() {
		ago := relativeTime(wt.Insights.LastActivity)
		statusLine += " " + dimStyle.Render(ago)
	}
	s.WriteString(fmt.Sprintf("%-12s %s\n", dimStyle.Render("STATUS:"), stStyle.Render(statusLine)))

	// Session slug
	session := wt.Insights.Slug
	if session == "" {
		session = wt.Insights.SessionID
		if len(session) > 8 {
			session = session[:8]
		}
	}
	if session == "" {
		session = "—"
	}
	s.WriteString(fmt.Sprintf("%-12s %s\n", dimStyle.Render("SESSION:"), dimStyle.Render(session)))

	// Model + Mode (hide default mode)
	mdl := wt.Insights.Model
	if mdl != "" {
		mdl = modelShort(mdl)
	}
	mode := wt.Insights.Mode
	modelMode := "—"
	if mdl != "" && mode != "" {
		modelMode = mdl + " / " + mode
	} else if mdl != "" {
		modelMode = mdl
	} else if mode != "" {
		modelMode = mode
	}
	s.WriteString(fmt.Sprintf("%-12s %s\n", dimStyle.Render("MODEL:"), dimStyle.Render(modelMode)))

	// Cost
	costStr := "—"
	if wt.Insights.CostUSD > 0 {
		costStr = fmt.Sprintf("$%.2f", wt.Insights.CostUSD)
	}
	s.WriteString(fmt.Sprintf("%-12s %s\n", dimStyle.Render("COST:"), dimStyle.Render(costStr)))

	// Context bar — use input tokens if available, fall back to file size
	if wt.Insights.InputTokens > 0 {
		contextBar := renderContextBarTokens(wt.Insights.InputTokens)
		s.WriteString(fmt.Sprintf("%-12s %s\n", dimStyle.Render("CONTEXT:"), contextBar))
	} else {
		contextBar := renderContextBar(wt.Insights.SessionSize)
		s.WriteString(fmt.Sprintf("%-12s %s\n", dimStyle.Render("CONTEXT:"), contextBar))
	}

	// Branch ahead/behind
	if wt.Insights.AheadBehind != "" {
		s.WriteString(fmt.Sprintf("%-12s %s\n", dimStyle.Render("BRANCH:"), dimStyle.Render(wt.Insights.AheadBehind)))
	}

	// Turn stats — only show when working
	if wt.Insights.Status == agent.StatusWorking && (wt.Insights.TurnDurationS > 0 || wt.Insights.MessageCount > 0) {
		turnStr := fmt.Sprintf("%ds / %d msgs", wt.Insights.TurnDurationS, wt.Insights.MessageCount)
		s.WriteString(fmt.Sprintf("%-12s %s\n", dimStyle.Render("TURN:"), dimStyle.Render(turnStr)))
	}

	// Task
	task := wt.Insights.CurrentTask
	if task == "" {
		task = "—"
	}
	s.WriteString("TASK:\n")
	for _, line := range wrapText(task, width-2) {
		s.WriteString("  " + dimStyle.Render(line) + "\n")
	}

	if len(wt.Insights.GitLog) > 0 {
		s.WriteString("\n" + boldStyle.Render("GIT LOG") + "\n")
		for _, entry := range wt.Insights.GitLog {
			if len(entry) > width-4 {
				entry = entry[:width-7] + "..."
			}
			s.WriteString("  " + dimStyle.Render("• "+entry) + "\n")
		}
	}

	return s.String()
}

func relativeTime(t time.Time) string {
	d := time.Since(t)
	switch {
	case d < time.Minute:
		return "just now"
	case d < time.Hour:
		m := int(d.Minutes())
		if m == 1 {
			return "1m ago"
		}
		return fmt.Sprintf("%dm ago", m)
	case d < 24*time.Hour:
		h := int(d.Hours())
		if h == 1 {
			return "1h ago"
		}
		return fmt.Sprintf("%dh ago", h)
	default:
		days := int(d.Hours() / 24)
		if days == 1 {
			return "1d ago"
		}
		return fmt.Sprintf("%dd ago", days)
	}
}

func wrapText(text string, width int) []string {
	if len(text) <= width {
		return []string{text}
	}

	var lines []string
	words := strings.Fields(text)
	currentLine := ""

	for _, word := range words {
		if len(currentLine)+len(word)+1 <= width {
			if currentLine != "" {
				currentLine += " "
			}
			currentLine += word
		} else {
			if currentLine != "" {
				lines = append(lines, currentLine)
			}
			currentLine = word
		}
	}
	if currentLine != "" {
		lines = append(lines, currentLine)
	}

	return lines
}

func modelShort(model string) string {
	m := strings.ToLower(model)
	switch {
	case strings.Contains(m, "opus"):
		return "opus"
	case strings.Contains(m, "haiku"):
		return "haiku"
	default:
		return "sonnet"
	}
}

func renderContextBarTokens(tokens int) string {
	const maxTokens = 200_000
	percent := float64(tokens) / float64(maxTokens)
	if percent > 1 {
		percent = 1
	}

	bars := 10
	filled := int(percent * float64(bars))
	empty := bars - filled

	bar := strings.Repeat("█", filled) + strings.Repeat("░", empty)
	tokenK := tokens / 1000
	label := fmt.Sprintf("%d%% (%dk/%dk)", int(percent*100), tokenK, maxTokens/1000)

	var barStyle lipgloss.Style
	switch {
	case percent >= 0.8:
		barStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("196"))
	case percent >= 0.5:
		barStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("226"))
	default:
		barStyle = activeStyle
	}

	return dimStyle.Render("[") + barStyle.Render(bar) + dimStyle.Render("] ") + dimStyle.Render(label)
}

func renderContextBar(size int64) string {
	const maxSize int64 = 10 * 1024 * 1024
	percent := float64(size) / float64(maxSize)
	if percent > 1 {
		percent = 1
	}
	if percent < 0 {
		percent = 0
	}

	bars := 10
	filled := int(percent * float64(bars))
	empty := bars - filled

	bar := strings.Repeat("█", filled) + strings.Repeat("░", empty)
	percentStr := fmt.Sprintf("%d%%", int(percent*100))

	var barStyle lipgloss.Style
	switch {
	case percent >= 0.8:
		barStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("196"))
	case percent >= 0.5:
		barStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("226"))
	default:
		barStyle = activeStyle
	}

	return dimStyle.Render("[") + barStyle.Render(bar) + dimStyle.Render("] ") + dimStyle.Render(percentStr)
}
