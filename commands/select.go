package commands

import (
	"bufio"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/moosecode/mori/agent"
	"github.com/moosecode/mori/tui"
)

func Select() {
	worktrees, err := List()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}

	tui.Run(worktrees)
}

func List() ([]tui.Worktree, error) {
	if _, err := exec.Command("git", "rev-parse", "--git-dir").Output(); err != nil {
		return nil, fmt.Errorf("not a git repository")
	}

	out, err := exec.Command("git", "worktree", "list", "--porcelain").Output()
	if err != nil {
		return nil, fmt.Errorf("failed to list worktrees: %w", err)
	}

	gitDir, _ := exec.Command("git", "rev-parse", "--git-dir").Output()
	mainPath := filepath.Dir(strings.TrimSpace(string(gitDir)))

	branchOut, _ := exec.Command("git", "branch", "--show-current").Output()
	currentBranch := strings.TrimSpace(string(branchOut))

	scanner := bufio.NewScanner(strings.NewReader(string(out)))
	var wts []tui.Worktree
	var current tui.Worktree

	for scanner.Scan() {
		line := scanner.Text()
		if strings.HasPrefix(line, "worktree ") {
			if current.Path != "" {
				wts = append(wts, current)
			}
			current = tui.Worktree{Path: strings.TrimPrefix(line, "worktree ")}
		} else if strings.HasPrefix(line, "branch ") {
			parts := strings.Split(line, "/")
			current.Branch = parts[len(parts)-1]
			if current.Branch == currentBranch {
				current.IsMain = true
			}
		}
	}
	if current.Path != "" {
		wts = append(wts, current)
	}

	home, _ := os.UserHomeDir()

	for i := range wts {
		wts[i].RelativePath = makeRelativePath(wts[i].Path, mainPath, home)
		wts[i].ClaudeSession, wts[i].ClaudeStale = agent.CheckSession(wts[i].Path)
		wts[i].Insights = agent.GetInsights(wts[i].Path)
	}

	return wts, nil
}

func makeRelativePath(path, mainPath, home string) string {
	rel := path
	if strings.HasPrefix(rel, home) {
		rel = "~" + rel[len(home):]
	}

	parts := strings.Split(rel, "/")
	if len(parts) > 0 {
		name := parts[len(parts)-1]
		if name == filepath.Base(mainPath) {
			return "./main"
		}
		return "./" + name
	}
	return rel
}
