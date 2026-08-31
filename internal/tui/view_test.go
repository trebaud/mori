package tui

import (
	"regexp"
	"strings"
	"testing"
	"time"

	"charm.land/lipgloss/v2"

	"github.com/trebaud/mori/v2/internal"
)

func testWorktrees(n int) []internal.Worktree {
	wts := make([]internal.Worktree, 0, n)
	for i := 1; i <= n; i++ {
		wts = append(wts, internal.Worktree{
			Path:        "/home/t/.mori/worktrees/repo/wt",
			Branch:      "feat/" + strings.Repeat("x", i),
			DisplayPath: "~/.mori/worktrees/repo/wt",
			Head:        "bbbbbbb",
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

// A worktree is one line, except the selected one, which adds its path.
func TestRenderRowHeights(t *testing.T) {
	m := newTestModel(t, 4, 80, 30)
	cols := m.rowColumns(80)
	for i := range m.filtered {
		want := rowHeight
		if i == m.cursor {
			want += detailHeight
		}
		lines := m.renderRow(i, 80, cols)
		if len(lines) != want {
			t.Errorf("row %d: %d lines, want %d", i, len(lines), want)
		}
		for _, ln := range lines {
			if strings.Contains(ln, "\n") {
				t.Errorf("row %d holds an embedded newline: %q", i, plain(ln))
			}
		}
	}
}

// The selected worktree shows the full path it is about to hand back, and
// only the selected one — a path under every row would bury the list.
func TestSelectedRowShowsPath(t *testing.T) {
	m := newTestModel(t, 4, 80, 30)
	m.cursor = 2
	cols := m.rowColumns(80)

	selected := plain(strings.Join(m.renderRow(2, 80, cols), "\n"))
	if want := m.worktrees[m.filtered[2]].DisplayPath; !strings.Contains(selected, want) {
		t.Errorf("selected row is missing its path %q:\n%s", want, selected)
	}
	if !strings.HasPrefix(selected, cursorGlyph) {
		t.Errorf("selected row does not start with the cursor: %q", selected)
	}

	other := plain(strings.Join(m.renderRow(0, 80, cols), "\n"))
	if strings.Contains(other, m.worktrees[m.filtered[0]].DisplayPath) {
		t.Errorf("unselected row spelled out its path: %q", other)
	}
	if strings.Contains(other, cursorGlyph) {
		t.Errorf("unselected row drew a cursor: %q", other)
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
			for j, ln := range m.renderRow(i, width, cols) {
				if got := lipgloss.Width(ln); got != width {
					t.Errorf("width=%d row=%d line=%d: %d columns, want %d", width, i, j, got, width)
				}
			}
		}
	}
}

// A narrow terminal drops columns from the right rather than squeezing the
// branch name away.
func TestNarrowRowsShedColumns(t *testing.T) {
	wide := newTestModel(t, 4, 120, 30).rowColumns(120)
	if wide.head == 0 || wide.sync == 0 || wide.age == 0 {
		t.Fatalf("a wide terminal should keep every column: %+v", wide)
	}
	narrow := newTestModel(t, 4, minViewWidth, 30).rowColumns(minViewWidth)
	if narrow.branch < minBranchWidth && narrow.head != 0 {
		t.Errorf("narrow terminal kept HEAD while starving the branch: %+v", narrow)
	}
}

// The column labels only earn their row if they sit exactly over the values
// they name, at every width and whichever columns the width has shed.
func TestColumnHeaderAlignsWithRows(t *testing.T) {
	for _, width := range []int{44, 60, 80, 120} {
		m := newTestModel(t, 3, width, 30)
		c := m.rowColumns(width)
		header := plain(m.renderColumnHeader(c))
		row := plain(m.renderRow(0, width, c)[0])
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
		// branch and head are left-aligned: their left edges line up.
		if got, want := col(t, header, labelBranch), col(t, row, m.rowLabel(wt)); got != want {
			t.Errorf("width=%d: branch label at column %d, value at %d", width, got, want)
		}
		if c.head > 0 {
			if got, want := col(t, header, labelHead), col(t, row, wt.Head); got != want {
				t.Errorf("width=%d: head label at column %d, value at %d", width, got, want)
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
	}
	for name, mode := range modes {
		// n=0 covers the empty state, which centers itself rather than
		// following the columns.
		for _, n := range []int{0, 6} {
			for _, width := range []int{50, 80, 120} {
				m := newTestModel(t, n, width, 24)
				m.mode = mode
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

		cards := map[string]string{
			"create": m.renderCreateCard(width),
			"delete": m.renderDeleteCard(width),
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
