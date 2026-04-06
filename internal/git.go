package git

import (
	"fmt"
	"math/rand/v2"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

const suffixChars = "abcdefghijklmnopqrstuvwxyz0123456789"

// RandomSuffix returns a 5-character random alphanumeric string.
func RandomSuffix() string {
	b := make([]byte, 5)
	for i := range b {
		b[i] = suffixChars[rand.IntN(len(suffixChars))]
	}
	return string(b)
}

// IsGitRepo returns true if path contains a .git directory (not a worktree .git file).
func IsGitRepo(path string) bool {
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

// GetDefaultBranch returns the repository's default branch name (e.g. "main" or "master")
// by checking origin/HEAD, then probing for common branch names.
func GetDefaultBranch(repo string) string {
	out, err := exec.Command("git", "-C", repo, "symbolic-ref", "refs/remotes/origin/HEAD").Output()
	if err == nil {
		parts := strings.Split(strings.TrimSpace(string(out)), "/")
		if len(parts) > 0 {
			return parts[len(parts)-1]
		}
	}
	// Check which common default branch exists
	for _, name := range []string{"main", "master"} {
		if exec.Command("git", "-C", repo, "rev-parse", "--verify", "--quiet", name).Run() == nil {
			return name
		}
	}
	return "main"
}
