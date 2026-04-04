# Mori

> *mori* (森) — Japanese for "forest." A place where many trees grow together.

A TUI for managing git worktrees with Claude Code agent insights.

## Features

- Browse and switch between git worktrees
- Claude Code session detection (ACTIVE / STALE / NONE)
- Agent insights panel: status, session, cost, context pressure, current task, git log
- Auto-refresh every 5 seconds
- Shell integration — selecting a worktree `cd`s you into it

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
mori                        # Launch the TUI
mori new                    # Create a new worktree (random branch)
mori new feat               # Create worktree with branch name "feat"
mori new feat --claude      # Create and launch Claude Code in it
mori new feat --repo /path  # Create in a specific repo
mori list                   # List worktrees (table)
mori list --json            # List worktrees (JSON)
mori remove feat            # Remove a worktree
mori remove feat --force    # Remove without confirmation
```

### Commands

| Command | Alias | Description |
|---------|-------|-------------|
| `mori` | | Launch interactive TUI |
| `mori new [branch]` | | Create a new worktree |
| `mori list` | `ls` | List worktrees non-interactively |
| `mori remove <branch>` | `rm` | Remove a worktree |
| `mori help` | | Show help |
| `mori version` | | Show version |

### Flags

**new:**
| Flag | Description |
|------|-------------|
| `-c`, `--claude` | Launch Claude Code after creating |
| `-r`, `--repo PATH` | Repository root (default: current directory) |

**remove:**
| Flag | Description |
|------|-------------|
| `-f`, `--force` | Skip confirmation prompts |

**list:**
| Flag | Description |
|------|-------------|
| `--json` | Output as JSON |

### Keybindings

| Key | Action |
|-----|--------|
| `j` / `k` or `↑` / `↓` | Navigate |
| `Enter` | Select worktree and cd into it |
| `i` | Toggle agent insights panel |
| `q` | Quit |

### Agent Insights

Press `i` to see details for the selected worktree:

```
 AGENT INSIGHTS
────────────────────────────────────────────────────────
STATUS:      [WORKING]
SESSION:     c43dee9c
COST:        $1.42
CONTEXT:     [██████░░░░] 62%
TASK:
  Refactor JWT middleware to use the new Redis cache.

GIT LOG
  • 10 minutes ago: fix: cache miss on init
  • 1 hour ago: feat: add redis layer
```

## Requirements

- Go 1.21+
- Git
- Claude Code (optional, for agent insights)

## License

MIT
