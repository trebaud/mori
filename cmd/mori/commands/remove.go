package commands

import (
	"bufio"
	"fmt"
	"os"
	"strings"

	"github.com/moosecode/mori/internal"
)

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

	if !force {
		if internal.HasActiveSession(*target) {
			return fmt.Errorf("worktree '%s' has an active Claude session (%s). Use --force to remove anyway", branch, target.Insights.Status)
		}

		if internal.HasUncommittedChanges(target.Path) {
			fmt.Fprintf(os.Stderr, "Warning: worktree '%s' has uncommitted changes.\n", branch)
			fmt.Fprintf(os.Stderr, "Remove anyway? [y/N] ")
			reader := bufio.NewReader(os.Stdin)
			answer, _ := reader.ReadString('\n')
			if strings.TrimSpace(strings.ToLower(answer)) != "y" {
				return fmt.Errorf("aborted")
			}
		}
	}

	fmt.Fprintf(os.Stderr, "Removing worktree '%s'... ", branch)

	if err := internal.RemoveWorktree(target.Path, force); err != nil {
		fmt.Fprintf(os.Stderr, "\033[1;31m✖\033[0m\n")
		return err
	}

	fmt.Fprintf(os.Stderr, "\033[1;32m✔\033[0m\n")
	return nil
}
