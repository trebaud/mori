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
- `worktree.go` — the `Worktree` model, create/remove, sorting. `List()` returns
  every worktree git reports, including the main working tree, so lookups can
  give precise errors; `ListLinked()` drops main and is what every display uses.
- `paths.go` — where mori keeps state. Worktrees go in
  `~/.mori/worktrees/<repo>/<branch>`, never inside the repository: nested,
  git treats them as an untracked embedded repo and `git add -A` commits a
  gitlink. A `.mori-repo` marker disambiguates same-named repositories.
- `git/git.go` — thin wrappers around the git CLI. Nothing else shells out to git.
- `config.go` / `config_loader.go` — post-create hooks from `.mori.json` or
  `~/.mori/settings.json`. A hook's combined output is kept, not discarded:
  when one fails it is the only account of why, and both the CLI and the TUI
  print it.

The TUI is given no list to start with. `Init` asks for one, so the first
frame — brand, chrome, a spinner — is on screen while git is still being
queried. Keys typed in that window are held in `pendingKeys` and replayed
when the list lands, since drawing early is worthless if it also means
swallowing what the user types at the frame it bought. Every refresh beat costs several git processes per worktree, so a
list that comes back identical doubles the interval up to `refreshMax`, a
keypress or the terminal regaining focus puts it back to `refreshEvery`, and
a blurred window is not polled at all.

**`internal/tui/`** — Bubble Tea UI, Elm architecture, one concern per file:
`model.go` (state, messages, layout math), `update.go` (key handling),
`view.go` (rendering), `commands.go` (`tea.Cmd` effects), `theme.go`,
`utils.go`. The TUI renders to `/dev/tty` so stdout stays clean for the
selected path.

The layout has two shapes. Under `splitMinWidth` it is one column and the
selected worktree's path sits at a fixed row above the status line. At or
above it, `listWidth()` columns of list sit beside a `paneWidth()` pane that
describes whatever the cursor is on; `i` folds the pane away. Both end on the
same column as the top bar, which is why `listWidth` subtracts a margin.

The list is a viewport of single-line rows — one line per worktree, selected
or not. A selected row that grew a second line pushed everything under it down
and back on every keypress. `renderRows` must always return exactly
`listHeight()` rows or the footer jumps. Selection is a `>` caret in the
gutter plus the accent on the branch name — nothing in the list paints a
background.

After a create, the caret moves to the new worktree and an underline sweeps
once across its name. The sweep is clocked from the `refreshedMsg` that first
carries the row, not from the create that asked for one: `ListLinked` shells
out to git, and a sweep started earlier would be half spent before there was
anything to point at.

`detailBody` is the detail set — the full path, the git state spelled out,
and the tail of `git log` for that branch. The side pane and the card `i`
floats on a narrow terminal are both that, framed differently. Its content
outgrows a short terminal, so every line carries a drop tier and
`fitDetailLines` sheds whole tiers — padding, then the fields the row already
showed, then history oldest-first — until it fits. The compositor clips
anything past the bottom, border included, so the pane must size itself rather
than trust the terminal.

`/` filters fuzzily: `fuzzyMatch` takes the query's runes in order but not
necessarily together, scoring adjacency and word boundaries, and a query ranks
the list as well as narrowing it (a branch hit outscores the same query found
in a path). `applyFilter` resets the cursor to the best match when the query
changes and otherwise holds it on the same *worktree*, not the same index — a
background refresh must never slide a different worktree under a key about to
be pressed.

The pane follows the cursor through `paneFollow`, called once in `Update`
after every key rather than in each handler that can move the selection. It
only *schedules* the `git log`: `detailWantedMsg` carries the cursor
generation it was queued under and is dropped if the cursor has moved on, so
scrolling a long list costs one query, for the row you stop on.

The help screen is the one view that cannot drop anything — every key has to
be reachable — so it folds into columns where the width allows and scrolls
where it does not (`packColumns`, `helpBody`). Its descriptions are written to
fit `helpColumnWidth`; a longer one is truncated rather than allowed to shove
the column beside it out of line.

`renderColumnHeader` draws the labels through the same `cell` calls a row
uses, so labels stay over the values they name; column widths are measured on
the labels too, so a label is never truncated by the rows beneath it.

The rightmost column is the HEAD commit's subject, not its sha — every row's
sha reads the same, and the subject is what tells two week-old branches
apart. It absorbs whatever width the branch column did not take, so no width
leaves a gulf in the middle of a row. `internal.List` fetches it from the same
`git log` that already dates HEAD.

**`tests/`** — Integration tests that build the binary and run it against temp
git repos. Unit tests live next to the code they cover.
