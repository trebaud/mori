# Mori

> *mori* (森) — Japanese for "forest." A place where many trees grow together.

A TUI for managing git worktrees with Claude Code agent insights.

## Features

- Browse, create, and delete git worktrees without leaving the TUI
- Side-by-side worktree list + agent insights panel
- Session status tracking: WORKING, IDLE, WAITING, NONE
- Per-worktree insights: cost, context usage, current task, git log, ahead/behind
- Search/filter (`/`) and sort (`s`) worktrees
- Adaptive refresh: 2s when agents are active, 10s when idle
- Selecting a worktree `cd`s into it via shell wrapper

## Install

```bash
git clone https://github.com/moosecode/mori.git
cd mori
./install.sh
```

This builds the binary, installs it to your PATH, and adds a `mori` shell function to your rc file so selecting a worktree `cd`s you into it.

### Manual

```bash
go build -o mori .
cp mori /usr/local/bin/
```

Then add to your `~/.zshrc` or `~/.bashrc`:

```bash
mori() {
    local target_dir=$(command mori "$@")
    if [ -d "$target_dir" ]; then
        cd "$target_dir"
    fi
}
```

## Usage

```bash
mori                           # Launch the TUI
mori new                       # Create a new worktree (random branch)
mori new feat --claude         # Create and launch Claude Code in it
mori open feat                 # Switch to worktree by branch name
mori list                      # List worktrees (table)
mori list --json               # List with full insights as JSON
mori list --status working     # Filter by agent status
mori status                    # One-line summary of all worktrees
mori remove feat               # Remove a worktree
mori remove feat --force       # Remove without confirmation
```

### Commands

| Command | Alias | Description |
|---------|-------|-------------|
| `mori` | | Launch interactive TUI |
| `mori new [branch]` | | Create a new worktree |
| `mori open <branch>` | | Switch to worktree by name |
| `mori list` | `ls` | List worktrees non-interactively |
| `mori status` | | Show worktree summary |
| `mori remove <branch>` | `rm` | Remove a worktree |
| `mori help` | | Show help |
| `mori version` | | Show version |

### Flags

**new:**
| Flag | Description |
|------|-------------|
| `-c`, `--claude` | Launch Claude Code after creating |
| `-r`, `--repo PATH` | Repository root (default: current directory) |

**list:**
| Flag | Description |
|------|-------------|
| `--json` | Output as JSON (includes cost, model, task, etc.) |
| `--status STATUS` | Filter by status (working, idle, waiting, none) |

**remove:**
| Flag | Description |
|------|-------------|
| `-f`, `--force` | Skip confirmation prompts |

### TUI Keybindings

| Key | Action |
|-----|--------|
| `j`/`k`, `↑`/`↓` | Navigate |
| `Enter` | Select worktree and cd into it |
| `i` | Toggle insights panel |
| `n` | Create new worktree |
| `d` / `D` | Delete worktree / force delete |
| `/` | Filter by branch or path |
| `s` | Cycle sort (status / activity / name) |
| `r` | Refresh now |
| `?` | Show all keybindings |
| `q` | Quit |

### Agent Insights

Press `i` to see details for the selected worktree:

```
 AGENT INSIGHTS
────────────────────────────────────────────────────────
STATUS:      ● [WORKING] Bash
SESSION:     cozy-gathering-iverson
MODEL:       opus / acceptEdits
COST:        $1.42
CONTEXT:     [██████░░░░] 62% (124k/200k)
BRANCH:      +3/-0
TASK:
  Refactor JWT middleware to use the new Redis cache.

GIT LOG
  • 10 minutes ago: fix: cache miss on init
  • 1 hour ago: feat: add redis layer
```

On wide terminals (120+), insights display side-by-side with the worktree list.

## Requirements

- Go 1.21+
- Git
- Claude Code (optional, for agent insights)

## License

MIT
