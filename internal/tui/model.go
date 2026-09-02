package tui

import (
	"fmt"
	"sort"
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
	rowHeight = 1
	// baseChromeHeight is what surrounds the list in every layout: a blank
	// line, the top bar, a blank line, the column labels, the status line,
	// the footer, and a trailing line. A one-column layout adds one more for
	// the selected worktree's path; the split layout puts that in the pane.
	baseChromeHeight = 7
	minViewWidth     = 44
	minViewHeight    = 12
	// maxContentWidth caps how wide a single column of list grows. Past it the
	// branch name and the git state sit too far apart to read as one row.
	maxContentWidth = 100
	// splitMinWidth is where the layout stops being one column. Below it there
	// is no room for a pane that does not starve the list.
	splitMinWidth = 120
	// The pane takes about a third of the terminal, between these bounds: less
	// than paneMinWidth cannot hold a commit subject, more than paneMaxWidth
	// is a column of short lines in a wide field of nothing.
	paneMinWidth = 44
	paneMaxWidth = 76
	// maxSplitWidth caps the two columns together. A very wide terminal gets
	// the layout it can read, not all the columns it can hold.
	maxSplitWidth = 180
	// paneGap separates the two columns.
	paneGap = 2
	// refreshEvery is how soon after a change mori looks again. Every beat
	// shells out to git once per worktree, so a list that keeps coming back
	// identical backs off — doubling to refreshMax — rather than paying that
	// every fifteen seconds for a repository nobody is touching. Any keypress
	// puts it back to the base, and so does the terminal regaining focus.
	refreshEvery = 15 * time.Second
	refreshMax   = 2 * time.Minute
	// detailDebounce is how long the cursor must rest before the pane asks git
	// for history. Held-down `j` should scroll a list, not fork a process per
	// row it passes.
	detailDebounce = 120 * time.Millisecond
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
	// output is what the step wrote. Only kept for a step that failed, where
	// it is the whole reason the card is still on screen.
	output string
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

// creatingDone marks a create that has finished but whose card is still up
// because something went wrong and the card is holding the output.

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

// detailWantedMsg fires once the cursor has rested long enough for the side
// pane to be worth a `git log`. seq is the cursor generation it was scheduled
// under; a later move makes it stale and it is dropped.
type detailWantedMsg struct {
	seq          int
	branch, path string
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

	sortMode internal.SortMode
	// lastQuery is the filter the current ordering was built for. A query
	// that has not changed leaves the cursor on its worktree; a new one puts
	// it on the best match.
	lastQuery   string
	archived    map[string]bool
	showArchive bool
	showHelp    bool

	// loading is true until the first list comes back. mori starts drawing
	// before it knows what to draw: querying git for a repository with twenty
	// worktrees takes long enough that a blank terminal would be the first
	// thing the user saw.
	loading bool
	// refreshInterval is the current beat, between refreshEvery and
	// refreshMax. fingerprint is what the last list looked like, so an
	// unchanged one can be recognised without diffing.
	refreshInterval time.Duration
	fingerprint     string
	// focused tracks terminal focus where the terminal reports it. A window
	// nobody is looking at does not need polling.
	focused bool

	scrollOffset int // first visible card
	deleteTarget int // index into filtered
	// paneOpen governs the side pane on a terminal wide enough for one. It is
	// on by default — the pane is the layout, not an extra — and `i` folds it
	// away for a session that wants nothing but the list.
	paneOpen bool
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
	// detailSeq counts cursor moves. A debounced history load carries the seq
	// it was scheduled under and is dropped if the cursor has moved on since,
	// so only the row the user actually stopped on costs a `git log`.
	detailSeq int

	// sweepBranch is the freshly created worktree being highlighted, empty
	// once the sweep has run its course.
	sweepBranch string
	sweepFrame  int

	animFrame      int
	creatingBranch string
	creatingSteps  []creatingStep
	creatingChan   chan tea.Msg
	// creatingDone holds the card open after a failed create so the output is
	// readable. A create that went fine closes on its own.
	creatingDone bool
}

func newModel(worktrees []internal.Worktree, repoLabel, baseBranch string) model {
	ti := textinput.New()
	ti.CharLimit = 60
	ti.Prompt = ""

	m := model{
		worktrees:       worktrees,
		selected:        -1,
		loading:         len(worktrees) == 0,
		focused:         true,
		repoLabel:       repoLabel,
		baseBranch:      baseBranch,
		textInput:       ti,
		mode:            modeNormal,
		sortMode:        internal.SortDefault,
		archived:        loadArchived(),
		paneOpen:        true,
		refreshInterval: refreshEvery,
	}
	m.applyFilter()
	return m
}

func (m model) Init() tea.Cmd {
	// The list is asked for here rather than before the program starts, so the
	// first frame is on screen — brand, chrome and a spinner — while git is
	// still being queried.
	return tea.Batch(refreshCmd(), tickCmd(refreshEvery), spinnerTickCmd())
}

// fingerprint is a cheap stand-in for the list's contents: everything the
// display would draw differently, and nothing else.
func fingerprint(wts []internal.Worktree) string {
	var b strings.Builder
	for _, wt := range wts {
		fmt.Fprintf(&b, "%s\x1f%s\x1f%s\x1f%d\x1f%d\x1f%d\x1f%d\n",
			wt.Branch, wt.Head, wt.Subject, wt.Dirty, wt.Ahead, wt.Behind, wt.LastCommit.Unix())
	}
	return b.String()
}

// --- Layout helpers ---

// splitView reports whether the layout is two columns: the list, and a pane
// describing whatever the cursor is on. With nothing to list there is nothing
// for a pane to describe, and the empty state would rather have the width.
func (m model) splitView() bool {
	return m.paneOpen && m.width >= splitMinWidth && len(m.filtered) > 0
}

// contentWidth is how much of the terminal the layout uses. One column stops
// growing at maxContentWidth; two columns keep going, to a point.
func (m model) contentWidth() int {
	w := m.width
	if w <= 0 {
		w = 100
	}
	if m.splitView() {
		return min(w, maxSplitWidth)
	}
	return min(w, maxContentWidth)
}

// paneWidth is the side pane's width, zero when there is no pane.
func (m model) paneWidth() int {
	if !m.splitView() {
		return 0
	}
	return min(max(m.contentWidth()/3, paneMinWidth), paneMaxWidth)
}

// listWidth is what the rows are measured against — the whole content width,
// less the pane, the gap before it, and the one-column right margin the top
// bar and the footer already keep, so all three end on the same column.
func (m model) listWidth() int {
	if p := m.paneWidth(); p > 0 {
		return m.contentWidth() - p - paneGap - 1
	}
	return m.contentWidth()
}

// chromeHeight is the number of rows the list does not get. The one-column
// layout spends one of them on the selected worktree's path, at a fixed
// position above the status line: inlined under its row instead, it shifted
// every row below the cursor by a line on every keypress.
func (m model) chromeHeight() int {
	if m.splitView() {
		return baseChromeHeight
	}
	return baseChromeHeight + 1
}

// listHeight is the number of rows available for the worktree list.
func (m model) listHeight() int {
	if m.height <= 0 {
		return 12
	}
	h := m.height - m.chromeHeight()
	if h < rowHeight {
		h = rowHeight
	}
	return h
}

// visibleRows is how many worktrees fit in the list area. Every row is one
// line, so this is the list height — less one when the list overflows and the
// bottom row goes to the scroll hint instead of a worktree. The viewport is
// always exactly listHeight lines, so the footer never moves.
func (m model) visibleRows() int {
	h := m.listHeight()
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

// query is the active filter, trimmed. The text input is shared with the
// create and delete cards, so what it holds is only a filter while the list
// is the thing on screen.
func (m model) query() string {
	if m.mode != modeNormal && m.mode != modeSearch {
		return ""
	}
	return strings.TrimSpace(m.textInput.Value())
}

// applyFilter rebuilds the visible index list from the archive state, the
// search query, and the active sort mode.
//
// A query ranks as well as filters: fuzzy hits are ordered best-first, which
// is the whole point of typing one, and the chosen sort mode goes back to
// ordering the list the moment the query is cleared. A branch hit outscores
// the same query found in a path — you are usually naming a branch.
func (m *model) applyFilter() {
	query := m.query()
	// The cursor holds its worktree across a rebuild it did not ask for — a
	// background refresh, an archive toggle — so a reordered list never slides
	// a different worktree under a key about to be pressed. A changed query is
	// different: there the cursor belongs on the best match.
	var keep string
	changedQuery := query != m.lastQuery
	if !changedQuery {
		if wt := m.selectedWorktree(); wt != nil {
			keep = wt.Label()
		}
	}
	m.lastQuery = query

	type hit struct{ idx, score int }
	var hits []hit
	for i, wt := range m.worktrees {
		if !m.showArchive && m.archived[wt.Branch] {
			continue
		}
		positions, score, ok := fuzzyMatch(wt.Label(), query)
		if ok {
			hits = append(hits, hit{i, score + scoreBoundary*len(positions)})
			continue
		}
		if _, score, ok := fuzzyMatch(wt.DisplayPath, query); ok {
			// Found, but in the path. Worth showing, not worth showing first.
			hits = append(hits, hit{i, score - 1000})
		}
	}

	m.filtered = m.filtered[:0]
	for _, h := range hits {
		m.filtered = append(m.filtered, h.idx)
	}
	if query == "" {
		internal.SortIndices(m.worktrees, m.filtered, m.sortMode)
	} else {
		score := make(map[int]int, len(hits))
		for _, h := range hits {
			score[h.idx] = h.score
		}
		sort.SliceStable(m.filtered, func(a, b int) bool {
			return score[m.filtered[a]] > score[m.filtered[b]]
		})
	}

	switch {
	case changedQuery:
		// Every keystroke re-ranks, so the cursor belongs on the best match
		// rather than at whatever depth it happened to be.
		m.cursor = 0
	case keep != "" && m.focusBranch(keep):
		// Put back where it was.
	case m.cursor >= len(m.filtered):
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
