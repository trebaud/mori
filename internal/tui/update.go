package tui

import (
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/trebaud/mori/internal"
)

// Update is the Elm update function — it takes a message and returns the next
// model plus any effects to run.
func (m model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		m.adjustScroll()
		return m, nil

	case tickMsg:
		if m.statusMsg != nil && time.Now().After(m.statusMsg.expires) {
			m.statusMsg = nil
		}
		// Don't refresh under an open overlay: create, creating, and the
		// delete confirmation all hold indices into the current list.
		if m.mode == modeNormal || m.mode == modeSearch {
			return m, tea.Batch(tickCmd(), refreshCmd())
		}
		return m, tickCmd()

	case refreshedMsg:
		if msg.err == nil {
			m.worktrees = msg.worktrees
			m.applyFilter()
		}
		return m, nil

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
		branch := m.creatingBranch
		m.creatingBranch = ""
		switch {
		case msg.err != nil:
			m.statusMsg = errorStatus("create failed: " + msg.err.Error())
			return m, nil
		case len(msg.warnings) > 0:
			m.statusMsg = errorStatus("created " + branch + " (hooks failed: " + strings.Join(msg.warnings, ", ") + ")")
		default:
			m.statusMsg = infoStatus("created " + branch)
		}
		return m, refreshCmd()

	case worktreeRemovedMsg:
		if msg.err != nil {
			m.statusMsg = errorStatus("remove failed: " + msg.err.Error())
			return m, nil
		}
		m.statusMsg = infoStatus("worktree removed")
		return m, refreshCmd()

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
		// Creation is not cancellable mid-flight; only a hard quit gets out.
		if key == "ctrl+c" {
			return m, tea.Quit
		}
		return m, nil
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
		// Back out of one layer at a time so a single esc always lands the
		// user closer to the plain list.
		switch {
		case m.showHelp:
			m.showHelp = false
		case m.textInput.Value() != "":
			m.textInput.SetValue("")
			m.applyFilter()
		case m.showArchive:
			m.showArchive = false
			m.applyFilter()
		}

	case "up", "k":
		if m.cursor > 0 {
			m.cursor--
		}
		m.adjustScroll()
	case "down", "j":
		if m.cursor < len(m.filtered)-1 {
			m.cursor++
		}
		m.adjustScroll()
	case "g", "home":
		m.cursor = 0
		m.adjustScroll()
	case "G", "end":
		m.cursor = max(0, len(m.filtered)-1)
		m.adjustScroll()
	case "ctrl+d", "pgdown":
		m.cursor = min(m.cursor+m.visibleCards(), max(0, len(m.filtered)-1))
		m.adjustScroll()
	case "ctrl+u", "pgup":
		m.cursor = max(0, m.cursor-m.visibleCards())
		m.adjustScroll()

	case "enter", "o":
		if wt := m.selectedWorktree(); wt != nil {
			m.selected = m.filtered[m.cursor]
			// Set the clipboard before quitting so the copy actually lands.
			return m, tea.Sequence(tea.SetClipboard(wt.Path), tea.Quit)
		}

	case "y":
		if wt := m.selectedWorktree(); wt != nil {
			m.statusMsg = infoStatus("path yanked to clipboard")
			return m, tea.SetClipboard(wt.Path)
		}

	case "n":
		m.mode = modeCreate
		m.textInput.SetValue("")
		m.textInput.Placeholder = "branch name"
		return m, m.textInput.Focus()

	case "d", "D":
		if wt := m.selectedWorktree(); wt != nil {
			if wt.IsMain {
				m.statusMsg = errorStatus("cannot delete the main worktree")
			} else {
				m.mode = modeConfirmDelete
				m.deleteTarget = m.cursor
			}
		}

	case "/":
		m.mode = modeSearch
		m.textInput.Placeholder = "filter by branch or path…"
		return m, m.textInput.Focus()

	case "s":
		m.sortMode = m.sortMode.Next()
		m.applyFilter()

	case "r":
		m.statusMsg = loadingStatus("refreshing…")
		return m, refreshCmd()

	case "x":
		if wt := m.selectedWorktree(); wt != nil {
			switch {
			case wt.IsMain:
				m.statusMsg = errorStatus("cannot archive the main worktree")
			case wt.Branch == "":
				// The archive is keyed by branch, so detached heads can't join.
				m.statusMsg = errorStatus("cannot archive a detached worktree")
			case m.archived[wt.Branch]:
				delete(m.archived, wt.Branch)
				saveArchived(m.archived)
				m.statusMsg = infoStatus("unarchived " + wt.Branch)
			default:
				m.archived[wt.Branch] = true
				saveArchived(m.archived)
				m.statusMsg = infoStatus("archived " + wt.Branch)
			}
			m.applyFilter()
		}
	case "X":
		m.showArchive = !m.showArchive
		m.applyFilter()
		if m.showArchive {
			m.statusMsg = infoStatus("showing archived worktrees")
		} else {
			m.statusMsg = infoStatus("hiding archived worktrees")
		}

	case "?":
		m.showHelp = !m.showHelp
	}
	return m, nil
}

func (m model) handleSearchKey(msg tea.KeyPressMsg, key string) (tea.Model, tea.Cmd) {
	switch key {
	case "ctrl+c":
		return m, tea.Quit
	case "esc":
		// Abandon the search entirely: clear the query and restore the list.
		m.mode = modeNormal
		m.textInput.SetValue("")
		m.textInput.Blur()
		m.applyFilter()
		return m, nil
	case "enter":
		// Keep the query applied and hand navigation back to the list.
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
		m.textInput.SetValue("")
		m.textInput.Blur()
		m.applyFilter()
		return m, nil
	case "enter":
		branch := strings.TrimSpace(m.textInput.Value())
		if branch == "" {
			branch = "wt-" + internal.RandomSuffix()
		}
		m.textInput.SetValue("")
		m.textInput.Blur()
		m.applyFilter()
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

func (m model) handleDeleteKey(key string) (tea.Model, tea.Cmd) {
	switch key {
	case "y", "Y", "enter":
		m.mode = modeNormal
		if m.deleteTarget < len(m.filtered) {
			wt := m.worktrees[m.filtered[m.deleteTarget]]
			m.statusMsg = loadingStatus("removing " + wt.Label() + "…")
			// Force: the confirmation already spelled out any uncommitted work.
			return m, removeWorktreeCmd(wt.Path, true)
		}
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
