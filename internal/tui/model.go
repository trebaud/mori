package tui

import (
	"strings"
	"time"

	"charm.land/bubbles/v2/textinput"
	tea "charm.land/bubbletea/v2"

	"github.com/trebaud/mori/internal"
)

// Layout constants. A worktree card is three rendered rows (branch, path,
// git state) plus one blank row of breathing space.
const (
	cardHeight    = 4
	chromeHeight  = 6 // blank, top bar, blank, status line, footer, trailing
	minViewWidth  = 44
	minViewHeight = 12
	refreshEvery  = 15 * time.Second
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
}

// --- Model (Elm: Model) ---

type model struct {
	worktrees []internal.Worktree
	filtered  []int // indices into worktrees, in display order
	cursor    int   // index into filtered
	selected  int   // index into worktrees once the user picks one, else -1

	repoLabel     string
	currentBranch string
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

	animFrame      int
	creatingBranch string
	creatingSteps  []creatingStep
	creatingChan   chan tea.Msg
}

func newModel(worktrees []internal.Worktree, repoLabel, currentBranch string) model {
	ti := textinput.New()
	ti.CharLimit = 60
	ti.Prompt = ""

	m := model{
		worktrees:     worktrees,
		selected:      -1,
		repoLabel:     repoLabel,
		currentBranch: currentBranch,
		textInput:     ti,
		mode:          modeNormal,
		sortMode:      internal.SortDefault,
		archived:      loadArchived(),
	}
	m.applyFilter()
	return m
}

func (m model) Init() tea.Cmd {
	return tickCmd()
}

// --- Layout helpers ---

// listHeight is the number of rows available for worktree cards.
func (m model) listHeight() int {
	if m.height <= 0 {
		return cardHeight * 3
	}
	h := m.height - chromeHeight
	if h < cardHeight {
		h = cardHeight
	}
	return h
}

// visibleCards is how many worktree cards fit in the list area.
func (m model) visibleCards() int {
	n := m.listHeight() / cardHeight
	if n < 1 {
		return 1
	}
	return n
}

// adjustScroll keeps the cursor inside the viewport and clamps scrollOffset so
// we never scroll past the last card.
func (m *model) adjustScroll() {
	n := m.visibleCards()
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
