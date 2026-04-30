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
	return runMoriEnv(t, dir, nil, args...)
}

// runMoriEnv runs mori with extra env vars appended after the parent env.
// By default MORI_GH_PATH is set to an unreachable path so tests don't hit a
// real gh on the host; tests that need a stub can override via extraEnv.
func runMoriEnv(t *testing.T, dir string, extraEnv []string, args ...string) moriResult {
	t.Helper()
	cmd := exec.Command(moriBinary, args...)
	cmd.Dir = dir
	env := append(os.Environ(), "MORI_GH_PATH=/nonexistent/gh")
	cmd.Env = append(env, extraEnv...)

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

	return moriResult{
		stdout:   outBuf.String(),
		stderr:   errBuf.String(),
		exitCode: exitCode,
	}
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

// initTestRepo creates a temp git repo with one commit on "main".
func initTestRepo(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()

	gitInRepo(t, dir, "init", "-b", "main")
	os.WriteFile(filepath.Join(dir, "README.md"), []byte("# test\n"), 0644)
	gitInRepo(t, dir, "add", "README.md")
	gitInRepo(t, dir, "commit", "-m", "initial commit")

	return dir
}

// initTestRepoWithWorktree creates a repo and a worktree via the mori binary.
func initTestRepoWithWorktree(t *testing.T, branch string) string {
	t.Helper()
	dir := initTestRepo(t)
	res := runMori(t, dir, "new", branch, "-r", dir)
	if res.exitCode != 0 {
		t.Fatalf("setup: failed to create worktree %q: %s", branch, res.stderr)
	}
	return dir
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
		for _, keyword := range []string{"mori new", "mori list", "mori remove", "mori open", "mori status"} {
			if !strings.Contains(res.stdout, keyword) {
				t.Errorf("mori %s: help text missing %q", arg, keyword)
			}
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

	if !strings.Contains(res.stderr, "feature-x") {
		t.Errorf("stderr missing branch name, got: %q", res.stderr)
	}
	if !strings.Contains(res.stderr, "Creating branch from main") {
		t.Errorf("stderr missing base branch info, got: %q", res.stderr)
	}

	wtDir := filepath.Join(dir, ".claude", "worktrees", "feature-x")
	if _, err := os.Stat(wtDir); os.IsNotExist(err) {
		t.Errorf("worktree directory does not exist: %s", wtDir)
	}
}

func TestNewAutoBranch(t *testing.T) {
	t.Parallel()
	dir := initTestRepo(t)

	res := runMori(t, dir, "new", "-r", dir)
	if res.exitCode != 0 {
		t.Fatalf("exit %d, stderr: %s", res.exitCode, res.stderr)
	}

	pattern := regexp.MustCompile(`wt-[a-z0-9]{5}`)
	if !pattern.MatchString(res.stderr) {
		t.Errorf("expected auto-generated branch name matching wt-xxxxx in stderr, got: %q", res.stderr)
	}

	// Verify the worktree dir was created
	wtBase := filepath.Join(dir, ".claude", "worktrees")
	entries, err := os.ReadDir(wtBase)
	if err != nil {
		t.Fatalf("cannot read worktrees dir: %v", err)
	}
	found := false
	for _, e := range entries {
		if pattern.MatchString(e.Name()) {
			found = true
			break
		}
	}
	if !found {
		t.Error("no worktree directory matching wt-xxxxx found")
	}
}

func TestNewFromInsideRepo(t *testing.T) {
	t.Parallel()
	dir := initTestRepo(t)

	// Run without -r flag, cwd is the repo
	res := runMori(t, dir, "new", "inside-test")
	if res.exitCode != 0 {
		t.Fatalf("exit %d, stderr: %s", res.exitCode, res.stderr)
	}

	wtDir := filepath.Join(dir, ".claude", "worktrees", "inside-test")
	if _, err := os.Stat(wtDir); os.IsNotExist(err) {
		t.Errorf("worktree directory does not exist: %s", wtDir)
	}
}

func TestNewNotAGitRepo(t *testing.T) {
	t.Parallel()
	dir := t.TempDir() // plain dir, no git init

	res := runMori(t, dir, "new", "some-branch", "-r", dir)
	if res.exitCode == 0 {
		t.Error("expected non-zero exit for non-repo dir")
	}
	if !strings.Contains(res.stderr, "not a git repository") {
		t.Errorf("expected 'not a git repository' in stderr, got: %q", res.stderr)
	}
}

func TestNewNoCommits(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	gitInRepo(t, dir, "init", "-b", "main")

	res := runMori(t, dir, "new", "branch", "-r", dir)
	if res.exitCode == 0 {
		t.Error("expected non-zero exit for repo with no commits")
	}
	if !strings.Contains(res.stderr, "no commits") {
		t.Errorf("expected 'no commits' in stderr, got: %q", res.stderr)
	}
}

func TestNewDuplicateBranch(t *testing.T) {
	t.Parallel()
	dir := initTestRepoWithWorktree(t, "dupe")

	res := runMori(t, dir, "new", "dupe", "-r", dir)
	if res.exitCode == 0 {
		t.Error("expected non-zero exit for duplicate branch")
	}
}

// ---------------------------------------------------------------------------
// Group 3: mori list
// ---------------------------------------------------------------------------

func TestListBasic(t *testing.T) {
	t.Parallel()
	dir := initTestRepo(t)

	res := runMori(t, dir, "list")
	if res.exitCode != 0 {
		t.Fatalf("exit %d, stderr: %s", res.exitCode, res.stderr)
	}

	for _, header := range []string{"PATH", "BRANCH", "SESSION", "COST"} {
		if !strings.Contains(res.stdout, header) {
			t.Errorf("table output missing header %q", header)
		}
	}
	if !strings.Contains(res.stdout, "main") {
		t.Error("table output missing 'main' branch")
	}
}

func TestListWithWorktrees(t *testing.T) {
	t.Parallel()
	dir := initTestRepo(t)
	runMori(t, dir, "new", "alpha", "-r", dir)
	runMori(t, dir, "new", "beta", "-r", dir)

	res := runMori(t, dir, "list")
	if res.exitCode != 0 {
		t.Fatalf("exit %d, stderr: %s", res.exitCode, res.stderr)
	}

	for _, branch := range []string{"main", "alpha", "beta"} {
		if !strings.Contains(res.stdout, branch) {
			t.Errorf("list output missing branch %q", branch)
		}
	}
}

func TestListJSON(t *testing.T) {
	t.Parallel()
	dir := initTestRepoWithWorktree(t, "json-test")

	res := runMori(t, dir, "list", "--json")
	if res.exitCode != 0 {
		t.Fatalf("exit %d, stderr: %s", res.exitCode, res.stderr)
	}

	var items []map[string]any
	if err := json.Unmarshal([]byte(res.stdout), &items); err != nil {
		t.Fatalf("invalid JSON output: %v\nraw: %s", err, res.stdout)
	}

	if len(items) < 2 {
		t.Fatalf("expected at least 2 entries (main + json-test), got %d", len(items))
	}

	// Verify required keys exist
	for _, item := range items {
		for _, key := range []string{"path", "branch", "session"} {
			if _, ok := item[key]; !ok {
				t.Errorf("JSON entry missing key %q: %v", key, item)
			}
		}
	}

	// Verify our branch is present
	found := false
	for _, item := range items {
		if item["branch"] == "json-test" {
			found = true
			break
		}
	}
	if !found {
		t.Error("JSON output missing 'json-test' branch")
	}
}

func TestListInvalidStatus(t *testing.T) {
	t.Parallel()
	dir := initTestRepo(t)

	res := runMori(t, dir, "list", "--status", "invalid")
	if res.exitCode == 0 {
		t.Error("expected non-zero exit for invalid status filter")
	}
	if !strings.Contains(res.stderr, "invalid status") {
		t.Errorf("expected 'invalid status' in stderr, got: %q", res.stderr)
	}
}

func TestListStatusFilterNone(t *testing.T) {
	t.Parallel()
	dir := initTestRepoWithWorktree(t, "filtered")

	res := runMori(t, dir, "list", "--status", "none")
	if res.exitCode != 0 {
		t.Fatalf("exit %d, stderr: %s", res.exitCode, res.stderr)
	}

	// All test worktrees have no Claude sessions, so all should be StatusNone
	if !strings.Contains(res.stdout, "PATH") {
		t.Error("expected table output with headers")
	}
}

func TestListNotInRepo(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()

	res := runMori(t, dir, "list")
	if res.exitCode == 0 {
		t.Error("expected non-zero exit outside a git repo")
	}
	if !strings.Contains(res.stderr, "not a git repository") {
		t.Errorf("expected 'not a git repository' in stderr, got: %q", res.stderr)
	}
}

// ---------------------------------------------------------------------------
// Group 4: mori open
// ---------------------------------------------------------------------------

func TestOpenExistingBranch(t *testing.T) {
	t.Parallel()
	dir := initTestRepoWithWorktree(t, "open-me")

	stub, err := filepath.Abs("fixtures/claude-stub.sh")
	if err != nil {
		t.Fatal(err)
	}

	res := runMoriEnv(t, dir, []string{"MORI_CLAUDE_PATH=" + stub}, "open", "open-me")
	if res.exitCode != 0 {
		t.Fatalf("exit %d, stderr: %s", res.exitCode, res.stderr)
	}

	path := strings.TrimSpace(res.stdout)
	if path == "" {
		t.Fatal("expected stub to print worktree path, got empty")
	}
	if _, err := os.Stat(path); os.IsNotExist(err) {
		t.Errorf("printed path does not exist: %s", path)
	}
}

func TestOpenNonexistentBranch(t *testing.T) {
	t.Parallel()
	dir := initTestRepo(t)

	res := runMori(t, dir, "open", "nonexistent")
	if res.exitCode == 0 {
		t.Error("expected non-zero exit for nonexistent branch")
	}
	if !strings.Contains(res.stderr, "no worktree found") {
		t.Errorf("expected 'no worktree found' in stderr, got: %q", res.stderr)
	}
}

func TestOpenNoBranch(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()

	res := runMori(t, dir, "open")
	if res.exitCode == 0 {
		t.Error("expected non-zero exit when no branch given")
	}
	if !strings.Contains(res.stderr, "Usage") {
		t.Errorf("expected usage message in stderr, got: %q", res.stderr)
	}
}

// ---------------------------------------------------------------------------
// Group 5: mori remove
// ---------------------------------------------------------------------------

func TestRemoveWorktree(t *testing.T) {
	t.Parallel()
	dir := initTestRepoWithWorktree(t, "remove-me")

	wtDir := filepath.Join(dir, ".claude", "worktrees", "remove-me")
	if _, err := os.Stat(wtDir); os.IsNotExist(err) {
		t.Fatal("setup: worktree dir should exist before remove")
	}

	res := runMori(t, dir, "remove", "remove-me", "--force")
	if res.exitCode != 0 {
		t.Fatalf("exit %d, stderr: %s", res.exitCode, res.stderr)
	}

	if !strings.Contains(res.stderr, "Removing worktree") {
		t.Errorf("expected removal message in stderr, got: %q", res.stderr)
	}

	if _, err := os.Stat(wtDir); !os.IsNotExist(err) {
		t.Error("worktree directory still exists after removal")
	}

	// Verify it's gone from the list
	listRes := runMori(t, dir, "list", "--json")
	if strings.Contains(listRes.stdout, "remove-me") {
		t.Error("removed worktree still appears in list")
	}
}

func TestRemoveNonexistentBranch(t *testing.T) {
	t.Parallel()
	dir := initTestRepo(t)

	res := runMori(t, dir, "remove", "ghost", "--force")
	if res.exitCode == 0 {
		t.Error("expected non-zero exit for nonexistent branch")
	}
	if !strings.Contains(res.stderr, "no worktree found") {
		t.Errorf("expected 'no worktree found' in stderr, got: %q", res.stderr)
	}
}

func TestRemoveNoBranch(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()

	res := runMori(t, dir, "remove")
	if res.exitCode == 0 {
		t.Error("expected non-zero exit when no branch given")
	}
	if !strings.Contains(res.stderr, "Usage") {
		t.Errorf("expected usage message in stderr, got: %q", res.stderr)
	}
}

func TestRemoveMainWorktree(t *testing.T) {
	t.Parallel()
	dir := initTestRepo(t)

	res := runMori(t, dir, "remove", "main", "--force")
	if res.exitCode == 0 {
		t.Error("expected non-zero exit when removing main worktree")
	}
	if !strings.Contains(res.stderr, "cannot remove the main worktree") {
		t.Errorf("expected 'cannot remove the main worktree' in stderr, got: %q", res.stderr)
	}
}

// ---------------------------------------------------------------------------
// Group 6: mori status
// ---------------------------------------------------------------------------

func TestStatusBasic(t *testing.T) {
	t.Parallel()
	dir := initTestRepo(t)

	res := runMori(t, dir, "status")
	if res.exitCode != 0 {
		t.Fatalf("exit %d, stderr: %s", res.exitCode, res.stderr)
	}

	if !strings.Contains(res.stdout, "1 worktrees") {
		t.Errorf("expected '1 worktrees' in output, got: %q", res.stdout)
	}
}

func TestStatusWithWorktrees(t *testing.T) {
	t.Parallel()
	dir := initTestRepo(t)
	runMori(t, dir, "new", "st-a", "-r", dir)
	runMori(t, dir, "new", "st-b", "-r", dir)

	res := runMori(t, dir, "status")
	if res.exitCode != 0 {
		t.Fatalf("exit %d, stderr: %s", res.exitCode, res.stderr)
	}

	if !strings.Contains(res.stdout, "3 worktrees") {
		t.Errorf("expected '3 worktrees', got: %q", res.stdout)
	}
	if !strings.Contains(res.stdout, "no session") {
		t.Errorf("expected 'no session' in status, got: %q", res.stdout)
	}
}

// ---------------------------------------------------------------------------
// Group 7: Post-Create Hooks
// ---------------------------------------------------------------------------

func TestNewWithProjectHooks(t *testing.T) {
	t.Parallel()
	dir := initTestRepo(t)

	configJSON := `{"post_create": [{"name": "Install deps", "cmd": "echo hook-ran > hook-proof.txt"}]}`
	os.WriteFile(filepath.Join(dir, ".mori.json"), []byte(configJSON), 0644)

	res := runMori(t, dir, "new", "hooked", "-r", dir)
	if res.exitCode != 0 {
		t.Fatalf("exit %d, stderr: %s", res.exitCode, res.stderr)
	}

	if !strings.Contains(res.stderr, "Install deps") {
		t.Errorf("expected hook name in stderr, got: %q", res.stderr)
	}

	proofFile := filepath.Join(dir, ".claude", "worktrees", "hooked", "hook-proof.txt")
	data, err := os.ReadFile(proofFile)
	if err != nil {
		t.Fatalf("hook proof file not found: %v", err)
	}
	if !strings.Contains(string(data), "hook-ran") {
		t.Errorf("expected 'hook-ran' in proof file, got: %q", string(data))
	}
}

// ---------------------------------------------------------------------------
// Group 8: End-to-End Lifecycle
// ---------------------------------------------------------------------------

func TestFullLifecycle(t *testing.T) {
	t.Parallel()
	dir := initTestRepo(t)
	branch := "lifecycle-test"

	// 1. Create worktree
	res := runMori(t, dir, "new", branch, "-r", dir)
	if res.exitCode != 0 {
		t.Fatalf("new: exit %d, stderr: %s", res.exitCode, res.stderr)
	}

	wtDir := filepath.Join(dir, ".claude", "worktrees", branch)
	if _, err := os.Stat(wtDir); os.IsNotExist(err) {
		t.Fatal("worktree dir not created")
	}

	// 2. List shows it
	res = runMori(t, dir, "list", "--json")
	if res.exitCode != 0 {
		t.Fatalf("list: exit %d", res.exitCode)
	}
	if !strings.Contains(res.stdout, branch) {
		t.Fatalf("list JSON missing branch %q", branch)
	}

	// 3. Open launches claude inside the worktree
	stub, err := filepath.Abs("fixtures/claude-stub.sh")
	if err != nil {
		t.Fatal(err)
	}
	res = runMoriEnv(t, dir, []string{"MORI_CLAUDE_PATH=" + stub}, "open", branch)
	if res.exitCode != 0 {
		t.Fatalf("open: exit %d", res.exitCode)
	}
	openedPath := strings.TrimSpace(res.stdout)
	if _, err := os.Stat(openedPath); os.IsNotExist(err) {
		t.Fatalf("opened path does not exist: %s", openedPath)
	}

	// 4. Status shows correct count
	res = runMori(t, dir, "status")
	if res.exitCode != 0 {
		t.Fatalf("status: exit %d", res.exitCode)
	}
	if !strings.Contains(res.stdout, "2 worktrees") {
		t.Errorf("expected '2 worktrees', got: %q", res.stdout)
	}

	// 5. Remove it
	res = runMori(t, dir, "remove", branch, "--force")
	if res.exitCode != 0 {
		t.Fatalf("remove: exit %d, stderr: %s", res.exitCode, res.stderr)
	}

	// 6. List no longer shows it
	res = runMori(t, dir, "list", "--json")
	if strings.Contains(res.stdout, branch) {
		t.Error("removed branch still in list")
	}

	// 7. Status shows reduced count
	res = runMori(t, dir, "status")
	if !strings.Contains(res.stdout, "1 worktrees") {
		t.Errorf("expected '1 worktrees' after removal, got: %q", res.stdout)
	}
}

// ---------------------------------------------------------------------------
// Group 5: PR status (gh integration)
// ---------------------------------------------------------------------------

func TestListJSONOmitsPRWhenGhMissing(t *testing.T) {
	t.Parallel()
	dir := initTestRepoWithWorktree(t, "feat-no-gh")

	// runMori already sets MORI_GH_PATH=/nonexistent/gh; so gh fetches fail.
	res := runMori(t, dir, "list", "--json")
	if res.exitCode != 0 {
		t.Fatalf("exit %d, stderr: %s", res.exitCode, res.stderr)
	}
	if strings.Contains(res.stdout, `"pr"`) {
		t.Errorf("expected no pr field when gh missing, got: %s", res.stdout)
	}
}

func TestListJSONIncludesPRWithGhStub(t *testing.T) {
	t.Parallel()
	dir := initTestRepoWithWorktree(t, "feat-with-pr")

	stub, err := filepath.Abs("fixtures/gh-stub-pr.sh")
	if err != nil {
		t.Fatal(err)
	}

	res := runMoriEnv(t, dir, []string{"MORI_GH_PATH=" + stub}, "list", "--json")
	if res.exitCode != 0 {
		t.Fatalf("exit %d, stderr: %s", res.exitCode, res.stderr)
	}

	var items []map[string]any
	if err := json.Unmarshal([]byte(res.stdout), &items); err != nil {
		t.Fatalf("invalid json: %v\n%s", err, res.stdout)
	}

	var found bool
	for _, item := range items {
		if item["branch"] != "feat-with-pr" {
			continue
		}
		pr, ok := item["pr"].(map[string]any)
		if !ok {
			t.Fatalf("expected pr object on worktree, got: %v", item)
		}
		if int(pr["number"].(float64)) != 42 {
			t.Errorf("expected PR number 42, got: %v", pr["number"])
		}
		if pr["state"] != "open" {
			t.Errorf("expected state=open, got: %v", pr["state"])
		}
		found = true
	}
	if !found {
		t.Errorf("did not find feat-with-pr worktree in output: %s", res.stdout)
	}
}

func TestListJSONOmitsPRWhenStubReturnsEmpty(t *testing.T) {
	t.Parallel()
	dir := initTestRepoWithWorktree(t, "feat-no-pr")

	stub, err := filepath.Abs("fixtures/gh-stub-empty.sh")
	if err != nil {
		t.Fatal(err)
	}

	res := runMoriEnv(t, dir, []string{"MORI_GH_PATH=" + stub}, "list", "--json")
	if res.exitCode != 0 {
		t.Fatalf("exit %d, stderr: %s", res.exitCode, res.stderr)
	}
	if strings.Contains(res.stdout, `"pr"`) {
		t.Errorf("expected no pr field when stub returns empty, got: %s", res.stdout)
	}
}
