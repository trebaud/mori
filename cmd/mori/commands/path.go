package commands

import (
	"fmt"
	"os"

	"github.com/trebaud/mori/internal"
)

// Path prints the worktree directory for a branch, the non-interactive
// counterpart to picking one in the TUI.
func Path(branch string) error {
	worktrees, err := internal.List()
	if err != nil {
		return err
	}

	target := internal.FindByBranch(worktrees, branch)
	if target == nil {
		return fmt.Errorf("no worktree found for branch '%s'", branch)
	}

	fmt.Fprintln(os.Stdout, target.Path)
	return nil
}
