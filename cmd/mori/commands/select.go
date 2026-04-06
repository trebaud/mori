package commands

import (
	"fmt"
	"os"

	"github.com/moosecode/mori/internal"
)

func Select() {
	worktrees, err := internal.List()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}

	internal.Run(worktrees)
}
