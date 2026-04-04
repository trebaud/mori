package commands

import (
	"fmt"

	"github.com/moosecode/mori/tui"
)

func Open(branch string) error {
	worktrees, err := List()
	if err != nil {
		return err
	}

	var target *tui.Worktree
	for i := range worktrees {
		if worktrees[i].Branch == branch {
			target = &worktrees[i]
			break
		}
	}

	if target == nil {
		return fmt.Errorf("no worktree found for branch '%s'", branch)
	}

	fmt.Print(target.Path)
	return nil
}
