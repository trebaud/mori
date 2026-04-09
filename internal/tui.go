package internal

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"syscall"
	"time"

	"charm.land/bubbles/v2/textinput"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/moosecode/mori/internal/agent"
	"github.com/moosecode/mori/internal/git"
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
	barHighStyle  = lipgloss.NewStyle().Foreground(lipgloss.Color("196"))
	barMedStyle   = lipgloss.NewStyle().Foreground(lipgloss.Color("226"))
)

type inputMode int

const (
	modeNormal inputMode = iota
	modeSearch
	modeCreate
	modeConfirmDelete
	modeMessage // send message to WAITING agent
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
		return "default"
	}
}

type statusFilter int

const (
	filterAll statusFilter = iota
	filterWorking
	filterWaiting
	filterIdle
	filterNone
)

func (f statusFilter) String() string {
	switch f {
	case filterWorking:
		return "WORKING"
	case filterWaiting:
		return "WAITING"
	case filterIdle:
		return "IDLE"
	case filterNone:
		return "NONE"
	default:
		return "ALL"
	}
}

func (f statusFilter) matches(status agent.StatusType) bool {
	switch f {
	case filterWorking:
		return status == agent.StatusWorking
	case filterWaiting:
		return status == agent.StatusWait
	case filterIdle:
		return status == agent.StatusIdle
	case filterNone:
		return status == agent.StatusNone
	default:
		return true
	}
}

type statusMsg struct {
	text    string
	isError bool
	expires time.Time
}

// worktreeCreatedMsg is sent when a worktree creation completes
type worktreeCreatedMsg struct {
	err      error
	warnings []string // post-create hook failures
}

// worktreeRemovedMsg is sent when a worktree removal completes
type worktreeRemovedMsg struct {
	err error
}

type model struct {
	worktrees     []Worktree // all worktrees (unfiltered)
	filtered      []int      // indices into worktrees matching current filter
	cursor        int
	selected      int // -1 means nothing selected, otherwise index into worktrees
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

	// Sort & filter
	sortMode     sortMode
	statusFilter statusFilter

	// Delete confirmation
	deleteTarget int  // index in filtered list
	forceDelete  bool // true when triggered by D key

	// Archive
	archived    map[string]bool // branch names that are archived
	showArchive bool            // when true, show archived worktrees
}

func Run(worktrees []Worktree) {
	currentBranch := git.CurrentBranch()

	ti := textinput.New()
	ti.CharLimit = 60

	filtered := make([]int, len(worktrees))
	for i := range worktrees {
		filtered[i] = i
	}

	p := tea.NewProgram(model{
		worktrees:     worktrees,
		filtered:      filtered,
		selected:      -1,
		currentBranch: currentBranch,
		showInsights:  false,
		tick:          time.Now(),
		textInput:     ti,
		mode:          modeNormal,
		sortMode:      sortDefault,
		archived:      loadArchived(),
	})

	m, err := p.Run()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error running TUI: %v\n", err)
		os.Exit(1)
	}

	if finalModel, ok := m.(model); ok && finalModel.selected >= 0 {
		claudePath, err := exec.LookPath("claude")
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error: claude not found in PATH\n")
			os.Exit(1)
		}
		wt := finalModel.worktrees[finalModel.selected]

		// Build args: use --worktree only for non-default branches
		// (the default branch lives in the main repo, not a worktree)
		baseArgs := []string{"--tmux"}
		defaultBranch := git.DefaultBranch(".")
		if wt.Branch != defaultBranch {
			baseArgs = append(baseArgs, "--worktree", filepath.Base(wt.Path))
		}

		if wt.Insights.SessionID != "" {
			args := append([]string{"--resume", wt.Insights.SessionID}, baseArgs...)
			fmt.Fprintf(os.Stderr, "\n  %s\n\n", dimStyle.Render("claude "+strings.Join(args, " ")))
			cmd := exec.Command(claudePath, args...)
			cmd.Stdin = os.Stdin
			cmd.Stdout = os.Stdout
			cmd.Stderr = os.Stderr
			if err := cmd.Run(); err == nil {
				os.Exit(0)
			}
			// Resume failed (session may no longer exist), start fresh
		}

		args := append([]string{"claude"}, baseArgs...)
		fmt.Fprintf(os.Stderr, "\n  %s\n\n", dimStyle.Render(strings.Join(args, " ")))
		syscall.Exec(claudePath, args, os.Environ())
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
	searchActive := m.mode == modeSearch && query != ""

	m.filtered = nil
	for i, wt := range m.worktrees {
		// Archive filter
		if !m.showArchive && m.archived[wt.Branch] {
			continue
		}
		// Text search filter
		if searchActive {
			if !strings.Contains(strings.ToLower(wt.Branch), query) &&
				!strings.Contains(strings.ToLower(wt.RelativePath), query) {
				continue
			}
		}
		// Status filter
		if !m.statusFilter.matches(wt.Insights.Status) {
			continue
		}
		m.filtered = append(m.filtered, i)
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

func statusRank(s agent.StatusType) int {
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
		} else if len(msg.warnings) > 0 {
			m.statusMsg = &statusMsg{text: "Created (warnings: " + strings.Join(msg.warnings, ", ") + ")", isError: true, expires: time.Now().Add(5 * time.Second)}
			m.refreshWorktreeList()
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

	case messageSentMsg:
		if msg.err != nil {
			m.statusMsg = &statusMsg{text: "Message failed: " + msg.err.Error(), isError: true, expires: time.Now().Add(5 * time.Second)}
		} else {
			m.statusMsg = &statusMsg{text: "Message sent", expires: time.Now().Add(3 * time.Second)}
		}
		return m, nil

	case tea.KeyPressMsg:
		return m.handleKey(msg)
	}
	return m, nil
}

func (m model) handleKey(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	key := msg.String()

	switch m.mode {
	case modeSearch:
		return m.handleSearchKey(msg, key)
	case modeCreate:
		return m.handleCreateKey(msg, key)
	case modeConfirmDelete:
		return m.handleDeleteKey(key)
	case modeMessage:
		return m.handleMessageKey(msg, key)
	default:
		return m.handleNormalKey(key)
	}
}

func (m model) handleNormalKey(key string) (tea.Model, tea.Cmd) {
	switch key {
	case "ctrl+c", "q":
		return m, tea.Quit

	// Navigation
	case "up", "k":
		if m.cursor > 0 {
			m.cursor--
		}
	case "down", "j":
		if m.cursor < len(m.filtered)-1 {
			m.cursor++
		}
	case "g":
		m.cursor = 0
	case "G":
		if len(m.filtered) > 0 {
			m.cursor = len(m.filtered) - 1
		}
	case "ctrl+d":
		half := m.height / 4 // approximate half-page
		if half < 1 {
			half = 5
		}
		m.cursor += half
		if m.cursor >= len(m.filtered) {
			m.cursor = max(0, len(m.filtered)-1)
		}
	case "ctrl+u":
		half := m.height / 4
		if half < 1 {
			half = 5
		}
		m.cursor -= half
		if m.cursor < 0 {
			m.cursor = 0
		}

	// Open worktree (was enter, now o)
	case "o":
		if wt := m.selectedWorktree(); wt != nil {
			if wt.IsMain {
				m.statusMsg = &statusMsg{text: "Cannot open default branch (--tmux requires --worktree)", isError: true, expires: time.Now().Add(3 * time.Second)}
				return m, nil
			}
			m.selected = m.filtered[m.cursor]
			return m, tea.Quit
		}

	// Enter toggles insights for selected worktree
	case "enter":
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
		return m, m.textInput.Focus()
	case "n":
		m.mode = modeCreate
		m.textInput.Placeholder = "branch name (empty for random)"
		m.textInput.SetValue("")
		return m, m.textInput.Focus()
	case "d":
		if wt := m.selectedWorktree(); wt != nil {
			if wt.IsMain {
				m.statusMsg = &statusMsg{text: "Cannot delete main worktree", isError: true, expires: time.Now().Add(3 * time.Second)}
			} else if wt.Insights.Status == agent.StatusWorking || wt.Insights.Status == agent.StatusWait {
				m.statusMsg = &statusMsg{text: "Worktree has active session — use D to force", isError: true, expires: time.Now().Add(3 * time.Second)}
			} else {
				m.mode = modeConfirmDelete
				m.deleteTarget = m.cursor
				m.forceDelete = false
			}
		}
	case "D":
		if wt := m.selectedWorktree(); wt != nil {
			if wt.IsMain {
				m.statusMsg = &statusMsg{text: "Cannot delete main worktree", isError: true, expires: time.Now().Add(3 * time.Second)}
			} else {
				m.mode = modeConfirmDelete
				m.deleteTarget = m.cursor
				m.forceDelete = true
			}
		}

	// Yank (copy path) — vim convention
	case "y":
		if wt := m.selectedWorktree(); wt != nil {
			m.statusMsg = &statusMsg{text: "Path yanked to clipboard", expires: time.Now().Add(3 * time.Second)}
			return m, tea.SetClipboard(wt.Path)
		}

	// Sort & filter
	case "s":
		m.sortMode = (m.sortMode + 1) % 4
		m.applyFilter()
	case "f":
		m.statusFilter = (m.statusFilter + 1) % 5
		m.applyFilter()

	// Jump to next WORKING/WAITING worktree
	case "w":
		if len(m.filtered) > 0 {
			for offset := 1; offset <= len(m.filtered); offset++ {
				idx := (m.cursor + offset) % len(m.filtered)
				wt := m.worktrees[m.filtered[idx]]
				if wt.Insights.Status == agent.StatusWorking || wt.Insights.Status == agent.StatusWait {
					m.cursor = idx
					return m, nil
				}
			}
			m.statusMsg = &statusMsg{text: "No active worktrees", expires: time.Now().Add(2 * time.Second)}
		}

	// Message a WAITING agent
	case "m":
		if wt := m.selectedWorktree(); wt != nil {
			if wt.Insights.Status != agent.StatusWait {
				m.statusMsg = &statusMsg{text: "Agent is not WAITING", isError: true, expires: time.Now().Add(3 * time.Second)}
			} else {
				m.mode = modeMessage
				m.textInput.Placeholder = "message to send..."
				m.textInput.SetValue("")
				return m, m.textInput.Focus()
			}
		}

	// Archive/unarchive
	case "x":
		if wt := m.selectedWorktree(); wt != nil {
			if wt.IsMain {
				m.statusMsg = &statusMsg{text: "Cannot archive main worktree", isError: true, expires: time.Now().Add(3 * time.Second)}
			} else if m.archived[wt.Branch] {
				delete(m.archived, wt.Branch)
				saveArchived(m.archived)
				m.statusMsg = &statusMsg{text: "Unarchived " + wt.Branch, expires: time.Now().Add(3 * time.Second)}
			} else {
				m.archived[wt.Branch] = true
				saveArchived(m.archived)
				m.statusMsg = &statusMsg{text: "Archived " + wt.Branch, expires: time.Now().Add(3 * time.Second)}
				m.applyFilter()
			}
		}
	case "X":
		m.showArchive = !m.showArchive
		m.applyFilter()
		if m.showArchive {
			m.statusMsg = &statusMsg{text: "Showing archived worktrees", expires: time.Now().Add(2 * time.Second)}
		} else {
			m.statusMsg = &statusMsg{text: "Hiding archived worktrees", expires: time.Now().Add(2 * time.Second)}
		}
	}
	return m, nil
}

func (m model) handleSearchKey(msg tea.KeyPressMsg, key string) (tea.Model, tea.Cmd) {
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

func (m model) handleCreateKey(msg tea.KeyPressMsg, key string) (tea.Model, tea.Cmd) {
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
			return m, m.removeWorktreeCmd(wt.Path, m.forceDelete)
		}
		m.mode = modeNormal
	case "n", "N", "esc", "ctrl+c":
		m.mode = modeNormal
	}
	return m, nil
}

// messageSentMsg is sent when a message to a WAITING agent completes
type messageSentMsg struct {
	err error
}

func (m model) handleMessageKey(msg tea.KeyPressMsg, key string) (tea.Model, tea.Cmd) {
	switch key {
	case "ctrl+c":
		return m, tea.Quit
	case "esc":
		m.mode = modeNormal
		m.textInput.Blur()
		return m, nil
	case "enter":
		text := strings.TrimSpace(m.textInput.Value())
		if text == "" {
			m.mode = modeNormal
			m.textInput.Blur()
			return m, nil
		}
		wt := m.selectedWorktree()
		if wt == nil {
			m.mode = modeNormal
			m.textInput.Blur()
			return m, nil
		}
		m.mode = modeNormal
		m.textInput.Blur()
		m.statusMsg = &statusMsg{text: "Sending message...", expires: time.Now().Add(10 * time.Second)}
		return m, m.sendMessageCmd(wt.Path, text)
	}

	var cmd tea.Cmd
	m.textInput, cmd = m.textInput.Update(msg)
	return m, cmd
}

func (m model) sendMessageCmd(worktreePath, text string) tea.Cmd {
	return func() tea.Msg {
		claudePath, err := exec.LookPath("claude")
		if err != nil {
			return messageSentMsg{err: fmt.Errorf("claude not found in PATH")}
		}
		cmd := exec.Command(claudePath, "--worktree", filepath.Base(worktreePath), "--yes", "-p", text)
		if err := cmd.Run(); err != nil {
			return messageSentMsg{err: err}
		}
		return messageSentMsg{}
	}
}

func (m model) createWorktreeCmd(branch string) tea.Cmd {
	return func() tea.Msg {
		repoRoot := m.findRepoRoot()
		if branch == "" {
			branch = "wt-" + RandomSuffix()
		}

		result, err := CreateWorktree(repoRoot, branch)
		if err != nil {
			return worktreeCreatedMsg{err: err}
		}

		var warnings []string
		for _, hr := range result.HookResults {
			if !hr.Success {
				warnings = append(warnings, hr.Name)
			}
		}
		return worktreeCreatedMsg{warnings: warnings}
	}
}

func (m model) removeWorktreeCmd(path string, force bool) tea.Cmd {
	return func() tea.Msg {
		if err := RemoveWorktree(path, force); err != nil {
			return worktreeRemovedMsg{err: err}
		}
		return worktreeRemovedMsg{}
	}
}

func (m model) findRepoRoot() string {
	root, err := git.FindMainRepo(".")
	if err != nil {
		return "."
	}
	return root
}

func (m *model) refreshWorktreeList() {
	if wts, err := List(); err == nil {
		m.worktrees = wts
		m.applyFilter()
	}
}

// --- View ---

func (m model) View() tea.View {
	totalWidth := m.width
	if totalWidth == 0 {
		totalWidth = 140
	}

	if m.showHelp {
		return tea.NewView(m.viewHelp(totalWidth))
	}

	if m.showInsights {
		if totalWidth >= sideByMinWidth {
			return tea.NewView(m.viewSideBySide(totalWidth))
		}
		if totalWidth >= listOnlyMinWidth {
			return tea.NewView(m.viewStacked(totalWidth))
		}
	}
	return tea.NewView(m.viewListOnly(totalWidth))
}

func (m model) renderHeader() string {
	header := " " + headerStyle.Render("MORI") + dimStyle.Render(" | Current: ") + selectedStyle.Render(m.currentBranch)
	if m.statusFilter != filterAll {
		header += dimStyle.Render(" | Filter: ") + footerStyle.Render(m.statusFilter.String())
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
	leftColStyle := lipgloss.NewStyle().Width(listWidth)

	for i := 0; i < maxLines; i++ {
		left := leftColStyle.Render(leftLines[i])
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
			{"g / G", "Jump to first / last"},
			{"Ctrl+d/u", "Half-page down / up"},
			{"w", "Jump to next WORKING/WAITING"},
			{"o", "Open Claude Code in worktree"},
			{"Enter", "Toggle insights panel"},
			{"q, Ctrl+C", "Quit"},
		}},
		{"Actions", []struct{ key, desc string }{
			{"n", "Create new worktree"},
			{"d", "Delete worktree"},
			{"D", "Force delete (skip active check)"},
			{"y", "Yank (copy) worktree path"},
			{"m", "Message a WAITING agent"},
			{"r", "Refresh insights now"},
			{"?", "Toggle this help"},
		}},
		{"Search & Sort", []struct{ key, desc string }{
			{"/", "Search by branch/path"},
			{"s", "Cycle sort (default/status/activity/name)"},
			{"f", "Cycle status filter (all/working/waiting/idle/none)"},
			{"Esc", "Clear filter / cancel"},
		}},
		{"Archive", []struct{ key, desc string }{
			{"x", "Archive / unarchive worktree"},
			{"X", "Toggle show archived"},
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
	case modeMessage:
		return " " + waitingStyle.Render("Message: ") + m.textInput.View()
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
	if m.mode == modeMessage {
		return footerStyle.Render("[Enter] Send  [Esc] Cancel")
	}

	var parts []string
	parts = append(parts, "[?] Help  [o] Open  [q] Quit")

	// Sort & filter indicators
	var indicators []string
	indicators = append(indicators, "sort:"+m.sortMode.String())
	if m.statusFilter != filterAll {
		indicators = append(indicators, "filter:"+m.statusFilter.String())
	}
	if m.showArchive {
		indicators = append(indicators, "archived:shown")
	}
	parts = append(parts, strings.Join(indicators, " "))

	return footerStyle.Render(strings.Join(parts, "  "))
}

func colWidths(width int) (int, int, int, int) {
	branchW := 34
	activityW := 10
	sessionW := 12
	contextW := 10
	if width > 100 {
		branchW = 44
		activityW = 12
	}
	return branchW, activityW, sessionW, contextW
}

func (m model) renderWorktreeList(width int) string {
	var s strings.Builder

	branchW, activityW, sessionW, contextW := colWidths(width)
	tableW := 4 + branchW + activityW + sessionW + contextW

	s.WriteString(dimStyle.Render(strings.Repeat("─", tableW)) + "\n")
	s.WriteString(lipgloss.JoinHorizontal(lipgloss.Top,
		dimStyle.Width(4).Render(""),
		boldStyle.Width(branchW).Render("BRANCH"),
		dimStyle.Width(activityW).Render("ACTIVITY"),
		boldStyle.Width(sessionW).Render("SESSION"),
		dimStyle.Width(contextW).Render("CONTEXT"),
	) + "\n")
	s.WriteString(dimStyle.Render(strings.Repeat("─", tableW)) + "\n")

	for i, wtIdx := range m.filtered {
		s.WriteString(renderWorktreeRow(m, i, wtIdx, branchW, activityW, sessionW, contextW) + "\n")
	}

	if len(m.filtered) == 0 {
		s.WriteString(dimStyle.Render("  No matching worktrees") + "\n")
	}

	return s.String()
}

func statusIcon(status agent.StatusType) string {
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

func statusStyle(status agent.StatusType) lipgloss.Style {
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

// Row background styles for status colorization
var (
	rowWorkingBg = lipgloss.NewStyle().Background(lipgloss.Color("52"))  // faint red/orange
	rowWaitingBg = lipgloss.NewStyle().Background(lipgloss.Color("58"))  // faint yellow
	rowDefaultBg = lipgloss.NewStyle()
)

func rowBgStyle(status agent.StatusType) lipgloss.Style {
	switch status {
	case agent.StatusWorking:
		return rowWorkingBg
	case agent.StatusWait:
		return rowWaitingBg
	default:
		return rowDefaultBg
	}
}

func renderWorktreeRow(m model, cursorIdx, wtIdx int, branchW, activityW, sessionW, contextW int) string {
	wt := m.worktrees[wtIdx]

	trunc := func(s string, w int) string {
		if len(s) > w-3 {
			return s[:w-3] + "..."
		}
		return s
	}

	checkbox := "[ ] "

	branchLabel := wt.Branch
	if wt.IsMain {
		branchLabel = "⊙ " + branchLabel
	}
	if m.archived[wt.Branch] {
		branchLabel = "⌀ " + branchLabel
	}
	branchVal := trunc(branchLabel, branchW)

	activityVal := "—"
	if !wt.Insights.LastActivity.IsZero() {
		activityVal = relativeTime(wt.Insights.LastActivity)
	}

	icon := statusIcon(wt.Insights.Status)
	sessionVal := icon + " " + string(wt.Insights.Status)
	stStyle := statusStyle(wt.Insights.Status)

	// Inline context usage
	contextVal := renderInlineContext(wt.Insights)

	if m.cursor == cursorIdx {
		checkbox = selectedStyle.Render("[*] ")
		branchVal = selectedStyle.Width(branchW).Render(branchVal)
		activityVal = dimStyle.Width(activityW).Render(activityVal)
		sessionVal = stStyle.Width(sessionW).Render(sessionVal)
		contextVal = lipgloss.NewStyle().Width(contextW).Render(contextVal)
	} else {
		checkbox = dimStyle.Render(checkbox)
		branchVal = dimStyle.Width(branchW).Render(branchVal)
		activityVal = dimStyle.Width(activityW).Render(activityVal)
		sessionVal = stStyle.Width(sessionW).Render(sessionVal)
		contextVal = lipgloss.NewStyle().Width(contextW).Render(contextVal)
	}

	row := lipgloss.JoinHorizontal(lipgloss.Top, checkbox, branchVal, activityVal, sessionVal, contextVal)

	// Apply row background tint for WORKING/WAITING
	if bg := rowBgStyle(wt.Insights.Status); wt.Insights.Status == agent.StatusWorking || wt.Insights.Status == agent.StatusWait {
		row = bg.Render(row)
	}

	return row
}

func renderInlineContext(ins agent.Insights) string {
	if ins.InputTokens <= 0 {
		return "—"
	}
	maxTokens := contextMaxTokens(ins.Model)
	percent := float64(ins.InputTokens) / float64(maxTokens)
	if percent > 1 {
		percent = 1
	}
	pct := int(percent * 100)

	var style lipgloss.Style
	switch {
	case percent >= 0.8:
		style = barHighStyle
	case percent >= 0.5:
		style = barMedStyle
	default:
		style = activeStyle
	}
	return style.Render(fmt.Sprintf("%d%%", pct))
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
		mdl = agent.ModelTier(mdl)
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
		maxTokens := contextMaxTokens(wt.Insights.Model)
		percent := float64(wt.Insights.InputTokens) / float64(maxTokens)
		tokenK := wt.Insights.InputTokens / 1000
		label := fmt.Sprintf("%d%% (%dk/%dk)", int(percent*100), tokenK, maxTokens/1000)
		s.WriteString(fmt.Sprintf("%-12s %s\n", dimStyle.Render("CONTEXT:"), renderContextBar(percent, label)))
	} else {
		const maxSize int64 = 10 * 1024 * 1024
		percent := float64(wt.Insights.SessionSize) / float64(maxSize)
		label := fmt.Sprintf("%d%%", int(percent*100))
		s.WriteString(fmt.Sprintf("%-12s %s\n", dimStyle.Render("CONTEXT:"), renderContextBar(percent, label)))
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

func contextMaxTokens(model string) int {
	switch agent.ModelTier(model) {
	case "opus":
		return 1_000_000
	default:
		return 200_000
	}
}

func renderContextBar(percent float64, label string) string {
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

	var barStyle lipgloss.Style
	switch {
	case percent >= 0.8:
		barStyle = barHighStyle
	case percent >= 0.5:
		barStyle = barMedStyle
	default:
		barStyle = activeStyle
	}

	return dimStyle.Render("[") + barStyle.Render(bar) + dimStyle.Render("] ") + dimStyle.Render(label)
}

// --- Archive persistence ---

func archivePath() string {
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".mori", "archived.json")
}

func loadArchived() map[string]bool {
	data, err := os.ReadFile(archivePath())
	if err != nil {
		return make(map[string]bool)
	}
	var branches []string
	if json.Unmarshal(data, &branches) != nil {
		return make(map[string]bool)
	}
	m := make(map[string]bool, len(branches))
	for _, b := range branches {
		m[b] = true
	}
	return m
}

func saveArchived(m map[string]bool) {
	var branches []string
	for b := range m {
		branches = append(branches, b)
	}
	sort.Strings(branches)
	data, _ := json.Marshal(branches)
	os.MkdirAll(filepath.Dir(archivePath()), 0755)
	os.WriteFile(archivePath(), data, 0644)
}
