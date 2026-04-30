package internal

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/trebaud/mori/internal/git"
)

// LaunchClaude hands the terminal to the `claude` CLI for the given worktree,
// attempting to resume the existing session when one is known. The agent runs
// inside tmux via `--tmux`, with `--worktree` set when the worktree isn't the
// repo's default branch.
func LaunchClaude(wt Worktree) error {
	claudePath, err := exec.LookPath("claude")
	if err != nil {
		return fmt.Errorf("claude not found in PATH")
	}

	baseArgs := []string{"--tmux"}
	defaultBranch := git.DefaultBranch(".")
	if wt.Branch != defaultBranch {
		baseArgs = append(baseArgs, "--worktree", filepath.Base(wt.Path))
	}

	if wt.Insights.SessionID != "" {
		args := append([]string{"--resume", wt.Insights.SessionID}, baseArgs...)
		fmt.Fprintf(os.Stderr, "\n  \033[0;90m%s\033[0m\n\n", "claude "+strings.Join(args, " "))
		cmd := exec.Command(claudePath, args...)
		cmd.Dir = wt.Path
		cmd.Stdin = os.Stdin
		cmd.Stdout = os.Stdout
		cmd.Stderr = os.Stderr
		if err := cmd.Run(); err == nil {
			return nil
		}
	}

	fmt.Fprintf(os.Stderr, "\n  \033[0;90m%s\033[0m\n\n", "claude "+strings.Join(baseArgs, " "))
	cmd := exec.Command(claudePath, baseArgs...)
	cmd.Dir = wt.Path
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}
