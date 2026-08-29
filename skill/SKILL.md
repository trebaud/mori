---
name: mori
description: >
  Manage Git worktrees using mori.
  Use when the user asks to create, list, locate, or remove git worktrees,
  or to set up mori configuration.
  Keywords: worktree, mori, branch, git worktree, parallel branches.
license: Apache-2.0
metadata:
  version: 2.0.0
  author: trebaud
allowed-tools: Bash Read Write Edit Glob Grep
---

# Mori — Git Worktree Manager

Mori is a TUI and CLI for managing Git worktrees. It lists worktrees with their
git state and does CRUD on them. Nothing else.

## When to Use

- User asks to create, list, locate, or remove a worktree
- User says "new worktree", "worktree for X", or "mori"
- User wants to configure post-create hooks (`.mori.json`)
- User wants competing implementations side by side, each in its own worktree

## CLI Reference

| Command | Alias | Description |
|---|---|---|
| `mori` | | Launch the TUI (interactive only — don't run this from a script) |
| `mori new [branch]` | | Create a worktree; random name if omitted. Prints the new directory |
| `mori list` | `ls` | Table: path, branch, changes, sync |
| `mori list --json` | | JSON with path, branch, head, dirty, ahead, behind, last_commit |
| `mori path <branch>` | `open` | Print the worktree directory for a branch |
| `mori remove <branch>` | `rm` | Remove a worktree (prompts if dirty) |
| `mori remove <branch> -f` | | Force remove, no prompt |

Worktrees are created under `~/.mori/worktrees/<repo>/<branch>`, off the repo's
default branch. They deliberately live outside the repository, so a new worktree
never shows up in `git status`.

## Usage Notes

- **Prefer `--json`** when you need to reason about worktree state. Fields:
  `path`, `branch`, `display_path`, `head`, `main`, `detached`, `dirty`,
  `ahead`, `behind`, `last_commit`.
- **Never run bare `mori`** — it opens an interactive TUI. Use `mori list` or
  `mori path` instead.
- **`mori new` prints the directory on stdout**, so you can `cd "$(mori new feat)"`.
- **`mori remove` prompts on uncommitted changes.** In a non-interactive
  context pass `-f`, and only after checking `dirty` in `mori list --json`.

## Configuration

`.mori.json` in the repo root (or `~/.mori/settings.json` globally) runs
commands in each new worktree:

```json
{
  "post_create": [
    { "name": "Installing dependencies", "cmd": "npm install" },
    { "name": "Copying env", "cmd": "cp ~/Code/myproject/.env .env" }
  ]
}
```

Steps run in the new worktree directory, in order. A failing step warns but
doesn't abort the rest. The worktree is not adjacent to the repo, so relative
paths like `../.env` will not resolve — use absolute ones.
