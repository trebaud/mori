package commands

import (
	"encoding/json"
	"fmt"
	"os"
	"text/tabwriter"

	"github.com/moosecode/mori/tui"
)

func PrintList(jsonOutput bool) error {
	worktrees, err := List()
	if err != nil {
		return err
	}

	if jsonOutput {
		return printJSON(worktrees)
	}

	return printTable(worktrees)
}

type worktreeJSON struct {
	Path    string `json:"path"`
	Branch  string `json:"branch"`
	Session string `json:"session"`
}

func sessionLabel(wt tui.Worktree) string {
	return string(wt.Insights.Status)
}

func printJSON(worktrees []tui.Worktree) error {
	var items []worktreeJSON
	for _, wt := range worktrees {
		items = append(items, worktreeJSON{
			Path:    wt.Path,
			Branch:  wt.Branch,
			Session: sessionLabel(wt),
		})
	}
	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	return enc.Encode(items)
}

func printTable(worktrees []tui.Worktree) error {
	w := tabwriter.NewWriter(os.Stdout, 0, 4, 2, ' ', 0)
	fmt.Fprintln(w, "PATH\tBRANCH\tSESSION")
	for _, wt := range worktrees {
		fmt.Fprintf(w, "%s\t%s\t%s\n", wt.RelativePath, wt.Branch, sessionLabel(wt))
	}
	return w.Flush()
}
