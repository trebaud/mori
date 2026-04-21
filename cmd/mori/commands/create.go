package commands

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/trebaud/mori/internal"
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

	absRepo, err := internal.ResolveRepo(repo)
	if err != nil {
		return err
	}

	branch := opts.Branch
	if branch == "" {
		branch = "wt-" + internal.RandomSuffix()
	}

	fmt.Fprintf(os.Stderr, "\n  \033[1;35m◆ %s\033[0m\n\n", branch)

	result, err := internal.CreateWorktree(absRepo, branch)
	if err != nil {
		fmt.Fprintf(os.Stderr, "    \033[1;31m✖\033[0m %v\n", err)
		return err
	}

	fmt.Fprintf(os.Stderr, "    Creating branch from %s... \033[1;32m✔\033[0m\n", result.BaseBranch)
	for _, hr := range result.HookResults {
		if hr.Success {
			fmt.Fprintf(os.Stderr, "    %s... \033[1;32m✔\033[0m\n", hr.Name)
		} else {
			fmt.Fprintf(os.Stderr, "    %s... \033[1;33m⚠\033[0m\n", hr.Name)
		}
	}

	fmt.Fprintf(os.Stderr, "\n  \033[0;90m%s\033[0m\n\n", result.Dir)

	if opts.LaunchClaude {
		claudePath, err := exec.LookPath("claude")
		if err != nil {
			fmt.Fprintf(os.Stderr, "    \033[1;31m✖\033[0m claude not found in PATH\n")
			return nil
		}

		args := []string{"--tmux"}
		if branch != result.BaseBranch {
			args = append(args, "--worktree", filepath.Base(result.Dir))
		}

		fmt.Fprintf(os.Stderr, "  \033[0;90m%s\033[0m\n\n", "claude "+strings.Join(args, " "))
		cmd := exec.Command(claudePath, args...)
		cmd.Dir = result.Dir
		cmd.Stdin = os.Stdin
		cmd.Stdout = os.Stdout
		cmd.Stderr = os.Stderr
		cmd.Run()
	}

	return nil
}
