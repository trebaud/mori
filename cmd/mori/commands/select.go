package commands

import (
	"fmt"
	"os"

	"github.com/trebaud/mori/v2/internal"
	"github.com/trebaud/mori/v2/internal/tui"
)

// Select launches the TUI and prints the picked worktree path on stdout, so
// `cd "$(mori)"` lands the user in that worktree.
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
