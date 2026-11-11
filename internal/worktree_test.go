package internal

import (
	"testing"
	"time"
)

const porcelain = `worktree /home/t/repo
HEAD 1111111111111111111111111111111111111111
branch refs/heads/main

worktree /home/t/.mori/worktrees/repo/feat-parser
HEAD 2222222222222222222222222222222222222222
branch refs/heads/feat/parser

worktree /home/t/.mori/worktrees/repo/spike
HEAD 3333333333333333333333333333333333333333
detached

worktree /home/t/bare
bare
`

func TestParseList(t *testing.T) {
	wts := parseList(porcelain)
	if len(wts) != 3 {
		t.Fatalf("got %d worktrees, want 3 (bare entries are skipped)", len(wts))
	}

	if wts[0].Branch != "main" || wts[0].Head != "1111111" {
		t.Errorf("main: got branch=%q head=%q", wts[0].Branch, wts[0].Head)
	}
	// Branch names with slashes must survive the refs/heads/ prefix strip.
	if wts[1].Branch != "feat/parser" {
		t.Errorf("got branch %q, want feat/parser", wts[1].Branch)
	}
	if !wts[2].Detached || wts[2].Branch != "" {
		t.Errorf("detached: got detached=%v branch=%q", wts[2].Detached, wts[2].Branch)
	}
}

func TestLabel(t *testing.T) {
	cases := []struct {
		wt   Worktree
		want string
	}{
		{Worktree{Branch: "feat/parser"}, "feat/parser"},
		{Worktree{Head: "abc1234", Detached: true}, "(detached abc1234)"},
		{Worktree{}, "(detached)"},
	}
	for _, c := range cases {
		if got := c.wt.Label(); got != c.want {
			t.Errorf("Label() = %q, want %q", got, c.want)
		}
	}
}

func TestDisplayPath(t *testing.T) {
	const main, home = "/home/t/repo", "/home/t"
	cases := []struct{ path, want string }{
		{"/home/t/repo", "~/repo"},
		{"/home/t/.mori/worktrees/repo/feat", "~/.mori/worktrees/repo/feat"},
		// Worktrees created before they moved out of the repo read better
		// relative to it.
		{"/home/t/repo/.claude/worktrees/legacy", ".claude/worktrees/legacy"},
		{"/srv/wt", "/srv/wt"},
		// A sibling whose name merely starts with home's is not under it.
		{"/home/tomato/wt", "/home/tomato/wt"},
	}
	for _, c := range cases {
		if got := displayPath(c.path, main, home); got != c.want {
			t.Errorf("displayPath(%q) = %q, want %q", c.path, got, c.want)
		}
	}
}

func TestSortIndices(t *testing.T) {
	now := time.Now()
	wts := []Worktree{
		{Branch: "zeta", Created: now.Add(-time.Minute)},
		{Branch: "main", IsMain: true, LastCommit: now},
		{Branch: "alpha", Created: now.Add(-24 * time.Hour)},
		{Branch: "beta", Created: now.Add(-time.Hour)},
	}

	order := func(mode SortMode) []string {
		idx := []int{0, 1, 2, 3}
		SortIndices(wts, idx, mode)
		out := make([]string, len(idx))
		for i, j := range idx {
			out[i] = wts[j].Branch
		}
		return out
	}

	// The main worktree leads in every mode, whatever its own dates say.
	cases := []struct {
		mode SortMode
		want []string
	}{
		{SortCreated, []string{"main", "zeta", "beta", "alpha"}},
		{SortName, []string{"main", "alpha", "beta", "zeta"}},
	}
	for _, c := range cases {
		got := order(c.mode)
		for i := range c.want {
			if got[i] != c.want[i] {
				t.Errorf("sort %s: got %v, want %v", c.mode, got, c.want)
				break
			}
		}
	}
}

func TestSortModeNextCycles(t *testing.T) {
	if m := SortCreated.Next().Next(); m != SortCreated {
		t.Errorf("Next() did not cycle back to created after 2 steps, got %v", m)
	}
}

func TestAgePrefersCreationTime(t *testing.T) {
	now := time.Now()
	created := Worktree{LastCommit: now.Add(-9 * time.Hour), Created: now.Add(-time.Minute)}
	if got := created.Age(); !got.Equal(created.Created) {
		t.Errorf("Age() = %v, want the creation time %v", got, created.Created)
	}
	// The main working tree has no creation time; it falls back to HEAD.
	main := Worktree{LastCommit: now.Add(-9 * time.Hour)}
	if got := main.Age(); !got.Equal(main.LastCommit) {
		t.Errorf("Age() = %v, want the HEAD timestamp %v", got, main.LastCommit)
	}
}
