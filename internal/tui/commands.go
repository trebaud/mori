package tui

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/trebaud/mori/v2/internal"
	"github.com/trebaud/mori/v2/internal/git"
)

// --- Effects (Elm: Cmd) ---

// tickCmd schedules the next background refresh beat, at whatever interval the
// model has backed off to.
func tickCmd(every time.Duration) tea.Cmd {
	return tea.Tick(every, func(t time.Time) tea.Msg { return tickMsg(t) })
}

// refreshCmd re-queries git off the UI goroutine.
func refreshCmd() tea.Cmd {
	return func() tea.Msg {
		wts, err := internal.ListLinked()
		return refreshedMsg{worktrees: wts, err: err}
	}
}

// stepStartedMsg fires when a creation step begins running.
type stepStartedMsg struct{ name string }

// stepCompletedMsg fires when a creation step finishes (success or failure).
type stepCompletedMsg struct {
	name    string
	success bool
	output  string
}

// spinnerTickMsg drives the spinner while the creating overlay is open.
type spinnerTickMsg struct{}

// planCreateSteps returns the steps CreateWorktree will perform, pre-populated
// so the overlay can show the whole plan up front.
func planCreateSteps(repoRoot, branch, base string) []creatingStep {
	baseBranch := base
	if baseBranch == "" {
		baseBranch = git.DefaultBranch(repoRoot)
	}
	dir := internal.TildePath(internal.WorktreeDir(repoRoot, branch))

	steps := []creatingStep{{
		name:  "Creating branch from " + baseBranch,
		cmd:   fmt.Sprintf("git worktree add %s -b %s %s", dir, branch, baseBranch),
		state: stepPending,
	}}

	for _, st := range internal.Load(repoRoot).PostCreate {
		steps = append(steps, creatingStep{name: st.Name, cmd: st.Cmd, state: stepPending})
	}
	return steps
}

// startCreateWorktreeCmd kicks off worktree creation in a goroutine, streaming
// per-step progress over a channel. The returned channel is drained one message
// at a time by waitStepCmd; the returned tea.Cmd reads the first message.
func startCreateWorktreeCmd(branch, base string) (chan tea.Msg, tea.Cmd) {
	ch := make(chan tea.Msg, 64)
	go func() {
		defer close(ch)

		cb := &internal.HookCallbacks{
			OnStart: func(name string) { ch <- stepStartedMsg{name: name} },
			OnComplete: func(name string, success bool, output string) {
				ch <- stepCompletedMsg{name: name, success: success, output: output}
			},
		}

		result, err := internal.CreateWorktree(findRepoRoot(), branch, base, cb)
		if err != nil {
			ch <- worktreeCreatedMsg{err: err}
			return
		}

		var warnings []string
		for _, hr := range result.HookResults {
			if !hr.Success {
				warnings = append(warnings, hr.Name)
			}
		}
		ch <- worktreeCreatedMsg{warnings: warnings}
	}()
	return ch, waitStepCmd(ch)
}

// waitStepCmd reads one message from the streaming channel. Update re-issues it
// after each message until the channel closes.
func waitStepCmd(ch chan tea.Msg) tea.Cmd {
	if ch == nil {
		return nil
	}
	return func() tea.Msg {
		msg, ok := <-ch
		if !ok {
			return nil
		}
		return msg
	}
}

// spinnerTickCmd schedules the next spinner frame. 80ms is the readable pace
// for braille dots — the same interval the CLI's spinner runs at.
func spinnerTickCmd() tea.Cmd {
	return tea.Tick(80*time.Millisecond, func(time.Time) tea.Msg { return spinnerTickMsg{} })
}

// loadDetailCmd reads the tail of a worktree's history off the UI goroutine.
// detailCommitLimit bounds the query; the pane renders as many as the
// terminal has room for.
const detailCommitLimit = 12

// scheduleDetailCmd waits out the debounce, then asks for the history if the
// cursor is still where it was.
func scheduleDetailCmd(seq int, branch, path string) tea.Cmd {
	return tea.Tick(detailDebounce, func(time.Time) tea.Msg {
		return detailWantedMsg{seq: seq, branch: branch, path: path}
	})
}

func loadDetailCmd(branch, path string) tea.Cmd {
	return func() tea.Msg {
		commits, err := git.RecentCommits(path, detailCommitLimit)
		return detailLoadedMsg{branch: branch, commits: commits, err: err}
	}
}

// sweepTickMsg advances the highlight over a newly created worktree.
type sweepTickMsg struct{}

// sweepTickCmd schedules the next frame of that highlight. Faster than the
// spinner: this one is a single pass, not a wait.
func sweepTickCmd() tea.Cmd {
	return tea.Tick(sweepInterval, func(time.Time) tea.Msg { return sweepTickMsg{} })
}

// removeWorktreeCmd removes a worktree and emits worktreeRemovedMsg, carrying
// back what `u` would need to put it there again.
func removeWorktreeCmd(wt internal.Worktree) tea.Cmd {
	return func() tea.Msg {
		// Force: the confirmation already spelled out any uncommitted work,
		// and for a dirty worktree it asked for the branch name in full.
		if err := internal.RemoveWorktree(wt.Path, true); err != nil {
			return worktreeRemovedMsg{err: err}
		}
		msg := worktreeRemovedMsg{}
		if wt.Branch != "" {
			msg.removed = &removedWorktree{
				branch: wt.Branch, path: wt.Path, displayPath: wt.DisplayPath,
			}
		}
		return msg
	}
}

// restoreWorktreeCmd checks a removed worktree's branch back out where it was.
func restoreWorktreeCmd(rm removedWorktree) tea.Cmd {
	return func() tea.Msg {
		return worktreeRestoredMsg{
			branch: rm.branch,
			err:    internal.RestoreWorktree(findRepoRoot(), rm.path, rm.branch),
		}
	}
}

func findRepoRoot() string {
	root, err := git.FindMainRepo(".")
	if err != nil {
		return "."
	}
	return root
}

// --- Archive persistence ---

func archivePath() string {
	return filepath.Join(internal.MoriHome(), "archived.json")
}

func loadArchived() map[string]bool {
	data, err := os.ReadFile(archivePath())
	if err != nil {
		return make(map[string]bool)
	}
	var branches []string
	if json.Unmarshal(data, &branches) != nil {
		return make(map[string]bool)
	}
	m := make(map[string]bool, len(branches))
	for _, b := range branches {
		m[b] = true
	}
	return m
}

func saveArchived(m map[string]bool) {
	branches := make([]string, 0, len(m))
	for b := range m {
		branches = append(branches, b)
	}
	sort.Strings(branches)
	data, _ := json.Marshal(branches)
	os.MkdirAll(filepath.Dir(archivePath()), 0o755)
	os.WriteFile(archivePath(), data, 0o644)
}
