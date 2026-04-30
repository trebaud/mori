package commands

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

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

	var spin *stepSpinner
	cb := &internal.HookCallbacks{
		OnStart: func(name string) {
			spin = startSpinner(name)
		},
		OnComplete: func(name string, success bool) {
			if spin != nil {
				spin.finish(success)
				spin = nil
			}
		},
	}

	result, err := internal.CreateWorktree(absRepo, branch, cb)
	if err != nil {
		fmt.Fprintf(os.Stderr, "    \033[1;31m✖\033[0m %v\n", err)
		return err
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

// stepSpinner renders an animated spinner next to a step name on stderr,
// then replaces it with a checkmark (or warning) when finish is called.
type stepSpinner struct {
	name string
	stop chan struct{}
	done chan struct{}
}

func startSpinner(name string) *stepSpinner {
	s := &stepSpinner{
		name: name,
		stop: make(chan struct{}),
		done: make(chan struct{}),
	}
	go func() {
		defer close(s.done)
		frames := []string{"⠋", "⠙", "⠹", "⠸", "⠼", "⠴", "⠦", "⠧", "⠇", "⠏"}
		i := 0
		render := func() {
			fmt.Fprintf(os.Stderr, "\r\033[K    \033[1;36m%s\033[0m %s...", frames[i], name)
		}
		render()
		ticker := time.NewTicker(80 * time.Millisecond)
		defer ticker.Stop()
		for {
			select {
			case <-s.stop:
				return
			case <-ticker.C:
				i = (i + 1) % len(frames)
				render()
			}
		}
	}()
	return s
}

func (s *stepSpinner) finish(success bool) {
	close(s.stop)
	<-s.done
	mark := "\033[1;32m✔\033[0m"
	if !success {
		mark = "\033[1;33m⚠\033[0m"
	}
	fmt.Fprintf(os.Stderr, "\r\033[K    %s... %s\n", s.name, mark)
}
