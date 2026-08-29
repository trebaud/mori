package tui

import (
	"strings"
	"testing"
	"time"

	"charm.land/lipgloss/v2"

	"github.com/trebaud/mori/internal"
)

func testWorktrees(n int) []internal.Worktree {
	wts := make([]internal.Worktree, 0, n)
	wts = append(wts, internal.Worktree{
		Path: "/repo", Branch: "main", DisplayPath: "~/repo", Head: "aaaaaaa",
		IsMain: true, LastCommit: time.Now().Add(-2 * time.Hour),
	})
	for i := 1; i < n; i++ {
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

func TestRenderCardHasFixedHeight(t *testing.T) {
	m := newTestModel(t, 4, 80, 30)
	for i := range m.filtered {
		if got := len(m.renderCard(i, 80)); got != cardHeight {
			t.Errorf("card %d: got %d lines, want %d", i, got, cardHeight)
		}
	}
}

// The list viewport must never change height, or the footer would jump as
// worktrees are created, filtered, or removed.
func TestRenderCardsFillsListHeight(t *testing.T) {
	for _, n := range []int{0, 1, 4, 40} {
		for _, height := range []int{14, 24, 25, 50} {
			m := newTestModel(t, n, 80, height)
			if got := len(m.renderCards(80)); got != m.listHeight() {
				t.Errorf("n=%d height=%d: got %d rows, want %d", n, height, got, m.listHeight())
			}
		}
	}
}

func TestRenderCardsShowsScrollHint(t *testing.T) {
	m := newTestModel(t, 40, 80, 24)
	out := strings.Join(m.renderCards(80), "\n")
	if !strings.Contains(out, "more") {
		t.Errorf("expected a scroll hint when worktrees overflow the viewport:\n%s", out)
	}

	m.cursor = len(m.filtered) - 1
	m.adjustScroll()
	out = strings.Join(m.renderCards(80), "\n")
	if !strings.Contains(out, "above") {
		t.Errorf("expected an above hint once scrolled to the end:\n%s", out)
	}
}

// Every card row is padded to the same width so the right-hand column (age,
// HEAD) lines up down the list.
func TestRenderCardRowsShareWidth(t *testing.T) {
	for _, width := range []int{60, 80, 120} {
		m := newTestModel(t, 4, width, 30)
		for i := range m.filtered {
			rows := m.renderCard(i, width)
			first, second := lipgloss.Width(rows[0]), lipgloss.Width(rows[1])
			if first != second {
				t.Errorf("width=%d card=%d: row widths differ (%d vs %d)", width, i, first, second)
			}
			if first > width {
				t.Errorf("width=%d card=%d: row overflows terminal (%d)", width, i, first)
			}
		}
	}
}

func TestViewNeverExceedsTerminalWidth(t *testing.T) {
	for _, width := range []int{50, 80, 120} {
		m := newTestModel(t, 6, width, 24)
		for _, line := range strings.Split(m.View().Content, "\n") {
			if w := lipgloss.Width(line); w > width {
				t.Errorf("width=%d: line of %d columns: %q", width, w, line)
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
