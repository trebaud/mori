// Package internal holds mori's worktree model and configuration.
package internal

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/trebaud/mori/v2/internal/git"
)

// Worktree is a single git worktree plus the git state mori displays for it.
type Worktree struct {
	Path        string    // absolute path on disk
	Branch      string    // branch name, empty when detached
	DisplayPath string    // path as shown in the UI (repo-relative or ~-collapsed)
	Head        string    // short HEAD sha
	IsMain      bool      // true for the main working tree
	Detached    bool      // true when HEAD is not on a branch
	Dirty       int       // number of files with uncommitted changes
	Ahead       int       // commits ahead of the default branch
	Behind      int       // commits behind the default branch
	LastCommit  time.Time // timestamp of HEAD
	Subject     string    // subject line of HEAD
	Created     time.Time // when the worktree was created, zero for the main tree

	sha string // full HEAD sha, for batching git queries
}

// Age is the time the worktree has existed, which is what a listing means by
// "age": these are short-lived directories, and the date on the commit they
// were branched from says nothing about when you made one. Falls back to the
// HEAD timestamp when the creation time is unavailable — the main working
// tree, or a worktree git created some other way.
func (w Worktree) Age() time.Time {
	if !w.Created.IsZero() {
		return w.Created
	}
	return w.LastCommit
}

// Label is the worktree's display name: its branch, or a detached HEAD marker.
func (w Worktree) Label() string {
	if w.Branch != "" {
		return w.Branch
	}
	if w.Head != "" {
		return "(detached " + w.Head + ")"
	}
	return "(detached)"
}

// DirtyUnknown is Dirty before `git status` has been read for the worktree.
// It is not zero on purpose: zero means clean, and a worktree nobody has
// looked at yet is not known to be clean.
const DirtyUnknown = -1

// List returns every worktree of the repository containing the working
// directory — the main working tree included — enriched with git state.
func List() ([]Worktree, error) {
	wts, err := ListQuick()
	if err != nil {
		return nil, err
	}
	FillDirty(wts)
	return wts, nil
}

// ListQuick is List without the uncommitted-change counts, which it leaves at
// DirtyUnknown. Those are the slow part — `git status` stats every tracked
// file of every worktree — while everything else comes from a handful of git
// processes however many worktrees there are: one `git log` dates every HEAD
// and one `for-each-ref` counts every branch against the default. The TUI
// draws this first and fills the counts in after.
func ListQuick() ([]Worktree, error) {
	mainPath, err := git.FindMainRepo(".")
	if err != nil {
		return nil, fmt.Errorf("not a git repository")
	}

	out, err := git.WorktreeList(mainPath)
	if err != nil {
		return nil, fmt.Errorf("failed to list worktrees: %w", err)
	}

	wts := parseList(out)
	base := git.DefaultBranch(mainPath)
	home := homeDir()

	var shas []string
	for i := range wts {
		wts[i].IsMain = wts[i].Path == mainPath
		wts[i].DisplayPath = displayPath(wts[i].Path, mainPath, home)
		wts[i].Created = git.Created(wts[i].Path)
		wts[i].Dirty = DirtyUnknown
		// An unborn branch reports an all-zero HEAD, which would fail the
		// whole batch; it has no commit to date anyway.
		if strings.Trim(wts[i].sha, "0") != "" {
			shas = append(shas, wts[i].sha)
		}
	}

	var (
		stamps   map[string]git.CommitStamp
		stampErr error
		counts   map[string][2]int
		wg       sync.WaitGroup
	)
	wg.Add(2)
	go func() { defer wg.Done(); stamps, stampErr = git.CommitStamps(mainPath, shas) }()
	// On failure counts stays nil, and every row asks for itself below.
	go func() { defer wg.Done(); counts, _ = git.BranchesAheadBehind(mainPath, base) }()
	wg.Wait()

	// Whatever the batches could not answer — an old git, a detached HEAD,
	// a sha git refused — is asked per worktree, as it always was.
	forEach(len(wts), func(i int) {
		w := &wts[i]
		if stampErr == nil {
			st := stamps[w.sha]
			w.LastCommit, w.Subject = st.When, st.Subject
		} else {
			w.LastCommit, w.Subject = git.LastCommit(w.Path)
		}
		if w.Branch == base {
			return
		}
		if ab, ok := counts[w.Branch]; ok {
			w.Ahead, w.Behind = ab[0], ab[1]
		} else {
			w.Ahead, w.Behind = git.AheadBehind(w.Path, base)
		}
	})

	return wts, nil
}

// FillDirty counts the uncommitted changes of each worktree in place.
func FillDirty(wts []Worktree) {
	forEach(len(wts), func(i int) { wts[i].Dirty = git.DirtyCount(wts[i].Path) })
}

// forEach runs fn for 0..n-1, as many at a time as there are CPUs. Each call
// is a git process, and `git status` is disk-bound: fifty of them started at
// once only queue on the same disk and the same scheduler, and slow down the
// terminal they are meant to be filling.
func forEach(n int, fn func(i int)) {
	sem := make(chan struct{}, runtime.NumCPU())
	var wg sync.WaitGroup
	for i := range n {
		wg.Add(1)
		sem <- struct{}{}
		go func() {
			defer func() { <-sem; wg.Done() }()
			fn(i)
		}()
	}
	wg.Wait()
}

// parseList turns `git worktree list --porcelain` output into Worktrees.
// Bare repository entries are skipped — they have no working tree to manage.
func parseList(output string) []Worktree {
	scanner := bufio.NewScanner(strings.NewReader(output))
	var wts []Worktree
	var current *Worktree
	bare := false

	flush := func() {
		if current != nil && !bare {
			wts = append(wts, *current)
		}
		current, bare = nil, false
	}

	for scanner.Scan() {
		line := scanner.Text()
		switch {
		case strings.HasPrefix(line, "worktree "):
			flush()
			current = &Worktree{Path: strings.TrimPrefix(line, "worktree ")}
		case current == nil:
			// Attribute before any worktree header; ignore.
		case strings.HasPrefix(line, "HEAD "):
			sha := strings.TrimPrefix(line, "HEAD ")
			current.sha = sha
			if len(sha) > 7 {
				sha = sha[:7]
			}
			current.Head = sha
		case strings.HasPrefix(line, "branch "):
			// Keep slashes: refs/heads/feat/parser -> feat/parser.
			current.Branch = strings.TrimPrefix(strings.TrimPrefix(line, "branch "), "refs/heads/")
		case line == "detached":
			current.Detached = true
		case line == "bare":
			bare = true
		}
	}
	flush()

	return wts
}

// displayPath renders a worktree path for the UI with the home directory
// collapsed to ~. Worktrees created before they moved out of the repository
// still live inside it, and read better relative to the repo root.
func displayPath(path, mainPath, home string) string {
	if path != mainPath && strings.HasPrefix(path, mainPath+string(filepath.Separator)) {
		return path[len(mainPath)+1:]
	}
	// The separator matters: without it a home of /home/tom claims every path
	// under /home/tomato and renders it as "~ato/...".
	if home != "" && (path == home || strings.HasPrefix(path, home+string(filepath.Separator))) {
		return "~" + path[len(home):]
	}
	return path
}

// homeDir is the home directory with symlinks resolved. git reports worktree
// paths resolved, so an unresolved home — /home/me pointing somewhere else on
// the disk — never matches one by prefix, and every path prints in full.
func homeDir() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	if resolved, err := filepath.EvalSymlinks(home); err == nil {
		return resolved
	}
	return home
}

// CreateResult holds the outcome of a worktree creation.
type CreateResult struct {
	Dir         string
	BaseBranch  string
	HookResults []HookResult
}

// CreateWorktree creates a new git worktree on a new branch cut from base,
// then runs the post-create hooks. An empty base means the repository's
// default branch, which is what most worktrees are cut from; naming another
// is how you stack one branch on top of the one you are already working on.
// When cb is non-nil, progress is reported per step so callers can render
// live feedback.
func CreateWorktree(repoRoot, branch, base string, cb *HookCallbacks) (CreateResult, error) {
	hasCommits, err := git.HasCommits(repoRoot)
	if err != nil {
		return CreateResult{}, fmt.Errorf("failed to check repo: %w", err)
	}
	if !hasCommits {
		return CreateResult{}, fmt.Errorf("repository has no commits, cannot create worktree. Make at least one commit first")
	}

	baseBranch := base
	if baseBranch == "" {
		baseBranch = git.DefaultBranch(repoRoot)
	}
	if err := claimRepoDir(repoRoot); err != nil {
		return CreateResult{}, fmt.Errorf("failed to prepare %s: %w", WorktreesRoot(), err)
	}
	dir := WorktreeDir(repoRoot, branch)

	branchStep := "Creating branch from " + baseBranch
	if cb != nil && cb.OnStart != nil {
		cb.OnStart(branchStep)
	}
	addErr := git.AddWorktree(repoRoot, dir, branch, baseBranch)
	if cb != nil && cb.OnComplete != nil {
		var out string
		if addErr != nil {
			out = addErr.Error()
		}
		cb.OnComplete(branchStep, addErr == nil, out)
	}
	if addErr != nil {
		return CreateResult{}, fmt.Errorf("failed to create worktree: %w", addErr)
	}

	cfg := Load(repoRoot)
	return CreateResult{
		Dir:         dir,
		BaseBranch:  baseBranch,
		HookResults: RunPostCreateHooks(dir, cfg.PostCreate, cb),
	}, nil
}

// RemoveWorktree removes the git worktree at path.
func RemoveWorktree(path string, force bool) error {
	if err := git.RemoveWorktree(path, force); err != nil {
		return fmt.Errorf("failed to remove worktree: %w", err)
	}
	return nil
}

// RestoreWorktree checks a branch back out at path, undoing a removal. The
// branch outlives `git worktree remove`, so everything that was committed
// comes back; what was uncommitted when it went does not.
func RestoreWorktree(repoRoot, path, branch string) error {
	if branch == "" {
		return fmt.Errorf("cannot restore a detached worktree")
	}
	if err := git.AttachWorktree(repoRoot, path, branch); err != nil {
		return fmt.Errorf("failed to restore worktree: %w", err)
	}
	return nil
}

// FindByBranch returns the worktree on the given branch, or nil.
func FindByBranch(worktrees []Worktree, branch string) *Worktree {
	for i := range worktrees {
		if worktrees[i].Branch == branch {
			return &worktrees[i]
		}
	}
	return nil
}

// SortMode orders a worktree list.
type SortMode int

const (
	// SortDefault keeps git's order, which starts at the main worktree.
	SortDefault SortMode = iota
	// SortRecent puts the most recently committed worktrees first.
	SortRecent
	// SortName orders alphabetically by branch.
	SortName
)

func (s SortMode) String() string {
	switch s {
	case SortRecent:
		return "recent"
	case SortName:
		return "name"
	default:
		return "default"
	}
}

// Next cycles to the following sort mode.
func (s SortMode) Next() SortMode { return (s + 1) % 3 }

// SortIndices stably reorders indices into wts according to mode.
func SortIndices(wts []Worktree, indices []int, mode SortMode) {
	if mode == SortDefault {
		return
	}
	sort.SliceStable(indices, func(a, b int) bool {
		x, y := wts[indices[a]], wts[indices[b]]
		switch mode {
		case SortRecent:
			return x.Age().After(y.Age())
		case SortName:
			return strings.ToLower(x.Label()) < strings.ToLower(y.Label())
		}
		return false
	})
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
