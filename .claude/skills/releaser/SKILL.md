---
name: releaser
description: >
  Create a new mori release: build cross-platform artifacts with scripts/releaser.sh,
  generate a changelog since the previous tag, and publish a GitHub release via gh.
  Use when the user says "release", "cut a release", "publish v1.x", or "new version".
allowed-tools: Bash
---

# Release mori

Create a new GitHub release for the mori project.

## Arguments

`$ARGUMENTS` may be:
- A full version tag: `v1.2.0` — used as-is, skips version analysis
- A bump keyword: `patch`, `minor`, or `major` — overrides the inferred bump
- Empty — infer the bump level from the changes (see step 1)

## Steps

### 1. Determine the version bump

Get the latest tag and the commits since it:
```bash
git describe --tags --abbrev=0
git log <LATEST_TAG>..HEAD --oneline --no-merges
git diff <LATEST_TAG>..HEAD
```

If `$ARGUMENTS` is a full version tag (starts with `v`), skip this analysis and use it directly.

Otherwise, analyze the commits and diff to classify the bump:

**MAJOR** — any of:
- A commit message contains `BREAKING CHANGE:` or a type with `!` (e.g. `feat!:`, `fix!:`)
- A public API, CLI flag, config key, or command name was removed or its behavior changed in an incompatible way
- A public interface or struct field was removed or renamed in a breaking way

**MINOR** — none of the above, and any of:
- A commit type is `feat:` or similar (new feature added)
- A new CLI command, flag, or config option was introduced
- New functionality added without breaking existing behavior

**PATCH** — only backwards-compatible fixes, chores, or documentation:
- Commit types: `fix:`, `chore:`, `docs:`, `refactor:`, `perf:`, `test:`, `style:`
- No new features, no breaking changes

If `$ARGUMENTS` is a bump keyword (`patch`, `minor`, `major`), use that instead of the inferred level.

Compute the next version by incrementing the appropriate component of the latest tag:
- `major` → bump first number, reset minor and patch to 0
- `minor` → bump second number, reset patch to 0
- `patch` → bump third number

Always prefix with `v` (e.g. `v1.2.3`).

Present the inferred bump level, the reasoning, and the resolved version to the user, then ask for confirmation before proceeding.

### 2. Build release artifacts

```bash
bash scripts/releaser.sh <VERSION>
```

This produces `./dist/*.tar.gz` and `./dist/*.sha256` for all supported platforms. Fail loudly if the script exits non-zero.

### 3. Generate changelog

Get the previous tag (the one before the latest):
```bash
git tag --sort=-version:refname | sed -n '2p'
```

Then collect commits between the previous tag and HEAD:
```bash
git log <PREV_TAG>..HEAD --oneline --no-merges
```

Format the notes as:

```
## What's Changed

- <commit message>
- <commit message>
...

**Full changelog**: https://github.com/trebaud/mori/compare/<PREV_TAG>...<NEW_VERSION>
```

### 4. Create the GitHub release

```bash
gh release create <VERSION> ./dist/*.tar.gz ./dist/*.sha256 \
  --title "<VERSION>" \
  --notes "<NOTES>"
```

Print the release URL when done.
