package tui

import (
	"strings"
	"time"

	"charm.land/bubbles/v2/textinput"
	tea "charm.land/bubbletea/v2"

	"github.com/trebaud/mori/v2/internal"
	"github.com/trebaud/mori/v2/internal/git"
)

// Layout constants. A worktree is one row — branch, git state, sync, age and
// HEAD in aligned columns — and the selected one spends a second row on its
// full path.
const (
	rowHeight     = 1
	detailHeight  = 1 // the path line under the selected worktree
	chromeHeight  = 7 // blank, top bar, blank, column labels, status, footer, trailing
	minViewWidth  = 44
	minViewHeight = 12
	// maxContentWidth caps how wide the layout grows. Past it the two columns
	// of a card sit too far apart to read as one row.
	maxContentWidth = 100
	refreshEvery    = 15 * time.Second
)

// A worktree that was just created is one more row in a list of twenty. For
// a moment after it appears, an underline sweeps once across its name — the
// list paints no backgrounds, and an underline reads on a monochrome terminal
// as well as a colored one.
const (
	sweepWindow   = 4  // columns lit at once
	sweepFrames   = 14 // steps the sweep takes, whatever the name's length
	sweepInterval = 45 * time.Millisecond
)

// Status-message bucket durations.
const (
	statusInfoDuration  = 2500 * time.Millisecond
	statusErrorDuration = 4 * time.Second
	statusLoadingMax    = 30 * time.Second
)

// --- Modes ---

type inputMode int

const (
	modeNormal inputMode = iota
	modeSearch
	modeCreate
	modeCreating
	modeConfirmDelete
	modeDetail
)

type stepState int

const (
	stepPending stepState = iota
	stepRunning
	stepSucceeded
	stepFailed
)

type creatingStep struct {
	name  string
	cmd   string
	state stepState
}

// --- Messages (Elm: Msg) ---

type statusMsg struct {
	text      string
	isError   bool
	isLoading bool
	expires   time.Time
}

// removedWorktree is what `u` needs to undo a removal. `git worktree remove`
// takes the directory and leaves the branch, so checking the branch back out
// at the same path restores everything that had been committed.
type removedWorktree struct {
	branch, path, displayPath string
}

// refreshedMsg carries a freshly queried worktree list.
type refreshedMsg struct {
	worktrees []internal.Worktree
	err       error
}

// tickMsg drives the periodic background refresh.
type tickMsg time.Time

type worktreeCreatedMsg struct {
	err      error
	warnings []string
}

type worktreeRemovedMsg struct {
	err error
	// removed is the worktree that went, kept so `u` can put it back. Nil
	// when the removal failed or the worktree had no branch to check out.
	removed *removedWorktree
}

// worktreeRestoredMsg answers an undo.
type worktreeRestoredMsg struct {
	branch string
	err    error
}

// detailLoadedMsg carries the history the detail pane asked git for. branch
// identifies which request it answers, so a pane closed and reopened on
// another worktree never renders the previous one's log.
type detailLoadedMsg struct {
	branch  string
	commits []git.Commit
	err     error
}

// --- Model (Elm: Model) ---

type model struct {
	worktrees []internal.Worktree
	filtered  []int // indices into worktrees, in display order
	cursor    int   // index into filtered
	// selected is the worktree the session hands back on exit, or -1. `enter`
	// sets it and quits: mori prints that path on stdout, and the shell
	// function from `mori shell-init` turns the print into a cd. `y` is the
	// second way out, for when you want the path somewhere else.
	selected int

	repoLabel     string
	baseBranch    string
	width, height int

	mode      inputMode
	textInput textinput.Model
	statusMsg *statusMsg

	sortMode    internal.SortMode
	archived    map[string]bool
	showArchive bool
	showHelp    bool

	scrollOffset int // first visible card
	deleteTarget int // index into filtered
	// deleteNeedsName is set when the worktree being confirmed has
	// uncommitted work. A keystroke is too cheap for something unrecoverable,
	// so that case asks for the branch name typed out.
	deleteNeedsName bool
	// undo remembers the last worktree removed, so `u` can put it back.
	undo *removedWorktree

	detailBranch  string // the worktree the open pane describes
	detailCommits []git.Commit
	detailErr     error
	detailLoading bool

	// sweepBranch is the freshly created worktree being highlighted, empty
	// once the sweep has run its course.
	sweepBranch string
	sweepFrame  int

	animFrame      int
	creatingBranch string
	creatingSteps  []creatingStep
	creatingChan   chan tea.Msg
}

func newModel(worktrees []internal.Worktree, repoLabel, baseBranch string) model {
	ti := textinput.New()
	ti.CharLimit = 60
	ti.Prompt = ""

	m := model{
		worktrees:  worktrees,
		selected:   -1,
		repoLabel:  repoLabel,
		baseBranch: baseBranch,
		textInput:  ti,
		mode:       modeNormal,
		sortMode:   internal.SortDefault,
		archived:   loadArchived(),
	}
	m.applyFilter()
	return m
}

func (m model) Init() tea.Cmd {
	return tickCmd()
}

// --- Layout helpers ---

// listHeight is the number of rows available for the worktree list.
func (m model) listHeight() int {
	if m.height <= 0 {
		return 12
	}
	h := m.height - chromeHeight
	if h < rowHeight {
		h = rowHeight
	}
	return h
}

// visibleRows is how many worktrees fit in the list area. One line is always
// held back for the selected worktree's path, and when the list overflows the
// bottom row goes to the scroll hint instead of a worktree — so the viewport
// is always exactly listHeight lines and the footer never moves, wherever the
// cursor sits and whether or not there is more to scroll to.
func (m model) visibleRows() int {
	h := m.listHeight()
	if len(m.filtered) > 0 {
		h -= detailHeight
	}
	if len(m.filtered) > h {
		h--
	}
	if h < 1 {
		return 1
	}
	return h
}

// adjustScroll keeps the cursor inside the viewport and clamps scrollOffset so
// we never scroll past the last card.
func (m *model) adjustScroll() {
	n := m.visibleRows()
	if m.cursor < m.scrollOffset {
		m.scrollOffset = m.cursor
	}
	if m.cursor >= m.scrollOffset+n {
		m.scrollOffset = m.cursor - n + 1
	}
	maxOffset := len(m.filtered) - n
	if maxOffset < 0 {
		maxOffset = 0
	}
	if m.scrollOffset > maxOffset {
		m.scrollOffset = maxOffset
	}
	if m.scrollOffset < 0 {
		m.scrollOffset = 0
	}
}

// syncInputWidth sizes the text input for whichever prompt is showing. The
// width also governs the placeholder: bubbles copies it into a buffer of
// Width+1 runes, so an unset width renders exactly one character of it.
func (m *model) syncInputWidth() {
	width := m.width
	if width <= 0 {
		width = 80
	}

	var avail int
	if m.mode == modeCreate || m.mode == modeConfirmDelete {
		// Card interior, less the frame and the "❯  " prefix.
		avail = cardWidth(width, createCardMaxWidth) - 6
	} else {
		// Status line, less the "/ " prefix and a trailing column.
		avail = width - 4
	}
	// A text input renders Width()+1 columns — the value or placeholder plus a
	// trailing cursor cell — so ask for one less than the room we have.
	m.textInput.SetWidth(max(8, avail-1))
}

// --- State helpers ---

func (m model) selectedWorktree() *internal.Worktree {
	if m.cursor >= 0 && m.cursor < len(m.filtered) {
		return &m.worktrees[m.filtered[m.cursor]]
	}
	return nil
}

// applyFilter rebuilds the visible index list from the archive state, the
// search query, and the active sort mode.
func (m *model) applyFilter() {
	query := strings.ToLower(strings.TrimSpace(m.textInput.Value()))

	m.filtered = nil
	for i, wt := range m.worktrees {
		if !m.showArchive && m.archived[wt.Branch] {
			continue
		}
		if query != "" &&
			!strings.Contains(strings.ToLower(wt.Label()), query) &&
			!strings.Contains(strings.ToLower(wt.DisplayPath), query) {
			continue
		}
		m.filtered = append(m.filtered, i)
	}
	internal.SortIndices(m.worktrees, m.filtered, m.sortMode)

	if m.cursor >= len(m.filtered) {
		m.cursor = max(0, len(m.filtered)-1)
	}
	m.adjustScroll()
}

// focusBranch puts the cursor on a branch if it is on show, so the worktree
// that just appeared is the one under the caret. It reports whether the
// branch was there; a filter that excludes it leaves the cursor alone.
func (m *model) focusBranch(branch string) bool {
	for i, idx := range m.filtered {
		if m.worktrees[idx].Branch == branch {
			m.cursor = i
			m.adjustScroll()
			return true
		}
	}
	return false
}

// dirtyCount is how many visible worktrees have uncommitted changes.
func (m model) dirtyCount() int {
	n := 0
	for _, i := range m.filtered {
		if m.worktrees[i].Dirty > 0 {
			n++
		}
	}
	return n
}
