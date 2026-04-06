package main

import (
	"flag"
	"fmt"
	"os"

	"github.com/moosecode/mori/cmd/mori/commands"
)

const version = "1.1.0"

func main() {
	if len(os.Args) < 2 {
		commands.Select()
		return
	}

	switch os.Args[1] {
	case "new":
		runNew(os.Args[2:])
	case "list", "ls":
		runList(os.Args[2:])
	case "remove", "rm":
		runRemove(os.Args[2:])
	case "open":
		runOpen(os.Args[2:])
	case "status":
		runStatus()
	case "version", "--version", "-v":
		fmt.Printf("mori v%s\n", version)
	case "help", "--help", "-h":
		printHelp()
	default:
		fmt.Fprintf(os.Stderr, "unknown command: %s\nRun 'mori help' for usage.\n", os.Args[1])
		os.Exit(1)
	}
}

func runNew(args []string) {
	fs := flag.NewFlagSet("new", flag.ExitOnError)
	var (
		launchClaude bool
		repoDir      string
	)
	fs.BoolVar(&launchClaude, "c", false, "Launch Claude Code after creating")
	fs.BoolVar(&launchClaude, "claude", false, "Launch Claude Code after creating")
	fs.StringVar(&repoDir, "r", "", "Repository root")
	fs.StringVar(&repoDir, "repo", "", "Repository root")
	fs.Parse(args)

	branch := fs.Arg(0)

	if err := commands.Create(commands.CreateOptions{
		Repo:         repoDir,
		Branch:       branch,
		LaunchClaude: launchClaude,
	}); err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
}

func runList(args []string) {
	fs := flag.NewFlagSet("list", flag.ExitOnError)
	var (
		jsonOutput   bool
		statusFilter string
	)
	fs.BoolVar(&jsonOutput, "json", false, "Output as JSON")
	fs.StringVar(&statusFilter, "status", "", "Filter by status (working, idle, waiting, none)")
	fs.Parse(args)

	if err := commands.PrintList(jsonOutput, statusFilter); err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
}

func runRemove(args []string) {
	fs := flag.NewFlagSet("remove", flag.ExitOnError)
	var force bool
	fs.BoolVar(&force, "f", false, "Skip confirmation prompts")
	fs.BoolVar(&force, "force", false, "Skip confirmation prompts")
	fs.Parse(args)

	branch := fs.Arg(0)
	if branch == "" {
		fmt.Fprintf(os.Stderr, "Usage: mori remove <branch> [--force]\n")
		os.Exit(1)
	}

	if err := commands.Remove(branch, force); err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
}

func runOpen(args []string) {
	fs := flag.NewFlagSet("open", flag.ExitOnError)
	fs.Parse(args)

	branch := fs.Arg(0)
	if branch == "" {
		fmt.Fprintf(os.Stderr, "Usage: mori open <branch>\n")
		os.Exit(1)
	}

	if err := commands.Open(branch); err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
}

func runStatus() {
	if err := commands.Status(); err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
}

func printHelp() {
	fmt.Printf(`Mori v%s - Git worktree manager with Claude Code insights

Usage:
  mori                          Launch interactive TUI
  mori new [branch] [flags]     Create a new worktree
  mori list [flags]             List worktrees
  mori remove <branch> [flags]  Remove a worktree
  mori open <branch>            Print worktree path for branch
  mori status                   Show worktree summary

Flags (new):
  -c, --claude      Launch Claude Code after creating
  -r, --repo PATH   Repository root (default: current directory)

Flags (list):
      --json            Output as JSON (includes insights data)
      --status STATUS   Filter by status (working, idle, waiting, none)

Flags (remove):
  -f, --force       Skip confirmation prompts

Global:
  help, --help      Show this help message
  version, --version  Show version

TUI keys:
  j/k, arrows    Navigate        i       Toggle insights
  Enter          Select (copy cd) q       Quit
  n              New worktree     d       Delete worktree
  /              Filter           s       Cycle sort mode
  r              Refresh          ?       Show all keybindings
`, version)
}
