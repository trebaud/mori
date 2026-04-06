package commands

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"text/tabwriter"

	"github.com/moosecode/mori/agent"
	"github.com/moosecode/mori/worktree"
)

var validStatuses = map[agent.StatusType]bool{
	agent.StatusWorking: true,
	agent.StatusIdle:    true,
	agent.StatusWait:    true,
	agent.StatusNone:    true,
}

func PrintList(jsonOutput bool, statusFilter string) error {
	worktrees, err := worktree.List()
	if err != nil {
		return err
	}

	if statusFilter != "" {
		target := agent.StatusType(strings.ToUpper(statusFilter))
		if !validStatuses[target] {
			return fmt.Errorf("invalid status '%s'. Valid values: working, idle, waiting, none", statusFilter)
		}
		worktrees = filterByStatus(worktrees, target)
	}

	if jsonOutput {
		return printJSON(worktrees)
	}

	return printTable(worktrees)
}

func filterByStatus(wts []worktree.Worktree, target agent.StatusType) []worktree.Worktree {
	var result []worktree.Worktree
	for _, wt := range wts {
		if wt.Insights.Status == target {
			result = append(result, wt)
		}
	}
	return result
}

type worktreeJSON struct {
	Path         string  `json:"path"`
	Branch       string  `json:"branch"`
	Session      string  `json:"session"`
	Model        string  `json:"model,omitempty"`
	Mode         string  `json:"mode,omitempty"`
	Cost         float64 `json:"cost,omitempty"`
	Task         string  `json:"task,omitempty"`
	LastActivity string  `json:"last_activity,omitempty"`
	AheadBehind  string  `json:"ahead_behind,omitempty"`
}

func sessionLabel(wt worktree.Worktree) string {
	return string(wt.Insights.Status)
}

func printJSON(worktrees []worktree.Worktree) error {
	var items []worktreeJSON
	for _, wt := range worktrees {
		item := worktreeJSON{
			Path:    wt.Path,
			Branch:  wt.Branch,
			Session: sessionLabel(wt),
		}
		if wt.Insights.Model != "" {
			item.Model = wt.Insights.Model
		}
		if wt.Insights.Mode != "" {
			item.Mode = wt.Insights.Mode
		}
		if wt.Insights.CostUSD > 0 {
			item.Cost = wt.Insights.CostUSD
		}
		if wt.Insights.CurrentTask != "" {
			item.Task = wt.Insights.CurrentTask
		}
		if !wt.Insights.LastActivity.IsZero() {
			item.LastActivity = wt.Insights.LastActivity.Format("2006-01-02T15:04:05Z07:00")
		}
		if wt.Insights.AheadBehind != "" {
			item.AheadBehind = wt.Insights.AheadBehind
		}
		items = append(items, item)
	}
	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	return enc.Encode(items)
}

func printTable(worktrees []worktree.Worktree) error {
	w := tabwriter.NewWriter(os.Stdout, 0, 4, 2, ' ', 0)
	fmt.Fprintln(w, "PATH\tBRANCH\tSESSION\tCOST")
	for _, wt := range worktrees {
		cost := ""
		if wt.Insights.CostUSD > 0 {
			cost = fmt.Sprintf("$%.2f", wt.Insights.CostUSD)
		}
		fmt.Fprintf(w, "%s\t%s\t%s\t%s\n", wt.RelativePath, wt.Branch, sessionLabel(wt), cost)
	}
	return w.Flush()
}
