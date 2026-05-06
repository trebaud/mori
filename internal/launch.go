package internal

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/trebaud/mori/internal/bg"
	"github.com/trebaud/mori/internal/git"
)

// LaunchClaude hands the terminal to the `claude` CLI for the given worktree.
//
// If a live `claude --bg` session exists for the worktree, we attach to it
// (`claude attach <id>`) so the user picks up the conversation that's already
// running. Otherwise we start a fresh interactive session in the worktree,
// resuming the on-disk JSONL transcript when one is known.
func LaunchClaude(wt Worktree) error {
	claudePath := os.Getenv("MORI_CLAUDE_PATH")
	if claudePath == "" {
		p, err := exec.LookPath("claude")
		if err != nil {
			return fmt.Errorf("claude not found in PATH")
		}
		claudePath = p
	}

	// Attach to a dispatched bg session even if it's finished — `claude attach`
	// restarts a fresh process from where it left off, so the user can read the
	// final output, scroll the transcript, or send a follow-up prompt.
	if sess := bg.FindByCwd(wt.Path); sess != nil {
		fmt.Fprintf(os.Stderr, "\n  \033[0;90m%s\033[0m\n\n", "claude attach "+sess.ID)
		cmd := exec.Command(claudePath, "attach", sess.ID)
		cmd.Dir = wt.Path
		cmd.Stdin = os.Stdin
		cmd.Stdout = os.Stdout
		cmd.Stderr = os.Stderr
		return cmd.Run()
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
