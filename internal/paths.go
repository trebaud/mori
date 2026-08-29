package internal

import (
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"strings"
)

// repoMarker records which repository owns a directory under the worktrees
// root, so two repositories that share a directory name don't collide.
const repoMarker = ".mori-repo"

// MoriHome is the directory holding mori's own state: settings, the archive
// list, and worktrees.
func MoriHome() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return ".mori"
	}
	return filepath.Join(home, ".mori")
}

// WorktreesRoot is the parent of every repository's worktree directory.
//
// Worktrees live outside the repository on purpose: nested inside it, git sees
// them as an untracked embedded repository, and `git add -A` commits a gitlink
// unless every repo remembers to ignore the path.
func WorktreesRoot() string {
	return filepath.Join(MoriHome(), "worktrees")
}

// RepoSegment names the directory under WorktreesRoot that holds a
// repository's worktrees. It is the repository's own directory name so paths
// stay readable; when a *different* repository already claims that name, a
// short hash of the path is appended to keep the two apart.
func RepoSegment(repoRoot string) string {
	base := filepath.Base(repoRoot)
	if base == "." || base == string(filepath.Separator) || strings.TrimSpace(base) == "" {
		base = "repo"
	}

	owner, err := os.ReadFile(filepath.Join(WorktreesRoot(), base, repoMarker))
	if err == nil && strings.TrimSpace(string(owner)) != repoRoot {
		return base + "-" + shortHash(repoRoot)
	}
	return base
}

// WorktreeDir returns the directory a branch's worktree lives in.
func WorktreeDir(repoRoot, branch string) string {
	return filepath.Join(WorktreesRoot(), RepoSegment(repoRoot), branch)
}

// claimRepoDir creates the repository's worktree directory and marks it as
// owned, so RepoSegment keeps returning the same name for this repository.
func claimRepoDir(repoRoot string) error {
	dir := filepath.Join(WorktreesRoot(), RepoSegment(repoRoot))
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	marker := filepath.Join(dir, repoMarker)
	if _, err := os.Stat(marker); err == nil {
		return nil
	}
	return os.WriteFile(marker, []byte(repoRoot+"\n"), 0o644)
}

func shortHash(s string) string {
	sum := sha256.Sum256([]byte(s))
	return hex.EncodeToString(sum[:])[:6]
}

// TildePath collapses the user's home directory to ~ for display.
func TildePath(path string) string {
	home, err := os.UserHomeDir()
	if err != nil || home == "" || !strings.HasPrefix(path, home) {
		return path
	}
	return "~" + path[len(home):]
}
