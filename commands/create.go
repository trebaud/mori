package commands

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/moosecode/mori/config"
)

type CreateOptions struct {
	Repo         string
	Branch       string
	LaunchClaude bool
}

func Create(opts CreateOptions) error {
	repo := opts.Repo
	if repo == "" {
		repo = "."
	}

	absRepo, err := filepath.Abs(repo)
	if err != nil {
		return fmt.Errorf("cannot resolve repo dir '%s'", repo)
	}

	if !isMainRepo(absRepo) {
		mainRepo, err := findMainRepo(absRepo)
		if err != nil {
			return fmt.Errorf("not a git repository or worktree")
		}
		absRepo = mainRepo
	}

	branch := opts.Branch
	if branch == "" {
		branch = "wt-" + randomSuffix()
	}

	return createWorktree(absRepo, branch, opts.LaunchClaude)
}

func isMainRepo(path string) bool {
	// Check if .git is a directory (main repo) vs a file (worktree)
	info, err := os.Stat(filepath.Join(path, ".git"))
	if err != nil {
		return false
	}
	return info.IsDir()
}

func findMainRepo(worktreePath string) (string, error) {
	out, err := exec.Command("git", "-C", worktreePath, "rev-parse", "--show-toplevel").Output()
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(out)), nil
}

func getMainBranch(repo string) string {
	out, err := exec.Command("git", "-C", repo, "branch", "--show-current").Output()
	if err == nil {
		branch := strings.TrimSpace(string(out))
		if branch != "" {
			return branch
		}
	}

	out, err = exec.Command("git", "-C", repo, "symbolic-ref", "refs/remotes/origin/HEAD").Output()
	if err == nil {
		parts := strings.Split(string(out), "/")
		if len(parts) >= 3 {
			return parts[len(parts)-1]
		}
	}

	out, err = exec.Command("git", "-C", repo, "rev-parse", "--abbrev-ref", "HEAD").Output()
	if err == nil {
		branch := strings.TrimSpace(string(out))
		if branch != "HEAD" {
			return branch
		}
	}

	return "main"
}

func repoHasCommits(repo string) (bool, error) {
	out, err := exec.Command("git", "-C", repo, "rev-list", "--count", "HEAD").Output()
	if err != nil {
		return false, nil
	}
	return strings.TrimSpace(string(out)) != "0", nil
}

func createWorktree(repo, branch string, launchClaude bool) error {
	worktreeDir := filepath.Join(repo, ".claude", "worktrees", branch)

	fmt.Fprintf(os.Stderr, "\n  \033[1;35m◆ %s\033[0m\n\n", branch)

	if hasCommits, err := repoHasCommits(repo); err != nil || !hasCommits {
		fmt.Fprintf(os.Stderr, "    Checking for commits... \033[1;31m✖\033[0m\n")
		if err != nil {
			return fmt.Errorf("failed to check repo: %w", err)
		}
		return fmt.Errorf("repository has no commits, cannot create worktree. Make at least one commit first")
	}

	mainBranch := getMainBranch(repo)

	fmt.Fprintf(os.Stderr, "    Creating branch from %s... ", mainBranch)

	if err := exec.Command("git", "-C", repo, "worktree", "add", worktreeDir, "-b", branch, mainBranch).Run(); err != nil {
		fmt.Fprintf(os.Stderr, "\033[1;31m✖\033[0m\n")
		return fmt.Errorf("failed to create worktree: %w", err)
	}
	fmt.Fprintf(os.Stderr, "\033[1;32m✔\033[0m\n")

	cfg := config.Load(repo)
	if len(cfg.PostCreate) > 0 {
		setupWorktree(worktreeDir, cfg.PostCreate)
	}

	fmt.Fprintf(os.Stderr, "\n  \033[0;90mcd %s\033[0m\n\n", worktreeDir)

	fmt.Print(worktreeDir)

	if launchClaude {
		fmt.Fprintf(os.Stderr, "\n\033[0;36m  Launching Claude Code...\033[0m\n")
		exec.Command("claude", "--worktree", branch).Run()
	}

	return nil
}

func setupWorktree(worktreeDir string, steps []config.Step) {
	for _, step := range steps {
		fmt.Fprintf(os.Stderr, "    %s... ", step.Name)
		cmd := exec.Command("sh", "-c", step.Cmd)
		cmd.Dir = worktreeDir
		if err := cmd.Run(); err != nil {
			fmt.Fprintf(os.Stderr, "\033[1;33m⚠\033[0m\n")
		} else {
			fmt.Fprintf(os.Stderr, "\033[1;32m✔\033[0m\n")
		}
	}
}

func randomSuffix() string {
	out, err := exec.Command("sh", "-c", "LC_ALL=C tr -dc 'a-z0-9' </dev/urandom | head -c5").Output()
	if err != nil || len(string(out)) == 0 {
		return fmt.Sprintf("%d", os.Getpid())
	}
	return string(out)
}
