package internal

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// withHome points mori's state directory at a temp tree for the test.
func withHome(t *testing.T) string {
	t.Helper()
	home := t.TempDir()
	t.Setenv("HOME", home)
	return home
}

func TestWorktreeDirLivesOutsideTheRepo(t *testing.T) {
	home := withHome(t)

	got := WorktreeDir("/home/t/Code/mori", "feat")
	want := filepath.Join(home, ".mori", "worktrees", "mori", "feat")
	if got != want {
		t.Errorf("WorktreeDir = %q, want %q", got, want)
	}
	if strings.HasPrefix(got, "/home/t/Code/mori/") {
		t.Errorf("worktree must not be nested inside the repository: %q", got)
	}
}

func TestRepoSegmentUsesTheRepoName(t *testing.T) {
	withHome(t)

	if got := RepoSegment("/home/t/Code/mori"); got != "mori" {
		t.Errorf("RepoSegment = %q, want %q", got, "mori")
	}
}

// Claiming is idempotent: the same repository keeps the same directory.
func TestRepoSegmentStableAfterClaim(t *testing.T) {
	withHome(t)
	const repo = "/home/t/Code/mori"

	for i := 0; i < 2; i++ {
		if err := claimRepoDir(repo); err != nil {
			t.Fatalf("claim %d: %v", i, err)
		}
		if got := RepoSegment(repo); got != "mori" {
			t.Fatalf("after claim %d: RepoSegment = %q, want %q", i, got, "mori")
		}
	}
}

// Two repositories with the same directory name must not share a worktree
// directory, or the second one's `git worktree add` would fail.
func TestRepoSegmentDisambiguatesCollision(t *testing.T) {
	withHome(t)
	const first, second = "/home/t/Code/api", "/home/t/work/api"

	if err := claimRepoDir(first); err != nil {
		t.Fatalf("claim: %v", err)
	}

	if got := RepoSegment(second); got == "api" {
		t.Fatal("second repo reused the first repo's directory")
	}
	if got := RepoSegment(second); !strings.HasPrefix(got, "api-") {
		t.Errorf("RepoSegment = %q, want an api- prefixed name", got)
	}
	// Deterministic: the same repo always resolves to the same directory.
	if RepoSegment(second) != RepoSegment(second) {
		t.Error("RepoSegment is not deterministic")
	}
	if WorktreeDir(first, "feat") == WorktreeDir(second, "feat") {
		t.Error("colliding repos resolved to the same worktree directory")
	}
}

func TestClaimRepoDirWritesMarker(t *testing.T) {
	home := withHome(t)
	const repo = "/home/t/Code/mori"

	if err := claimRepoDir(repo); err != nil {
		t.Fatalf("claim: %v", err)
	}
	data, err := os.ReadFile(filepath.Join(home, ".mori", "worktrees", "mori", repoMarker))
	if err != nil {
		t.Fatalf("marker not written: %v", err)
	}
	if strings.TrimSpace(string(data)) != repo {
		t.Errorf("marker = %q, want %q", data, repo)
	}
}

func TestTildePath(t *testing.T) {
	home := withHome(t)

	if got, want := TildePath(filepath.Join(home, ".mori", "worktrees")), "~/.mori/worktrees"; got != want {
		t.Errorf("TildePath = %q, want %q", got, want)
	}
	if got := TildePath("/srv/elsewhere"); got != "/srv/elsewhere" {
		t.Errorf("TildePath = %q, want the path unchanged", got)
	}
}
