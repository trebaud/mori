package tui

import (
	"sort"
	"strings"
	"time"

	"charm.land/bubbles/v2/textarea"
	"charm.land/bubbles/v2/textinput"
	tea "charm.land/bubbletea/v2"

	"github.com/trebaud/mori/internal"
	"github.com/trebaud/mori/internal/agent"
)

// --- Modes and filters ---

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

// --- Messages (Elm: Msg) ---

type statusMsg struct {
	text      string
	isError   bool
	isLoading bool
	expires   time.Time
}

type worktreeCreatedMsg struct {
	err      error
	warnings []string
}

type worktreeRemovedMsg struct {
	err error
}

type messageSentMsg struct {
	err     error
	logPath string
}

// --- Model (Elm: Model) ---

type model struct {
	worktrees     []internal.Worktree
	filtered      []int
	cursor        int
	selected      int
	currentBranch string
	width         int
	height        int
	showInsights  bool
	showHelp      bool
	tick          time.Time

	mode         inputMode
	textInput    textinput.Model
	messageInput textarea.Model
	statusMsg    *statusMsg

	sortMode     sortMode
	statusFilter statusFilter

	deleteTarget int
	forceDelete  bool

	archived    map[string]bool
	showArchive bool
}

func newModel(worktrees []internal.Worktree, currentBranch string) model {
	ti := textinput.New()
	ti.CharLimit = 60

	filtered := make([]int, len(worktrees))
	for i := range worktrees {
		filtered[i] = i
	}

	return model{
		worktrees:     worktrees,
		filtered:      filtered,
		selected:      -1,
		currentBranch: currentBranch,
		tick:          time.Now(),
		textInput:     ti,
		messageInput:  newMessageTextarea(),
		mode:          modeNormal,
		sortMode:      sortDefault,
		archived:      loadArchived(),
	}
}

func (m model) Init() tea.Cmd {
	return tea.Tick(m.tickInterval(), func(t time.Time) tea.Msg {
		return t
	})
}

// --- State helpers ---

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

func (m model) selectedWorktree() *internal.Worktree {
	if m.cursor < len(m.filtered) {
		return &m.worktrees[m.filtered[m.cursor]]
	}
	return nil
}

// messageInputWidth returns the inner width for the prompt textarea so it fits inside its framed card.
func (m model) messageInputWidth() int {
	total := m.width
	if total == 0 {
		total = 80
	}
	cardW := total - 4
	if cardW > 100 {
		cardW = 100
	}
	if cardW < 30 {
		cardW = 30
	}
	return cardW - 4
}

func (m *model) refreshInsights() {
	for i := range m.worktrees {
		m.worktrees[i].Insights = agent.GetInsights(m.worktrees[i].Path)
	}
	m.tick = time.Now()
}

func (m *model) refreshWorktreeList() {
	if wts, err := internal.List(); err == nil {
		m.worktrees = wts
		m.applyFilter()
	}
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
