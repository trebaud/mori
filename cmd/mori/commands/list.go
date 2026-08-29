package commands

import (
	"encoding/json"
	"fmt"
	"os"
	"text/tabwriter"
	"time"

	"github.com/trebaud/mori/internal"
)

// PrintList writes every worktree of the current repository to stdout, as a
// table by default or as JSON for scripting.
func PrintList(jsonOutput bool) error {
	worktrees, err := internal.List()
	if err != nil {
		return err
	}

	if jsonOutput {
		return printJSON(worktrees)
	}
	return printTable(worktrees)
}

type worktreeJSON struct {
	Path        string `json:"path"`
	Branch      string `json:"branch,omitempty"`
	DisplayPath string `json:"display_path"`
	Head        string `json:"head,omitempty"`
	Main        bool   `json:"main,omitempty"`
	Detached    bool   `json:"detached,omitempty"`
	Dirty       int    `json:"dirty"`
	Ahead       int    `json:"ahead"`
	Behind      int    `json:"behind"`
	LastCommit  string `json:"last_commit,omitempty"`
}

func printJSON(worktrees []internal.Worktree) error {
	items := make([]worktreeJSON, 0, len(worktrees))
	for _, wt := range worktrees {
		item := worktreeJSON{
			Path:        wt.Path,
			Branch:      wt.Branch,
			DisplayPath: wt.DisplayPath,
			Head:        wt.Head,
			Main:        wt.IsMain,
			Detached:    wt.Detached,
			Dirty:       wt.Dirty,
			Ahead:       wt.Ahead,
			Behind:      wt.Behind,
		}
		if !wt.LastCommit.IsZero() {
			item.LastCommit = wt.LastCommit.Format(time.RFC3339)
		}
		items = append(items, item)
	}

	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	return enc.Encode(items)
}

func printTable(worktrees []internal.Worktree) error {
	w := tabwriter.NewWriter(os.Stdout, 0, 4, 2, ' ', 0)
	fmt.Fprintln(w, "PATH\tBRANCH\tCHANGES\tSYNC")
	for _, wt := range worktrees {
		changes := "clean"
		if wt.Dirty > 0 {
			changes = fmt.Sprintf("%d dirty", wt.Dirty)
		}
		fmt.Fprintf(w, "%s\t%s\t%s\t%s\n", wt.DisplayPath, wt.Label(), changes, syncLabel(wt))
	}
	return w.Flush()
}

func syncLabel(wt internal.Worktree) string {
	switch {
	case wt.Ahead > 0 && wt.Behind > 0:
		return fmt.Sprintf("+%d/-%d", wt.Ahead, wt.Behind)
	case wt.Ahead > 0:
		return fmt.Sprintf("+%d", wt.Ahead)
	case wt.Behind > 0:
		return fmt.Sprintf("-%d", wt.Behind)
	default:
		return "-"
	}
}
