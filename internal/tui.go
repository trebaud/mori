package internal

import (
	"fmt"
	"image/color"
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
	tickFast         = 2 * time.Second
	tickSlow         = 10 * time.Second
	sideByMinWidth   = 120
	listOnlyMinWidth = 80
)

func insightsPaneWidth(total int) int {
	w := total / 3
	if w < 44 {
		w = 44
	}
	if w > 70 {
		w = 70
	}
	return w
}

// Palette — narrowed to one accent + functional colors; everything else is grayscale.
var (
	colAccent  = lipgloss.Color("205") // brand magenta
	colSuccess = lipgloss.Color("78")  // soft green
	colWarn    = lipgloss.Color("214") // amber
	colDanger  = lipgloss.Color("203") // soft red
	colInfo    = lipgloss.Color("117") // soft cyan

	colText   = lipgloss.Color("252")
	colMuted  = lipgloss.Color("245")
	colDim    = lipgloss.Color("240")
	colFaint  = lipgloss.Color("238")
	colRowBg  = lipgloss.Color("237") // selected row background
	colBorder = lipgloss.Color("238")
)

var (
	titleStyle    = lipgloss.NewStyle().Foreground(colAccent).Bold(true)
	textStyle     = lipgloss.NewStyle().Foreground(colText)
	mutedStyle    = lipgloss.NewStyle().Foreground(colMuted)
	dimStyle      = lipgloss.NewStyle().Foreground(colDim)
	boldStyle     = lipgloss.NewStyle().Bold(true)
	headingStyle  = lipgloss.NewStyle().Foreground(colMuted).Bold(true)
	selectedStyle = lipgloss.NewStyle().Foreground(colAccent).Bold(true)

	workingStyle = lipgloss.NewStyle().Foreground(colWarn).Bold(true)
	waitingStyle = lipgloss.NewStyle().Foreground(colInfo).Bold(true)
	idleStyle    = lipgloss.NewStyle().Foreground(colSuccess)
	noneStyle    = lipgloss.NewStyle().Foreground(colDim)

	errorStyle   = lipgloss.NewStyle().Foreground(colDanger)
	successStyle = lipgloss.NewStyle().Foreground(colSuccess)

	barHighStyle = lipgloss.NewStyle().Foreground(colDanger)
	barMedStyle  = lipgloss.NewStyle().Foreground(colWarn)
	barLowStyle  = lipgloss.NewStyle().Foreground(colSuccess)

	borderStyle = lipgloss.NewStyle().Foreground(colBorder)
)

type inputMode int

const (
	modeNormal inputMode = iota
	modeSearch
	modeCreate
	modeConfirmDelete
	modeMessage
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
		return "working"
	case filterWaiting:
		return "waiting"
	case filterIdle:
		return "idle"
	case filterNone:
		return "none"
	default:
		return "all"
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
	text      string
	isError   bool
	isLoading bool
	expires   time.Time
}

// Durations for the three status-message buckets. Keeping this small and
// named keeps timings cohesive across the UI instead of ad-hoc per call site.
const (
	statusInfoDuration  = 2500 * time.Millisecond
	statusErrorDuration = 4 * time.Second
	statusLoadingMax    = 30 * time.Second
)

type worktreeCreatedMsg struct {
	err      error
	warnings []string
}

type worktreeRemovedMsg struct {
	err error
}

type model struct {
	worktrees     []Worktree
	filtered      []int
	cursor        int
	selected      int
	currentBranch string
	width         int
	height        int
	showInsights  bool
	showHelp      bool
	tick          time.Time

	mode      inputMode
	textInput textinput.Model
	statusMsg *statusMsg

	sortMode     sortMode
	statusFilter statusFilter

	deleteTarget int
	forceDelete  bool

	archived    map[string]bool
	showArchive bool
}

func Run(worktrees []Worktree) {
	for {
		currentBranch := git.CurrentBranch()

		ti := textinput.New()
		ti.CharLimit = 60

		if refreshed, err := List(); err == nil {
			worktrees = refreshed
		}

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

		finalModel, ok := m.(model)
		if !ok || finalModel.selected < 0 {
			return
		}

		claudePath, err := exec.LookPath("claude")
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error: claude not found in PATH\n")
			os.Exit(1)
		}
		wt := finalModel.worktrees[finalModel.selected]

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
				continue
			}
		}

		fmt.Fprintf(os.Stderr, "\n  %s\n\n", dimStyle.Render("claude "+strings.Join(baseArgs, " ")))
		cmd := exec.Command(claudePath, baseArgs...)
		cmd.Stdin = os.Stdin
		cmd.Stdout = os.Stdout
		cmd.Stderr = os.Stderr
		cmd.Run()
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
		if !m.showArchive && m.archived[wt.Branch] {
			continue
		}
		if searchActive {
			if !strings.Contains(strings.ToLower(wt.Branch), query) &&
				!strings.Contains(strings.ToLower(wt.RelativePath), query) {
				continue
			}
		}
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
		if m.statusMsg != nil && time.Now().After(m.statusMsg.expires) {
			m.statusMsg = nil
		}
		return m, tea.Tick(m.tickInterval(), func(t time.Time) tea.Msg {
			return t
		})

	case worktreeCreatedMsg:
		if msg.err != nil {
			m.statusMsg = &statusMsg{text: "create failed: " + msg.err.Error(), isError: true, expires: time.Now().Add(statusErrorDuration)}
		} else if len(msg.warnings) > 0 {
			m.statusMsg = &statusMsg{text: "created (warnings: " + strings.Join(msg.warnings, ", ") + ")", isError: true, expires: time.Now().Add(statusErrorDuration)}
			m.refreshWorktreeList()
		} else {
			m.statusMsg = &statusMsg{text: "worktree created", expires: time.Now().Add(statusInfoDuration)}
			m.refreshWorktreeList()
		}
		return m, nil

	case worktreeRemovedMsg:
		if msg.err != nil {
			m.statusMsg = &statusMsg{text: "remove failed: " + msg.err.Error(), isError: true, expires: time.Now().Add(statusErrorDuration)}
		} else {
			m.statusMsg = &statusMsg{text: "worktree removed", expires: time.Now().Add(statusInfoDuration)}
			m.refreshWorktreeList()
		}
		return m, nil

	case messageSentMsg:
		if msg.err != nil {
			m.statusMsg = &statusMsg{text: "launch failed: " + msg.err.Error(), isError: true, expires: time.Now().Add(statusErrorDuration)}
		} else {
			text := "agent launched"
			if msg.logPath != "" {
				text = "agent launched — log: " + msg.logPath
			}
			m.statusMsg = &statusMsg{text: text, expires: time.Now().Add(statusInfoDuration)}
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
		half := m.height / 4
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

	case "o":
		if wt := m.selectedWorktree(); wt != nil {
			if wt.IsMain {
				m.statusMsg = &statusMsg{text: "cannot open default branch (--tmux requires --worktree)", isError: true, expires: time.Now().Add(statusErrorDuration)}
				return m, nil
			}
			m.selected = m.filtered[m.cursor]
			return m, tea.Quit
		}

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
		m.textInput.Placeholder = "filter by branch or path…"
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
				m.statusMsg = &statusMsg{text: "cannot delete main worktree", isError: true, expires: time.Now().Add(statusErrorDuration)}
			} else if HasActiveSession(*wt) {
				m.statusMsg = &statusMsg{text: "worktree has active session — use D to force", isError: true, expires: time.Now().Add(statusErrorDuration)}
			} else {
				m.mode = modeConfirmDelete
				m.deleteTarget = m.cursor
				m.forceDelete = false
			}
		}
	case "D":
		if wt := m.selectedWorktree(); wt != nil {
			if wt.IsMain {
				m.statusMsg = &statusMsg{text: "cannot delete main worktree", isError: true, expires: time.Now().Add(statusErrorDuration)}
			} else {
				m.mode = modeConfirmDelete
				m.deleteTarget = m.cursor
				m.forceDelete = true
			}
		}

	case "y":
		if wt := m.selectedWorktree(); wt != nil {
			m.statusMsg = &statusMsg{text: "path yanked to clipboard", expires: time.Now().Add(statusInfoDuration)}
			return m, tea.SetClipboard(wt.Path)
		}

	case "s":
		m.sortMode = (m.sortMode + 1) % 4
		m.applyFilter()
	case "f":
		m.statusFilter = (m.statusFilter + 1) % 5
		m.applyFilter()

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
			m.statusMsg = &statusMsg{text: "no active worktrees", expires: time.Now().Add(statusInfoDuration)}
		}

	case "m":
		if m.selectedWorktree() != nil {
			m.mode = modeMessage
			m.textInput.Placeholder = "prompt for new claude agent…"
			m.textInput.CharLimit = 500
			m.textInput.SetValue("")
			return m, m.textInput.Focus()
		}

	case "x":
		if wt := m.selectedWorktree(); wt != nil {
			if wt.IsMain {
				m.statusMsg = &statusMsg{text: "cannot archive main worktree", isError: true, expires: time.Now().Add(statusErrorDuration)}
			} else if m.archived[wt.Branch] {
				delete(m.archived, wt.Branch)
				saveArchived(m.archived)
				m.statusMsg = &statusMsg{text: "unarchived " + wt.Branch, expires: time.Now().Add(statusInfoDuration)}
			} else {
				m.archived[wt.Branch] = true
				saveArchived(m.archived)
				m.statusMsg = &statusMsg{text: "archived " + wt.Branch, expires: time.Now().Add(statusInfoDuration)}
				m.applyFilter()
			}
		}
	case "X":
		m.showArchive = !m.showArchive
		m.applyFilter()
		if m.showArchive {
			m.statusMsg = &statusMsg{text: "showing archived worktrees", expires: time.Now().Add(statusInfoDuration)}
		} else {
			m.statusMsg = &statusMsg{text: "hiding archived worktrees", expires: time.Now().Add(statusInfoDuration)}
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
		m.statusMsg = &statusMsg{text: "creating worktree…", isLoading: true, expires: time.Now().Add(statusLoadingMax)}
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
			m.statusMsg = &statusMsg{text: "removing worktree…", isLoading: true, expires: time.Now().Add(statusLoadingMax)}
			return m, m.removeWorktreeCmd(wt.Path, m.forceDelete)
		}
		m.mode = modeNormal
	case "n", "N", "esc", "ctrl+c":
		m.mode = modeNormal
	}
	return m, nil
}

type messageSentMsg struct {
	err     error
	logPath string
}

func (m model) handleMessageKey(msg tea.KeyPressMsg, key string) (tea.Model, tea.Cmd) {
	switch key {
	case "ctrl+c":
		return m, tea.Quit
	case "esc":
		m.mode = modeNormal
		m.textInput.Blur()
		m.textInput.CharLimit = 60
		return m, nil
	case "enter":
		text := strings.TrimSpace(m.textInput.Value())
		if text == "" {
			m.mode = modeNormal
			m.textInput.Blur()
			m.textInput.CharLimit = 60
			return m, nil
		}
		wt := m.selectedWorktree()
		if wt == nil {
			m.mode = modeNormal
			m.textInput.Blur()
			m.textInput.CharLimit = 60
			return m, nil
		}
		m.mode = modeNormal
		m.textInput.Blur()
		m.textInput.CharLimit = 60
		m.statusMsg = &statusMsg{text: "launching agent…", isLoading: true, expires: time.Now().Add(statusLoadingMax)}
		return m, m.launchAgentCmd(*wt, text)
	}

	var cmd tea.Cmd
	m.textInput, cmd = m.textInput.Update(msg)
	return m, cmd
}

func (m model) launchAgentCmd(wt Worktree, text string) tea.Cmd {
	return func() tea.Msg {
		claudePath, err := exec.LookPath("claude")
		if err != nil {
			return messageSentMsg{err: fmt.Errorf("claude not found in PATH")}
		}

		var args []string
		if wt.Insights.SessionID != "" {
			args = append(args, "--resume", wt.Insights.SessionID)
		}
		args = append(args, "--dangerously-skip-permissions", "-p", text)

		cmd := exec.Command(claudePath, args...)
		cmd.Dir = wt.Path

		logPath := filepath.Join(os.TempDir(), fmt.Sprintf("mori-agent-%s-%d.log", filepath.Base(wt.Path), time.Now().Unix()))
		logFile, logErr := os.Create(logPath)
		if logErr == nil {
			fmt.Fprintf(logFile, "mori launch @ %s\ncwd: %s\ncmd: %s %s\n---\n",
				time.Now().Format(time.RFC3339), wt.Path, claudePath, strings.Join(args, " "))
			cmd.Stdout = logFile
			cmd.Stderr = logFile
		}
		devNull, err := os.OpenFile(os.DevNull, os.O_RDONLY, 0)
		if err == nil {
			cmd.Stdin = devNull
			defer devNull.Close()
		}
		cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}

		if err := cmd.Start(); err != nil {
			if logFile != nil {
				logFile.Close()
			}
			return messageSentMsg{err: err}
		}
		_ = cmd.Process.Release()
		return messageSentMsg{logPath: logPath}
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

	var v tea.View
	switch {
	case m.showHelp:
		v = tea.NewView(m.viewHelp(totalWidth))
	case m.showInsights && totalWidth >= sideByMinWidth:
		v = tea.NewView(m.viewSideBySide(totalWidth))
	case m.showInsights && totalWidth >= listOnlyMinWidth:
		v = tea.NewView(m.viewStacked(totalWidth))
	default:
		v = tea.NewView(m.viewListOnly(totalWidth))
	}
	v.AltScreen = true
	return v
}

// statusCounts returns (working, waiting, idle, none) over unfiltered worktrees.
func (m model) statusCounts() (w, q, i, n int) {
	for _, wt := range m.worktrees {
		switch wt.Insights.Status {
		case agent.StatusWorking:
			w++
		case agent.StatusWait:
			q++
		case agent.StatusIdle:
			i++
		default:
			n++
		}
	}
	return
}

func (m model) renderTopBar(width int) string {
	brand := titleStyle.Render("◆ MORI")
	branch := mutedStyle.Render("on ") + textStyle.Render(m.currentBranch)

	w, q, i, _ := m.statusCounts()
	var pills []string
	if w > 0 {
		pills = append(pills, workingStyle.Render(fmt.Sprintf("● %d working", w)))
	}
	if q > 0 {
		pills = append(pills, waitingStyle.Render(fmt.Sprintf("◐ %d waiting", q)))
	}
	if i > 0 {
		pills = append(pills, idleStyle.Render(fmt.Sprintf("○ %d idle", i)))
	}
	right := strings.Join(pills, mutedStyle.Render("  ·  "))

	left := brand + "  " + branch
	return padBetween(left, right, width-2)
}

// padBetween places `left` and `right` on the same line, separated to fill width.
func padBetween(left, right string, width int) string {
	lw := lipgloss.Width(left)
	rw := lipgloss.Width(right)
	gap := width - lw - rw
	if gap < 1 {
		gap = 1
	}
	return " " + left + strings.Repeat(" ", gap) + right + " "
}

// renderFrame draws a rounded-border frame around content with an optional inline title.
// The title sits on the top border like: ╭─ title ─────────╮
func renderFrame(content string, width int, title string) string {
	// width is the OUTER width (including 2 border cols)
	innerW := width - 2
	if innerW < 4 {
		innerW = 4
	}

	var top string
	if title != "" {
		leadDashes := 2
		titleText := " " + title + " "
		titleWidth := lipgloss.Width(titleText)
		tail := innerW - leadDashes - titleWidth
		if tail < 1 {
			tail = 1
		}
		top = borderStyle.Render("╭"+strings.Repeat("─", leadDashes)) +
			titleText +
			borderStyle.Render(strings.Repeat("─", tail)+"╮")
	} else {
		top = borderStyle.Render("╭" + strings.Repeat("─", innerW) + "╮")
	}
	bottom := borderStyle.Render("╰" + strings.Repeat("─", innerW) + "╯")

	// Pad each line to innerW and wrap with │ │
	lines := strings.Split(content, "\n")
	var out strings.Builder
	out.WriteString(top + "\n")
	for _, ln := range lines {
		pad := innerW - lipgloss.Width(ln)
		if pad < 0 {
			pad = 0
		}
		out.WriteString(borderStyle.Render("│") + ln + strings.Repeat(" ", pad) + borderStyle.Render("│") + "\n")
	}
	out.WriteString(bottom)
	return out.String()
}

func (m model) viewListOnly(width int) string {
	innerW := width - 2

	top := m.renderTopBar(width)
	list := m.renderWorktreeList(innerW)
	framed := renderFrame(list, width, "worktrees")

	footer := m.renderInputLine() + "\n" + m.renderFooter(width-2)

	return "\n" + top + "\n\n" + framed + "\n" + footer + "\n"
}

func (m model) viewStacked(width int) string {
	innerW := width - 2

	top := m.renderTopBar(width)
	list := m.renderWorktreeList(innerW)
	listFrame := renderFrame(list, width, "worktrees")

	insights := renderInsightsPanel(m, innerW-2)
	insightsFrame := renderFrame(insights, width, "agent insights")

	footer := m.renderInputLine() + "\n" + m.renderFooter(width-2)

	return "\n" + top + "\n\n" + listFrame + "\n" + insightsFrame + "\n" + footer + "\n"
}

func (m model) viewSideBySide(width int) string {
	// Each frame is outer width including 2 border cols. A 1-col gutter sits between them.
	rightW := insightsPaneWidth(width)
	leftW := width - rightW - 1

	leftContent := m.renderWorktreeList(leftW - 2)
	rightContent := renderInsightsPanel(m, rightW-2)

	// Equalize heights so both frames' bottom borders align.
	leftContent, rightContent = padToSameHeight(leftContent, rightContent)

	leftFrame := renderFrame(leftContent, leftW, "worktrees")
	rightFrame := renderFrame(rightContent, rightW, "agent insights")

	joined := lipgloss.JoinHorizontal(lipgloss.Top, leftFrame, " ", rightFrame)

	top := m.renderTopBar(width)
	footer := m.renderInputLine() + "\n" + m.renderFooter(width-2)

	return "\n" + top + "\n\n" + joined + "\n" + footer + "\n"
}

func padToSameHeight(a, b string) (string, string) {
	la := strings.Count(a, "\n")
	lb := strings.Count(b, "\n")
	if !strings.HasSuffix(a, "\n") {
		la++
	}
	if !strings.HasSuffix(b, "\n") {
		lb++
	}
	if la < lb {
		a += strings.Repeat("\n", lb-la)
	} else if lb < la {
		b += strings.Repeat("\n", la-lb)
	}
	return a, b
}

func (m model) viewHelp(width int) string {
	w := 60
	if width < w+4 {
		w = width - 4
	}

	var content strings.Builder

	bindings := []struct {
		section string
		keys    []struct{ key, desc string }
	}{
		{"navigation", []struct{ key, desc string }{
			{"j/k, ↑/↓", "move cursor"},
			{"g / G", "jump to first / last"},
			{"ctrl+d/u", "half-page down / up"},
			{"w", "jump to next working/waiting"},
			{"o", "open claude code in worktree"},
			{"enter", "toggle insights panel"},
			{"q, ctrl+c", "quit"},
		}},
		{"actions", []struct{ key, desc string }{
			{"n", "create new worktree"},
			{"d", "delete worktree"},
			{"D", "force delete"},
			{"y", "yank (copy) worktree path"},
			{"m", "launch agent with prompt (background)"},
			{"r", "refresh insights now"},
			{"?", "toggle this help"},
		}},
		{"search & sort", []struct{ key, desc string }{
			{"/", "search by branch/path"},
			{"s", "cycle sort"},
			{"f", "cycle status filter"},
			{"esc", "clear filter / cancel"},
		}},
		{"archive", []struct{ key, desc string }{
			{"x", "archive / unarchive"},
			{"X", "toggle show archived"},
		}},
	}

	for i, section := range bindings {
		if i > 0 {
			content.WriteString("\n")
		}
		content.WriteString(headingStyle.Render(section.section) + "\n")
		for _, b := range section.keys {
			keyCell := selectedStyle.Render(b.key)
			pad := 14 - lipgloss.Width(keyCell)
			if pad < 1 {
				pad = 1
			}
			content.WriteString("  " + keyCell + strings.Repeat(" ", pad) + mutedStyle.Render(b.desc) + "\n")
		}
	}

	framed := renderFrame(content.String(), w+2, "keybindings")
	return "\n" + m.renderTopBar(width) + "\n\n" + framed + "\n" + mutedStyle.Render(" [?] close  [q] quit") + "\n"
}

func (m model) renderInputLine() string {
	switch m.mode {
	case modeSearch:
		return " " + titleStyle.Render("/") + " " + m.textInput.View()
	case modeCreate:
		return " " + titleStyle.Render("new worktree ›") + " " + m.textInput.View()
	case modeMessage:
		return " " + waitingStyle.Render("agent prompt ›") + " " + m.textInput.View()
	case modeConfirmDelete:
		if m.deleteTarget < len(m.filtered) {
			wt := m.worktrees[m.filtered[m.deleteTarget]]
			return " " + errorStyle.Render("delete "+wt.Branch+"? ") + mutedStyle.Render("[y/N]")
		}
		return ""
	default:
		if m.statusMsg != nil && time.Now().Before(m.statusMsg.expires) {
			switch {
			case m.statusMsg.isLoading:
				return " " + mutedStyle.Render("⋯ "+m.statusMsg.text)
			case m.statusMsg.isError:
				return " " + errorStyle.Render("✗ "+m.statusMsg.text)
			default:
				return " " + successStyle.Render("✓ "+m.statusMsg.text)
			}
		}
		return ""
	}
}

func (m model) renderFooter(width int) string {
	if m.mode == modeSearch {
		return " " + mutedStyle.Render("[enter] apply  [esc] clear  [↑/↓] navigate")
	}
	if m.mode == modeMessage {
		return " " + mutedStyle.Render("[enter] launch  [esc] cancel")
	}

	insightsHint := "[enter] insights"
	if m.showInsights {
		insightsHint = "[enter] hide"
	}
	left := mutedStyle.Render("[?] help  [o] open  " + insightsHint + "  [q] quit")

	var indicators []string
	indicators = append(indicators, mutedStyle.Render("sort ")+textStyle.Render(m.sortMode.String()))
	if m.statusFilter != filterAll {
		indicators = append(indicators, mutedStyle.Render("filter ")+textStyle.Render(m.statusFilter.String()))
	}
	if m.showArchive {
		indicators = append(indicators, mutedStyle.Render("archived"))
	}
	right := strings.Join(indicators, dimStyle.Render("  ·  "))

	return padBetween(left, right, width)
}

// --- List rendering ---

func colWidths(width int) (branchW, activityW, statusW, contextW int) {
	activityW, statusW, contextW = 10, 12, 10
	if width > 100 {
		activityW = 12
	}
	branchW = width - 2 /*accent+space*/ - activityW - statusW - contextW - 3 /*gutters*/
	if branchW < 20 {
		branchW = 20
	}
	if branchW > 60 {
		branchW = 60
	}
	return
}

func (m model) renderWorktreeList(width int) string {
	var s strings.Builder

	branchW, activityW, statusW, contextW := colWidths(width)

	// Column headers (dim, lowercase)
	header := "  " + // accent column placeholder (2 cols)
		mutedStyle.Width(branchW).Render("branch") + " " +
		mutedStyle.Width(activityW).Render("activity") + " " +
		mutedStyle.Width(statusW).Render("status") + " " +
		mutedStyle.Width(contextW).Render("context")
	s.WriteString(header + "\n")
	s.WriteString(dimStyle.Render(strings.Repeat("─", width)) + "\n")

	for i, wtIdx := range m.filtered {
		s.WriteString(renderWorktreeRow(m, i, wtIdx, width, branchW, activityW, statusW, contextW) + "\n")
	}

	if len(m.filtered) == 0 {
		s.WriteString("\n  " + mutedStyle.Render("no matching worktrees") + "\n")
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
		return idleStyle
	default:
		return noneStyle
	}
}

// statusColor returns the bare color associated with a status, for accent bars.
func statusColor(status agent.StatusType) color.Color {
	switch status {
	case agent.StatusWorking:
		return colWarn
	case agent.StatusWait:
		return colInfo
	case agent.StatusIdle:
		return colSuccess
	default:
		return colFaint
	}
}

func renderWorktreeRow(m model, cursorIdx, wtIdx, rowW, branchW, activityW, statusW, contextW int) string {
	wt := m.worktrees[wtIdx]
	selected := m.cursor == cursorIdx

	trunc := func(s string, w int) string {
		if lipgloss.Width(s) > w {
			if w <= 1 {
				return s[:w]
			}
			return s[:w-1] + "…"
		}
		return s
	}

	// Branch label with prefix markers.
	branchLabel := wt.Branch
	if wt.IsMain {
		branchLabel = "★ " + branchLabel
	}
	if m.archived[wt.Branch] {
		branchLabel = "◌ " + branchLabel
	}
	branchLabel = trunc(branchLabel, branchW)

	activity := "—"
	if !wt.Insights.LastActivity.IsZero() {
		activity = relativeTime(wt.Insights.LastActivity)
	}

	statusText := statusIcon(wt.Insights.Status) + " " + strings.ToLower(string(wt.Insights.Status))
	contextText := renderInlineContextRaw(wt.Insights)

	// Each span below sets both fg AND bg (when selected) so terminal resets
	// between spans don't leave un-highlighted gaps.
	bg := func(st lipgloss.Style) lipgloss.Style {
		if selected {
			return st.Background(colRowBg)
		}
		return st
	}
	sep := bg(lipgloss.NewStyle()).Render(" ")

	// Accent bar — accent color on selected row; status color on active rows.
	var accentBar string
	switch {
	case selected:
		accentBar = bg(lipgloss.NewStyle().Foreground(colAccent)).Render("▌")
	case wt.Insights.Status == agent.StatusWorking || wt.Insights.Status == agent.StatusWait:
		accentBar = lipgloss.NewStyle().Foreground(statusColor(wt.Insights.Status)).Render("▏")
	default:
		accentBar = " "
	}

	var branchStyle, activityStyle, statusCellStyle, contextCellStyle lipgloss.Style
	if selected {
		branchStyle = lipgloss.NewStyle().Foreground(colText).Bold(true).Width(branchW)
		activityStyle = lipgloss.NewStyle().Foreground(colMuted).Width(activityW)
		statusCellStyle = statusStyle(wt.Insights.Status).Width(statusW)
		contextCellStyle = contextFgStyle(wt.Insights).Width(contextW)
	} else {
		branchStyle = lipgloss.NewStyle().Foreground(colText).Width(branchW)
		activityStyle = lipgloss.NewStyle().Foreground(colDim).Width(activityW)
		statusCellStyle = statusStyle(wt.Insights.Status).Width(statusW)
		contextCellStyle = contextFgStyle(wt.Insights).Width(contextW)
	}

	branchCell := bg(branchStyle).Render(branchLabel)
	activityCell := bg(activityStyle).Render(activity)
	statusCell := bg(statusCellStyle).Render(statusText)
	contextCell := bg(contextCellStyle).Render(contextText)

	row := accentBar + sep + branchCell + sep + activityCell + sep + statusCell + sep + contextCell

	// Pad any trailing width so the highlight fills to the right edge.
	if selected {
		cur := lipgloss.Width(row)
		if cur < rowW {
			row += bg(lipgloss.NewStyle()).Render(strings.Repeat(" ", rowW-cur))
		}
	}
	return row
}

// renderInlineContextRaw returns the context percent text without styling;
// caller applies the style (so bg can be attached uniformly).
func renderInlineContextRaw(ins agent.Insights) string {
	if ins.InputTokens <= 0 {
		return "—"
	}
	maxTokens := contextMaxTokens(ins.Model)
	percent := float64(ins.InputTokens) / float64(maxTokens)
	if percent > 1 {
		percent = 1
	}
	return fmt.Sprintf("%d%%", int(percent*100))
}

func contextFgStyle(ins agent.Insights) lipgloss.Style {
	if ins.InputTokens <= 0 {
		return mutedStyle
	}
	maxTokens := contextMaxTokens(ins.Model)
	percent := float64(ins.InputTokens) / float64(maxTokens)
	switch {
	case percent >= 0.8:
		return barHighStyle
	case percent >= 0.5:
		return barMedStyle
	default:
		return barLowStyle
	}
}

// padCell right-pads a styled string to the given visual width.
func padCell(s string, w int) string {
	cur := lipgloss.Width(s)
	if cur >= w {
		return s
	}
	return s + strings.Repeat(" ", w-cur)
}

// --- Insights panel (card layout) ---

func renderInsightsPanel(m model, width int) string {
	var s strings.Builder

	wt := m.selectedWorktree()
	if wt == nil {
		return "\n  " + mutedStyle.Render("no worktree selected") + "\n"
	}

	// Header card: status pill + model/cost on the right.
	pill := renderStatusPill(wt.Insights.Status)
	var rightParts []string
	if wt.Insights.Model != "" {
		rightParts = append(rightParts, textStyle.Render(agent.ModelTier(wt.Insights.Model)))
	}
	if wt.Insights.Mode != "" && wt.Insights.Mode != "default" {
		rightParts = append(rightParts, mutedStyle.Render(wt.Insights.Mode))
	}
	if wt.Insights.CostUSD > 0 {
		rightParts = append(rightParts, successStyle.Render(fmt.Sprintf("$%.2f", wt.Insights.CostUSD)))
	}
	right := strings.Join(rightParts, mutedStyle.Render(" · "))

	s.WriteString("\n")
	s.WriteString(padBetween(pill, right, width) + "\n")

	// Status detail line — last tool or error badge or relative time.
	var detail string
	if wt.Insights.HasError {
		detail = errorStyle.Render("⚠ last tool errored")
	} else if wt.Insights.LastTool != "" && wt.Insights.Status == agent.StatusWorking {
		detail = mutedStyle.Render("running ") + textStyle.Render(wt.Insights.LastTool)
	} else if wt.Insights.Status == agent.StatusIdle && !wt.Insights.LastActivity.IsZero() {
		detail = mutedStyle.Render("last active " + relativeTime(wt.Insights.LastActivity))
	}
	if detail != "" {
		s.WriteString(" " + detail + "\n")
	}

	// Context bar (full panel width).
	s.WriteString("\n")
	s.WriteString(renderContextRow(wt.Insights, width-2) + "\n")

	// Key/value section.
	s.WriteString("\n")
	s.WriteString(kvRow(" session", insightsSessionLabel(wt.Insights), width) + "\n")
	if wt.Insights.AheadBehind != "" {
		s.WriteString(kvRow(" branch", textStyle.Render(wt.Insights.AheadBehind), width) + "\n")
	}
	if wt.Insights.Status == agent.StatusWorking && (wt.Insights.TurnDurationS > 0 || wt.Insights.MessageCount > 0) {
		turn := fmt.Sprintf("%ds · %d msgs", wt.Insights.TurnDurationS, wt.Insights.MessageCount)
		s.WriteString(kvRow(" turn", textStyle.Render(turn), width) + "\n")
	}

	// Task section.
	task := wt.Insights.CurrentTask
	if task == "" {
		task = "—"
	}
	s.WriteString("\n")
	s.WriteString(" " + headingStyle.Render("task") + "\n")
	for _, line := range wrapText(task, width-3) {
		s.WriteString("   " + textStyle.Render(line) + "\n")
	}

	// Git log section.
	if len(wt.Insights.GitLog) > 0 {
		s.WriteString("\n")
		s.WriteString(" " + headingStyle.Render("recent commits") + "\n")
		for _, entry := range wt.Insights.GitLog {
			line := entry
			if lipgloss.Width(line) > width-5 {
				line = line[:width-6] + "…"
			}
			s.WriteString("   " + mutedStyle.Render("•") + " " + textStyle.Render(line) + "\n")
		}
	}

	return s.String()
}

func insightsSessionLabel(ins agent.Insights) string {
	if ins.Slug != "" {
		return textStyle.Render(ins.Slug)
	}
	if ins.SessionID != "" {
		s := ins.SessionID
		if len(s) > 8 {
			s = s[:8]
		}
		return mutedStyle.Render(s)
	}
	return mutedStyle.Render("—")
}

// renderStatusPill returns a bold, colored badge with a leading glyph.
func renderStatusPill(status agent.StatusType) string {
	glyph := statusIcon(status)
	text := strings.ToLower(string(status))
	style := statusStyle(status).Padding(0, 1)
	return style.Render(glyph + " " + text)
}

// kvRow formats a two-column label/value row, right-aligned value.
func kvRow(label, value string, width int) string {
	labelCell := mutedStyle.Render(label)
	lw := lipgloss.Width(labelCell)
	vw := lipgloss.Width(value)
	gap := width - lw - vw - 1
	if gap < 1 {
		gap = 1
	}
	return labelCell + strings.Repeat(" ", gap) + value + " "
}

// renderContextRow renders a smooth (8ths-precision) progress bar with label.
func renderContextRow(ins agent.Insights, width int) string {
	var percent float64
	var label string
	if ins.InputTokens > 0 {
		maxTokens := contextMaxTokens(ins.Model)
		percent = float64(ins.InputTokens) / float64(maxTokens)
		tokenK := ins.InputTokens / 1000
		label = fmt.Sprintf("%d%% · %dk/%dk", int(percent*100), tokenK, maxTokens/1000)
	} else {
		const maxSize int64 = 10 * 1024 * 1024
		if maxSize > 0 {
			percent = float64(ins.SessionSize) / float64(maxSize)
		}
		label = fmt.Sprintf("%d%%", int(percent*100))
	}

	labelW := lipgloss.Width(label)
	barW := width - labelW - 3 // " " + bar + " " + label
	if barW < 8 {
		barW = 8
	}
	bar := renderSmoothBar(percent, barW)
	return " " + bar + " " + mutedStyle.Render(label)
}

// renderSmoothBar renders a horizontal progress bar with 8x sub-cell precision
// using the Unicode block-element partial-fill characters.
func renderSmoothBar(percent float64, width int) string {
	if percent < 0 {
		percent = 0
	}
	if percent > 1 {
		percent = 1
	}

	partials := []string{"", "▏", "▎", "▍", "▌", "▋", "▊", "▉"}
	totalEighths := int(percent * float64(width) * 8)
	fullCells := totalEighths / 8
	remainder := totalEighths % 8
	if fullCells > width {
		fullCells = width
		remainder = 0
	}

	var fg color.Color
	switch {
	case percent >= 0.8:
		fg = colDanger
	case percent >= 0.5:
		fg = colWarn
	default:
		fg = colSuccess
	}
	filledStyle := lipgloss.NewStyle().Foreground(fg)
	trackStyle := lipgloss.NewStyle().Foreground(colFaint)

	var b strings.Builder
	b.WriteString(filledStyle.Render(strings.Repeat("█", fullCells)))
	remain := width - fullCells
	if remainder > 0 && remain > 0 {
		b.WriteString(filledStyle.Render(partials[remainder]))
		remain--
	}
	if remain > 0 {
		b.WriteString(trackStyle.Render(strings.Repeat("─", remain)))
	}
	return b.String()
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
	if lipgloss.Width(text) <= width {
		return []string{text}
	}

	var lines []string
	words := strings.Fields(text)
	currentLine := ""

	for _, word := range words {
		projected := lipgloss.Width(word)
		if currentLine != "" {
			projected += lipgloss.Width(currentLine) + 1
		}
		if projected <= width {
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
