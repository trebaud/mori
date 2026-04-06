#!/usr/bin/env bash
set -euo pipefail

# mori installer
# Usage: git clone ... && cd mori && ./install.sh

RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
CYAN='\033[0;36m'
BOLD='\033[1m'
DIM='\033[2m'
RESET='\033[0m'

SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
BIN_NAME="mori"
GO_MIN_VERSION="1.21"

info()  { printf "${CYAN}[*]${RESET} %s\n" "$*"; }
ok()    { printf "${GREEN}[+]${RESET} %s\n" "$*"; }
warn()  { printf "${YELLOW}[!]${RESET} %s\n" "$*"; }
fail()  { printf "${RED}[x]${RESET} %s\n" "$*"; exit 1; }

header() {
  echo ""
  printf "${BOLD}${CYAN}"
  cat << 'ART'
                       _
   _ __ ___   ___  _ __(_)
  | '_ ` _ \ / _ \| '__| |
  | | | | | | (_) | |  | |
  |_| |_| |_|\___/|_|  |_|

ART
  printf "${RESET}"
  printf "${DIM}  Git worktree manager with Claude Code insights${RESET}\n"
  echo ""
}

# -------------------------------------------------------------------
# System checks
# -------------------------------------------------------------------

check_os() {
  case "$(uname -s)" in
    Darwin) OS="macos" ;;
    Linux)  OS="linux" ;;
    *)      fail "Unsupported OS: $(uname -s). macOS or Linux required." ;;
  esac
  ok "OS: $(uname -s) $(uname -m)"
}

check_git() {
  if ! command -v git &>/dev/null; then
    fail "git is not installed. Install it first:
    macOS:  xcode-select --install
    Linux:  sudo apt install git  (or your distro's equivalent)"
  fi
  ok "git: $(git --version | head -1)"
}

# Compare semver: returns 0 if $1 >= $2
version_gte() {
  [ "$(printf '%s\n' "$1" "$2" | sort -V | head -1)" = "$2" ]
}

check_go() {
  if ! command -v go &>/dev/null; then
    fail "go is not installed. Install it first:
    macOS:  brew install go
    Linux:  https://go.dev/doc/install"
  fi
  local ver
  ver="$(go version | grep -oE '[0-9]+\.[0-9]+(\.[0-9]+)?' | head -1)"
  if ! version_gte "$ver" "$GO_MIN_VERSION"; then
    fail "go v${ver} found but v${GO_MIN_VERSION}+ required."
  fi
  ok "go: v${ver}"
}

# -------------------------------------------------------------------
# Build & install
# -------------------------------------------------------------------

build_binary() {
  info "Building mori..."
  cd "$SCRIPT_DIR"
  go build -o "$BIN_NAME" ./cmd/mori
  ok "Binary built: $SCRIPT_DIR/$BIN_NAME"
}

install_binary() {
  local bin_dir

  if [ -w "/usr/local/bin" ]; then
    bin_dir="/usr/local/bin"
  else
    bin_dir="$HOME/.local/bin"
    mkdir -p "$bin_dir"
  fi

  rm -f "$bin_dir/$BIN_NAME"
  cp "$SCRIPT_DIR/$BIN_NAME" "$bin_dir/$BIN_NAME"
  chmod +x "$bin_dir/$BIN_NAME"
  ok "Installed: $bin_dir/$BIN_NAME"

  if ! echo "$PATH" | tr ':' '\n' | grep -qx "$bin_dir"; then
    warn "$bin_dir is not on your PATH"
    printf "${YELLOW}  Add this to your shell profile:${RESET}\n"
    printf "${BOLD}    export PATH=\"\$HOME/.local/bin:\$PATH\"${RESET}\n"
    echo ""
  fi

  # Export for use in shell function setup
  INSTALL_BIN_DIR="$bin_dir"
}

# -------------------------------------------------------------------
# Main
# -------------------------------------------------------------------

header
info "Starting installation..."
echo ""

check_os
check_git
check_go
echo ""

build_binary
install_binary

echo ""
printf "${GREEN}${BOLD}  Installation complete!${RESET}\n"
echo ""
printf "  ${BOLD}Run it:${RESET}\n"
printf "    ${CYAN}mori${RESET}                              # interactive TUI\n"
printf "    ${CYAN}mori new feat --claude${RESET}             # create worktree + launch Claude\n"
printf "    ${CYAN}mori list${RESET}                          # list worktrees\n"
echo ""
printf "  ${DIM}Source: $SCRIPT_DIR${RESET}\n"
printf "  ${DIM}Binary: ${INSTALL_BIN_DIR:-/usr/local/bin}/$BIN_NAME${RESET}\n"
echo ""
