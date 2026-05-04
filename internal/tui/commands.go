package tui

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"syscall"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/trebaud/mori/internal"
	"github.com/trebaud/mori/internal/git"
	"github.com/trebaud/mori/internal/github"
)

// prFetchedMsg is emitted by fetchPRCmd once a gh fetch completes.
type prFetchedMsg struct {
	branch string
	info   *github.PRInfo
}

func fetchPRCmd(branch string) tea.Cmd {
	return func() tea.Msg {
		info, _ := github.Refresh(branch)
		return prFetchedMsg{branch: branch, info: info}
	}
}

func fetchAllPRsCmd(wts []internal.Worktree) tea.Cmd {
	if !github.IsAvailable() {
		return nil
	}
	var cmds []tea.Cmd
	for _, wt := range wts {
		if wt.IsMain || wt.Branch == "" {
			continue
		}
		cmds = append(cmds, fetchPRCmd(wt.Branch))
	}
	if len(cmds) == 0 {
		return nil
	}
	return tea.Batch(cmds...)
}

// --- Effects (Elm: Cmd) ---

// stepStartedMsg fires when a creation step begins running.
type stepStartedMsg struct{ name string }

// stepCompletedMsg fires when a creation step finishes (success or failure).
type stepCompletedMsg struct {
	name    string
	success bool
}

// spinnerTickMsg drives the spinner animation while the creating overlay is open.
type spinnerTickMsg struct{}

// planCreateSteps returns the list of steps that CreateWorktree will perform,
// pre-populated so the overlay can show the full plan up front.
func planCreateSteps(repoRoot, branch string) []creatingStep {
	baseBranch := git.DefaultBranch(repoRoot)
	dir := internal.WorktreeDir(repoRoot, branch)
	relDir := strings.TrimPrefix(dir, repoRoot+"/")

	steps := []creatingStep{{
		name:  "Creating branch from " + baseBranch,
		cmd:   fmt.Sprintf("git worktree add %s -b %s %s", relDir, branch, baseBranch),
		state: stepPending,
	}}

	cfg := internal.Load(repoRoot)
	for _, st := range cfg.PostCreate {
		steps = append(steps, creatingStep{name: st.Name, cmd: st.Cmd, state: stepPending})
	}
	return steps
}

// startCreateWorktreeCmd kicks off worktree creation in a goroutine, streaming
// per-step progress over a channel. The returned channel is read one message at
// a time by waitStepCmd; the returned tea.Cmd reads the first message.
func startCreateWorktreeCmd(branch string) (chan tea.Msg, tea.Cmd) {
	ch := make(chan tea.Msg, 64)
	go func() {
		defer close(ch)
		repoRoot := findRepoRoot()

		cb := &internal.HookCallbacks{
			OnStart:    func(name string) { ch <- stepStartedMsg{name: name} },
			OnComplete: func(name string, success bool) { ch <- stepCompletedMsg{name: name, success: success} },
		}

		result, err := internal.CreateWorktree(repoRoot, branch, cb)
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

// waitStepCmd reads one message from the streaming channel. The Update function
// re-issues this cmd after each message until the channel is closed.
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

// spinnerTickCmd schedules the next spinner frame.
func spinnerTickCmd() tea.Cmd {
	return tea.Tick(100*time.Millisecond, func(_ time.Time) tea.Msg {
		return spinnerTickMsg{}
	})
}

// removeWorktreeCmd removes a worktree and emits worktreeRemovedMsg.
func removeWorktreeCmd(path string, force bool) tea.Cmd {
	return func() tea.Msg {
		if err := internal.RemoveWorktree(path, force); err != nil {
			return worktreeRemovedMsg{err: err}
		}
		return worktreeRemovedMsg{}
	}
}

// launchAgentCmd starts a detached claude agent in the worktree and emits messageSentMsg.
func launchAgentCmd(wt internal.Worktree, text string) tea.Cmd {
	return func() tea.Msg {
		claudePath, err := exec.LookPath("claude")
		if err != nil {
			return messageSentMsg{err: fmt.Errorf("claude not found in PATH")}
		}

		var args []string
		if wt.Insights.SessionID != "" {
			args = append(args, "--resume", wt.Insights.SessionID)
		}
		args = append(args, "--dangerously-skip-permissions", "-p", text)

		cmd := exec.Command(claudePath, args...)
		cmd.Dir = wt.Path

		logPath := filepath.Join(os.TempDir(), fmt.Sprintf("mori-agent-%s-%d.log", filepath.Base(wt.Path), time.Now().Unix()))
		logFile, logErr := os.Create(logPath)
		if logErr == nil {
			fmt.Fprintf(logFile, "mori launch @ %s\ncwd: %s\ncmd: %s %s\n---\n",
				time.Now().Format(time.RFC3339), wt.Path, claudePath, strings.Join(args, " "))
			cmd.Stdout = logFile
			cmd.Stderr = logFile
		}
		devNull, err := os.OpenFile(os.DevNull, os.O_RDONLY, 0)
		if err == nil {
			cmd.Stdin = devNull
			defer devNull.Close()
		}
		cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}

		if err := cmd.Start(); err != nil {
			if logFile != nil {
				logFile.Close()
			}
			return messageSentMsg{err: err}
		}
		_ = cmd.Process.Release()
		return messageSentMsg{logPath: logPath}
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
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".mori", "archived.json")
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
	var branches []string
	for b := range m {
		branches = append(branches, b)
	}
	sort.Strings(branches)
	data, _ := json.Marshal(branches)
	os.MkdirAll(filepath.Dir(archivePath()), 0755)
	os.WriteFile(archivePath(), data, 0644)
}
