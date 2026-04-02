package main

import (
	"bufio"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// --- Styles ---
var (
	headerStyle  = lipgloss.NewStyle().Foreground(lipgloss.Color("205")).Bold(true).MarginBottom(1)
	activeStyle  = lipgloss.NewStyle().Foreground(lipgloss.Color("42")).Bold(true)
	noneStyle    = lipgloss.NewStyle().Foreground(lipgloss.Color("240"))
	cursorStyle  = lipgloss.NewStyle().Foreground(lipgloss.Color("212")).Bold(true)
	footerStyle  = lipgloss.NewStyle().Foreground(lipgloss.Color("243")).MarginTop(1)
	mainStyle    = lipgloss.NewStyle().Foreground(lipgloss.Color("75")).Background(lipgloss.Color("235"))
	dirtyStyle   = lipgloss.NewStyle().Foreground(lipgloss.Color("214"))
	cleanStyle   = lipgloss.NewStyle().Foreground(lipgloss.Color("76"))
	colWidthPath = 45
	colWidthBran = 28
)

// --- Types ---
type Worktree struct {
	Path          string
	Branch        string
	ClaudeSession bool
	IsMain        bool
	IsDirty       bool
}

type model struct {
	worktrees []Worktree
	cursor    int
	selected  string
}

// --- Logic: Git & Claude Detection ---
func getWorktrees() []Worktree {
	out, _ := exec.Command("git", "worktree", "list", "--porcelain").Output()
	scanner := bufio.NewScanner(strings.NewReader(string(out)))

	var wts []Worktree
	var current Worktree
	var mainPath string

	gitDir, _ := exec.Command("git", "rev-parse", "--git-dir").Output()
	mainPath = filepath.Dir(strings.TrimSpace(string(gitDir)))

	for scanner.Scan() {
		line := scanner.Text()
		if strings.HasPrefix(line, "worktree ") {
			if current.Path != "" {
				wts = append(wts, current)
			}
			current = Worktree{Path: strings.TrimPrefix(line, "worktree ")}
		} else if strings.HasPrefix(line, "branch ") {
			parts := strings.Split(line, "/")
			current.Branch = parts[len(parts)-1]
			current.ClaudeSession = checkClaudeSession(current.Branch)
		} else if strings.HasPrefix(line, "detached") {
			current.Branch = "(detached)"
		}
	}
	if current.Path != "" {
		wts = append(wts, current)
	}

	for i := range wts {
		wts[i].IsMain = wts[i].Path == mainPath
		wts[i].IsDirty = checkDirty(wts[i].Path)
	}

	return wts
}

func checkDirty(path string) bool {
	out, err := exec.Command("git", "status", "--porcelain").Output()
	if err != nil {
		return false
	}
	return len(strings.TrimSpace(string(out))) > 0
}

func checkClaudeSession(branch string) bool {
	// Claude Code typically stores sessions in ~/.claude/sessions
	home, _ := os.UserHomeDir()
	sessionPath := filepath.Join(home, ".claude", "sessions", branch)
	_, err := os.Stat(sessionPath)
	return err == nil
}

// --- Bubble Tea Implementation ---
func (m model) Init() tea.Cmd { return nil }

func (m model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
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
			m.selected = m.worktrees[m.cursor].Path
			return m, tea.Quit
		}
	}
	return m, nil
}

func (m model) View() string {
	s := headerStyle.Render("Worktree Manager") + "\n\n"

	for i, wt := range m.worktrees {
		cursor := "  "
		if m.cursor == i {
			cursor = cursorStyle.Render(">")
		}

		displayPath := shortenPath(wt.Path)
		if wt.IsMain {
			displayPath = mainStyle.Render(displayPath + " ←")
		}

		status := ""
		if wt.IsDirty {
			status = dirtyStyle.Render("●")
		} else {
			status = cleanStyle.Render("○")
		}

		session := ""
		if wt.ClaudeSession {
			session = " " + activeStyle.Render("claude")
		}

		branchDisplay := wt.Branch
		if wt.Branch == "" {
			branchDisplay = noneStyle.Render("—")
		}

		row := fmt.Sprintf("%s %s %s %-25s%s\n", cursor, status, displayPath, branchDisplay, session)
		if m.cursor == i {
			s += cursorStyle.Background(lipgloss.Color("235")).Padding(0).Render(" " + strings.TrimLeft(row, " "))
		} else {
			s += row
		}
	}

	s += footerStyle.Render("\n[↑/↓] Navigate • [Enter] Select • [q] Quit • ● dirty ○ clean")
	return s
}

func shortenPath(path string) string {
	home, _ := os.UserHomeDir()
	if strings.HasPrefix(path, home) {
		path = "~" + path[len(home):]
	}

	if len(path) > colWidthPath-3 {
		return "..." + path[len(path)-colWidthPath+3:]
	}
	return path
}

func main() {
	p := tea.NewProgram(model{worktrees: getWorktrees()})
	m, err := p.Run()
	if err != nil {
		fmt.Printf("Error: %v", err)
		os.Exit(1)
	}

	// Output the selected path so the shell wrapper can catch it
	if finalModel, ok := m.(model); ok && finalModel.selected != "" {
		// Use stderr for UI and stdout for the path to avoid pollution
		fmt.Print(finalModel.selected)
	}
}
