package tui

import (
	"regexp"
	"strings"
	"testing"
	"time"

	"charm.land/lipgloss/v2"

	"github.com/trebaud/mori/internal"
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

func TestRenderRowIsOneLine(t *testing.T) {
	m := newTestModel(t, 4, 80, 30)
	cols := m.rowColumns(80)
	for i := range m.filtered {
		row := m.renderRow(i, 80, cols)
		if strings.Contains(row, "\n") {
			t.Errorf("row %d spans more than one line: %q", i, plain(row))
		}
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
	if !strings.Contains(out, "more") {
		t.Errorf("expected a scroll hint when worktrees overflow the viewport:\n%s", out)
	}

	m.cursor = len(m.filtered) - 1
	m.adjustScroll()
	out = strings.Join(m.renderRows(80), "\n")
	if !strings.Contains(out, "above") {
		t.Errorf("expected an above hint once scrolled to the end:\n%s", out)
	}
}

// Every row is padded to exactly the terminal width, so the selected row's
// tint runs edge to edge and the columns line up down the list.
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
	if wide.head == 0 || wide.sync == 0 || wide.age == 0 {
		t.Fatalf("a wide terminal should keep every column: %+v", wide)
	}
	narrow := newTestModel(t, 4, minViewWidth, 30).rowColumns(minViewWidth)
	if narrow.branch < minBranchWidth && narrow.head != 0 {
		t.Errorf("narrow terminal kept HEAD while starving the branch: %+v", narrow)
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
		for _, width := range []int{50, 80, 120} {
			m := newTestModel(t, 6, width, 24)
			m.mode = mode
			m.textInput.Placeholder = "filter by branch or path…"
			m.syncInputWidth()
			for _, line := range strings.Split(m.View().Content, "\n") {
				if w := lipgloss.Width(line); w > width {
					t.Errorf("%s width=%d: line of %d columns: %q", name, width, w, line)
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
