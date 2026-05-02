# Mori

[![Go](https://img.shields.io/badge/go-1.21%2B-00ADD8?logo=go)](https://go.dev)
[![License](https://img.shields.io/badge/license-MIT-blue.svg)](#license)
[![Release](https://img.shields.io/github/v/release/trebaud/mori)](https://github.com/trebaud/mori/releases)

> *mori* (森) — Japanese for "forest." A place where many trees grow together.

A TUI for managing git worktrees with Claude Code agent insights.

![Mori TUI](https://github.com/user-attachments/assets/5384c293-4654-49fa-84bb-b7cfcfce5385)

## Why

Running multiple Claude Code agents in parallel means juggling tmux panes, branches, and `git worktree` commands. Mori puts them in one view so you can see what each agent is doing and what it's costing.

## How it compares

|                       | `git worktree` + tmux | Multiple `claude` shells | Mori |
| --------------------- | --------------------- | ------------------------ | ---- |
| Side-by-side worktree list | ✗               | ✗                        | ✓    |
| Agent status (working / waiting / idle) | ✗     | ✗                        | ✓    |
| Cost & context usage  | ✗                     | per-session              | ✓    |
| PR state per branch   | ✗                     | ✗                        | ✓    |
| One key to create / delete / open | ✗         | ✗                        | ✓    |

## Features

- Browse, create, and delete git worktrees without leaving the TUI
- Per-worktree agent insights: status, cost, context usage, current task, git log, ahead/behind
- PR tracking via [`gh`](https://cli.github.com/)
- Search, sort, and filter worktrees
- Adaptive refresh: 2s when agents are active, 10s when idle
- Includes a [Claude Code skill](skill/SKILL.md) so Claude can run mori commands

## Install

```bash
go install github.com/trebaud/mori/cmd/mori@latest
```

Make sure `$(go env GOPATH)/bin` is on your `$PATH`.

Or build from source:

```bash
git clone https://github.com/trebaud/mori.git
cd mori
./scripts/install.sh
```

The script checks dependencies, builds the binary, and installs it to `/usr/local/bin` or `~/.local/bin`.

## Usage

```bash
mori                           # Launch the TUI
mori new feat --claude         # Create a worktree and launch Claude Code in it
mori open feat                 # Attach to the Claude session for a worktree
mori list --json               # List worktrees with full insights
mori status                    # One-line summary of all worktrees
mori remove feat               # Remove a worktree
```

Press `?` inside the TUI for keybindings.

## Configuration

Global: `~/.mori/settings.json`. Per-project: `.mori.json` in the repo root (overrides global).

Run commands automatically after creating a worktree:

```json
{
  "post_create": [
    { "name": "Installing dependencies", "cmd": "npm install" },
    { "name": "Copying env", "cmd": "cp ../.env .env" }
  ]
}
```

Each step runs in the new worktree directory. A failing step prints a warning but doesn't block the rest.

## Claude Code Skill

Install the [skill](skill/SKILL.md) so Claude can run mori commands directly:

```bash
cp -r skill ~/.claude/skills/mori
```

This is useful when you want competing implementations side by side: start each one in its own worktree, watch them in the TUI, and remove the ones you don't keep.

## Diagnostics

Agent insights are parsed from Claude Code session logs at `~/.claude/projects/<encoded-worktree-path>/*.jsonl`. If a worktree shows no agent state, check that:

- A `claude` session has been started in that worktree at least once
- The worktree path resolves (no broken symlinks)
- `gh auth status` is healthy if PR state is missing

## Requirements

- Go 1.21+
- Git
- [tmux](https://github.com/tmux/tmux) — manages Claude Code sessions
- [Claude Code](https://docs.anthropic.com/en/docs/claude-code) — for agent sessions and insights
- [gh](https://cli.github.com/) — optional, for PR status

## License

MIT
