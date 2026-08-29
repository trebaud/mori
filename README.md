# Mori

[![Go](https://img.shields.io/badge/go-1.21%2B-00ADD8?logo=go)](https://go.dev)
[![License](https://img.shields.io/badge/license-MIT-blue.svg)](#license)
[![Release](https://img.shields.io/github/v/release/trebaud/mori)](https://github.com/trebaud/mori/releases)

> *mori* (森) — Japanese for "forest." A place where many trees grow together.

A small TUI for managing git worktrees.

![Mori demo](assets/demo.gif)

## Why

`git worktree` is great and its CLI is not. Mori lists every worktree in a repo
with the state you actually care about — dirty files, ahead/behind, last commit —
and lets you create, delete, and jump into them without leaving the list.

That's the whole scope. No agent tracking, no PR status, no session management.

## Features

- Browse the repo's worktrees, with dirty count, ahead/behind, and last commit
- Create and delete worktrees from the TUI or the CLI
- Pick a worktree and `cd` into it (mori prints the path on exit)
- Worktrees live outside the repo, so `git status` stays clean
- Post-create hooks so a new worktree comes up ready to work in
- Filter, sort, and archive worktrees you're not looking at
- JSON output for scripting

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

The script checks dependencies, builds the binary, and installs it to
`/usr/local/bin` or `~/.local/bin`.

## Usage

```bash
mori                  # TUI; prints the picked worktree path on exit
mori new feat         # Create a worktree on a new branch "feat"
mori list             # Table of worktrees
mori list --json      # Same, machine-readable
mori path feat        # Print the directory for branch "feat"
mori remove feat      # Remove a worktree (asks before discarding changes)
```

Press `?` inside the TUI for keybindings.

### Shell integration

Picking a worktree prints its path on stdout, so a one-line shell function turns
mori into a worktree switcher:

```bash
mc() { cd "$(mori)" || return; }
```

The TUI itself draws to the terminal, not to stdout, so `$(mori)` only ever
captures the path.

## Keybindings

| Key | Action |
|---|---|
| `j`/`k`, `↑`/`↓` | Move between worktrees |
| `g` / `G` | Jump to first / last |
| `ctrl+d` / `ctrl+u` | Page down / up |
| `enter`, `o` | Pick a worktree — prints its path on exit |
| `n` | Create a worktree |
| `d` | Delete the selected worktree |
| `y` | Yank the path to the clipboard |
| `r` | Refresh git state now |
| `/` | Filter by branch or path |
| `s` | Cycle sort (default, recent, name) |
| `x` / `X` | Archive / show archived |
| `?` | Help |
| `q`, `ctrl+c` | Quit |

Listings cover git's *linked* worktrees. The repository's own working tree —
the one you're standing in — isn't something to switch to or delete, so it
never appears.

## Where worktrees live

Worktrees are created under `~/.mori/worktrees/<repo>/<branch>`, not inside the
repository:

```
~/.mori/
├── settings.json
├── archived.json
└── worktrees/
    ├── mori/
    │   ├── feat-parser/
    │   └── fix-theme/
    └── other-project/
        └── feat-parser/
```

Keeping them out of the working tree means git never sees them: no `.gitignore`
entry to remember, and no `git add -A` accidentally committing a worktree as an
embedded repository. If two repositories share a directory name, the second one
gets a short hash appended (`api-3f9a2b`) so they don't collide.

Worktrees created by earlier versions under `<repo>/.claude/worktrees` keep
working — mori lists whatever `git worktree list` reports. Only new ones go to
the new location.

## Configuration

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

Each step runs in the new worktree directory. A failing step prints a warning
but doesn't block the rest. Note that the worktree is not next to the repo, so
use absolute paths when copying files in.

## Claude Code Skill

Install the [skill](skill/SKILL.md) so Claude can run mori commands directly:

```bash
cp -r skill ~/.claude/skills/mori
```

## Requirements

- Go 1.21+
- Git

## License

MIT
