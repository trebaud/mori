// Command mori is a terminal UI and CLI for managing git worktrees.
package main

import (
	"fmt"
	"os"
	"runtime/debug"
	"strings"

	"github.com/spf13/cobra"

	"github.com/trebaud/mori/v2/cmd/mori/commands"
)

// ldflagsVersion can be set at build time via -ldflags "-X main.ldflagsVersion=$(git describe --tags)".
// When installed via `go install ...@tag`, it falls back to the module version
// embedded by the Go toolchain (e.g. v1.1.1).
var ldflagsVersion = "dev"

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

func main() {
	if err := rootCmd().Execute(); err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
}

func rootCmd() *cobra.Command {
	root := &cobra.Command{
		Use:           "mori",
		Short:         "Git worktree manager",
		Version:       version,
		SilenceUsage:  true,
		SilenceErrors: true,
		Args:          cobra.NoArgs,
		RunE: func(*cobra.Command, []string) error {
			return commands.Select()
		},
	}
	root.SetVersionTemplate("mori v{{.Version}}\n")
	root.Flags().BoolP("version", "v", false, "Show version")
	tmpl := helpTemplate()
	root.SetHelpTemplate(tmpl)
	root.SetUsageTemplate(tmpl)

	root.AddCommand(newCmd(), listCmd(), removeCmd(), pathCmd(), versionCmd())
	return root
}

func newCmd() *cobra.Command {
	var repoDir string
	cmd := &cobra.Command{
		Use:   "new [branch]",
		Short: "Create a new worktree",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(_ *cobra.Command, args []string) error {
			var branch string
			if len(args) > 0 {
				branch = args[0]
			}
			return commands.Create(commands.CreateOptions{Repo: repoDir, Branch: branch})
		},
	}
	cmd.Flags().StringVarP(&repoDir, "repo", "r", "", "Repository root (default: current directory)")
	return cmd
}

func listCmd() *cobra.Command {
	var jsonOutput bool
	cmd := &cobra.Command{
		Use:     "list",
		Aliases: []string{"ls"},
		Short:   "List worktrees",
		Args:    cobra.NoArgs,
		RunE: func(*cobra.Command, []string) error {
			return commands.PrintList(jsonOutput)
		},
	}
	cmd.Flags().BoolVar(&jsonOutput, "json", false, "Output as JSON")
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
	cmd.Flags().BoolVarP(&force, "force", "f", false, "Skip the uncommitted-changes prompt")
	return cmd
}

func pathCmd() *cobra.Command {
	return &cobra.Command{
		Use:     "path <branch>",
		Aliases: []string{"open"},
		Short:   "Print the worktree directory for a branch",
		Args:    cobra.MaximumNArgs(1),
		RunE: func(_ *cobra.Command, args []string) error {
			if len(args) == 0 {
				return fmt.Errorf("Usage: mori path <branch>")
			}
			return commands.Path(args[0])
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
	return `Mori v` + version + ` - Git worktree manager

Usage:
  mori                          Launch the TUI; prints the picked worktree path
  mori new [branch] [flags]     Create a new worktree
  mori list [flags]             List worktrees
  mori path <branch>            Print the worktree directory for a branch
  mori remove <branch> [flags]  Remove a worktree

Flags (new):
  -r, --repo PATH   Repository root (default: current directory)

Flags (list):
      --json        Output as JSON

Flags (remove):
  -f, --force       Skip the uncommitted-changes prompt

Global:
  help, --help        Show this help message
  version, --version  Show version

TUI keys:
  j/k, arrows   Navigate            n   New worktree
  enter         Pick (prints path)  d   Delete worktree
  /             Filter              s   Cycle sort
  y             Yank path           r   Refresh
  x / X         Archive             ?   All keybindings
  q             Quit
`
}
