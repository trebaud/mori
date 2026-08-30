# Mori

[![Go](https://img.shields.io/badge/go-1.26%2B-00ADD8?logo=go)](https://go.dev)
[![License](https://img.shields.io/badge/license-MIT-blue.svg)](#license)
[![Release](https://img.shields.io/github/v/release/trebaud/mori)](https://github.com/trebaud/mori/releases)

> *mori* (森) — Japanese for "forest." A place where many trees grow together.

A small TUI for managing git worktrees.

## Install

```bash
go install github.com/trebaud/mori/v2/cmd/mori@latest
```

The `/v2` is part of the module path, not a typo. Without it Go resolves the
v1 line and installs v1.5.0. Make sure `$(go env GOPATH)/bin` is on your
`$PATH`.

Prebuilt binaries for Linux, macOS and Windows are attached to every
[release](https://github.com/trebaud/mori/releases), each with a `.sha256`
sibling to check it against:

```bash
shasum -a 256 -c mori-v2.0.0-linux-amd64.sha256
```

Or build from source with `./scripts/install.sh`, which checks dependencies,
builds the binary, and installs it to `/usr/local/bin` or `~/.local/bin`.
Building needs Go 1.26.1 or newer.

Pick one method and stay with it. `go install` writes to
`$(go env GOPATH)/bin`, `install.sh` to `/usr/local/bin` or `~/.local/bin`; if
both are on your `$PATH`, whichever comes first wins and an old copy can
shadow a new one. `mori version` tells you which one you're actually running.

## Usage

```bash
mori                  # TUI; prints the picked worktree path on exit
mori new feat         # Create a worktree on a new branch "feat"
mori list             # Table of worktrees
mori list --json      # Same, machine-readable
mori path feat        # Print the directory for branch "feat"
mori remove feat      # Remove a worktree (asks before discarding changes)
```

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
but doesn't block the rest. The worktree is not next to the repo, so use
absolute paths when copying files in.

## Claude Code Skill

Install the [skill](skill/SKILL.md) so Claude can run mori commands directly:

```bash
cp -r skill ~/.claude/skills/mori
```

## License

MIT
