package tui

import (
	"fmt"
	"os"
	"os/exec"
	"strings"
	"time"

	"github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/moosecode/mori/agent"
)

const (
	insightsWidth = 40
	tickInterval  = 5 * time.Second
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
	borderStyle   = lipgloss.NewStyle().Foreground(lipgloss.Color("240"))
)

type model struct {
	worktrees     []Worktree
	cursor        int
	selected      string
	currentBranch string
	width         int
	height        int
	showInsights  bool
	tick          time.Time
}

func Run(worktrees []Worktree) {
	currentBranch := ""
	if out, err := exec.Command("git", "branch", "--show-current").Output(); err == nil {
		currentBranch = strings.TrimSpace(string(out))
	}

	p := tea.NewProgram(model{
		worktrees:     worktrees,
		currentBranch: currentBranch,
		showInsights:  false,
		tick:          time.Now(),
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

func (m model) Init() tea.Cmd {
	return tea.Tick(tickInterval, func(t time.Time) tea.Msg {
		return t
	})
}

func (m model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
	case time.Time:
		m.tick = msg
		if m.cursor < len(m.worktrees) && m.worktrees[m.cursor].Insights.Available {
			m.worktrees[m.cursor].Insights = agent.GetInsights(m.worktrees[m.cursor].Path)
		}
		return m, tea.Tick(tickInterval, func(t time.Time) tea.Msg {
			return t
		})
	case tea.KeyMsg:
		switch msg.String() {
		case "ctrl+c", "q":
			return m, tea.Quit
		case "up", "k":
			if m.cursor > 0 {
				m.cursor--
			}
		case "down", "j":
			if m.cursor < len(m.worktrees)-1 {
				m.cursor++
			}
		case "enter":
			if len(m.worktrees) > 0 {
				m.selected = m.worktrees[m.cursor].Path
			}
			return m, tea.Quit
		case "i":
			m.showInsights = !m.showInsights
		}
	}
	return m, nil
}

func (m model) View() string {
	totalWidth := m.width
	if totalWidth == 0 {
		totalWidth = 140
	}

	if m.showInsights && totalWidth > 100 {
		return m.viewWithSplit(totalWidth)
	}
	return m.viewListOnly(totalWidth)
}

func (m model) viewListOnly(width int) string {
	var s strings.Builder

	s.WriteString("\n")
	s.WriteString(" " + headerStyle.Render("MORI") + dimStyle.Render(" | Current: ") + selectedStyle.Render(m.currentBranch) + "\n")
	s.WriteString("\n")
	s.WriteString(renderWorktreeList(m, width) + "\n")
	s.WriteString(footerStyle.Render("[i] Insights  [Enter] Switch  [q] Quit") + "\n")

	return s.String()
}

func (m model) viewWithSplit(width int) string {
	var s strings.Builder

	s.WriteString("\n")
	s.WriteString(" " + headerStyle.Render("MORI") + dimStyle.Render(" | Current: ") + selectedStyle.Render(m.currentBranch) + "\n")
	s.WriteString("\n")

	s.WriteString(renderWorktreeList(m, width) + "\n")
	s.WriteString("\n")
	s.WriteString(renderInsightsPanel(m) + "\n")

	s.WriteString(footerStyle.Render("[i] Hide Insights  [Enter] Switch  [q] Quit") + "\n")

	return s.String()
}

func colWidths(width int) (int, int, int) {
	pathW := 30
	branchW := 25
	sessionW := 10
	if width > 100 {
		pathW = 40
		branchW = 30
	}
	return pathW, branchW, sessionW
}

func renderWorktreeList(m model, width int) string {
	var s strings.Builder

	pathW, branchW, sessionW := colWidths(width)
	tableW := 4 + pathW + branchW + sessionW

	s.WriteString(dimStyle.Render(strings.Repeat("─", tableW)) + "\n")
	s.WriteString(lipgloss.JoinHorizontal(0,
		dimStyle.Width(4).Render(""),
		boldStyle.Width(pathW).Render("PATH"),
		boldStyle.Width(branchW).Render("BRANCH"),
		boldStyle.Width(sessionW).Render("SESSION"),
	) + "\n")
	s.WriteString(dimStyle.Render(strings.Repeat("─", tableW)) + "\n")

	for i := range m.worktrees {
		s.WriteString(renderWorktreeRow(m, i, width) + "\n")
	}

	return s.String()
}

func renderWorktreeListWithHeader(m model, width int) string {
	var s strings.Builder

	pathW, branchW, sessionW := colWidths(width)
	tableW := 4 + pathW + branchW + sessionW

	s.WriteString(dimStyle.Render(strings.Repeat("─", tableW)) + "\n")
	s.WriteString(lipgloss.JoinHorizontal(0,
		dimStyle.Width(4).Render(""),
		boldStyle.Width(pathW).Render("PATH"),
		boldStyle.Width(branchW).Render("BRANCH"),
		boldStyle.Width(sessionW).Render("SESSION"),
	) + "\n")
	s.WriteString(dimStyle.Render(strings.Repeat("─", tableW)) + "\n")

	return s.String()
}

func renderWorktreeRow(m model, idx int, width int) string {
	wt := m.worktrees[idx]
	pathW, branchW, sessionW := colWidths(width)

	trunc := func(s string, w int) string {
		if len(s) > w-3 {
			return s[:w-3] + "..."
		}
		return s
	}

	checkbox := "[ ] "
	pathVal := trunc(wt.RelativePath, pathW)
	branchVal := trunc(wt.Branch, branchW)
	sessionVal := "[NONE]"
	if wt.ClaudeSession {
		sessionVal = "[ACTIVE]"
	} else if wt.ClaudeStale {
		sessionVal = "[STALE]"
	}

	if m.cursor == idx {
		checkbox = selectedStyle.Render("[*] ")
		pathVal = selectedStyle.Width(pathW).Render(pathVal)
		branchVal = selectedStyle.Width(branchW).Render(branchVal)
		sessionVal = selectedStyle.Width(sessionW).Render(sessionVal)
	} else {
		checkbox = dimStyle.Render(checkbox)
		pathVal = dimStyle.Width(pathW).Render(pathVal)
		branchVal = dimStyle.Width(branchW).Render(branchVal)
		if wt.ClaudeSession {
			sessionVal = activeStyle.Width(sessionW).Render(sessionVal)
		} else if wt.ClaudeStale {
			sessionVal = workingStyle.Width(sessionW).Render(sessionVal)
		} else {
			sessionVal = noneStyle.Width(sessionW).Render(sessionVal)
		}
	}

	return lipgloss.JoinHorizontal(0, checkbox, pathVal, branchVal, sessionVal)
}

func renderInsightsPanel(m model) string {
	width := 60
	var s strings.Builder

	s.WriteString(boldStyle.Render(" AGENT INSIGHTS") + "\n")
	s.WriteString(dimStyle.Render(strings.Repeat("─", width)) + "\n")

	if m.cursor >= len(m.worktrees) {
		s.WriteString(dimStyle.Render("No worktree selected"))
		return s.String()
	}

	wt := m.worktrees[m.cursor]

	statusStr := string(wt.Insights.Status)
	statusStyle := noneStyle
	switch wt.Insights.Status {
	case agent.StatusWorking:
		statusStyle = workingStyle
	case agent.StatusWait:
		statusStyle = waitingStyle
	case agent.StatusIdle:
		statusStyle = activeStyle
	}

	s.WriteString(fmt.Sprintf("%-12s %s\n", dimStyle.Render("STATUS:"), statusStyle.Render("["+statusStr+"]")))

	sessionID := wt.Insights.SessionID
	if sessionID == "" {
		sessionID = "—"
	} else if len(sessionID) > 8 {
		sessionID = sessionID[:8]
	}
	s.WriteString(fmt.Sprintf("%-12s %s\n", dimStyle.Render("SESSION:"), dimStyle.Render(sessionID)))

	costStr := "—"
	if wt.Insights.CostUSD > 0 {
		costStr = fmt.Sprintf("$%.2f", wt.Insights.CostUSD)
	}
	s.WriteString(fmt.Sprintf("%-12s %s\n", dimStyle.Render("COST:"), dimStyle.Render(costStr)))

	contextBar := renderContextBar(wt.Insights.SessionSize)
	s.WriteString(fmt.Sprintf("%-12s %s\n", dimStyle.Render("CONTEXT:"), contextBar))

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

	return dimStyle.Render("[") + activeStyle.Render(bar) + dimStyle.Render("] ") + dimStyle.Render(percentStr)
}
