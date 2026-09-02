package commands

import (
	"fmt"
	"os"

	"github.com/trebaud/mori/v2/internal/tui"
)

// Select launches the TUI and prints the picked worktree path on stdout —
// nothing at all if the user quit without picking one. The shell function
// from `mori shell-init` turns that print into a cd; without it, `cd "$(mori)"`
// does the same by hand.
func Select() error {
	// No listing here: the TUI asks for one itself, so it can be on screen
	// while git is still answering. A repository that turns out not to be one
	// is caught by tui.Run before the alt screen opens.
	path, err := tui.Run()
	if err != nil {
		return err
	}
	if path != "" {
		fmt.Fprintln(os.Stdout, path)
	}
	return nil
}
