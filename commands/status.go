package commands

import (
	"fmt"

	"github.com/moosecode/mori/agent"
)

func Status() error {
	worktrees, err := List()
	if err != nil {
		return err
	}

	counts := map[agent.AgentStatusType]int{}
	for _, wt := range worktrees {
		counts[wt.Insights.Status]++
	}

	total := len(worktrees)
	parts := []string{}

	if n := counts[agent.StatusWorking]; n > 0 {
		parts = append(parts, fmt.Sprintf("%d working", n))
	}
	if n := counts[agent.StatusWait]; n > 0 {
		parts = append(parts, fmt.Sprintf("%d waiting", n))
	}
	if n := counts[agent.StatusIdle]; n > 0 {
		parts = append(parts, fmt.Sprintf("%d idle", n))
	}
	if n := counts[agent.StatusNone]; n > 0 {
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
