// Package tui renders mori's interactive terminal UI.
//
// It follows the Elm architecture, one concern per file:
//
//   - model.go     state, messages, Init, layout helpers
//   - update.go    Update + per-mode key handlers
//   - commands.go  tea.Cmd side effects + archive persistence
//   - view.go      View + rendering
//   - theme.go     colors and styles
//   - utils.go     formatters, framing, overlay compositing
package tui

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	tea "charm.land/bubbletea/v2"

	"github.com/trebaud/mori/v2/internal/git"
)

// Run launches the TUI and returns the path of the worktree the user picked,
// or "" if they quit without picking one.
//
// It takes no list: the model asks for one in Init, so the first frame is
// drawn while git is still being queried rather than after. Listing a
// repository with twenty worktrees means four git processes each, and a
// second of blank terminal is the wrong first impression.
//
// The UI is read from and drawn to the controlling terminal rather than to
// stdin/stdout, so callers can print the chosen path on stdout and users can
// write `cd "$(mori)"`.
func Run() (string, error) {
	// /dev/tty is the controlling terminal regardless of how stdin and stdout
	// are redirected; failing to open it means there is nobody to drive the UI.
	tty, err := os.OpenFile("/dev/tty", os.O_RDWR, 0)
	if err != nil {
		return "", fmt.Errorf("mori needs an interactive terminal — use `mori list` or `mori path <branch>`")
	}
	defer tty.Close()

	DetectAndApplyTheme(tty)

	repoRoot, err := git.FindMainRepo(".")
	if err != nil {
		return "", err
	}

	m := newModel(nil, repoLabel(repoRoot), git.DefaultBranch(repoRoot))
	final, err := tea.NewProgram(m, tea.WithInput(tty), tea.WithOutput(tty)).Run()
	if err != nil {
		return "", fmt.Errorf("running TUI: %w", err)
	}

	result, ok := final.(model)
	if !ok || result.selected < 0 {
		return "", nil
	}
	return result.worktrees[result.selected].Path, nil
}

// repoLabel renders the repository root for the top bar, with $HOME collapsed.
func repoLabel(root string) string {
	if home, err := os.UserHomeDir(); err == nil && home != "" && strings.HasPrefix(root, home) {
		return "~" + root[len(home):]
	}
	return filepath.Clean(root)
}
