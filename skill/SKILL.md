---
name: mori
description: >
  Manage Git worktrees and monitor Claude Code agent sessions using mori.
  Use when the user asks to create, list, switch, or remove worktrees,
  check agent status/cost, or set up mori configuration.
  Keywords: worktree, mori, branch, agent status, cost tracking, session.
license: Apache-2.0
metadata:
  version: 1.0.0
  author: trebaud
allowed-tools: Bash Read Write Edit Glob Grep
---

# Mori — Git Worktree & Agent Session Manager

Mori is a TUI and CLI for managing Git worktrees with built-in Claude Code agent insights (status, cost, context usage).

## When to Use

- User asks to create, switch to, list, or remove a worktree
- User wants to check agent session status or cost across worktrees
- User says "new worktree", "switch branch", "mori", or "agent status"
- User wants to configure post-create hooks (`.mori.json`)

## CLI Reference

| Command | Alias | Description |
|---|---|---|
| `mori` | | Launch interactive TUI |
| `mori new [branch]` | | Create worktree (random name if omitted) |
| `mori new [branch] -c` | | Create worktree and launch Claude Code in it |
| `mori open <branch>` | | Print worktree path for branch |
| `mori list` | `ls` | Table view: path, branch, status, cost |
| `mori list --json` | | Full JSON with cost, model, task, context |
| `mori list --status working` | | Filter by status: working, idle, waiting, none |
| `mori status` | | One-line summary (e.g. "2 working, 1 idle") |
| `mori remove <branch>` | `rm` | Remove worktree (confirms if active/dirty) |
| `mori remove <branch> -f` | | Force remove, skip all checks |

## TUI Keybindings

| Key | Action |
|---|---|
| `j`/`k`, arrows | Navigate worktrees |
| `Enter` | Select worktree (copy `cd` command to clipboard) |
| `i` | Toggle insights panel (cost, context, task, git log) |
| `n` | Create new worktree |
| `d` / `D` | Delete worktree / force delete |
| `/` | Search/filter by branch or path |
| `s` | Cycle sort: default, status, activity, name |
| `r` | Refresh insights |
| `p` | Refresh PR status |
| `?` | Show help |
| `q` | Quit |

## Insights Panel

When visible (`i` key or wide terminal), shows per-worktree:

- Status — WORKING, IDLE, WAITING, or NONE
- Cost — cumulative USD from API token usage
- Context — token usage bar (input tokens / 200k max)
- Model — Claude model + permission mode
- Task — last user message (truncated)
- Git log — 5 most recent commits
- Branch — ahead/behind vs main
- Pull request — number, state (open/draft/merged/closed), title, URL (when `gh` is installed)

## Configuration

### Project config: `.mori.json` (repo root)

```json
{
  "post_create": [
    { "name": "Install deps", "cmd": "npm install" },
    { "name": "Copy env", "cmd": "cp ../.env .env" }
  ]
}
```

### Global config: `~/.mori/settings.json`

Same schema. Project config overrides global. Each step runs in the new worktree directory. Failures warn but don't block.

## Common Workflows

### Create a worktree and start working

```bash
mori new feature-auth --claude
# Creates worktree, runs post_create hooks, launches Claude Code
```

### Monitor all active agents

```bash
mori list --status working
# or
mori status
# "5 worktrees: 2 working, 1 waiting, 2 idle"
```

### Clean up finished work

```bash
mori remove feature-auth
# Warns if agent is active or there are uncommitted changes
```

### Set up hooks for a project

Create `.mori.json` in the repo root with `post_create` steps. These run automatically whenever `mori new` creates a worktree.

## Notes

- Worktrees are created at `.claude/worktrees/{branch}` relative to the repo root.
- The TUI adapts layout to terminal width: list-only (<80), stacked (80-119), side-by-side (120+).
- Refresh rate is adaptive: 2s when agents are active, 10s when idle.
- Active sessions (WORKING/WAITING) block deletion unless `--force` is used.
- PR status uses `gh`; gracefully omitted if `gh` is missing or unauthenticated.
