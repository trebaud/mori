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
)

// --- Effects (Elm: Cmd) ---

// createWorktreeCmd creates a worktree in the background and emits worktreeCreatedMsg.
func createWorktreeCmd(branch string) tea.Cmd {
	return func() tea.Msg {
		repoRoot := findRepoRoot()
		if branch == "" {
			branch = "wt-" + internal.RandomSuffix()
		}

		result, err := internal.CreateWorktree(repoRoot, branch, nil)
		if err != nil {
			return worktreeCreatedMsg{err: err}
		}

		var warnings []string
		for _, hr := range result.HookResults {
			if !hr.Success {
				warnings = append(warnings, hr.Name)
			}
		}
		return worktreeCreatedMsg{warnings: warnings}
	}
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
