package tui

import (
	"errors"
	"fmt"
	"regexp"
	"strings"
	"testing"
	"time"

	"charm.land/lipgloss/v2"

	"github.com/trebaud/mori/v2/internal"
	"github.com/trebaud/mori/v2/internal/git"
)

func testWorktrees(n int) []internal.Worktree {
	wts := make([]internal.Worktree, 0, n)
	for i := 1; i <= n; i++ {
		wts = append(wts, internal.Worktree{
			Path:        "/home/t/.mori/worktrees/repo/wt",
			Branch:      "feat/" + strings.Repeat("x", i),
			DisplayPath: "~/.mori/worktrees/repo/wt",
			Head:        "bbbbbbb",
			Subject:     "teach the parser about " + strings.Repeat("nested ", i) + "groups",
			Dirty:       i,
			Ahead:       i,
			LastCommit:  time.Now().Add(-time.Duration(i) * time.Minute),
		})
	}
	return wts
}

func newTestModel(t *testing.T, n, width, height int) model {
	t.Helper()
	ApplyTheme(true)
	m := newModel(testWorktrees(n), "~/repo", "main")
	m.archived = map[string]bool{}
	m.width, m.height = width, height
	m.applyFilter()
	return m
}

// Every worktree is exactly one line, selected or not. A selected row that
// grew a second line pushed everything under it down and back on every
// keypress, and the list rippled as the cursor moved.
func TestRenderRowIsAlwaysOneLine(t *testing.T) {
	m := newTestModel(t, 4, 80, 30)
	cols := m.rowColumns(80)
	for i := range m.filtered {
		if row := m.renderRow(i, 80, cols); strings.Contains(row, "\n") {
			t.Errorf("row %d is more than one line: %q", i, plain(row))
		}
	}
}

// No row spells its path out; the caret alone says which one is selected.
func TestRowsCarryNoPath(t *testing.T) {
	m := newTestModel(t, 4, 80, 30)
	m.cursor = 2
	cols := m.rowColumns(80)

	selected := plain(m.renderRow(2, 80, cols))
	if !strings.HasPrefix(selected, cursorGlyph) {
		t.Errorf("selected row does not start with the cursor: %q", selected)
	}
	for _, i := range []int{0, 2} {
		row := plain(m.renderRow(i, 80, cols))
		if want := m.worktrees[m.filtered[i]].DisplayPath; strings.Contains(row, want) {
			t.Errorf("row %d spelled out its path: %q", i, row)
		}
	}
	if strings.Contains(plain(m.renderRow(0, 80, cols)), cursorGlyph) {
		t.Errorf("unselected row drew a cursor")
	}
}

// The path moved out of the list, but it did not leave: the one-column layout
// keeps it at a fixed row of its own.
func TestSelectedPathSitsBelowTheList(t *testing.T) {
	m := newTestModel(t, 4, 80, 30)
	m.cursor = 2

	line := plain(m.renderSelectedPath(80))
	if want := m.worktrees[m.filtered[2]].DisplayPath; !strings.Contains(line, want) {
		t.Errorf("the path line is missing %q: %q", want, line)
	}
}

// The list viewport must never change height, or the footer would jump as
// worktrees are created, filtered, or removed.
func TestRenderCardsFillsListHeight(t *testing.T) {
	for _, n := range []int{0, 1, 4, 40} {
		for _, height := range []int{14, 24, 25, 50} {
			m := newTestModel(t, n, 80, height)
			if got := len(m.renderRows(80)); got != m.listHeight() {
				t.Errorf("n=%d height=%d: got %d rows, want %d", n, height, got, m.listHeight())
			}
		}
	}
}

func TestRenderCardsShowsScrollHint(t *testing.T) {
	m := newTestModel(t, 40, 80, 24)
	out := strings.Join(m.renderRows(80), "\n")
	if !strings.Contains(out, "below") {
		t.Errorf("expected a scroll hint when worktrees overflow the viewport:\n%s", out)
	}

	m.cursor = len(m.filtered) - 1
	m.adjustScroll()
	out = strings.Join(m.renderRows(80), "\n")
	if !strings.Contains(out, "above") {
		t.Errorf("expected an above hint once scrolled to the end:\n%s", out)
	}
}

// Every line of the list is padded to exactly the terminal width, so the
// columns line up down the list whatever each row is carrying.
func TestRenderRowsShareWidth(t *testing.T) {
	for _, width := range []int{44, 60, 80, 120} {
		m := newTestModel(t, 4, width, 30)
		cols := m.rowColumns(width)
		for i := range m.filtered {
			if got := lipgloss.Width(m.renderRow(i, width, cols)); got != width {
				t.Errorf("width=%d row=%d: %d columns, want %d", width, i, got, width)
			}
		}
	}
}

// A narrow terminal drops columns from the right rather than squeezing the
// branch name away.
func TestNarrowRowsShedColumns(t *testing.T) {
	wide := newTestModel(t, 4, 120, 30).rowColumns(120)
	if wide.subject == 0 || wide.sync == 0 || wide.age == 0 {
		t.Fatalf("a wide terminal should keep every column: %+v", wide)
	}
	narrow := newTestModel(t, 4, minViewWidth, 30).rowColumns(minViewWidth)
	if narrow.branch < minBranchWidth && narrow.subject != 0 {
		t.Errorf("narrow terminal kept the subject while starving the branch: %+v", narrow)
	}
}

// The column labels only earn their row if they sit exactly over the values
// they name, at every width and whichever columns the width has shed.
func TestColumnHeaderAlignsWithRows(t *testing.T) {
	for _, width := range []int{44, 60, 80, 120} {
		m := newTestModel(t, 3, width, 30)
		c := m.rowColumns(width)
		header := plain(m.renderColumnHeader(c))
		row := plain(m.renderRow(0, width, c))
		wt := m.worktrees[m.filtered[0]]

		// col is where sub starts, in display columns rather than bytes.
		col := func(t *testing.T, s, sub string) int {
			t.Helper()
			i := strings.Index(s, sub)
			if i < 0 {
				t.Fatalf("width=%d: %q is missing from %q", width, sub, s)
			}
			return lipgloss.Width(s[:i])
		}

		if strings.Contains(header, "…") {
			t.Errorf("width=%d: a column label was truncated: %q", width, header)
		}
		// branch and the subject are left-aligned: their left edges line up.
		if got, want := col(t, header, labelBranch), col(t, row, m.rowLabel(wt)); got != want {
			t.Errorf("width=%d: branch label at column %d, value at %d", width, got, want)
		}
		if c.subject > 0 {
			head := truncate(wt.Subject, c.subject)
			head = strings.TrimSuffix(head, "…")
			if got, want := col(t, header, labelSubject), col(t, row, head); got != want {
				t.Errorf("width=%d: subject label at column %d, value at %d", width, got, want)
			}
		}
		// changes is right-aligned: the right edges line up instead.
		changes := gitStateText(wt)
		got := col(t, header, labelChanges) + lipgloss.Width(labelChanges)
		want := col(t, row, changes) + lipgloss.Width(changes)
		if got != want {
			t.Errorf("width=%d: changes label ends at column %d, value at %d", width, got, want)
		}
	}
}

func TestViewNeverExceedsTerminalWidth(t *testing.T) {
	modes := map[string]inputMode{
		"normal": modeNormal,
		"search": modeSearch,
		"create": modeCreate,
		"delete": modeConfirmDelete,
		"detail": modeDetail,
	}
	for name, mode := range modes {
		// n=0 covers the empty state, which centers itself rather than
		// following the columns.
		for _, n := range []int{0, 6} {
			for _, width := range []int{50, 80, 120} {
				m := newTestModel(t, n, width, 24)
				m.mode = mode
				m.detailCommits = testCommits(6)
				m.textInput.Placeholder = "filter by branch or path…"
				m.syncInputWidth()
				for _, line := range strings.Split(m.View().Content, "\n") {
					if w := lipgloss.Width(line); w > width {
						t.Errorf("%s n=%d width=%d: line of %d columns: %q", name, n, width, w, line)
					}
				}
			}
		}
	}
}

// An over-wide input row pushed the card's right border out of alignment.
func TestOverlayCardRowsShareWidth(t *testing.T) {
	for _, width := range []int{50, 80, 120} {
		m := newTestModel(t, 3, width, 24)
		m.mode = modeCreate
		m.textInput.Placeholder = "branch name"
		m.syncInputWidth()

		m.detailCommits = testCommits(6)
		cards := map[string]string{
			"create": m.renderCreateCard(width),
			"delete": m.renderDeleteCard(width),
			"detail": m.renderDetailCard(width),
		}
		for name, card := range cards {
			rows := strings.Split(card, "\n")
			want := lipgloss.Width(rows[0])
			for i, row := range rows {
				if got := lipgloss.Width(row); got != want {
					t.Errorf("%s width=%d: row %d is %d columns, want %d:\n%s",
						name, width, i, got, want, plain(card))
					break
				}
			}
		}
	}
}

func testCommits(n int) []git.Commit {
	cs := make([]git.Commit, 0, n)
	for i := 1; i <= n; i++ {
		cs = append(cs, git.Commit{
			// Ten columns, not seven: git abbreviates a sha to whatever is
			// unambiguous in the repository, and a big one runs long.
			SHA:     fmt.Sprintf("%010d", i),
			Subject: "commit subject " + strings.Repeat("long ", i),
			When:    time.Now().Add(-time.Duration(i) * time.Hour),
		})
	}
	return cs
}

// The detail pane floats over the list, so it has to fit the terminal it
// floats in — otherwise the compositor clips its bottom border away.
func TestDetailCardFitsTerminalHeight(t *testing.T) {
	for _, height := range []int{minViewHeight, 16, 20, 24, 40, 60} {
		m := newTestModel(t, 6, 80, height)
		m.mode = modeDetail
		m.detailCommits = testCommits(detailCommitLimit)
		card := m.renderDetailCard(80)
		if got := lipgloss.Height(card); got > height {
			t.Errorf("height=%d: card is %d rows:\n%s", height, got, plain(card))
		}
	}
}

// The pane exists to spell out what the row only had glyphs for: the full
// path, the git state in words, and the commits behind the branch.
func TestDetailCardShowsStateAndHistory(t *testing.T) {
	m := newTestModel(t, 6, 80, 40)
	m.mode = modeDetail
	m.cursor = 2
	m.detailCommits = testCommits(3)
	wt := m.worktrees[m.filtered[2]]

	out := plain(m.renderDetailCard(80))
	for _, want := range []string{
		wt.Label(), wt.DisplayPath, wt.Head,
		"uncommitted", "ahead of main", "history", "0000000001",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("detail pane is missing %q:\n%s", want, out)
		}
	}
}

// A worktree whose history has not arrived yet, or has none, still renders a
// well-formed card rather than a heading over nothing.
func TestDetailCardWithoutHistory(t *testing.T) {
	for name, setup := range map[string]func(*model){
		"loading": func(m *model) { m.detailLoading = true },
		"empty":   func(m *model) {},
		"error":   func(m *model) { m.detailErr = errors.New("reading history: exit 128") },
	} {
		m := newTestModel(t, 3, 80, 24)
		m.mode = modeDetail
		setup(&m)
		rows := strings.Split(m.renderDetailCard(80), "\n")
		want := lipgloss.Width(rows[0])
		for i, row := range rows {
			if got := lipgloss.Width(row); got != want {
				t.Fatalf("%s: row %d is %d columns, want %d", name, i, got, want)
			}
		}
		for _, row := range rows {
			if strings.TrimSpace(plain(row)) == "history" {
				t.Errorf("%s: drew a history heading with no commits", name)
			}
		}
	}
}

// The history block measures its own sha and age columns. renderFrame clips
// an overlong line, so a subject sized against a guessed prefix would still
// look square — it would just lose its last words for no reason. Check the
// lines before the frame ever sees them.
func TestDetailHistoryLinesFitInnerWidth(t *testing.T) {
	const innerW = 60
	m := newTestModel(t, 3, 80, 40)
	m.detailCommits = testCommits(4)
	// Subjects longer than the pane can hold: a column sized too wide only
	// shows up once the text is long enough to reach the border.
	for i := range m.detailCommits {
		m.detailCommits[i].Subject = "commit subject " + strings.Repeat("long ", 20)
	}
	// A short sha among long ones: the column is as wide as the widest, and
	// the narrow one is padded up to it rather than shifting its subject left.
	m.detailCommits[2].SHA = "abc1234"

	// Every pane line opens with a one-column left margin, which sits outside
	// the innerW its content is sized against.
	const budget = innerW + 1

	var subjectCol = -1
	for _, ln := range m.detailHistoryLines(innerW) {
		if got := lipgloss.Width(ln.text); got > budget {
			t.Errorf("history line is %d columns, want at most %d:\n%s", got, budget, plain(ln.text))
		}
		if at := strings.Index(plain(ln.text), "commit subject"); at >= 0 {
			if subjectCol >= 0 && at != subjectCol {
				t.Errorf("subject starts at column %d, want %d:\n%s", at, subjectCol, plain(ln.text))
			}
			subjectCol = at
		}
	}
	if subjectCol < 0 {
		t.Fatal("no commit subjects rendered")
	}
}

func TestTruncate(t *testing.T) {
	cases := []struct {
		in   string
		w    int
		want string
	}{
		{"feat/parser", 20, "feat/parser"},
		{"feat/parser", 11, "feat/parser"},
		{"feat/parser", 6, "feat/…"},
		{"héllo", 3, "hé…"},
		{"anything", 0, ""},
	}
	for _, c := range cases {
		if got := truncate(c.in, c.w); got != c.want {
			t.Errorf("truncate(%q, %d) = %q, want %q", c.in, c.w, got, c.want)
		}
	}
}

var ansiPattern = regexp.MustCompile(`\x1b\[[0-9;]*m`)

// plain strips styling. bubbles renders the placeholder's first character
// separately as the virtual cursor, so the text is only contiguous once the
// escape sequences between spans are gone.
func plain(s string) string { return ansiPattern.ReplaceAllString(s, "") }

// bubbles copies the placeholder into a buffer of Width+1 runes, so an input
// left at width 0 renders exactly one character of it ("b" for "branch name").
func TestPromptPlaceholdersRenderInFull(t *testing.T) {
	for _, width := range []int{50, 80, 120} {
		create := newTestModel(t, 3, width, 24)
		create.mode = modeCreate
		create.textInput.Placeholder = "branch name"
		create.syncInputWidth()
		if got := plain(create.renderCreateCard(width)); !strings.Contains(got, "branch name") {
			t.Errorf("width=%d: create card lost its placeholder:\n%s", width, got)
		}

		search := newTestModel(t, 3, width, 24)
		search.mode = modeSearch
		search.textInput.Placeholder = "filter by branch or path…"
		search.syncInputWidth()
		if got := plain(search.renderStatusLine()); !strings.Contains(got, "filter by branch or path…") {
			t.Errorf("width=%d: search line lost its placeholder: %q", width, got)
		}
	}
}

// A terminal wide enough gets two columns. Nothing may run past the content
// width, and the pane's right border must land on the same column as the last
// character of the top bar — the layout has one right edge, not two.
func TestSplitLayoutColumnsLineUp(t *testing.T) {
	for _, width := range []int{splitMinWidth, 140, 160, 240} {
		m := newTestModel(t, 4, width, 30)
		if !m.splitView() {
			t.Fatalf("width=%d did not split", width)
		}
		edge := m.listWidth() + paneGap + m.paneWidth()
		if edge != m.contentWidth()-1 {
			t.Errorf("width=%d: the two columns span %d of %d", width, edge, m.contentWidth())
		}
		for i, ln := range strings.Split(m.View().Content, "\n") {
			if got := lipgloss.Width(ln); got > m.contentWidth() {
				t.Errorf("width=%d line %d runs to %d columns, past %d: %q",
					width, i, got, m.contentWidth(), plain(ln))
			}
			if got := lipgloss.Width(strings.TrimRight(plain(ln), " ")); got > edge {
				t.Errorf("width=%d line %d has ink at column %d, past the edge at %d: %q",
					width, i, got, edge, plain(ln))
			}
		}
	}
}

// Below splitMinWidth there is one column and no pane.
func TestNarrowTerminalKeepsOneColumn(t *testing.T) {
	m := newTestModel(t, 4, splitMinWidth-1, 30)
	if m.splitView() {
		t.Fatal("a terminal under splitMinWidth split anyway")
	}
	if m.paneWidth() != 0 {
		t.Errorf("a one-column layout reserved %d columns for a pane", m.paneWidth())
	}
	if m.listWidth() != m.contentWidth() {
		t.Errorf("the list got %d of %d columns", m.listWidth(), m.contentWidth())
	}
}

// The pane is as tall as the list beside it, whatever it has to say.
func TestPaneFillsTheListHeight(t *testing.T) {
	for _, height := range []int{minViewHeight, 20, 24, 50} {
		m := newTestModel(t, 4, 160, height)
		want := 1 + m.listHeight() // the column header, then the rows
		if got := len(m.renderPane(m.paneWidth(), want)); got != want {
			t.Errorf("height=%d: pane is %d lines, want %d", height, got, want)
		}
	}
}

// With nothing to list there is nothing to describe, so the empty state gets
// the whole width back.
func TestEmptyListDropsThePane(t *testing.T) {
	m := newTestModel(t, 0, 160, 24)
	if m.splitView() {
		t.Error("an empty list still drew a pane beside itself")
	}
}

// The list keeps exactly the same number of rows either way; the pane is paid
// for out of the width, not the height.
func TestBothLayoutsFillTheListHeight(t *testing.T) {
	for _, width := range []int{80, 160} {
		for _, n := range []int{0, 1, 4, 40} {
			m := newTestModel(t, n, width, 30)
			if got := len(m.renderRows(m.listWidth())); got != m.listHeight() {
				t.Errorf("width=%d n=%d: %d rows, want %d", width, n, got, m.listHeight())
			}
		}
	}
}

// The worktree you launched from is marked: knowing where you already are is
// half of deciding where to go.
func TestTheCurrentWorktreeIsMarked(t *testing.T) {
	m := newTestModel(t, 3, 80, 30)
	here := m.worktrees[m.filtered[1]]
	m.hereRoot = here.Path
	// The fixture shares one path across every worktree, so give this one its own.
	m.worktrees[m.filtered[1]].Path = here.Path + "/here"
	m.hereRoot = here.Path + "/here"

	if got := m.rowLabel(m.worktrees[m.filtered[1]]); !strings.HasPrefix(got, hereGlyph) {
		t.Errorf("the current worktree is unmarked: %q", got)
	}
	if got := m.rowLabel(m.worktrees[m.filtered[0]]); strings.HasPrefix(got, hereGlyph) {
		t.Errorf("another worktree wears the mark: %q", got)
	}

	var state string
	for _, f := range m.detailFields(m.worktrees[m.filtered[1]]) {
		if f.label == "state" {
			state = f.value
		}
	}
	if state != "you are here" {
		t.Errorf("the detail says %q, want %q", state, "you are here")
	}
}

// With no current worktree — mori launched from the main working tree, which
// the list does not carry — nothing is marked.
func TestNoMarkWithoutACurrentWorktree(t *testing.T) {
	m := newTestModel(t, 3, 80, 30)
	for _, i := range m.filtered {
		if strings.HasPrefix(m.rowLabel(m.worktrees[i]), hereGlyph) {
			t.Errorf("a row is marked with no current worktree set")
		}
	}
}
