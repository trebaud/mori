package internal

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"

	"github.com/trebaud/mori/internal/git"
)

// LaunchClaude hands the terminal to the `claude` CLI for the given worktree.
//
// We start a fresh interactive session in the worktree, resuming the on-disk
// JSONL transcript when one is known.
func LaunchClaude(wt Worktree) error {
	claudePath := os.Getenv("MORI_CLAUDE_PATH")
	if claudePath == "" {
		p, err := exec.LookPath("claude")
		if err != nil {
			return fmt.Errorf("claude not found in PATH")
		}
		claudePath = p
	}

	baseArgs := []string{"--tmux=classic"}
	defaultBranch := git.DefaultBranch(".")
	if wt.Branch != defaultBranch {
		baseArgs = append(baseArgs, "--worktree", filepath.Base(wt.Path))
	}

	// Remind the user how to leave without killing the session: tmux detach,
	// not ctrl+z (which claude binds to its own suspend).
	fmt.Fprintf(os.Stderr, "  \033[0;90m%s\033[0m\n", "press ctrl+b then d to detach — the session keeps running in the background")

	if wt.Insights.SessionID != "" {
		args := append([]string{"--resume", wt.Insights.SessionID}, baseArgs...)
		cmd := exec.Command(claudePath, args...)
		cmd.Dir = wt.Path
		cmd.Stdin = os.Stdin
		cmd.Stdout = os.Stdout
		cmd.Stderr = os.Stderr
		if err := cmd.Run(); err == nil {
			return nil
		}
	}

	cmd := exec.Command(claudePath, baseArgs...)
	cmd.Dir = wt.Path
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}
