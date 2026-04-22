package commands

import (
	"fmt"
	"os"

	"github.com/trebaud/mori/internal"
	"github.com/trebaud/mori/internal/tui"
)

func Select() {
	worktrees, err := internal.List()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}

	tui.Run(worktrees)
}
