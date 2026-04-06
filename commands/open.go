package commands

import (
	"fmt"

	"github.com/moosecode/mori/worktree"
)

func Open(branch string) error {
	worktrees, err := worktree.List()
	if err != nil {
		return err
	}

	target := worktree.FindByBranch(worktrees, branch)
	if target == nil {
		return fmt.Errorf("no worktree found for branch '%s'", branch)
	}

	fmt.Println(target.Path)
	return nil
}
