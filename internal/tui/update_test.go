package tui

import (
	"errors"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
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
	still := m.renderRow(1, 80, cols)

	m.sweepBranch = m.worktrees[m.filtered[1]].Branch
	for f := 0; f <= sweepFrames; f++ {
		m.sweepFrame = f
		row := m.renderRow(1, 80, cols)
		if plain(row) != plain(still) {
			t.Fatalf("frame %d: row reads %q, want %q", f, plain(row), plain(still))
		}
	}
}

// enter is the whole point of the session: it names a worktree and quits, and
// Run prints that path for the shell function to cd into.
func TestEnterPicksTheWorktreeUnderTheCursor(t *testing.T) {
	m := newTestModel(t, 3, 80, 24)
	m.cursor = 2

	next, cmd := m.handleNormalKey("enter")
	m = next.(model)

	if cmd == nil {
		t.Fatal("enter did not quit")
	}
	if m.selected != m.filtered[2] {
		t.Fatalf("enter selected %d, want %d", m.selected, m.filtered[2])
	}
}

// With nothing on show there is nothing to hand back, and enter must not quit
// on an empty list — the user is one `esc` away from having a list again.
func TestEnterOnAnEmptyListDoesNothing(t *testing.T) {
	m := newTestModel(t, 0, 80, 24)

	next, cmd := m.handleNormalKey("enter")
	m = next.(model)

	if cmd != nil {
		t.Fatal("enter quit with nothing selected")
	}
	if m.selected != -1 {
		t.Fatalf("enter selected %d on an empty list", m.selected)
	}
}

// The detail pane offers the same pick as the row it floats over.
func TestEnterPicksFromTheDetailPane(t *testing.T) {
	m := newTestModel(t, 3, 80, 24)
	m.mode = modeDetail
	m.cursor = 1

	next, cmd := m.handleDetailKey("enter")
	m = next.(model)

	if cmd == nil {
		t.Fatal("enter in the detail pane did not quit")
	}
	if m.selected != m.filtered[1] {
		t.Fatalf("detail enter selected %d, want %d", m.selected, m.filtered[1])
	}
}

// OSC 52 can be swallowed without a word, so the yank message must not claim
// the clipboard took anything — it spells the path out instead.
func TestYankMessageCarriesThePath(t *testing.T) {
	m := newTestModel(t, 3, 80, 24)

	next, _ := m.handleNormalKey("y")
	m = next.(model)

	if m.statusMsg == nil {
		t.Fatal("y left no status message")
	}
	if want := m.worktrees[m.filtered[m.cursor]].DisplayPath; !strings.Contains(m.statusMsg.text, want) {
		t.Errorf("yank message %q does not carry the path %q", m.statusMsg.text, want)
	}
	if strings.Contains(m.statusMsg.text, "clipboard") {
		t.Errorf("yank message claims the clipboard worked: %q", m.statusMsg.text)
	}
}

// `y` yanks a path one mode over. Binding it to "delete" as well would put an
// unrecoverable action under a key the hand already reaches for.
func TestDeleteConfirmationIgnoresY(t *testing.T) {
	m := newTestModel(t, 3, 80, 24)
	m.worktrees[m.filtered[0]].Dirty = 0
	m.cursor = 0
	next, _ := m.handleNormalKey("d")
	m = next.(model)

	for _, key := range []string{"y", "Y"} {
		after, cmd := m.handleDeleteKey(tea.KeyPressMsg{}, key)
		if cmd != nil {
			t.Fatalf("%q started a removal from the confirmation", key)
		}
		if after.(model).mode != modeConfirmDelete {
			t.Fatalf("%q left the confirmation", key)
		}
	}
}

// A clean worktree is a checkout away from coming back, so enter is enough.
func TestEnterDeletesACleanWorktree(t *testing.T) {
	m := newTestModel(t, 3, 80, 24)
	m.worktrees[m.filtered[1]].Dirty = 0
	m.cursor = 1
	next, _ := m.handleNormalKey("d")
	m = next.(model)

	if m.deleteNeedsName {
		t.Fatal("a clean worktree asked for its name to be typed")
	}
	after, cmd := m.handleDeleteKey(tea.KeyPressMsg{}, "enter")
	if cmd == nil {
		t.Fatal("enter did not remove the clean worktree")
	}
	if after.(model).mode != modeNormal {
		t.Fatal("the confirmation stayed open after enter")
	}
}

// A dirty one holds until its branch name is typed out: those files are the
// one thing undo cannot bring back.
func TestDirtyWorktreeNeedsItsNameTyped(t *testing.T) {
	m := newTestModel(t, 3, 80, 24)
	m.cursor = 0
	wt := m.worktrees[m.filtered[0]]
	if wt.Dirty == 0 {
		t.Fatal("test fixture is not dirty")
	}
	next, _ := m.handleNormalKey("d")
	m = next.(model)

	if !m.deleteNeedsName {
		t.Fatal("a dirty worktree went without asking for its name")
	}
	if _, cmd := m.handleDeleteKey(tea.KeyPressMsg{}, "enter"); cmd != nil {
		t.Fatal("enter removed a dirty worktree on an empty confirmation")
	}

	m.textInput.SetValue(wt.Label()[:2])
	if _, cmd := m.handleDeleteKey(tea.KeyPressMsg{}, "enter"); cmd != nil {
		t.Fatal("enter removed a dirty worktree on a half-typed name")
	}

	m.textInput.SetValue(wt.Label())
	if _, cmd := m.handleDeleteKey(tea.KeyPressMsg{}, "enter"); cmd == nil {
		t.Fatal("the typed-out name did not release the removal")
	}
}

// A removal arms undo, and `u` spends it exactly once.
func TestUndoRestoresTheLastRemoval(t *testing.T) {
	m := newTestModel(t, 3, 80, 24)

	if _, cmd := m.handleNormalKey("u"); cmd != nil {
		t.Fatal("u did something with nothing to restore")
	}

	next, _ := m.Update(worktreeRemovedMsg{removed: &removedWorktree{
		branch: "feat/gone", path: "/w/feat/gone", displayPath: "~/w/feat/gone",
	}})
	m = next.(model)
	if m.undo == nil {
		t.Fatal("a removal left nothing to undo")
	}
	if !strings.Contains(m.statusMsg.text, "u to restore") {
		t.Errorf("the removal did not offer the undo: %q", m.statusMsg.text)
	}

	after, cmd := m.handleNormalKey("u")
	m = after.(model)
	if cmd == nil {
		t.Fatal("u did not start a restore")
	}
	if m.undo != nil {
		t.Error("u left the undo armed for a second spend")
	}
}

// A detached worktree has no branch to check back out, so nothing is offered.
func TestDetachedRemovalArmsNoUndo(t *testing.T) {
	m := newTestModel(t, 3, 80, 24)
	next, _ := m.Update(worktreeRemovedMsg{})
	if next.(model).undo != nil {
		t.Fatal("a removal with no branch armed the undo anyway")
	}
}

// The pane follows the cursor, but only after it stops: a held-down `j` must
// not fork a `git log` for every row it passes.
func TestPaneFollowsTheCursorOnceItSettles(t *testing.T) {
	m := newTestModel(t, 4, 160, 30)
	if !m.splitView() {
		t.Fatal("a 160-column terminal did not split")
	}

	next, cmd := m.Update(tea.KeyPressMsg{Code: 'j', Text: "j"})
	m = next.(model)
	if cmd == nil {
		t.Fatal("moving the cursor scheduled no history load")
	}
	want := m.worktrees[m.filtered[m.cursor]].Label()
	if m.detailBranch != want {
		t.Fatalf("the pane points at %q, want %q", m.detailBranch, want)
	}
	if m.detailLoading {
		t.Error("the pane started loading before the debounce elapsed")
	}

	// The load that the first move scheduled arrives after a second move.
	stale := detailWantedMsg{seq: m.detailSeq, branch: m.detailBranch, path: "/w"}
	next, _ = m.Update(tea.KeyPressMsg{Code: 'j', Text: "j"})
	m = next.(model)
	if after, cmd := m.Update(stale); cmd != nil || after.(model).detailLoading {
		t.Error("a stale history request survived the cursor moving on")
	}

	fresh := detailWantedMsg{seq: m.detailSeq, branch: m.detailBranch, path: "/w"}
	after, cmd := m.Update(fresh)
	if cmd == nil || !after.(model).detailLoading {
		t.Error("the current history request was dropped")
	}
}

// On a narrow terminal `i` floats the detail; on a wide one the detail is
// already there, so the key folds the pane away instead.
func TestDetailKeyTogglesThePaneWhenThereIsOne(t *testing.T) {
	wide := newTestModel(t, 4, 160, 30)
	next, _ := wide.handleNormalKey("i")
	wide = next.(model)
	if wide.mode == modeDetail {
		t.Error("a wide terminal floated a second copy of the pane")
	}
	if wide.paneOpen {
		t.Error("i did not fold the pane away")
	}
	next, _ = wide.handleNormalKey("i")
	if !next.(model).paneOpen {
		t.Error("i did not bring the pane back")
	}

	narrow := newTestModel(t, 4, 80, 30)
	next, _ = narrow.handleNormalKey("i")
	if next.(model).mode != modeDetail {
		t.Error("a narrow terminal did not float the detail")
	}
}

// A hook that fails holds the card open with what it wrote. The four-second
// status line it used to get was never room to say why npm fell over.
func TestFailedHookHoldsTheCardOpen(t *testing.T) {
	m := newTestModel(t, 3, 80, 24)
	m.mode = modeCreating
	m.creatingBranch = "feat/new"
	m.creatingSteps = []creatingStep{
		{name: "Creating branch from main", cmd: "git worktree add …", state: stepSucceeded},
		{name: "install deps", cmd: "npm install", state: stepRunning},
	}

	next, _ := m.Update(stepCompletedMsg{
		name: "install deps", success: false, output: "npm ERR! code ELOCKVERIFY\nnpm ERR! stale lockfile",
	})
	m = next.(model)
	if got := m.creatingSteps[1].output; !strings.Contains(got, "ELOCKVERIFY") {
		t.Fatalf("the failed step kept no output: %q", got)
	}

	next, _ = m.Update(worktreeCreatedMsg{warnings: []string{"install deps"}})
	m = next.(model)
	if m.mode != modeCreating || !m.creatingDone {
		t.Fatalf("the card closed over a failed hook: mode=%v done=%v", m.mode, m.creatingDone)
	}
	if card := plain(m.renderCreatingCard(80)); !strings.Contains(card, "stale lockfile") {
		t.Errorf("the card does not show what the step wrote:\n%s", card)
	}

	after, cmd := m.handleCreatingKey("esc")
	if cmd == nil {
		t.Error("dismissing the card did not refresh the list")
	}
	if after.(model).mode != modeNormal {
		t.Error("esc did not dismiss the card")
	}
}

// A create that goes fine closes on its own — no keystroke to collect.
func TestCleanCreateClosesItsOwnCard(t *testing.T) {
	m := created(t, "feat/new")
	if m.mode != modeNormal || m.creatingDone {
		t.Fatalf("a clean create held its card open: mode=%v done=%v", m.mode, m.creatingDone)
	}
}

// Nothing gets out of a create that is still running but a hard quit.
func TestRunningCreateTakesNoKeys(t *testing.T) {
	m := newTestModel(t, 3, 80, 24)
	m.mode = modeCreating
	m.creatingSteps = []creatingStep{{name: "install deps", cmd: "npm i", state: stepRunning}}

	for _, key := range []string{"esc", "enter", "q"} {
		after, cmd := m.handleCreatingKey(key)
		if cmd != nil || after.(model).mode != modeCreating {
			t.Errorf("%q got out of a running create", key)
		}
	}
	if _, cmd := m.handleCreatingKey("ctrl+c"); cmd == nil {
		t.Error("ctrl+c did not quit a running create")
	}
}

// A list that keeps coming back the same earns a longer wait. Every beat is
// four git processes per worktree, and a repository nobody is touching should
// not cost that every fifteen seconds forever.
func TestUnchangedListBacksOff(t *testing.T) {
	m := newTestModel(t, 3, 80, 24)
	wts := testWorktrees(3)

	next, _ := m.Update(refreshedMsg{worktrees: wts})
	m = next.(model)
	if m.refreshInterval != refreshEvery {
		t.Fatalf("the first list moved the beat to %v", m.refreshInterval)
	}

	for i := 0; i < 8; i++ {
		next, _ = m.Update(refreshedMsg{worktrees: wts})
		m = next.(model)
	}
	if m.refreshInterval <= refreshEvery {
		t.Errorf("eight identical lists left the beat at %v", m.refreshInterval)
	}
	if m.refreshInterval > refreshMax {
		t.Errorf("the beat ran past its cap: %v", m.refreshInterval)
	}

	changed := testWorktrees(3)
	changed[0].Dirty++
	next, _ = m.Update(refreshedMsg{worktrees: changed})
	if got := next.(model).refreshInterval; got != refreshEvery {
		t.Errorf("a changed list left the beat at %v", got)
	}
}

// Anything the user does means look again soon.
func TestAKeypressResetsTheBeat(t *testing.T) {
	m := newTestModel(t, 3, 80, 24)
	m.refreshInterval = refreshMax

	next, _ := m.Update(tea.KeyPressMsg{Code: 'j', Text: "j"})
	if got := next.(model).refreshInterval; got != refreshEvery {
		t.Errorf("a keypress left the beat at %v", got)
	}
}

// An unfocused window is not polled, and regaining focus looks immediately.
func TestBlurredWindowIsNotPolled(t *testing.T) {
	m := newTestModel(t, 3, 80, 24)

	next, _ := m.Update(tea.BlurMsg{})
	m = next.(model)
	if m.focused {
		t.Fatal("a blur left the window focused")
	}
	// The tick still reschedules itself; it just does not query git. A single
	// command back means the beat and nothing else.
	if _, cmd := m.Update(tickMsg(time.Now())); cmd == nil {
		t.Error("the beat stopped entirely while blurred")
	}

	next, cmd := m.Update(tea.FocusMsg{})
	m = next.(model)
	if !m.focused || cmd == nil {
		t.Error("regaining focus did not look again")
	}
	if m.refreshInterval != refreshEvery {
		t.Errorf("regaining focus left the beat at %v", m.refreshInterval)
	}
}

// mori draws before it knows what to draw, and says so.
func TestFirstFrameSaysItIsStillLooking(t *testing.T) {
	m := newModel(nil, "~/repo", "main")
	m.width, m.height = 80, 24
	if !m.loading {
		t.Fatal("a model built with no worktrees did not start out loading")
	}
	if out := plain(m.View().Content); !strings.Contains(out, "counting the trees") {
		t.Errorf("the first frame does not say it is still looking:\n%s", out)
	}

	next, _ := m.Update(refreshedMsg{worktrees: testWorktrees(2)})
	if next.(model).loading {
		t.Error("the first list did not clear the loading state")
	}
}

// The filter is a guess at a half-remembered name, so the runes need only
// appear in order.
func TestFuzzyFilterFindsScatteredRunes(t *testing.T) {
	m := newTestModel(t, 3, 80, 24)
	m.worktrees = []internal.Worktree{
		{Branch: "feat/parser", Path: "/a", DisplayPath: "~/a"},
		{Branch: "chore/deps", Path: "/b", DisplayPath: "~/b"},
		{Branch: "feature-flags", Path: "/c", DisplayPath: "~/c"},
	}

	for _, tc := range []struct{ query, want string }{
		{"fp", "feat/parser"},     // a rune per word
		{"parser", "feat/parser"}, // the run itself
		{"deps", "chore/deps"},    // after the slash
		{"featureflags", "feature-flags"},
	} {
		m.textInput.SetValue(tc.query)
		m.lastQuery = "sentinel"
		m.applyFilter()
		if len(m.filtered) == 0 {
			t.Errorf("%q matched nothing", tc.query)
			continue
		}
		if got := m.worktrees[m.filtered[0]].Branch; got != tc.want {
			t.Errorf("%q ranked %q first, want %q", tc.query, got, tc.want)
		}
	}

	m.textInput.SetValue("zzz")
	m.lastQuery = "sentinel"
	m.applyFilter()
	if len(m.filtered) != 0 {
		t.Errorf("a query matching nothing kept %d worktrees", len(m.filtered))
	}
}

// A run of adjacent runes is the hit that feels right, so it must outrank the
// same letters scattered through a longer name.
func TestFuzzyRankingPrefersTheTighterHit(t *testing.T) {
	tight, _, ok := fuzzyMatch("parser", "parse")
	if !ok {
		t.Fatal("the exact run did not match")
	}
	loose, _, ok2 := fuzzyMatch("please-add-real-server-endpoint", "parse")
	if !ok2 {
		t.Fatal("the scattered runes did not match")
	}
	_, tightScore, _ := fuzzyMatch("parser", "parse")
	_, looseScore, _ := fuzzyMatch("please-add-real-server-endpoint", "parse")
	if tightScore <= looseScore {
		t.Errorf("the tight hit scored %d, the loose one %d", tightScore, looseScore)
	}
	if len(tight) != 5 || len(loose) != 5 {
		t.Errorf("a five-rune query matched %d and %d positions", len(tight), len(loose))
	}
}

// A branch hit beats the same query found in a path: you are usually naming
// a branch.
func TestBranchHitsOutrankPathHits(t *testing.T) {
	m := newTestModel(t, 3, 80, 24)
	m.worktrees = []internal.Worktree{
		{Branch: "chore/deps", Path: "/a", DisplayPath: "~/work/parser/a"},
		{Branch: "feat/parser", Path: "/b", DisplayPath: "~/b"},
	}
	m.textInput.SetValue("parser")
	m.lastQuery = "sentinel"
	m.applyFilter()

	if len(m.filtered) != 2 {
		t.Fatalf("expected both worktrees, got %d", len(m.filtered))
	}
	if got := m.worktrees[m.filtered[0]].Branch; got != "feat/parser" {
		t.Errorf("the path hit ranked first: %q", got)
	}
}

// A rebuild the cursor did not ask for — a background refresh, an archive
// toggle — must leave it on the same worktree. Otherwise a reordered list can
// slide a different one under a key about to be pressed.
func TestCursorHoldsItsWorktreeAcrossARefresh(t *testing.T) {
	m := newTestModel(t, 4, 80, 24)
	m.sortMode = internal.SortRecent
	m.applyFilter()
	m.cursor = 2
	want := m.worktrees[m.filtered[2]].Branch

	// The same worktrees, reordered by a fresh commit on the last one.
	wts := testWorktrees(4)
	wts[3].LastCommit = time.Now().Add(time.Hour)
	next, _ := m.Update(refreshedMsg{worktrees: wts})
	m = next.(model)

	if got := m.worktrees[m.filtered[m.cursor]].Branch; got != want {
		t.Errorf("the refresh moved the cursor from %q to %q", want, got)
	}
}

// Typing is different: a new query puts the cursor on the best match rather
// than chasing whatever it was on.
func TestANewQueryMovesTheCursorToTheBestMatch(t *testing.T) {
	m := newTestModel(t, 4, 80, 24)
	m.cursor = 3

	m.textInput.SetValue("feat/x")
	m.applyFilter()
	if m.cursor != 0 {
		t.Errorf("a new query left the cursor at %d, want the top match", m.cursor)
	}
}

// The floating detail is a preview to walk the list with, not something to
// open and shut on every row.
func TestDetailCardFollowsTheCursor(t *testing.T) {
	m := newTestModel(t, 4, 80, 24) // too narrow to split, so `i` floats it
	next, _ := m.Update(tea.KeyPressMsg{Code: 'i', Text: "i"})
	m = next.(model)
	if m.mode != modeDetail {
		t.Fatal("i did not float the detail")
	}
	first := m.detailBranch

	next, cmd := m.Update(tea.KeyPressMsg{Code: 'j', Text: "j"})
	m = next.(model)
	if m.cursor != 1 {
		t.Fatalf("j did not move the cursor under the card: %d", m.cursor)
	}
	if m.mode != modeDetail {
		t.Fatal("j closed the card")
	}
	if m.detailBranch == first {
		t.Errorf("the card still describes %q after the cursor moved", first)
	}
	if cmd == nil {
		t.Error("moving under the card scheduled no history load")
	}
}
