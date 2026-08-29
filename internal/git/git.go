// Package git wraps the git CLI with the small set of queries mori needs.
package git

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"
)

// IsRepo returns true if path contains a .git directory (not a worktree .git file).
func IsRepo(path string) bool {
	info, err := os.Stat(filepath.Join(path, ".git"))
	if err != nil {
		return false
	}
	return info.IsDir()
}

// FindMainRepo returns the main repository root for any path inside a repo or worktree.
func FindMainRepo(path string) (string, error) {
	out, err := exec.Command("git", "-C", path, "rev-parse", "--path-format=absolute", "--git-common-dir").Output()
	if err != nil {
		return "", fmt.Errorf("not a git repository: %w", err)
	}
	// --git-common-dir returns the .git directory; the repo root is its parent.
	return filepath.Dir(strings.TrimSpace(string(out))), nil
}

var (
	defaultBranchMu    sync.RWMutex
	defaultBranchCache = make(map[string]string)
)

// DefaultBranch returns the repository's default branch name (e.g. "main" or
// "master") by checking origin/HEAD, then probing for common branch names.
// Results are cached per repo path for the lifetime of the process.
func DefaultBranch(repo string) string {
	defaultBranchMu.RLock()
	cached, ok := defaultBranchCache[repo]
	defaultBranchMu.RUnlock()
	if ok {
		return cached
	}

	branch := defaultBranchUncached(repo)

	defaultBranchMu.Lock()
	defaultBranchCache[repo] = branch
	defaultBranchMu.Unlock()
	return branch
}

func defaultBranchUncached(repo string) string {
	out, err := exec.Command("git", "-C", repo, "symbolic-ref", "refs/remotes/origin/HEAD").Output()
	if err == nil {
		parts := strings.Split(strings.TrimSpace(string(out)), "/")
		if len(parts) > 0 {
			return parts[len(parts)-1]
		}
	}
	for _, name := range []string{"main", "master"} {
		if exec.Command("git", "-C", repo, "rev-parse", "--verify", "--quiet", name).Run() == nil {
			return name
		}
	}
	return "main"
}

// CurrentBranch returns the branch name for the working directory.
func CurrentBranch() string {
	out, err := exec.Command("git", "branch", "--show-current").Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}

// WorktreeList returns the raw porcelain output of git worktree list.
func WorktreeList(repo string) (string, error) {
	out, err := exec.Command("git", "-C", repo, "worktree", "list", "--porcelain").Output()
	if err != nil {
		return "", err
	}
	return string(out), nil
}

// HasCommits returns true if the repo has at least one commit.
func HasCommits(repo string) (bool, error) {
	out, err := exec.Command("git", "-C", repo, "rev-list", "--count", "HEAD").Output()
	if err != nil {
		return false, nil
	}
	return strings.TrimSpace(string(out)) != "0", nil
}

// AddWorktree creates a new worktree at dir on a new branch based on baseBranch.
func AddWorktree(repo, dir, branch, baseBranch string) error {
	cmd := exec.Command("git", "-C", repo, "worktree", "add", dir, "-b", branch, baseBranch)
	if out, err := cmd.CombinedOutput(); err != nil {
		if msg := strings.TrimSpace(string(out)); msg != "" {
			return fmt.Errorf("%s", msg)
		}
		return err
	}
	return nil
}

// RemoveWorktree removes a git worktree. If force is true, --force is passed.
func RemoveWorktree(path string, force bool) error {
	args := []string{"worktree", "remove"}
	if force {
		args = append(args, "--force")
	}
	args = append(args, path)
	if out, err := exec.Command("git", args...).CombinedOutput(); err != nil {
		if msg := strings.TrimSpace(string(out)); msg != "" {
			return fmt.Errorf("%s", msg)
		}
		return err
	}
	return nil
}

// DirtyCount returns the number of files with uncommitted changes (staged,
// unstaged, or untracked) in the worktree at path.
func DirtyCount(path string) int {
	out, err := exec.Command("git", "-C", path, "status", "--porcelain").Output()
	if err != nil {
		return 0
	}
	trimmed := strings.TrimRight(string(out), "\n")
	if trimmed == "" {
		return 0
	}
	return strings.Count(trimmed, "\n") + 1
}

// LastCommit returns the timestamp of HEAD in the worktree at path.
// The zero time is returned when there is no commit or git fails.
func LastCommit(path string) time.Time {
	out, err := exec.Command("git", "-C", path, "log", "-1", "--format=%ct").Output()
	if err != nil {
		return time.Time{}
	}
	secs, err := strconv.ParseInt(strings.TrimSpace(string(out)), 10, 64)
	if err != nil {
		return time.Time{}
	}
	return time.Unix(secs, 0)
}

// AheadBehind counts commits the worktree's HEAD is ahead of and behind the
// repository's default branch. Both are 0 when the branch is the default one,
// has no merge base, or git fails.
func AheadBehind(path, base string) (ahead, behind int) {
	out, err := exec.Command("git", "-C", path, "rev-list", "--left-right", "--count", base+"...HEAD").Output()
	if err != nil {
		return 0, 0
	}
	fields := strings.Fields(strings.TrimSpace(string(out)))
	if len(fields) != 2 {
		return 0, 0
	}
	behind, _ = strconv.Atoi(fields[0])
	ahead, _ = strconv.Atoi(fields[1])
	return ahead, behind
}
