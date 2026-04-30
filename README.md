# Mori

> *mori* (森) — Japanese for "forest." A place where many trees grow together.

A TUI for managing git worktrees with Claude Code agent insights.

![Mori TUI](https://github.com/user-attachments/assets/5384c293-4654-49fa-84bb-b7cfcfce5385)

## Features

- Browse, create, and delete git worktrees without leaving the TUI
- Side-by-side worktree list + agent insights panel
- Session status tracking: WORKING, IDLE, WAITING, NONE
- Per-worktree insights: cost, context usage, current task, git log, ahead/behind
- PR tracking: shows GitHub PR state (open/draft/merged/closed) per branch via [`gh`](https://cli.github.com/)
- Search/filter (`/`) and sort (`s`) worktrees
- Adaptive refresh: 2s when agents are active, 10s when idle
- Includes a [Claude Code skill](skill/SKILL.md) so Claude can manage worktrees for you

## Install

With `go install`:

```bash
go install github.com/trebaud/mori/cmd/mori@latest
```

This drops the `mori` binary in `$(go env GOPATH)/bin` — make sure that's on your `$PATH`.

Or from source:

```bash
git clone https://github.com/trebaud/mori.git
cd mori
./scripts/install.sh
```

## Usage

```bash
mori                           # Launch the TUI
mori new                       # Create a new worktree (random branch)
mori new feat --claude         # Create and launch Claude Code in it
mori open feat                 # Open a claude session on this worktree
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
| `mori open <branch>` | | Open a claude session on this worktree |
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

**Navigation**
| Key | Action |
|-----|--------|
| `j`/`k`, `↑`/`↓` | Move cursor |
| `g` / `G` | Jump to first / last |
| `ctrl+d` / `ctrl+u` | Half-page down / up |
| `w` | Jump to next working/waiting worktree |
| `Enter` | Toggle insights panel |
| `o` | Open Claude Code in selected worktree |
| `q`, `ctrl+c` | Quit |

**Actions**
| Key | Action |
|-----|--------|
| `n` | Create new worktree |
| `d` / `D` | Delete worktree / force delete |
| `y` | Yank (copy) worktree path to clipboard |
| `m` | Send message to a waiting agent |
| `r` | Refresh now |
| `p` | Refresh PR status |
| `?` | Toggle keybindings help |

**Search, sort & filter**
| Key | Action |
|-----|--------|
| `/` | Search by branch or path |
| `s` | Cycle sort (default / status / activity / name) |
| `f` | Cycle status filter (all / working / waiting / idle / none) |
| `Esc` | Clear filter / cancel input |

**Archive**
| Key | Action |
|-----|--------|
| `x` | Archive / unarchive selected worktree |
| `X` | Toggle showing archived worktrees |

### Agent Insights

Press `Enter` to toggle the insights panel for the selected worktree:

```
 AGENT INSIGHTS
────────────────────────────────────────────────────────
STATUS:      ● [WORKING] Bash
SESSION:     cozy-gathering-iverson
MODEL:       opus / acceptEdits
COST:        $1.42
CONTEXT:     [██████░░░░] 62% (124k/200k)
BRANCH:      +3/-0
PR:          ● #1234 open · refactor JWT middleware
TASK:
  Refactor JWT middleware to use the new Redis cache.

GIT LOG
  • 10 minutes ago: fix: cache miss on init
  • 1 hour ago: feat: add redis layer
```

On wide terminals (120+), insights display side-by-side with the worktree list.

## Configuration

Mori supports global and per-project configuration.

- **Global:** `~/.mori/settings.json`
- **Project:** `.mori.json` in the repo root (overrides global)

### Post-create hooks

Run commands automatically after creating a worktree (e.g. install dependencies, copy env files):

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

Mori includes a [Claude Code skill](skills/mori/SKILL.md) so Claude knows how to run mori commands. Install it by copying the skill into your skills directory:

```bash
cp -r skills/mori ~/.agents/skills/mori
```

Once installed, Claude can create worktrees, check agent status, and manage branches directly.

### Parallel implementation workflows

You can also use the skill to run competing implementations side by side:

1. `mori new auth-approach-a --claude` — token-based auth
2. `mori new auth-approach-b --claude` — session-based auth
3. `mori list --status working` — monitor both agents as they work
4. Compare the results side-by-side, then keep the best one and `mori remove` the rest

Run them at the same time, watch cost and progress in the TUI, then keep the best one and `mori remove` the rest.

## Requirements

- Go 1.21+
- Git
- [tmux](https://github.com/tmux/tmux) (used to launch and manage Claude Code sessions)
- [Claude Code](https://docs.anthropic.com/en/docs/claude-code) (for agent sessions and insights)
- [gh](https://cli.github.com/) (optional, for PR status)

## License

MIT
