package tui

import (
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/trebaud/mori/internal"
	"github.com/trebaud/mori/internal/github"
	"github.com/trebaud/mori/internal/insights"
)

// Update is the Elm update function — it takes a message and returns the
// next model plus any effects to run.
func (m model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		m.adjustScroll()
		return m, nil

	case time.Time:
		m.refreshInsights()
		m.applyFilter()
		if m.hasActiveAgent() {
			m.animFrame++
		}
		if m.statusMsg != nil && time.Now().After(m.statusMsg.expires) {
			m.statusMsg = nil
		}
		return m, tea.Tick(m.tickInterval(), func(t time.Time) tea.Msg {
			return t
		})

	case stepStartedMsg:
		for i := range m.creatingSteps {
			if m.creatingSteps[i].name == msg.name && m.creatingSteps[i].state == stepPending {
				m.creatingSteps[i].state = stepRunning
				break
			}
		}
		return m, waitStepCmd(m.creatingChan)

	case stepCompletedMsg:
		for i := range m.creatingSteps {
			if m.creatingSteps[i].name == msg.name && m.creatingSteps[i].state == stepRunning {
				if msg.success {
					m.creatingSteps[i].state = stepSucceeded
				} else {
					m.creatingSteps[i].state = stepFailed
				}
				break
			}
		}
		return m, waitStepCmd(m.creatingChan)

	case spinnerTickMsg:
		if m.mode == modeCreating {
			m.animFrame++
			return m, spinnerTickCmd()
		}
		return m, nil

	case worktreeCreatedMsg:
		m.mode = modeNormal
		m.creatingChan = nil
		m.creatingSteps = nil
		m.creatingBranch = ""
		if msg.err != nil {
			m.statusMsg = &statusMsg{text: "create failed: " + msg.err.Error(), isError: true, expires: time.Now().Add(statusErrorDuration)}
		} else if len(msg.warnings) > 0 {
			m.statusMsg = &statusMsg{text: "created (warnings: " + strings.Join(msg.warnings, ", ") + ")", isError: true, expires: time.Now().Add(statusErrorDuration)}
			m.refreshWorktreeList()
		} else {
			m.statusMsg = &statusMsg{text: "worktree created", expires: time.Now().Add(statusInfoDuration)}
			m.refreshWorktreeList()
		}
		return m, fetchAllPRsCmd(m.worktrees)

	case worktreeRemovedMsg:
		if msg.err != nil {
			m.statusMsg = &statusMsg{text: "remove failed: " + msg.err.Error(), isError: true, expires: time.Now().Add(statusErrorDuration)}
		} else {
			m.statusMsg = &statusMsg{text: "worktree removed", expires: time.Now().Add(statusInfoDuration)}
			m.refreshWorktreeList()
		}
		return m, fetchAllPRsCmd(m.worktrees)

	case prFetchedMsg:
		for i := range m.worktrees {
			if m.worktrees[i].Branch == msg.branch {
				m.worktrees[i].PR = msg.info
			}
		}
		return m, nil

	case tea.KeyPressMsg:
		return m.handleKey(msg)
	}
	return m, nil
}

// --- Key dispatch ---

func (m model) handleKey(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	key := msg.String()

	switch m.mode {
	case modeSearch:
		return m.handleSearchKey(msg, key)
	case modeCreate:
		return m.handleCreateKey(msg, key)
	case modeCreating:
		return m.handleCreatingKey(key)
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

	case "esc":
		// Back out of any overlay/filter state in priority order so a single
		// Esc always lands the user closer to the canonical list view.
		switch {
		case m.showHelp:
			m.showHelp = false
		case m.showInsights:
			m.showInsights = false
		case m.statusFilter != filterAll:
			m.statusFilter = filterAll
			m.applyFilter()
		case m.showArchive:
			m.showArchive = false
			m.applyFilter()
		}
		return m, nil

	case "up", "k":
		if m.cursor > 0 {
			m.cursor--
			m.insightsScrollOffset = 0
		}
		m.adjustScroll()
	case "down", "j":
		if m.cursor < len(m.filtered)-1 {
			m.cursor++
			m.insightsScrollOffset = 0
		}
		m.adjustScroll()

	case "[":
		if m.showInsights && m.insightsScrollOffset > 0 {
			m.insightsScrollOffset--
		}
	case "]":
		if m.showInsights {
			m.insightsScrollOffset++
		}
	case "1", "2", "3", "4", "5":
		if m.showInsights {
			m.insightsTab = int(key[0] - '1')
			m.insightsScrollOffset = 0
		}
	case "g":
		m.cursor = 0
		m.adjustScroll()
	case "G":
		if len(m.filtered) > 0 {
			m.cursor = len(m.filtered) - 1
		}
		m.adjustScroll()
	case "ctrl+d":
		half := m.height / 4
		if half < 1 {
			half = 5
		}
		m.cursor += half
		if m.cursor >= len(m.filtered) {
			m.cursor = max(0, len(m.filtered)-1)
		}
		m.adjustScroll()
	case "ctrl+u":
		half := m.height / 4
		if half < 1 {
			half = 5
		}
		m.cursor -= half
		if m.cursor < 0 {
			m.cursor = 0
		}
		m.adjustScroll()

	case "o", "enter":
		if wt := m.selectedWorktree(); wt != nil {
			if wt.IsMain {
				m.statusMsg = errorStatus("cannot open default branch (--tmux requires --worktree)")
				return m, nil
			}
			// Quit the TUI either way; main.go decides between LaunchClaude
			// (fresh interactive) and AttachBg (attach to live --bg session)
			// based on whether a session exists for this worktree.
			m.selected = m.filtered[m.cursor]
			return m, tea.Quit
		}

	case "tab", "i":
		m.showInsights = !m.showInsights
		m.insightsScrollOffset = 0

	case "r":
		m.refreshInsights()
		m.applyFilter()
		return m, tea.Tick(m.tickInterval(), func(t time.Time) tea.Msg {
			return t
		})
	case "p":
		// When insights is open and the current worktree has a PR, p opens
		// it in the browser instead of triggering a full PR refresh.
		if m.showInsights {
			if wt := m.selectedWorktree(); wt != nil && wt.PR != nil && wt.PR.Number > 0 {
				m.statusMsg = infoStatus("opening PR…")
				return m, openPRInBrowserCmd(wt.PR.Number)
			}
		}
		if !github.IsAvailable() {
			m.statusMsg = errorStatus("gh not found in PATH")
			return m, nil
		}
		github.InvalidateAll()
		m.statusMsg = loadingStatus("refreshing PRs…")
		return m, fetchAllPRsCmd(m.worktrees)
	case "c":
		if m.showInsights {
			if wt := m.selectedWorktree(); wt != nil && wt.Insights.LastPrompt != "" {
				m.statusMsg = infoStatus("prompt yanked to clipboard")
				return m, tea.SetClipboard(wt.Insights.LastPrompt)
			}
		}
	case "l":
		if m.showInsights {
			if wt := m.selectedWorktree(); wt != nil && wt.Insights.LogPath != "" {
				m.statusMsg = infoStatus("log path yanked to clipboard")
				return m, tea.SetClipboard(wt.Insights.LogPath)
			}
		}
	case "?":
		m.showHelp = !m.showHelp
	case "/":
		m.mode = modeSearch
		m.textInput.Placeholder = "filter by branch or path…"
		m.textInput.SetValue("")
		return m, m.textInput.Focus()
	case "n":
		m.mode = modeCreate
		m.textInput.Placeholder = ""
		m.textInput.SetValue("")
		return m, m.textInput.Focus()
	case "D":
		if wt := m.selectedWorktree(); wt != nil {
			if wt.IsMain {
				m.statusMsg = errorStatus("cannot delete main worktree")
			} else {
				m.mode = modeConfirmDelete
				m.deleteTarget = m.cursor
				m.forceDelete = true
			}
		}

	case "y":
		if wt := m.selectedWorktree(); wt != nil {
			m.statusMsg = infoStatus("path yanked to clipboard")
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
				if wt.Insights.Status == insights.StatusWorking || wt.Insights.Status == insights.StatusWait {
					m.cursor = idx
					m.adjustScroll()
					return m, nil
				}
			}
			m.statusMsg = infoStatus("no active worktrees")
		}

	case "x":
		if wt := m.selectedWorktree(); wt != nil {
			switch {
			case wt.IsMain:
				m.statusMsg = errorStatus("cannot archive main worktree")
			case m.archived[wt.Branch]:
				delete(m.archived, wt.Branch)
				saveArchived(m.archived)
				m.statusMsg = infoStatus("unarchived " + wt.Branch)
			default:
				m.archived[wt.Branch] = true
				saveArchived(m.archived)
				m.statusMsg = infoStatus("archived " + wt.Branch)
				m.applyFilter()
			}
		}
	case "X":
		m.showArchive = !m.showArchive
		m.applyFilter()
		if m.showArchive {
			m.statusMsg = infoStatus("showing archived worktrees")
		} else {
			m.statusMsg = infoStatus("hiding archived worktrees")
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
		m.adjustScroll()
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
		if branch == "" {
			branch = "wt-" + internal.RandomSuffix()
		}
		m.textInput.Blur()
		m.mode = modeCreating
		m.creatingBranch = branch
		m.creatingSteps = planCreateSteps(findRepoRoot(), branch)
		ch, stepCmd := startCreateWorktreeCmd(branch)
		m.creatingChan = ch
		return m, tea.Batch(stepCmd, spinnerTickCmd())
	}

	var cmd tea.Cmd
	m.textInput, cmd = m.textInput.Update(msg)
	return m, cmd
}

func (m model) handleCreatingKey(key string) (tea.Model, tea.Cmd) {
	if key == "ctrl+c" {
		return m, tea.Quit
	}
	return m, nil
}

func (m model) handleDeleteKey(key string) (tea.Model, tea.Cmd) {
	switch key {
	case "y", "Y":
		if m.deleteTarget < len(m.filtered) {
			wt := m.worktrees[m.filtered[m.deleteTarget]]
			m.mode = modeNormal
			m.statusMsg = loadingStatus("removing worktree…")
			return m, removeWorktreeCmd(wt.Path, m.forceDelete)
		}
		m.mode = modeNormal
	case "n", "N", "esc", "ctrl+c":
		m.mode = modeNormal
	}
	return m, nil
}

// --- Status-message constructors ---

func infoStatus(text string) *statusMsg {
	return &statusMsg{text: text, expires: time.Now().Add(statusInfoDuration)}
}

func errorStatus(text string) *statusMsg {
	return &statusMsg{text: text, isError: true, expires: time.Now().Add(statusErrorDuration)}
}

func loadingStatus(text string) *statusMsg {
	return &statusMsg{text: text, isLoading: true, expires: time.Now().Add(statusLoadingMax)}
}
