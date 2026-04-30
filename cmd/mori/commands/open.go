package commands

import (
	"fmt"

	"github.com/trebaud/mori/internal"
)

func Open(branch string) error {
	worktrees, err := internal.List()
	if err != nil {
		return err
	}

	target := internal.FindByBranch(worktrees, branch)
	if target == nil {
		return fmt.Errorf("no worktree found for branch '%s'", branch)
	}

	return internal.LaunchClaude(*target)
}
