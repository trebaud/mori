# Mori

[![Go](https://img.shields.io/badge/go-1.26%2B-00ADD8?logo=go)](https://go.dev)
[![License](https://img.shields.io/badge/license-MIT-blue.svg)](#license)
[![Release](https://img.shields.io/github/v/release/trebaud/mori)](https://github.com/trebaud/mori/releases)

> *mori* (森) — Japanese for "forest." A place where many trees grow together.

A small TUI for managing git worktrees.

## Install

```bash
go install github.com/trebaud/mori/v2/cmd/mori@latest
```

Make sure `$(go env GOPATH)/bin` is on your `$PATH`.

Or build from source with `./scripts/install.sh`

## Usage

```bash
mori                  # TUI; enter cds into a worktree, `y` copies its path
mori new feat         # Create a worktree on a new branch "feat"
mori new feat --from x  # ...cut from branch "x" instead of the default
mori list             # Table of worktrees
mori list --json      # Same, machine-readable
mori path feat        # Print the directory for branch "feat"
mori remove feat      # Remove a worktree (asks before discarding changes)
mori shell-init       # Print the shell function behind the cd
```

## Shell integration

A program cannot change its parent shell's directory, so `mori` prints the
worktree you picked and a shell function does the `cd`. Add this to your rc:

```bash
eval "$(mori shell-init)"          # bash / zsh — ~/.bashrc, ~/.zshrc
mori shell-init fish | source      # fish — ~/.config/fish/config.fish
```

Only the bare `mori` call is wrapped; every subcommand still writes its own
output straight through. Without the function, `cd "$(mori)"` does the same
thing by hand.

## Config

Worktrees are created under `~/.mori/worktrees/<repo>/<branch>`

Global: `~/.mori/settings.json`. Per-project: `.mori.json` in the repo root
(replaces global).

Run commands automatically after creating a worktree:

```json
{
  "post_create": [
    { "name": "Installing dependencies", "cmd": "npm install" },
    { "name": "Copying env", "cmd": "cp ~/Code/myproject/.env .env" }
  ]
}
```

Each step runs in the new worktree directory.

## Claude Code Skill

Install the [skill](skill/SKILL.md) so Claude can run mori commands directly:

```bash
cp -r skill ~/.claude/skills/mori
```

## License

MIT
