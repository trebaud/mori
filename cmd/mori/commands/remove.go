package commands

import (
	"bufio"
	"fmt"
	"os"
	"strings"

	"github.com/trebaud/mori/internal"
)

// Remove deletes the worktree on the given branch. Without --force it asks
// before discarding uncommitted work.
func Remove(branch string, force bool) error {
	worktrees, err := internal.List()
	if err != nil {
		return err
	}

	target := internal.FindByBranch(worktrees, branch)
	if target == nil {
		return fmt.Errorf("no worktree found for branch '%s'", branch)
	}
	if target.IsMain {
		return fmt.Errorf("cannot remove the main worktree")
	}

	if !force && target.Dirty > 0 {
		fmt.Fprintf(progress, "Warning: worktree '%s' has %d uncommitted file(s).\n", branch, target.Dirty)
		fmt.Fprintf(progress, "Remove anyway? [y/N] ")
		answer, _ := bufio.NewReader(os.Stdin).ReadString('\n')
		if strings.TrimSpace(strings.ToLower(answer)) != "y" {
			return fmt.Errorf("aborted")
		}
	}

	fmt.Fprintf(progress, "Removing worktree '%s'... ", branch)
	if err := internal.RemoveWorktree(target.Path, true); err != nil {
		fmt.Fprintf(progress, "\033[1;31m✖\033[0m\n")
		return err
	}
	fmt.Fprintf(progress, "\033[1;32m✔\033[0m\n")
	return nil
}
