package tui

import (
	"bufio"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/moosecode/mori/agent"
)

type Worktree struct {
	Path         string
	Branch       string
	RelativePath string
	IsMain       bool
	Insights     agent.AgentStatus
}

// ListWorktrees queries git for all worktrees and enriches them with agent insights.
func ListWorktrees() ([]Worktree, error) {
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

	wts := parseWorktreeList(string(out), currentBranch)

	home, _ := os.UserHomeDir()

	for i := range wts {
		wts[i].RelativePath = MakeRelativePath(wts[i].Path, mainPath, home)
		wts[i].Insights = agent.GetInsights(wts[i].Path)
	}

	return wts, nil
}

// parseWorktreeList parses git worktree list --porcelain output into Worktree structs.
func parseWorktreeList(output, currentBranch string) []Worktree {
	scanner := bufio.NewScanner(strings.NewReader(output))
	var wts []Worktree
	var current Worktree

	for scanner.Scan() {
		line := scanner.Text()
		if strings.HasPrefix(line, "worktree ") {
			if current.Path != "" {
				wts = append(wts, current)
			}
			current = Worktree{Path: strings.TrimPrefix(line, "worktree ")}
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

	return wts
}

// MakeRelativePath converts an absolute worktree path into a short display path.
func MakeRelativePath(path, mainPath, home string) string {
	rel := path
	if home != "" && strings.HasPrefix(rel, home) {
		rel = "~" + rel[len(home):]
	}

	parts := strings.Split(rel, "/")
	if len(parts) > 0 {
		name := parts[len(parts)-1]
		if mainPath != "" && name == filepath.Base(mainPath) {
			return "./main"
		}
		return "./" + name
	}
	return rel
}
