#!/usr/bin/env bash
set -euo pipefail

# mori deploy script
# Creates a new semver tag, pushes it, and warms the Go module proxy
# so `go install github.com/trebaud/mori/cmd/mori@latest` picks it up.
#
# Usage:
#   ./scripts/deploy.sh                # bump patch
#   ./scripts/deploy.sh patch          # bump patch (1.1.3 -> 1.1.4)
#   ./scripts/deploy.sh minor          # bump minor (1.1.3 -> 1.2.0)
#   ./scripts/deploy.sh major          # bump major (1.1.3 -> 2.0.0)
#   ./scripts/deploy.sh v1.4.0         # explicit tag

RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
CYAN='\033[0;36m'
BOLD='\033[1m'
RESET='\033[0m'

info() { printf "${CYAN}[*]${RESET} %s\n" "$*"; }
ok()   { printf "${GREEN}[+]${RESET} %s\n" "$*"; }
warn() { printf "${YELLOW}[!]${RESET} %s\n" "$*"; }
fail() { printf "${RED}[x]${RESET} %s\n" "$*"; exit 1; }

REPO_ROOT="$(cd "$(dirname "$0")/.." && pwd)"
cd "$REPO_ROOT"

MODULE_PATH="$(go list -m 2>/dev/null || echo github.com/trebaud/mori)"
MAIN_BRANCH="main"
ARG="${1:-patch}"

# -------------------------------------------------------------------
# Preflight
# -------------------------------------------------------------------

[ -d .git ] || fail "Not a git repository."

current_branch="$(git symbolic-ref --short HEAD)"
if [ "$current_branch" != "$MAIN_BRANCH" ]; then
  fail "Must be on '$MAIN_BRANCH' branch (currently on '$current_branch')."
fi

if [ -n "$(git status --porcelain)" ]; then
  fail "Working tree is dirty. Commit or stash changes first."
fi

info "Fetching tags from origin..."
git fetch --tags --quiet origin

# Ensure local main is up to date
local_sha="$(git rev-parse HEAD)"
remote_sha="$(git rev-parse "origin/$MAIN_BRANCH")"
if [ "$local_sha" != "$remote_sha" ]; then
  fail "Local '$MAIN_BRANCH' is out of sync with origin. Pull/push first."
fi

# -------------------------------------------------------------------
# Compute next version
# -------------------------------------------------------------------

latest_tag="$(git tag -l 'v*.*.*' | sort -V | tail -1)"
[ -z "$latest_tag" ] && latest_tag="v0.0.0"
ok "Latest tag: $latest_tag"

bump_version() {
  local current="${1#v}"
  local part="$2"
  IFS='.' read -r major minor patch <<<"$current"
  case "$part" in
    major) echo "v$((major + 1)).0.0" ;;
    minor) echo "v${major}.$((minor + 1)).0" ;;
    patch) echo "v${major}.${minor}.$((patch + 1))" ;;
    *) fail "Unknown bump type: $part" ;;
  esac
}

case "$ARG" in
  major|minor|patch) NEW_TAG="$(bump_version "$latest_tag" "$ARG")" ;;
  v[0-9]*.[0-9]*.[0-9]*) NEW_TAG="$ARG" ;;
  *) fail "Invalid argument: '$ARG'. Use major|minor|patch or vX.Y.Z" ;;
esac

if git rev-parse "$NEW_TAG" >/dev/null 2>&1; then
  fail "Tag $NEW_TAG already exists."
fi

ok "New tag:    $NEW_TAG"

# -------------------------------------------------------------------
# Verify build & tests
# -------------------------------------------------------------------

info "Running go vet..."
go vet ./...
ok "vet passed"

info "Running tests..."
go test ./tests
ok "tests passed"

info "Building binary..."
go build -ldflags "-X main.ldflagsVersion=${NEW_TAG#v}" -o bin/mori ./cmd/mori
ok "build passed"

# -------------------------------------------------------------------
# Tag & push
# -------------------------------------------------------------------

echo ""
printf "${BOLD}About to tag and push:${RESET}\n"
printf "  module:  %s\n" "$MODULE_PATH"
printf "  tag:     %s\n" "$NEW_TAG"
printf "  commit:  %s\n" "$(git rev-parse --short HEAD)"
echo ""
read -r -p "Proceed? [y/N] " confirm
case "$confirm" in
  y|Y|yes|YES) ;;
  *) fail "Aborted." ;;
esac

info "Creating annotated tag $NEW_TAG..."
git tag -a "$NEW_TAG" -m "Release $NEW_TAG"

info "Pushing tag to origin..."
git push origin "$NEW_TAG"
ok "Tag pushed"

# -------------------------------------------------------------------
# Warm the Go proxy so `@latest` resolves immediately
# -------------------------------------------------------------------

info "Warming proxy.golang.org for ${MODULE_PATH}@${NEW_TAG}..."
if curl -fsSL "https://proxy.golang.org/${MODULE_PATH}/@v/${NEW_TAG}.info" >/dev/null 2>&1; then
  ok "Proxy cached the new version"
else
  warn "Proxy fetch failed — it may take a minute to propagate."
fi

echo ""
printf "${GREEN}${BOLD}  Released ${NEW_TAG}${RESET}\n\n"
printf "  Install with:\n"
printf "    ${CYAN}go install ${MODULE_PATH}/cmd/mori@latest${RESET}\n"
printf "    ${CYAN}go install ${MODULE_PATH}/cmd/mori@${NEW_TAG}${RESET}\n"
echo ""
