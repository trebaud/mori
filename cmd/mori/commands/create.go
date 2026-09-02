package commands

import (
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/trebaud/mori/v2/internal"
)

// CreateOptions configures a worktree creation.
type CreateOptions struct {
	Repo   string // repository root; defaults to the working directory
	Branch string // branch to create; a random name when empty
	From   string // branch to cut from; the repository default when empty
}

// Create makes a new worktree, streaming each setup step to stderr and
// printing the resulting directory on stdout.
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

	fmt.Fprintf(progress, "\n  \033[1;35m◆ %s\033[0m\n\n", branch)

	var spin *stepSpinner
	cb := &internal.HookCallbacks{
		OnStart: func(name string) {
			spin = startSpinner(name)
		},
		OnComplete: func(name string, success bool, output string) {
			if spin != nil {
				spin.finish(success)
				spin = nil
			}
			// A step that failed says why. Indented under its own name, so a
			// long build log still reads as belonging to that step.
			if !success && output != "" {
				for _, ln := range strings.Split(output, "\n") {
					fmt.Fprintf(progress, "      \033[0;90m%s\033[0m\n", ln)
				}
			}
		},
	}

	result, err := internal.CreateWorktree(absRepo, branch, opts.From, cb)
	if err != nil {
		fmt.Fprintf(progress, "    \033[1;31m✖\033[0m %v\n", err)
		return err
	}

	fmt.Fprintf(progress, "\n  \033[0;90m%s\033[0m\n\n", result.Dir)
	fmt.Fprintln(os.Stdout, result.Dir)

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
			fmt.Fprintf(progress, "\r\033[K    \033[1;36m%s\033[0m %s...", frames[i], name)
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
	fmt.Fprintf(progress, "\r\033[K    %s... %s\n", s.name, mark)
}
