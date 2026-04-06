package git

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
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
	gitCommon := strings.TrimSpace(string(out))
	// --git-common-dir returns the .git directory; the repo root is its parent
	return filepath.Dir(gitCommon), nil
}

// DefaultBranch returns the repository's default branch name (e.g. "main" or "master")
// by checking origin/HEAD, then probing for common branch names.
func DefaultBranch(repo string) string {
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

// Log returns the last n commit summaries for the given repo path.
func Log(repoPath string, n int) []string {
	out, err := exec.Command("git", "-C", repoPath, "log", "--oneline",
		"--pretty=format:%ar: %s", "-n", fmt.Sprintf("%d", n)).Output()
	if err != nil {
		return nil
	}
	lines := strings.Split(strings.TrimSpace(string(out)), "\n")
	if len(lines) == 1 && lines[0] == "" {
		return nil
	}
	return lines
}

// CurrentBranch returns the current branch name for the working directory.
func CurrentBranch() string {
	out, err := exec.Command("git", "branch", "--show-current").Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}

// WorktreeList returns the raw porcelain output of git worktree list.
func WorktreeList() (string, error) {
	out, err := exec.Command("git", "worktree", "list", "--porcelain").Output()
	if err != nil {
		return "", err
	}
	return string(out), nil
}

// AheadBehind returns a "+ahead/-behind" string relative to the default branch,
// or an empty string if even or on error.
func AheadBehind(repoPath string) string {
	mainBranch := DefaultBranch(repoPath)

	out, err := exec.Command("git", "-C", repoPath, "rev-list", "--left-right", "--count", mainBranch+"...HEAD").Output()
	if err != nil {
		return ""
	}

	parts := strings.Fields(strings.TrimSpace(string(out)))
	if len(parts) != 2 {
		return ""
	}

	behind, ahead := parts[0], parts[1]
	if ahead == "0" && behind == "0" {
		return ""
	}
	return fmt.Sprintf("+%s/-%s", ahead, behind)
}
