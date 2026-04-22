// Package tui renders mori's interactive terminal UI.
//
// It follows the Elm architecture. Each concern lives in its own file:
//
//   - model.go      state, messages, Init, helpers
//   - update.go     Update + per-mode key handlers
//   - commands.go   tea.Cmd side effects + archive persistence
//   - view.go       View + rendering
//   - theme.go      colors, styles, status styling
//   - utils.go      formatters and layout helpers
package tui

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	tea "charm.land/bubbletea/v2"

	"github.com/trebaud/mori/internal"
	"github.com/trebaud/mori/internal/git"
)

// Run launches the TUI. When the user picks a worktree, it hands control to
// the `claude` CLI in that worktree, then loops back into the TUI when claude exits.
func Run(worktrees []internal.Worktree) {
	for {
		currentBranch := git.CurrentBranch()

		if refreshed, err := internal.List(); err == nil {
			worktrees = refreshed
		}

		p := tea.NewProgram(newModel(worktrees, currentBranch))

		m, err := p.Run()
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error running TUI: %v\n", err)
			os.Exit(1)
		}

		final, ok := m.(model)
		if !ok || final.selected < 0 {
			return
		}

		launchClaude(final.worktrees[final.selected])
	}
}

// launchClaude hands the terminal to the `claude` CLI for the given worktree,
// attempting to resume the existing session when one is known.
func launchClaude(wt internal.Worktree) {
	claudePath, err := exec.LookPath("claude")
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: claude not found in PATH\n")
		os.Exit(1)
	}

	baseArgs := []string{"--tmux"}
	defaultBranch := git.DefaultBranch(".")
	if wt.Branch != defaultBranch {
		baseArgs = append(baseArgs, "--worktree", filepath.Base(wt.Path))
	}

	if wt.Insights.SessionID != "" {
		args := append([]string{"--resume", wt.Insights.SessionID}, baseArgs...)
		fmt.Fprintf(os.Stderr, "\n  %s\n\n", dimStyle.Render("claude "+strings.Join(args, " ")))
		cmd := exec.Command(claudePath, args...)
		cmd.Stdin = os.Stdin
		cmd.Stdout = os.Stdout
		cmd.Stderr = os.Stderr
		if err := cmd.Run(); err == nil {
			return
		}
	}

	fmt.Fprintf(os.Stderr, "\n  %s\n\n", dimStyle.Render("claude "+strings.Join(baseArgs, " ")))
	cmd := exec.Command(claudePath, baseArgs...)
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	cmd.Run()
}
