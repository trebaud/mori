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
		// Widening the terminal can bring the pane into being. Two copies of
		// the same detail is one too many, so the floating one steps aside.
		if m.splitView() && m.mode == modeDetail {
			m.mode = modeNormal
			m.detailBranch = ""
		}
		return m, m.paneFollow()

	case tickMsg:
		if m.statusMsg != nil && time.Now().After(m.statusMsg.expires) {
			m.statusMsg = nil
		}
		// Don't refresh under an open overlay: create, creating, the delete
		// confirmation and the detail pane all read the current list, and a
		// reordered list under them would retarget the keys they offer. Don't
		// refresh a window nobody is looking at either.
		quiet := m.mode != modeNormal && m.mode != modeSearch
		if quiet || !m.focused {
			return m, tickCmd(m.refreshInterval)
		}
		return m, tea.Batch(tickCmd(m.refreshInterval), refreshCmd())

	case tea.FocusMsg:
		// Coming back to the window is the moment the list is most likely to
		// be stale, and the moment the user is most likely to care.
		m.focused = true
		m.refreshInterval = refreshEvery
		return m, refreshCmd()

	case tea.BlurMsg:
		m.focused = false
		return m, nil

	case refreshedMsg:
		m.loading = false
		if msg.err != nil {
			m.statusMsg = errorStatus(msg.err.Error())
		} else {
			// A list that comes back identical earns a longer wait before the
			// next look. Anything the user does puts the beat back to base.
			if print := fingerprint(msg.worktrees); print == m.fingerprint {
				m.refreshInterval = min(m.refreshInterval*2, refreshMax)
			} else {
				m.fingerprint = print
				m.refreshInterval = refreshEvery
			}
			m.worktrees = msg.worktrees
			m.applyFilter()
		}
		if m.sweepBranch == "" {
			return withPaneFollow(m, nil)
		}
		// A sweep is clocked from the refresh that first carries its row, not
		// from the create that asked for one: querying git takes long enough
		// that a sweep started earlier would be half spent before the row it
		// highlights existed. The caret follows it in.
		switch found := m.focusBranch(m.sweepBranch); {
		case found && m.sweepFrame == 0:
			return withPaneFollow(m, sweepTickCmd())
		case !found:
			// Filtered out, or gone again. Nothing to highlight.
			m.sweepBranch = ""
		}
		return withPaneFollow(m, nil)

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
					m.creatingSteps[i].output = msg.output
				}
				break
			}
		}
		return m, waitStepCmd(m.creatingChan)

	case spinnerTickMsg:
		// Anything in flight spins: the create card's steps, and the status
		// line while a refresh or a removal is running.
		if m.loading || m.mode == modeCreating || m.detailLoading ||
			(m.statusMsg != nil && m.statusMsg.isLoading) {
			m.animFrame++
			return m, spinnerTickCmd()
		}
		return m, nil

	case worktreeCreatedMsg:
		m.creatingChan = nil
		// Something went wrong: the card stays up, holding what the failing
		// step wrote. A four-second status line was never room enough to say
		// why `npm install` fell over, and closing the card threw away the
		// only place that could.
		if msg.err != nil || len(msg.warnings) > 0 {
			m.creatingDone = true
			if msg.err != nil {
				m.statusMsg = errorStatus("create failed: " + msg.err.Error())
			}
			return m, nil
		}
		m.mode = modeNormal
		m.creatingSteps = nil
		branch := m.creatingBranch
		m.creatingBranch = ""
		m.statusMsg = infoStatus("created " + branch)
		m.sweepBranch = branch
		m.sweepFrame = 0
		return m, refreshCmd()

	case detailWantedMsg:
		// Stale: the cursor moved on while the debounce was running.
		if msg.seq != m.detailSeq || msg.branch != m.detailBranch {
			return m, nil
		}
		m.detailLoading = true
		return m, tea.Batch(loadDetailCmd(msg.branch, msg.path), spinnerTickCmd())

	case detailLoadedMsg:
		// Late arrivals for a pane that has since closed or moved on are dropped.
		if (m.mode != modeDetail && !m.splitView()) || msg.branch != m.detailBranch {
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
		m.undo = msg.removed
		if m.undo != nil {
			m.statusMsg = infoStatus("removed " + m.undo.branch + " — u to restore")
		} else {
			m.statusMsg = infoStatus("worktree removed")
		}
		return m, refreshCmd()

	case worktreeRestoredMsg:
		if msg.err != nil {
			m.statusMsg = errorStatus("restore failed: " + msg.err.Error())
			return m, nil
		}
		m.statusMsg = infoStatus("restored " + msg.branch)
		// Point the caret at it once the refresh carries it, the same way a
		// create does — a restore is a create as far as the list is concerned.
		m.sweepBranch = msg.branch
		m.sweepFrame = 0
		return m, refreshCmd()

	case tea.MouseClickMsg:
		// A click puts the cursor on the row under it. Nothing more: a click
		// that also picked the worktree and quit would be one twitch away
		// from ending the session by accident.
		if idx := m.rowAt(msg.Y); idx >= 0 {
			m.cursor = idx
			m.adjustScroll()
			return withPaneFollow(m, nil)
		}
		return m, nil

	case tea.MouseWheelMsg:
		switch msg.Button {
		case tea.MouseWheelUp:
			m.cursor = max(0, m.cursor-wheelRows)
		case tea.MouseWheelDown:
			m.cursor = min(m.cursor+wheelRows, max(0, len(m.filtered)-1))
		default:
			return m, nil
		}
		m.adjustScroll()
		return withPaneFollow(m, nil)

	case tea.KeyPressMsg:
		// Almost every key can land the cursor on a different worktree —
		// moving, filtering, sorting, archiving, unfolding the pane. Rather
		// than remember to re-point the pane in each one, do it once here,
		// after the key has had its say.
		// Someone is at the keyboard, so whatever the list has settled into,
		// look again soon rather than at the backed-off interval.
		m.refreshInterval = refreshEvery
		next, cmd := m.handleKey(msg)
		return withPaneFollow(next, cmd)
	}
	return m, nil
}

// withPaneFollow re-points the side pane at whatever the cursor now sits on,
// batching the history load behind whatever the handler already returned.
func withPaneFollow(next tea.Model, cmd tea.Cmd) (tea.Model, tea.Cmd) {
	m, ok := next.(model)
	if !ok {
		return next, cmd
	}
	if follow := m.paneFollow(); follow != nil {
		return m, tea.Batch(cmd, follow)
	}
	return m, cmd
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
		return m.handleDeleteKey(msg, key)
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
		case m.query() != "":
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
		// Wide enough for a pane, and the detail is already beside the list:
		// the key folds it away instead of floating a second copy over it.
		if m.width >= splitMinWidth {
			m.paneOpen = !m.paneOpen
			m.adjustScroll()
			return m, nil
		}
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

	case "n", "N":
		m.mode = modeCreate
		// `N` stacks on what the cursor is on; `n` cuts from the default.
		m.createBase = m.baseBranch
		if key == "N" {
			if wt := m.selectedWorktree(); wt != nil && wt.Branch != "" {
				m.createBase = wt.Branch
			}
		}
		m.textInput.SetValue("")
		m.textInput.Placeholder = "branch name"
		m.syncInputWidth()
		return m, m.textInput.Focus()

	case "d", "D":
		// The list holds only linked worktrees; the IsMain check backstops
		// that invariant because a removal takes the directory with it.
		if wt := m.selectedWorktree(); wt != nil && !wt.IsMain {
			m.mode = modeConfirmDelete
			m.deleteTarget = m.cursor
			m.deleteNeedsName = wt.Dirty > 0
			if m.deleteNeedsName {
				m.textInput.SetValue("")
				m.textInput.Placeholder = "type " + wt.Label() + " to confirm"
				m.syncInputWidth()
				return m, m.textInput.Focus()
			}
		}

	case "u":
		// Undo the last removal. The branch survived it, so this is a
		// checkout, not a resurrection: committed work comes back, whatever
		// was uncommitted when it went does not.
		if m.undo == nil {
			m.statusMsg = errorStatus("nothing to restore")
			return m, nil
		}
		rm := *m.undo
		m.undo = nil
		m.statusMsg = loadingStatus("restoring " + rm.branch + "…")
		return m, tea.Batch(restoreWorktreeCmd(rm), spinnerTickCmd())

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
	case "tab":
		// Change your mind about the base without leaving the card.
		if wt := m.selectedWorktree(); wt != nil && wt.Branch != "" && wt.Branch != m.baseBranch {
			if m.createBase == m.baseBranch {
				m.createBase = wt.Branch
			} else {
				m.createBase = m.baseBranch
			}
		}
		return m, nil
	case "esc":
		m.mode = modeNormal
		m.textInput.SetValue("")
		m.textInput.Blur()
		m.applyFilter()
		return m, nil
	case "enter":
		branch := strings.TrimSpace(m.textInput.Value())
		if m.branchProblem(branch) != "" {
			// Say nothing: the card is already showing what is wrong with it.
			return m, nil
		}
		if branch == "" {
			branch = "wt-" + internal.RandomSuffix()
		}
		m.textInput.SetValue("")
		m.textInput.Blur()
		m.applyFilter()
		m.mode = modeCreating
		m.creatingBranch = branch
		m.creatingSteps = planCreateSteps(findRepoRoot(), branch, m.createBase)
		ch, stepCmd := startCreateWorktreeCmd(branch, m.createBase)
		m.creatingChan = ch
		return m, tea.Batch(stepCmd, spinnerTickCmd())
	}

	var cmd tea.Cmd
	m.textInput, cmd = m.textInput.Update(msg)
	return m, cmd
}

// handleDeleteKey drives the confirmation. `y` is deliberately not bound: it
// yanks a path one mode over, and a hand that reaches for it out of habit
// must not be the thing that removes a worktree. A clean one goes on enter;
// a dirty one goes only once its branch name has been typed out in full.
// handleCreatingKey drives the create card. While the steps are running it
// takes nothing but a hard quit — a half-made worktree is worse than a slow
// one. Once something has failed the card is a report, and any of the usual
// ways out dismisses it.
func (m model) handleCreatingKey(key string) (tea.Model, tea.Cmd) {
	if key == "ctrl+c" {
		return m, tea.Quit
	}
	if !m.creatingDone {
		return m, nil
	}
	switch key {
	case "esc", "enter", "q":
		branch := m.creatingBranch
		m.mode = modeNormal
		m.creatingDone = false
		m.creatingSteps = nil
		m.creatingBranch = ""
		// The worktree may well be there even though a hook was not: git ran
		// first. Point the caret at it if the refresh finds it.
		m.sweepBranch = branch
		m.sweepFrame = 0
		return m, refreshCmd()
	}
	return m, nil
}

func (m model) handleDeleteKey(msg tea.KeyPressMsg, key string) (tea.Model, tea.Cmd) {
	switch key {
	case "ctrl+c":
		return m, tea.Quit
	case "esc":
		return m.cancelDelete(), nil
	case "enter":
		if m.deleteTarget >= len(m.filtered) {
			return m.cancelDelete(), nil
		}
		wt := m.worktrees[m.filtered[m.deleteTarget]]
		if m.deleteNeedsName && strings.TrimSpace(m.textInput.Value()) != wt.Label() {
			// Say nothing: the card already shows what it is waiting for, and
			// a scolding status line under a half-typed name is noise.
			return m, nil
		}
		m = m.cancelDelete()
		m.statusMsg = loadingStatus("removing " + wt.Label() + "…")
		return m, tea.Batch(removeWorktreeCmd(wt), spinnerTickCmd())
	}

	if !m.deleteNeedsName {
		if key == "n" || key == "N" {
			return m.cancelDelete(), nil
		}
		return m, nil
	}

	var cmd tea.Cmd
	m.textInput, cmd = m.textInput.Update(msg)
	return m, cmd
}

// cancelDelete leaves the confirmation and puts the shared text input back the
// way the list found it, filter and all.
func (m model) cancelDelete() model {
	m.mode = modeNormal
	m.deleteNeedsName = false
	m.textInput.SetValue("")
	m.textInput.Blur()
	m.applyFilter()
	return m
}

// handleDetailKey drives the floating detail. It is a read-only view, so it
// carries only the actions the list would have offered on the same row — pick
// it or yank its path — plus the movement keys, so the card is a preview you
// walk the list with rather than something to open and shut on every row.
func (m model) handleDetailKey(key string) (tea.Model, tea.Cmd) {
	switch key {
	case "ctrl+c":
		return m, tea.Quit
	case "enter":
		if m.cursor >= 0 && m.cursor < len(m.filtered) {
			m.selected = m.filtered[m.cursor]
			return m, tea.Quit
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

// paneFollow keeps the side pane pointed at the row under the cursor. It is
// cheap while the pane is closed — there is nothing to describe — and while
// it is open it only schedules a debounced history load, so scrolling through
// a long list costs one `git log`, for the row the cursor settles on.
func (m *model) paneFollow() tea.Cmd {
	// The floating card is the same pane on a narrower terminal, and follows
	// the cursor the same way.
	if !m.splitView() && m.mode != modeDetail {
		return nil
	}
	wt := m.selectedWorktree()
	if wt == nil {
		m.detailBranch = ""
		m.detailCommits = nil
		m.detailErr = nil
		m.detailLoading = false
		return nil
	}
	if wt.Label() == m.detailBranch {
		return nil
	}
	m.detailBranch = wt.Label()
	m.detailCommits = nil
	m.detailErr = nil
	m.detailLoading = false
	m.detailSeq++
	return scheduleDetailCmd(m.detailSeq, wt.Label(), wt.Path)
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
