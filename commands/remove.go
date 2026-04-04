package commands

import (
	"bufio"
	"fmt"
	"os"
	"os/exec"
	"strings"

	"github.com/moosecode/mori/agent"
)

func Remove(branch string, force bool) error {
	worktrees, err := List()
	if err != nil {
		return err
	}

	var targetPath string
	var isMain bool
	found := false
	for _, wt := range worktrees {
		if wt.Branch == branch {
			targetPath = wt.Path
			isMain = wt.IsMain
			found = true
			break
		}
	}

	if !found {
		return fmt.Errorf("no worktree found for branch '%s'", branch)
	}

	if isMain {
		return fmt.Errorf("cannot remove the main worktree")
	}

	if !force {
		hasSession, stale := agent.CheckSession(targetPath)
		if hasSession && !stale {
			return fmt.Errorf("worktree '%s' has an active Claude session. Use --force to remove anyway", branch)
		}

		out, _ := exec.Command("git", "-C", targetPath, "status", "--porcelain").Output()
		if len(strings.TrimSpace(string(out))) > 0 {
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

	args := []string{"worktree", "remove", targetPath}
	if force {
		args = []string{"worktree", "remove", "--force", targetPath}
	}

	if err := exec.Command("git", args...).Run(); err != nil {
		fmt.Fprintf(os.Stderr, "\033[1;31m✖\033[0m\n")
		return fmt.Errorf("failed to remove worktree: %w", err)
	}

	fmt.Fprintf(os.Stderr, "\033[1;32m✔\033[0m\n")
	return nil
}
