package commands

import (
	"fmt"
	"os"

	"github.com/moosecode/mori/tui"
	"github.com/moosecode/mori/worktree"
)

func Select() {
	worktrees, err := worktree.List()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}

	tui.Run(worktrees)
}
