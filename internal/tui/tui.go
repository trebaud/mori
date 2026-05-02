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

	tea "charm.land/bubbletea/v2"

	"github.com/trebaud/mori/internal"
	"github.com/trebaud/mori/internal/git"
)

// Run launches the TUI. When the user picks a worktree, it hands control to
// the `claude` CLI in that worktree, then loops back into the TUI when claude exits.
func Run(worktrees []internal.Worktree) {
	DetectAndApplyTheme()
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

		if err := internal.LaunchClaude(final.worktrees[final.selected]); err != nil {
			fmt.Fprintf(os.Stderr, "Error: %v\n", err)
			os.Exit(1)
		}
	}
}
