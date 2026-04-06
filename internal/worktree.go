package internal

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/moosecode/mori/internal/agent"
	"github.com/moosecode/mori/internal/git"
)

type Worktree struct {
	Path         string
	Branch       string
	RelativePath string
	IsMain       bool
	Insights     agent.Insights
}

// List queries git for all worktrees and enriches them with agent insights.
func List() ([]Worktree, error) {
	mainPath, err := git.FindMainRepo(".")
	if err != nil {
		return nil, fmt.Errorf("not a git repository")
	}

	out, err := git.WorktreeList()
	if err != nil {
		return nil, fmt.Errorf("failed to list worktrees: %w", err)
	}

	currentBranch := git.CurrentBranch()

	wts := parseList(out, currentBranch)

	home, _ := os.UserHomeDir()

	for i := range wts {
		wts[i].RelativePath = makeRelativePath(wts[i].Path, mainPath, home)
		wts[i].Insights = agent.GetInsights(wts[i].Path)
	}

	return wts, nil
}

// parseList parses git worktree list --porcelain output into Worktree structs.
func parseList(output, currentBranch string) []Worktree {
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

// WorktreeDir returns the standard directory path for a worktree branch.
func WorktreeDir(repoRoot, branch string) string {
	return filepath.Join(repoRoot, ".claude", "worktrees", branch)
}

// CreateResult holds the outcome of a worktree creation.
type CreateResult struct {
	Dir         string
	BaseBranch  string
	HookResults []HookResult
}

// CreateWorktree creates a new git worktree with the given branch name.
// It checks for commits, creates the worktree from the default branch,
// and runs post-create hooks.
func CreateWorktree(repoRoot, branch string) (CreateResult, error) {
	hasCommits, err := git.HasCommits(repoRoot)
	if err != nil {
		return CreateResult{}, fmt.Errorf("failed to check repo: %w", err)
	}
	if !hasCommits {
		return CreateResult{}, fmt.Errorf("repository has no commits, cannot create worktree. Make at least one commit first")
	}

	baseBranch := git.DefaultBranch(repoRoot)
	dir := WorktreeDir(repoRoot, branch)

	if err := git.AddWorktree(repoRoot, dir, branch, baseBranch); err != nil {
		return CreateResult{}, fmt.Errorf("failed to create worktree: %w", err)
	}

	cfg := Load(repoRoot)
	hookResults := RunPostCreateHooks(dir, cfg.PostCreate)

	return CreateResult{
		Dir:         dir,
		BaseBranch:  baseBranch,
		HookResults: hookResults,
	}, nil
}

// RemoveWorktree removes a git worktree at the given path.
func RemoveWorktree(path string, force bool) error {
	if err := git.RemoveWorktree(path, force); err != nil {
		return fmt.Errorf("failed to remove worktree: %w", err)
	}
	return nil
}

// FindByBranch returns the worktree matching the given branch name, or nil if not found.
func FindByBranch(worktrees []Worktree, branch string) *Worktree {
	for i := range worktrees {
		if worktrees[i].Branch == branch {
			return &worktrees[i]
		}
	}
	return nil
}

func makeRelativePath(path, mainPath, home string) string {
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
