package commands

import (
	"fmt"
	"os"

	"github.com/trebaud/mori/v2/internal"
	"github.com/trebaud/mori/v2/internal/tui"
)

// Select launches the TUI and prints the picked worktree path on stdout —
// nothing at all if the user quit without picking one. The shell function
// from `mori shell-init` turns that print into a cd; without it, `cd "$(mori)"`
// does the same by hand.
func Select() error {
	worktrees, err := internal.ListLinked()
	if err != nil {
		return err
	}

	path, err := tui.Run(worktrees)
	if err != nil {
		return err
	}
	if path != "" {
		fmt.Fprintln(os.Stdout, path)
	}
	return nil
}
