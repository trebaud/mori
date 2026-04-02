package main

import (
	"flag"
	"fmt"
	"os"

	"github.com/moosecode/mori/commands"
)

const version = "1.0.0"

var (
	showVersion  bool
	showHelp     bool
	createCmd    bool
	repoDir      string
	branchName   string
	launchClaude bool
)

func init() {
	flag.BoolVar(&showVersion, "v", false, "Show version")
	flag.BoolVar(&showHelp, "h", false, "Show help")
	flag.BoolVar(&showVersion, "version", false, "Show version")
	flag.BoolVar(&showHelp, "help", false, "Show help")
	flag.BoolVar(&createCmd, "create", false, "Create a new worktree")
	flag.StringVar(&repoDir, "r", "", "Repository root")
	flag.StringVar(&branchName, "b", "", "Branch name")
	flag.BoolVar(&launchClaude, "c", false, "Launch Claude Code")
}

func main() {
	flag.Parse()

	switch {
	case showVersion:
		fmt.Printf("Mori v%s\n", version)
	case showHelp:
		printHelp()
	case createCmd:
		if err := commands.Create(commands.CreateOptions{
			Repo:         repoDir,
			Branch:       branchName,
			LaunchClaude: launchClaude,
		}); err != nil {
			fmt.Fprintf(os.Stderr, "Error: %v\n", err)
			os.Exit(1)
		}
	default:
		commands.Select()
	}
}

func printHelp() {
	fmt.Printf(`Mori v%s

A TUI for managing git worktrees with Claude session detection.

Usage:
  mori [options]
  mori -create [options]

Commands:
  -create        Create a new worktree

Options (create):
  -r repo-dir     Repository root (default: current directory)
  -b branch-name  Branch to create (default: random wt-XXXXX)
  -c              Launch Claude Code after creating worktree

Options:
  -h, --help     Show this help message
  -v, --version  Show version

Navigation (TUI):
  ↑/↓ or k/j    Navigate worktrees
  i             Toggle agent insights panel
  Enter         Select worktree (outputs path to stdout)
  q             Quit

Requires:
  - Git repository
  - Claude Code (optional, for session detection)

`, version)
}
