package internal

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/trebaud/mori/internal/git"
	"github.com/trebaud/mori/internal/github"
	"github.com/trebaud/mori/internal/insights"
)

type Worktree struct {
	Path         string
	Branch       string
	RelativePath string
	IsMain       bool
	Insights     insights.Insights
	PR           *github.PRInfo
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

	wts := parseList(out)

	home, _ := os.UserHomeDir()

	for i := range wts {
		wts[i].RelativePath = makeRelativePath(wts[i].Path, mainPath, home)
		if wts[i].Path == mainPath {
			wts[i].IsMain = true
		}
		wts[i].Insights = insights.GetInsights(wts[i].Path)
		if !wts[i].IsMain && wts[i].Branch != "" {
			wts[i].PR = github.Lookup(wts[i].Branch)
		}
	}

	return wts, nil
}

// parseList parses git worktree list --porcelain output into Worktree structs.
func parseList(output string) []Worktree {
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
// and runs post-create hooks. If cb is non-nil, progress is reported per step
// so callers can render live feedback (e.g. a spinner).
func CreateWorktree(repoRoot, branch string, cb *HookCallbacks) (CreateResult, error) {
	hasCommits, err := git.HasCommits(repoRoot)
	if err != nil {
		return CreateResult{}, fmt.Errorf("failed to check repo: %w", err)
	}
	if !hasCommits {
		return CreateResult{}, fmt.Errorf("repository has no commits, cannot create worktree. Make at least one commit first")
	}

	baseBranch := git.DefaultBranch(repoRoot)
	dir := WorktreeDir(repoRoot, branch)

	branchStep := "Creating branch from " + baseBranch
	if cb != nil && cb.OnStart != nil {
		cb.OnStart(branchStep)
	}
	addErr := git.AddWorktree(repoRoot, dir, branch, baseBranch)
	if cb != nil && cb.OnComplete != nil {
		cb.OnComplete(branchStep, addErr == nil)
	}
	if addErr != nil {
		return CreateResult{}, fmt.Errorf("failed to create worktree: %w", addErr)
	}

	cfg := Load(repoRoot)
	hookResults := RunPostCreateHooks(dir, cfg.PostCreate, cb)

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

// HasActiveSession returns true if the worktree has a WORKING or WAITING agent session.
func HasActiveSession(wt Worktree) bool {
	return wt.Insights.Status == insights.StatusWorking || wt.Insights.Status == insights.StatusWait
}

// HasUncommittedChanges returns true if the given path has uncommitted git changes.
func HasUncommittedChanges(path string) bool {
	return git.HasUncommittedChanges(path)
}

// FilterByStatus returns worktrees matching the given status string.
// Valid values: working, idle, waiting, none.
func FilterByStatus(wts []Worktree, status string) ([]Worktree, error) {
	target := insights.StatusType(strings.ToUpper(status))
	valid := map[insights.StatusType]bool{
		insights.StatusWorking: true,
		insights.StatusIdle:    true,
		insights.StatusWait:    true,
		insights.StatusNone:    true,
	}
	if !valid[target] {
		return nil, fmt.Errorf("invalid status '%s'. Valid values: working, idle, waiting, none", status)
	}
	var result []Worktree
	for _, wt := range wts {
		if wt.Insights.Status == target {
			result = append(result, wt)
		}
	}
	return result, nil
}

// StatusCounts returns a map of status label to count across worktrees.
func StatusCounts(wts []Worktree) map[string]int {
	counts := make(map[string]int)
	for _, wt := range wts {
		counts[string(wt.Insights.Status)]++
	}
	return counts
}

// ResolveRepo resolves a path to the main git repository root.
func ResolveRepo(path string) (string, error) {
	absPath, err := filepath.Abs(path)
	if err != nil {
		return "", fmt.Errorf("cannot resolve path '%s'", path)
	}
	if git.IsRepo(absPath) {
		return absPath, nil
	}
	mainRepo, err := git.FindMainRepo(absPath)
	if err != nil {
		return "", fmt.Errorf("not a git repository or worktree")
	}
	return mainRepo, nil
}

func makeRelativePath(path, mainPath, home string) string {
	rel := path
	if home != "" && strings.HasPrefix(rel, home) {
		rel = "~" + rel[len(home):]
	}

	parts := strings.Split(rel, "/")
	if len(parts) > 0 {
		return "./" + parts[len(parts)-1]
	}
	return rel
}
