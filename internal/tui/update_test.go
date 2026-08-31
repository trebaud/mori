package tui

import (
	"errors"
	"strings"
	"testing"
	"time"

	"charm.land/lipgloss/v2"

	"github.com/trebaud/mori/v2/internal"
)

var errAny = errors.New("boom")

// created returns the model as it stands just after a successful create, with
// the sweep armed and waiting for the row to arrive.
func created(t *testing.T, branch string) model {
	t.Helper()
	m := newTestModel(t, 3, 80, 24)
	m.mode = modeCreating
	m.creatingBranch = branch
	next, _ := m.Update(worktreeCreatedMsg{})
	return next.(model)
}

// The sweep waits for the refresh that carries its row: a highlight started
// while git is still being queried would be half spent before the worktree it
// points at was on screen.
func TestSweepStartsWhenTheRowArrives(t *testing.T) {
	m := created(t, "feat/new")
	if m.sweepBranch != "feat/new" || m.sweepFrame != 0 {
		t.Fatalf("create armed the sweep as %q frame %d", m.sweepBranch, m.sweepFrame)
	}

	wts := append(testWorktrees(3), internal.Worktree{
		Branch: "feat/new", Path: "/w", DisplayPath: "~/w", Head: "ccccccc", LastCommit: time.Now(),
	})
	next, cmd := m.Update(refreshedMsg{worktrees: wts})
	m = next.(model)

	if cmd == nil {
		t.Fatal("the refresh carrying the new worktree did not start the sweep")
	}
	if got := m.worktrees[m.filtered[m.cursor]].Branch; got != "feat/new" {
		t.Errorf("caret sits on %q, want the worktree just created", got)
	}

	// The sweep runs once and then gets out of the way.
	for i := 0; i <= sweepFrames; i++ {
		next, cmd = m.Update(sweepTickMsg{})
		m = next.(model)
	}
	if m.sweepBranch != "" || cmd != nil {
		t.Errorf("sweep still running after %d frames: %q", sweepFrames+1, m.sweepBranch)
	}
}

// Nothing to highlight is not something to keep waiting on.
func TestSweepDropsAWorktreeItCannotSee(t *testing.T) {
	m := created(t, "feat/new")
	next, cmd := m.Update(refreshedMsg{worktrees: testWorktrees(3)})
	if m = next.(model); m.sweepBranch != "" || cmd != nil {
		t.Errorf("sweep held on to %q, which is not in the list", m.sweepBranch)
	}
}

// A failed create has nothing to point at.
func TestFailedCreateDoesNotSweep(t *testing.T) {
	m := newTestModel(t, 3, 80, 24)
	m.mode = modeCreating
	m.creatingBranch = "feat/doomed"
	next, _ := m.Update(worktreeCreatedMsg{err: errAny})
	if got := next.(model).sweepBranch; got != "" {
		t.Errorf("armed a sweep for %q after the create failed", got)
	}
}

// The lit window travels the whole name and leaves it as it found it. Every
// frame must render the same columns, or the row would jitter as it sweeps.
func TestRenderSweepTravelsWithoutResizing(t *testing.T) {
	ApplyTheme(true)
	const label = "feat/parser"

	lit := 0
	for f := 0; f <= sweepFrames; f++ {
		out := renderSweep(label, f, textStyle)
		if got := plain(out); got != label {
			t.Fatalf("frame %d renders %q, want %q", f, got, label)
		}
		if lipgloss.Width(out) != lipgloss.Width(label) {
			t.Fatalf("frame %d is %d columns, want %d", f, lipgloss.Width(out), lipgloss.Width(label))
		}
		if strings.Contains(out, "\x1b[4m") || strings.Contains(out, ";4m") {
			lit++
		}
	}
	if lit < sweepFrames/2 {
		t.Errorf("only %d of %d frames lit anything", lit, sweepFrames+1)
	}
	if out := renderSweep(label, sweepFrames, textStyle); strings.Contains(out, ";4m") {
		t.Errorf("the last frame is still lit: %q", out)
	}
}

// The row is padded from the label's width, so a sweeping row has to stay
// exactly as wide as a still one.
func TestSweepingRowKeepsItsWidth(t *testing.T) {
	m := newTestModel(t, 4, 80, 24)
	cols := m.rowColumns(80)
	still := m.renderRow(1, 80, cols)[0]

	m.sweepBranch = m.worktrees[m.filtered[1]].Branch
	for f := 0; f <= sweepFrames; f++ {
		m.sweepFrame = f
		row := m.renderRow(1, 80, cols)[0]
		if plain(row) != plain(still) {
			t.Fatalf("frame %d: row reads %q, want %q", f, plain(row), plain(still))
		}
	}
}
