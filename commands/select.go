package commands

import (
	"fmt"
	"os"

	"github.com/moosecode/mori/tui"
)

func Select() {
	worktrees, err := List()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}

	tui.Run(worktrees)
}

func List() ([]tui.Worktree, error) {
	return tui.ListWorktrees()
}
