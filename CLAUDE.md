# CLAUDE.md

## Commands

```bash
go build -o mori ./cmd/mori          # Build
go test ./tests                       # Run all tests
go test -run TestVersion ./tests      # Run a single test
```

## Architecture

Entry point is `cmd/mori/main.go` — manual arg parsing, dispatches to `cmd/mori/commands/` (one file per command).

**`internal/`** — Core logic:
- `tui.go` — Bubble Tea model. Input modes (Normal, Search, Create, ConfirmDelete), sort modes, adaptive layout by terminal width.
- `worktree.go` — Worktree model and operations.
- `config.go` — Post-create hooks from `.mori.json` or `~/.mori/settings.json`.
- `git/git.go` — Thin wrappers around git CLI.
- `agent/insights.go` — Parses Claude Code session JSONL logs from `~/.claude/projects/` for status, cost, context, model, and task.

**`tests/`** — Integration tests that build the binary and run it against temp git repos.
