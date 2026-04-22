package main

import (
	"fmt"
	"os"
	"runtime/debug"
	"strings"

	"github.com/spf13/cobra"

	"github.com/trebaud/mori/cmd/mori/commands"
)

// version can be set at build time via -ldflags "-X main.version=$(git describe --tags)".
// When installed via `go install ...@tag`, it falls back to the module version embedded
// by the Go toolchain (e.g. v1.1.1).
var version = resolveVersion()

func resolveVersion() string {
	if v := strings.TrimPrefix(ldflagsVersion, "v"); v != "" && v != "dev" {
		return v
	}
	if bi, ok := debug.ReadBuildInfo(); ok {
		if v := strings.TrimPrefix(bi.Main.Version, "v"); v != "" && v != "(devel)" {
			return v
		}
	}
	return "dev"
}

var ldflagsVersion = "dev"

func main() {
	if err := rootCmd().Execute(); err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
}

func rootCmd() *cobra.Command {
	root := &cobra.Command{
		Use:           "mori",
		Short:         "Git worktree manager with Claude Code insights",
		Version:       version,
		SilenceUsage:  true,
		SilenceErrors: true,
		Args:          cobra.NoArgs,
		Run: func(*cobra.Command, []string) {
			commands.Select()
		},
	}
	root.SetVersionTemplate("mori v{{.Version}}\n")
	root.Flags().BoolP("version", "v", false, "Show version")
	tmpl := helpTemplate()
	root.SetHelpTemplate(tmpl)
	root.SetUsageTemplate(tmpl)

	root.AddCommand(newCmd(), listCmd(), removeCmd(), openCmd(), statusCmd(), versionCmd())
	return root
}

func newCmd() *cobra.Command {
	var (
		launchClaude bool
		repoDir      string
	)
	cmd := &cobra.Command{
		Use:   "new [branch]",
		Short: "Create a new worktree",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(_ *cobra.Command, args []string) error {
			var branch string
			if len(args) > 0 {
				branch = args[0]
			}
			return commands.Create(commands.CreateOptions{
				Repo:         repoDir,
				Branch:       branch,
				LaunchClaude: launchClaude,
			})
		},
	}
	cmd.Flags().BoolVarP(&launchClaude, "claude", "c", false, "Launch Claude Code after creating")
	cmd.Flags().StringVarP(&repoDir, "repo", "r", "", "Repository root (default: current directory)")
	return cmd
}

func listCmd() *cobra.Command {
	var (
		jsonOutput   bool
		statusFilter string
	)
	cmd := &cobra.Command{
		Use:     "list",
		Aliases: []string{"ls"},
		Short:   "List worktrees",
		Args:    cobra.NoArgs,
		RunE: func(*cobra.Command, []string) error {
			return commands.PrintList(jsonOutput, statusFilter)
		},
	}
	cmd.Flags().BoolVar(&jsonOutput, "json", false, "Output as JSON (includes insights data)")
	cmd.Flags().StringVar(&statusFilter, "status", "", "Filter by status (working, idle, waiting, none)")
	return cmd
}

func removeCmd() *cobra.Command {
	var force bool
	cmd := &cobra.Command{
		Use:     "remove <branch>",
		Aliases: []string{"rm"},
		Short:   "Remove a worktree",
		Args:    cobra.MaximumNArgs(1),
		RunE: func(_ *cobra.Command, args []string) error {
			if len(args) == 0 {
				return fmt.Errorf("Usage: mori remove <branch> [--force]")
			}
			return commands.Remove(args[0], force)
		},
	}
	cmd.Flags().BoolVarP(&force, "force", "f", false, "Skip confirmation prompts")
	return cmd
}

func openCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "open <branch>",
		Short: "Print worktree path for branch",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(_ *cobra.Command, args []string) error {
			if len(args) == 0 {
				return fmt.Errorf("Usage: mori open <branch>")
			}
			return commands.Open(args[0])
		},
	}
}

func statusCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "status",
		Short: "Show worktree summary",
		Args:  cobra.NoArgs,
		RunE: func(*cobra.Command, []string) error {
			return commands.Status()
		},
	}
}

func versionCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "version",
		Short: "Show version",
		Args:  cobra.NoArgs,
		Run: func(*cobra.Command, []string) {
			fmt.Printf("mori v%s\n", version)
		},
	}
}

func helpTemplate() string {
	return `Mori v` + version + ` - Git worktree manager with Claude Code insights

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
  help, --help        Show this help message
  version, --version  Show version

TUI keys:
  j/k, arrows    Navigate        i       Toggle insights
  Enter          Select (copy cd) q       Quit
  n              New worktree     d       Delete worktree
  /              Filter           s       Cycle sort mode
  r              Refresh          ?       Show all keybindings
`
}
