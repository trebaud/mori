package tui

import (
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/trebaud/mori/v2/internal"
)

// Update is the Elm update function — it takes a message and returns the next
// model plus any effects to run.
func (m model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		m.syncInputWidth()
		m.adjustScroll()
		return m, nil

	case tickMsg:
		if m.statusMsg != nil && time.Now().After(m.statusMsg.expires) {
			m.statusMsg = nil
		}
		// Don't refresh under an open overlay: create, creating, the delete
		// confirmation and the detail pane all read the current list, and a
		// reordered list under them would retarget the keys they offer.
		if m.mode == modeNormal || m.mode == modeSearch {
			return m, tea.Batch(tickCmd(), refreshCmd())
		}
		return m, tickCmd()

	case refreshedMsg:
		if msg.err == nil {
			m.worktrees = msg.worktrees
			m.applyFilter()
		}
		if m.sweepBranch == "" {
			return m, nil
		}
		// A sweep is clocked from the refresh that first carries its row, not
		// from the create that asked for one: querying git takes long enough
		// that a sweep started earlier would be half spent before the row it
		// highlights existed. The caret follows it in.
		switch found := m.focusBranch(m.sweepBranch); {
		case found && m.sweepFrame == 0:
			return m, sweepTickCmd()
		case !found:
			// Filtered out, or gone again. Nothing to highlight.
			m.sweepBranch = ""
		}
		return m, nil

	case sweepTickMsg:
		if m.sweepBranch == "" {
			return m, nil
		}
		m.sweepFrame++
		if m.sweepFrame > sweepFrames {
			m.sweepBranch = ""
			m.sweepFrame = 0
			return m, nil
		}
		return m, sweepTickCmd()

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
		// Anything in flight spins: the create card's steps, and the status
		// line while a refresh or a removal is running.
		if m.mode == modeCreating || m.detailLoading || (m.statusMsg != nil && m.statusMsg.isLoading) {
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
		m.sweepBranch = branch
		m.sweepFrame = 0
		return m, refreshCmd()

	case detailLoadedMsg:
		// Late arrivals for a pane that has since closed or moved on are dropped.
		if m.mode != modeDetail || msg.branch != m.detailBranch {
			return m, nil
		}
		m.detailLoading = false
		m.detailCommits = msg.commits
		m.detailErr = msg.err
		return m, nil

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
	case modeDetail:
		return m.handleDetailKey(key)
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
		m.cursor = min(m.cursor+m.visibleRows(), max(0, len(m.filtered)-1))
		m.adjustScroll()
	case "ctrl+u", "pgup":
		m.cursor = max(0, m.cursor-m.visibleRows())
		m.adjustScroll()

	case "enter":
		// The one key that ends the session with a path. Run prints it on
		// stdout; the shell function turns that into a cd.
		if m.cursor >= 0 && m.cursor < len(m.filtered) {
			m.selected = m.filtered[m.cursor]
			return m, tea.Quit
		}

	case "i", "tab":
		if wt := m.selectedWorktree(); wt != nil {
			m.mode = modeDetail
			m.detailBranch = wt.Label()
			m.detailCommits = nil
			m.detailErr = nil
			m.detailLoading = true
			return m, tea.Batch(loadDetailCmd(wt.Label(), wt.Path), spinnerTickCmd())
		}

	case "y":
		if wt := m.selectedWorktree(); wt != nil {
			m.statusMsg = infoStatus(yankText(*wt))
			return m, tea.SetClipboard(wt.Path)
		}

	case "n":
		m.mode = modeCreate
		m.textInput.SetValue("")
		m.textInput.Placeholder = "branch name"
		m.syncInputWidth()
		return m, m.textInput.Focus()

	case "d", "D":
		// The list holds only linked worktrees; the IsMain check backstops
		// that invariant because deleting is irreversible.
		if wt := m.selectedWorktree(); wt != nil && !wt.IsMain {
			m.mode = modeConfirmDelete
			m.deleteTarget = m.cursor
		}

	case "/":
		m.mode = modeSearch
		m.textInput.Placeholder = "filter by branch or path…"
		m.syncInputWidth()
		return m, m.textInput.Focus()

	case "s":
		m.sortMode = m.sortMode.Next()
		m.applyFilter()

	case "r":
		m.statusMsg = loadingStatus("refreshing…")
		return m, tea.Batch(refreshCmd(), spinnerTickCmd())

	case "x":
		if wt := m.selectedWorktree(); wt != nil {
			switch {
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
	case "y", "Y":
		m.mode = modeNormal
		if m.deleteTarget < len(m.filtered) {
			wt := m.worktrees[m.filtered[m.deleteTarget]]
			m.statusMsg = loadingStatus("removing " + wt.Label() + "…")
			// Force: the confirmation already spelled out any uncommitted work.
			return m, tea.Batch(removeWorktreeCmd(wt.Path, true), spinnerTickCmd())
		}
	case "n", "N", "esc", "ctrl+c":
		m.mode = modeNormal
	}
	return m, nil
}

// handleDetailKey drives the detail pane. It is a read-only view, so it
// carries only the actions the list would have offered on the same row — pick
// it or yank its path — plus a way out.
func (m model) handleDetailKey(key string) (tea.Model, tea.Cmd) {
	switch key {
	case "ctrl+c":
		return m, tea.Quit
	case "enter":
		if m.cursor >= 0 && m.cursor < len(m.filtered) {
			m.selected = m.filtered[m.cursor]
			return m, tea.Quit
		}
	case "esc", "i", "tab", "q":
		m.mode = modeNormal
		m.detailBranch = ""
		m.detailCommits = nil
		m.detailErr = nil
		m.detailLoading = false
	case "y":
		if wt := m.selectedWorktree(); wt != nil {
			m.statusMsg = infoStatus(yankText(*wt))
			return m, tea.SetClipboard(wt.Path)
		}
	}
	return m, nil
}

// yankText is what the status line says after `y`. The clipboard is written
// with OSC 52, which a terminal is free to ignore — over ssh, in a tmux
// without set-clipboard, in a handful of emulators — and reports nothing
// back. So the message spells the path out rather than claiming the copy
// worked: if the clipboard swallowed it, the path is still on screen to
// select with the mouse.
func yankText(wt internal.Worktree) string {
	return "copied " + wt.DisplayPath
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
