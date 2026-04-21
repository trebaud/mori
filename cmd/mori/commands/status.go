package commands

import (
	"fmt"

	"github.com/moosecode/mori/internal"
)

func Status() error {
	worktrees, err := internal.List()
	if err != nil {
		return err
	}

	counts := internal.StatusCounts(worktrees)
	total := len(worktrees)
	parts := []string{}

	if n := counts["WORKING"]; n > 0 {
		parts = append(parts, fmt.Sprintf("%d working", n))
	}
	if n := counts["WAITING"]; n > 0 {
		parts = append(parts, fmt.Sprintf("%d waiting", n))
	}
	if n := counts["IDLE"]; n > 0 {
		parts = append(parts, fmt.Sprintf("%d idle", n))
	}
	if n := counts["NONE"]; n > 0 {
		parts = append(parts, fmt.Sprintf("%d no session", n))
	}

	summary := fmt.Sprintf("%d worktrees", total)
	if len(parts) > 0 {
		summary += ": "
		for i, p := range parts {
			if i > 0 {
				summary += ", "
			}
			summary += p
		}
	}

	fmt.Println(summary)
	return nil
}
