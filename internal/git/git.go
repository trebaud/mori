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

// IsMainRepo returns true if path contains a .git directory (not a worktree .git file).
func IsMainRepo(path string) bool {
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

// GetMainBranch determines the primary branch of a repository using multiple fallback strategies.
func GetMainBranch(repo string) string {
	// Try the current branch of the main repo
	out, err := exec.Command("git", "-C", repo, "branch", "--show-current").Output()
	if err == nil {
		branch := strings.TrimSpace(string(out))
		if branch != "" {
			return branch
		}
	}

	// Try origin/HEAD
	out, err = exec.Command("git", "-C", repo, "symbolic-ref", "refs/remotes/origin/HEAD").Output()
	if err == nil {
		parts := strings.Split(string(out), "/")
		if len(parts) >= 3 {
			return strings.TrimSpace(parts[len(parts)-1])
		}
	}

	// Try HEAD
	out, err = exec.Command("git", "-C", repo, "rev-parse", "--abbrev-ref", "HEAD").Output()
	if err == nil {
		branch := strings.TrimSpace(string(out))
		if branch != "HEAD" {
			return branch
		}
	}

	return "main"
}
