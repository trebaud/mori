package commands

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// The wrappers below all follow one rule: only the bare `mori` call is
// captured. Every subcommand writes something the user asked to see — a
// table, JSON, a path, a version — and swallowing that to cd would break it.
// So anything with arguments runs through untouched.
const bashInit = `mori() {
  if [ "$#" -gt 0 ]; then
    command mori "$@"
    return $?
  fi
  local dir
  dir=$(command mori) || return $?
  # Quitting without picking prints nothing. That is a normal exit, so it must
  # not leave a non-zero status behind for the prompt to report.
  if [ -n "$dir" ]; then
    cd -- "$dir"
  fi
}
`

const fishInit = `function mori --description 'git worktree manager'
    if test (count $argv) -gt 0
        command mori $argv
        return $status
    end
    set -l dir (command mori)
    if test -n "$dir"
        cd -- $dir
    end
end
`

// ShellInit prints a shell function that wraps the mori binary so the TUI's
// `enter` lands the shell in the chosen worktree. A child process cannot
// change its parent's directory, so mori prints the path and the function
// does the cd — the same arrangement zoxide and fzf use.
func ShellInit(shell string) error {
	if shell == "" {
		shell = detectShell()
	}
	switch shell {
	case "bash", "zsh", "ksh", "sh":
		fmt.Print(bashInit)
	case "fish":
		fmt.Print(fishInit)
	default:
		return fmt.Errorf("unsupported shell %q — supported: bash, zsh, fish", shell)
	}
	return nil
}

// detectShell names the shell from $SHELL, falling back to bash — whose
// wrapper is POSIX enough to work anywhere but fish.
func detectShell() string {
	base := filepath.Base(os.Getenv("SHELL"))
	if base == "" || base == "." || strings.HasPrefix(base, "-") {
		return "bash"
	}
	return base
}
