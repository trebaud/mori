# CLAUDE.md

## Commands

```bash
go build -o mori ./cmd/mori          # Build
go test ./...                         # Run all tests
go test -run TestVersion ./tests      # Run a single integration test
```

## Scope

Mori lists git worktrees and does CRUD on them. That's it. It does not track
agent sessions, launch other tools, or talk to GitHub — don't add those back.

## Architecture

Entry point is `cmd/mori/main.go` — a cobra root that dispatches to
`cmd/mori/commands/` (one file per command). Every command prints its result
(a path, a table, JSON) on stdout and progress on stderr, so `$(mori)` and
`$(mori path X)` are safe to capture.

**`internal/`** — Core logic:
- `worktree.go` — the `Worktree` model, `List()`, create/remove, sorting.
- `paths.go` — where mori keeps state. Worktrees go in
  `~/.mori/worktrees/<repo>/<branch>`, never inside the repository: nested,
  git treats them as an untracked embedded repo and `git add -A` commits a
  gitlink. A `.mori-repo` marker disambiguates same-named repositories.
- `git/git.go` — thin wrappers around the git CLI. Nothing else shells out to git.
- `config.go` / `config_loader.go` — post-create hooks from `.mori.json` or
  `~/.mori/settings.json`.

**`internal/tui/`** — Bubble Tea UI, Elm architecture, one concern per file:
`model.go` (state, messages, layout math), `update.go` (key handling),
`view.go` (rendering), `commands.go` (`tea.Cmd` effects), `theme.go`,
`utils.go`. The TUI renders to `/dev/tty` so stdout stays clean for the
selected path.

The list is a viewport of fixed-height cards: `cardHeight` rows per worktree,
`chromeHeight` rows of surrounding chrome. `renderCards` must always return
exactly `listHeight()` rows or the footer jumps.

**`tests/`** — Integration tests that build the binary and run it against temp
git repos. Unit tests live next to the code they cover.
