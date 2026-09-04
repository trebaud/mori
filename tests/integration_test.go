package tests

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

var moriBinary string

func TestMain(m *testing.M) {
	tmp, err := os.MkdirTemp("", "mori-test-bin-*")
	if err != nil {
		fmt.Fprintf(os.Stderr, "failed to create temp dir: %v\n", err)
		os.Exit(1)
	}
	defer os.RemoveAll(tmp)

	moriBinary = filepath.Join(tmp, "mori")
	build := exec.Command("go", "build", "-o", moriBinary, "../cmd/mori")
	build.Stderr = os.Stderr
	if err := build.Run(); err != nil {
		fmt.Fprintf(os.Stderr, "failed to build mori: %v\n", err)
		os.Exit(1)
	}

	os.Exit(m.Run())
}

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

type moriResult struct {
	stdout   string
	stderr   string
	exitCode int
}

func runMori(t *testing.T, dir string, args ...string) moriResult {
	return runMoriStdin(t, dir, "", args...)
}

// runMoriStdin runs mori with the given stdin content, for commands that prompt.
// HOME is redirected next to the repo so tests never touch the real ~/.mori.
func runMoriStdin(t *testing.T, dir, stdin string, args ...string) moriResult {
	t.Helper()
	cmd := exec.Command(moriBinary, args...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), "HOME="+testHome(dir))
	cmd.Stdin = strings.NewReader(stdin)

	var outBuf, errBuf bytes.Buffer
	cmd.Stdout = &outBuf
	cmd.Stderr = &errBuf

	err := cmd.Run()
	exitCode := 0
	if exitErr, ok := err.(*exec.ExitError); ok {
		exitCode = exitErr.ExitCode()
	} else if err != nil {
		t.Fatalf("failed to run mori %v: %v", args, err)
	}

	return moriResult{stdout: outBuf.String(), stderr: errBuf.String(), exitCode: exitCode}
}

func gitInRepo(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(),
		"GIT_AUTHOR_NAME=test",
		"GIT_AUTHOR_EMAIL=test@test.com",
		"GIT_COMMITTER_NAME=test",
		"GIT_COMMITTER_EMAIL=test@test.com",
	)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v failed: %v\n%s", args, err, out)
	}
	return string(out)
}

// testHome is the fake home directory for a repo under test: a sibling of the
// repo, so mori's state lands in the test's temp tree.
func testHome(repoDir string) string {
	return filepath.Join(filepath.Dir(repoDir), "home")
}

// worktreePath is where mori puts a branch's worktree for a repo under test.
func worktreePath(repoDir, branch string) string {
	return filepath.Join(testHome(repoDir), ".mori", "worktrees", filepath.Base(repoDir), branch)
}

// tempRoot is t.TempDir() with symlinks resolved. On macOS the temp directory
// sits under /var, which is a link to /private/var, and git reports every
// worktree path resolved. Handing the unresolved spelling to HOME and to the
// paths a test expects makes mori's output differ from it by prefix alone.
func tempRoot(t *testing.T) string {
	t.Helper()
	root, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatalf("failed to resolve temp dir: %v", err)
	}
	return root
}

// initTestRepo creates a temp git repo with one commit on "main", alongside a
// fake home directory for mori's state.
func initTestRepo(t *testing.T) string {
	t.Helper()
	root := tempRoot(t)
	dir := filepath.Join(root, "repo")
	if err := os.MkdirAll(filepath.Join(root, "home"), 0o755); err != nil {
		t.Fatalf("failed to create fake home: %v", err)
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("failed to create repo dir: %v", err)
	}

	gitInRepo(t, dir, "init", "-b", "main")
	os.WriteFile(filepath.Join(dir, "README.md"), []byte("# test\n"), 0o644)
	gitInRepo(t, dir, "add", "README.md")
	gitInRepo(t, dir, "commit", "-m", "initial commit")

	return dir
}

// initTestRepoWithWorktree creates a repo plus one worktree via the mori binary.
func initTestRepoWithWorktree(t *testing.T, branch string) string {
	t.Helper()
	dir := initTestRepo(t)
	res := runMori(t, dir, "new", branch, "-r", dir)
	if res.exitCode != 0 {
		t.Fatalf("setup: failed to create worktree %q: %s", branch, res.stderr)
	}
	return dir
}

type worktreeJSON struct {
	Path        string `json:"path"`
	Branch      string `json:"branch"`
	DisplayPath string `json:"display_path"`
	Head        string `json:"head"`
	IsMain      bool   `json:"is_main"`
	Detached    bool   `json:"detached"`
	Dirty       int    `json:"dirty"`
	Ahead       int    `json:"ahead"`
	Behind      int    `json:"behind"`
	LastCommit  string `json:"last_commit"`
}

func listJSON(t *testing.T, dir string) []worktreeJSON {
	t.Helper()
	res := runMori(t, dir, "list", "--json")
	if res.exitCode != 0 {
		t.Fatalf("mori list --json: exit %d, stderr: %s", res.exitCode, res.stderr)
	}
	var items []worktreeJSON
	if err := json.Unmarshal([]byte(res.stdout), &items); err != nil {
		t.Fatalf("invalid JSON: %v\n%s", err, res.stdout)
	}
	return items
}

func findByBranch(items []worktreeJSON, branch string) *worktreeJSON {
	for i := range items {
		if items[i].Branch == branch {
			return &items[i]
		}
	}
	return nil
}

// ---------------------------------------------------------------------------
// Group 1: Stateless commands
// ---------------------------------------------------------------------------

func TestVersion(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()

	for _, arg := range []string{"version", "--version", "-v"} {
		res := runMori(t, dir, arg)
		if res.exitCode != 0 {
			t.Errorf("mori %s: exit %d, stderr: %s", arg, res.exitCode, res.stderr)
		}
		if !strings.Contains(res.stdout, "mori v") {
			t.Errorf("mori %s: expected version string, got: %q", arg, res.stdout)
		}
	}
}

func TestHelp(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()

	for _, arg := range []string{"help", "--help", "-h"} {
		res := runMori(t, dir, arg)
		if res.exitCode != 0 {
			t.Errorf("mori %s: exit %d", arg, res.exitCode)
		}
		for _, keyword := range []string{"mori new", "mori list", "mori remove", "mori path"} {
			if !strings.Contains(res.stdout, keyword) {
				t.Errorf("mori %s: help text missing %q", arg, keyword)
			}
		}
	}
}

// The agent-tracking surface was dropped; make sure it stays gone.
func TestHelpHasNoAgentCommands(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()

	res := runMori(t, dir, "help")
	for _, gone := range []string{"claude", "Claude", "insights", "status", "PR"} {
		if strings.Contains(res.stdout, gone) {
			t.Errorf("help text still mentions %q:\n%s", gone, res.stdout)
		}
	}
}

func TestUnknownCommand(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()

	res := runMori(t, dir, "foobar")
	if res.exitCode == 0 {
		t.Error("expected non-zero exit for unknown command")
	}
	if !strings.Contains(res.stderr, "unknown command") {
		t.Errorf("expected 'unknown command' in stderr, got: %q", res.stderr)
	}
}

// ---------------------------------------------------------------------------
// Group 2: mori new
// ---------------------------------------------------------------------------

func TestNewWithBranch(t *testing.T) {
	t.Parallel()
	dir := initTestRepo(t)

	res := runMori(t, dir, "new", "feature-x", "-r", dir)
	if res.exitCode != 0 {
		t.Fatalf("exit %d, stderr: %s", res.exitCode, res.stderr)
	}

	wtPath := worktreePath(dir, "feature-x")
	if _, err := os.Stat(wtPath); err != nil {
		t.Errorf("worktree directory not created at %s: %v", wtPath, err)
	}
	if got := strings.TrimSpace(res.stdout); got != wtPath {
		t.Errorf("stdout = %q, want the worktree path %q", got, wtPath)
	}

	branches := gitInRepo(t, dir, "branch", "--list", "feature-x")
	if !strings.Contains(branches, "feature-x") {
		t.Errorf("branch feature-x not created, got: %q", branches)
	}
}

func TestNewAutoBranch(t *testing.T) {
	t.Parallel()
	dir := initTestRepo(t)

	res := runMori(t, dir, "new", "-r", dir)
	if res.exitCode != 0 {
		t.Fatalf("exit %d, stderr: %s", res.exitCode, res.stderr)
	}

	branches := gitInRepo(t, dir, "branch", "--list", "wt-*")
	if !regexp.MustCompile(`wt-[a-z0-9]{5}`).MatchString(branches) {
		t.Errorf("expected an auto-generated wt-XXXXX branch, got: %q", branches)
	}
}

func TestNewFromInsideRepo(t *testing.T) {
	t.Parallel()
	dir := initTestRepo(t)

	res := runMori(t, dir, "new", "inside-branch")
	if res.exitCode != 0 {
		t.Fatalf("exit %d, stderr: %s", res.exitCode, res.stderr)
	}
	if _, err := os.Stat(worktreePath(dir, "inside-branch")); err != nil {
		t.Errorf("worktree not created: %v", err)
	}
}

// Worktrees live outside the repository. Nested inside it, git reported them
// as an untracked embedded repo and `git add -A` committed a gitlink.
func TestNewLeavesRepoClean(t *testing.T) {
	t.Parallel()
	dir := initTestRepo(t)

	if res := runMori(t, dir, "new", "offsite", "-r", dir); res.exitCode != 0 {
		t.Fatalf("exit %d, stderr: %s", res.exitCode, res.stderr)
	}

	if status := gitInRepo(t, dir, "status", "--porcelain"); strings.TrimSpace(status) != "" {
		t.Errorf("creating a worktree dirtied the repo:\n%s", status)
	}
	if entries, err := os.ReadDir(filepath.Join(dir, ".claude")); err == nil {
		t.Errorf("worktree created inside the repo: .claude holds %d entries", len(entries))
	}
}

func TestNewNotAGitRepo(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()

	res := runMori(t, dir, "new", "some-branch")
	if res.exitCode == 0 {
		t.Error("expected non-zero exit outside a git repository")
	}
	if !strings.Contains(res.stderr, "not a git repository") {
		t.Errorf("expected 'not a git repository', got: %q", res.stderr)
	}
}

func TestNewNoCommits(t *testing.T) {
	t.Parallel()
	root := tempRoot(t)
	dir := filepath.Join(root, "repo")
	os.MkdirAll(dir, 0o755)
	gitInRepo(t, dir, "init", "-b", "main")

	res := runMori(t, dir, "new", "branch", "-r", dir)
	if res.exitCode == 0 {
		t.Error("expected non-zero exit in a repo with no commits")
	}
	if !strings.Contains(res.stderr, "no commits") {
		t.Errorf("expected a 'no commits' message, got: %q", res.stderr)
	}
}

func TestNewDuplicateBranch(t *testing.T) {
	t.Parallel()
	dir := initTestRepoWithWorktree(t, "dup")

	res := runMori(t, dir, "new", "dup", "-r", dir)
	if res.exitCode == 0 {
		t.Error("expected non-zero exit when the branch already exists")
	}
}

// ---------------------------------------------------------------------------
// Group 3: mori list
// ---------------------------------------------------------------------------

func TestListBasic(t *testing.T) {
	t.Parallel()
	dir := initTestRepoWithWorktree(t, "listable")

	res := runMori(t, dir, "list")
	if res.exitCode != 0 {
		t.Fatalf("exit %d, stderr: %s", res.exitCode, res.stderr)
	}
	for _, header := range []string{"PATH", "BRANCH", "CHANGES", "SYNC"} {
		if !strings.Contains(res.stdout, header) {
			t.Errorf("table missing %q column:\n%s", header, res.stdout)
		}
	}
	if !strings.Contains(res.stdout, "listable") {
		t.Errorf("expected branch 'listable' in the table:\n%s", res.stdout)
	}
}

// The main working tree is listed with the linked ones — it is where the
// branches come from, and a listing that hides it is missing the row you are
// most often standing in. It is flagged, because it is the one row mori will
// not remove.
func TestListIncludesMainWorktree(t *testing.T) {
	t.Parallel()
	dir := initTestRepo(t)

	items := listJSON(t, dir)
	if len(items) != 1 {
		t.Fatalf("a repo with no linked worktrees should list main alone, got %+v", items)
	}
	if !items[0].IsMain {
		t.Errorf("the main worktree is not flagged is_main: %+v", items[0])
	}

	res := runMori(t, dir, "list")
	if res.exitCode != 0 {
		t.Fatalf("exit %d, stderr: %s", res.exitCode, res.stderr)
	}
	if !strings.Contains(res.stdout, "main") {
		t.Errorf("main worktree missing from the table:\n%s", res.stdout)
	}

	// Listing it is not offering to delete it.
	if res := runMori(t, dir, "remove", "main"); !strings.Contains(res.stderr, "cannot remove the main worktree") {
		t.Errorf("expected a main-worktree error, got: %q", res.stderr)
	}
}

func TestListWithWorktrees(t *testing.T) {
	t.Parallel()
	dir := initTestRepoWithWorktree(t, "listed")

	res := runMori(t, dir, "ls")
	if res.exitCode != 0 {
		t.Fatalf("exit %d, stderr: %s", res.exitCode, res.stderr)
	}
	if !strings.Contains(res.stdout, "listed") {
		t.Errorf("expected branch 'listed' in output:\n%s", res.stdout)
	}
}

func TestListJSON(t *testing.T) {
	t.Parallel()
	dir := initTestRepoWithWorktree(t, "json-branch")

	items := listJSON(t, dir)
	if len(items) != 2 {
		t.Fatalf("got %d worktrees, want 2 (the new one plus main)", len(items))
	}

	wt := findByBranch(items, "json-branch")
	if wt == nil {
		t.Fatalf("worktree 'json-branch' missing from JSON: %+v", items)
	}
	if wt.DisplayPath != filepath.Join("~/.mori", "worktrees", "repo", "json-branch") {
		t.Errorf("display_path = %q", wt.DisplayPath)
	}
	// Short, not seven: git abbreviates a sha to whatever is unambiguous in
	// the repository, which is seven here and more in a large one. Pinning the
	// exact width would assert a rule git does not follow.
	if len(wt.Head) == 0 || len(wt.Head) >= 40 {
		t.Errorf("head = %q, want an abbreviated sha", wt.Head)
	}
	if wt.LastCommit == "" {
		t.Error("last_commit should be set")
	}
}

// A fresh worktree is clean and level with main; editing a file makes it dirty
// and committing puts it ahead.
func TestListJSONTracksGitState(t *testing.T) {
	t.Parallel()
	dir := initTestRepoWithWorktree(t, "stateful")
	wtDir := worktreePath(dir, "stateful")

	wt := findByBranch(listJSON(t, dir), "stateful")
	if wt.Dirty != 0 || wt.Ahead != 0 || wt.Behind != 0 {
		t.Fatalf("fresh worktree should be clean and level, got %+v", wt)
	}

	os.WriteFile(filepath.Join(wtDir, "new.txt"), []byte("hello\n"), 0o644)
	if wt = findByBranch(listJSON(t, dir), "stateful"); wt.Dirty != 1 {
		t.Errorf("dirty = %d, want 1 after adding an untracked file", wt.Dirty)
	}

	gitInRepo(t, wtDir, "add", "new.txt")
	gitInRepo(t, wtDir, "commit", "-m", "add file")
	wt = findByBranch(listJSON(t, dir), "stateful")
	if wt.Dirty != 0 {
		t.Errorf("dirty = %d, want 0 after committing", wt.Dirty)
	}
	if wt.Ahead != 1 || wt.Behind != 0 {
		t.Errorf("ahead/behind = %d/%d, want 1/0 after one commit", wt.Ahead, wt.Behind)
	}
}

func TestListNotInRepo(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()

	res := runMori(t, dir, "list")
	if res.exitCode == 0 {
		t.Error("expected non-zero exit outside a git repository")
	}
	if !strings.Contains(res.stderr, "not a git repository") {
		t.Errorf("expected 'not a git repository', got: %q", res.stderr)
	}
}

// ---------------------------------------------------------------------------
// Group 4: mori path
// ---------------------------------------------------------------------------

func TestPathExistingBranch(t *testing.T) {
	t.Parallel()
	dir := initTestRepoWithWorktree(t, "pathed")

	res := runMori(t, dir, "path", "pathed")
	if res.exitCode != 0 {
		t.Fatalf("exit %d, stderr: %s", res.exitCode, res.stderr)
	}
	if strings.TrimSpace(res.stdout) != worktreePath(dir, "pathed") {
		t.Errorf("stdout = %q, want the worktree path", res.stdout)
	}
}

// `open` is kept as an alias so existing muscle memory still works.
func TestPathOpenAlias(t *testing.T) {
	t.Parallel()
	dir := initTestRepoWithWorktree(t, "aliased")

	res := runMori(t, dir, "open", "aliased")
	if res.exitCode != 0 {
		t.Fatalf("exit %d, stderr: %s", res.exitCode, res.stderr)
	}
	if !strings.Contains(res.stdout, "aliased") {
		t.Errorf("stdout = %q, want the worktree path", res.stdout)
	}
}

func TestPathNonexistentBranch(t *testing.T) {
	t.Parallel()
	dir := initTestRepo(t)

	res := runMori(t, dir, "path", "ghost")
	if res.exitCode == 0 {
		t.Error("expected non-zero exit for an unknown branch")
	}
	if !strings.Contains(res.stderr, "no worktree found") {
		t.Errorf("expected 'no worktree found', got: %q", res.stderr)
	}
}

func TestPathNoBranch(t *testing.T) {
	t.Parallel()
	dir := initTestRepo(t)

	res := runMori(t, dir, "path")
	if res.exitCode == 0 {
		t.Error("expected non-zero exit with no branch argument")
	}
	if !strings.Contains(res.stderr, "Usage:") {
		t.Errorf("expected a usage message, got: %q", res.stderr)
	}
}

// ---------------------------------------------------------------------------
// Group 5: mori remove
// ---------------------------------------------------------------------------

func TestRemoveWorktree(t *testing.T) {
	t.Parallel()
	dir := initTestRepoWithWorktree(t, "removable")
	wtPath := worktreePath(dir, "removable")

	res := runMori(t, dir, "remove", "removable")
	if res.exitCode != 0 {
		t.Fatalf("exit %d, stderr: %s", res.exitCode, res.stderr)
	}
	if _, err := os.Stat(wtPath); !os.IsNotExist(err) {
		t.Errorf("worktree directory still exists at %s", wtPath)
	}
}

// Uncommitted work must not disappear without the user saying so.
func TestRemoveDirtyPromptsAndAborts(t *testing.T) {
	t.Parallel()
	dir := initTestRepoWithWorktree(t, "dirty")
	wtPath := worktreePath(dir, "dirty")
	os.WriteFile(filepath.Join(wtPath, "scratch.txt"), []byte("wip\n"), 0o644)

	res := runMoriStdin(t, dir, "n\n", "remove", "dirty")
	if res.exitCode == 0 {
		t.Error("expected non-zero exit when the user declines")
	}
	if !strings.Contains(res.stderr, "uncommitted") {
		t.Errorf("expected an uncommitted-changes warning, got: %q", res.stderr)
	}
	if _, err := os.Stat(wtPath); err != nil {
		t.Errorf("worktree should still exist after aborting: %v", err)
	}
}

func TestRemoveDirtyForce(t *testing.T) {
	t.Parallel()
	dir := initTestRepoWithWorktree(t, "forced")
	wtPath := worktreePath(dir, "forced")
	os.WriteFile(filepath.Join(wtPath, "scratch.txt"), []byte("wip\n"), 0o644)

	res := runMori(t, dir, "remove", "forced", "--force")
	if res.exitCode != 0 {
		t.Fatalf("exit %d, stderr: %s", res.exitCode, res.stderr)
	}
	if _, err := os.Stat(wtPath); !os.IsNotExist(err) {
		t.Errorf("worktree directory still exists at %s", wtPath)
	}
}

func TestRemoveNonexistentBranch(t *testing.T) {
	t.Parallel()
	dir := initTestRepo(t)

	res := runMori(t, dir, "remove", "ghost")
	if res.exitCode == 0 {
		t.Error("expected non-zero exit for an unknown branch")
	}
	if !strings.Contains(res.stderr, "no worktree found") {
		t.Errorf("expected 'no worktree found', got: %q", res.stderr)
	}
}

func TestRemoveNoBranch(t *testing.T) {
	t.Parallel()
	dir := initTestRepo(t)

	res := runMori(t, dir, "remove")
	if res.exitCode == 0 {
		t.Error("expected non-zero exit with no branch argument")
	}
	if !strings.Contains(res.stderr, "Usage:") {
		t.Errorf("expected a usage message, got: %q", res.stderr)
	}
}

func TestRemoveMainWorktree(t *testing.T) {
	t.Parallel()
	dir := initTestRepo(t)

	res := runMori(t, dir, "remove", "main")
	if res.exitCode == 0 {
		t.Error("expected non-zero exit when removing the main worktree")
	}
	if !strings.Contains(res.stderr, "cannot remove the main worktree") {
		t.Errorf("expected a main-worktree error, got: %q", res.stderr)
	}
}

// ---------------------------------------------------------------------------
// Group 6: configuration
// ---------------------------------------------------------------------------

func TestNewWithProjectHooks(t *testing.T) {
	t.Parallel()
	dir := initTestRepo(t)

	config := `{"post_create": [{"name": "Marker", "cmd": "echo hooked > hook.txt"}]}`
	os.WriteFile(filepath.Join(dir, ".mori.json"), []byte(config), 0o644)

	res := runMori(t, dir, "new", "hooked", "-r", dir)
	if res.exitCode != 0 {
		t.Fatalf("exit %d, stderr: %s", res.exitCode, res.stderr)
	}

	marker := filepath.Join(worktreePath(dir, "hooked"), "hook.txt")
	data, err := os.ReadFile(marker)
	if err != nil {
		t.Fatalf("post_create hook did not run: %v\nstderr: %s", err, res.stderr)
	}
	if strings.TrimSpace(string(data)) != "hooked" {
		t.Errorf("hook wrote %q, want 'hooked'", data)
	}
	if !strings.Contains(res.stderr, "Marker") {
		t.Errorf("expected the hook name in progress output, got: %q", res.stderr)
	}
}

// A failing hook is a warning, not a failure — the worktree still exists.
func TestNewWithFailingHook(t *testing.T) {
	t.Parallel()
	dir := initTestRepo(t)

	config := `{"post_create": [{"name": "Broken", "cmd": "exit 1"}]}`
	os.WriteFile(filepath.Join(dir, ".mori.json"), []byte(config), 0o644)

	res := runMori(t, dir, "new", "half-setup", "-r", dir)
	if res.exitCode != 0 {
		t.Fatalf("a failing hook should not fail the command: exit %d, stderr: %s", res.exitCode, res.stderr)
	}
	if _, err := os.Stat(worktreePath(dir, "half-setup")); err != nil {
		t.Errorf("worktree not created: %v", err)
	}
}

// ---------------------------------------------------------------------------
// Group 7: lifecycle
// ---------------------------------------------------------------------------

func TestFullLifecycle(t *testing.T) {
	t.Parallel()
	dir := initTestRepo(t)

	// Create three worktrees.
	for _, branch := range []string{"one", "two", "three"} {
		if res := runMori(t, dir, "new", branch, "-r", dir); res.exitCode != 0 {
			t.Fatalf("new %s: exit %d, stderr: %s", branch, res.exitCode, res.stderr)
		}
	}

	items := listJSON(t, dir)
	if len(items) != 4 {
		t.Fatalf("got %d worktrees, want 4 (three new ones plus main)", len(items))
	}

	// Each one resolves back to a real directory.
	for _, branch := range []string{"one", "two", "three"} {
		res := runMori(t, dir, "path", branch)
		if res.exitCode != 0 {
			t.Fatalf("path %s: exit %d, stderr: %s", branch, res.exitCode, res.stderr)
		}
		if _, err := os.Stat(strings.TrimSpace(res.stdout)); err != nil {
			t.Errorf("path %s points at a missing directory: %v", branch, err)
		}
	}

	// Remove them all.
	for _, branch := range []string{"one", "two", "three"} {
		if res := runMori(t, dir, "remove", branch); res.exitCode != 0 {
			t.Fatalf("remove %s: exit %d, stderr: %s", branch, res.exitCode, res.stderr)
		}
	}

	if items := listJSON(t, dir); len(items) != 1 {
		t.Errorf("got %d worktrees after cleanup, want 1 (main)", len(items))
	}
}
