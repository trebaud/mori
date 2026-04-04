#!/bin/bash

set -e

SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
BIN_NAME="mori"
BIN_DIR=""
SHELL_RC=""

if [ "$(uname)" = "Darwin" ]; then
    PLATFORM="macOS"
    if [ -w "/usr/local/bin" ]; then
        BIN_DIR="/usr/local/bin"
    elif [ -w "$HOME/.local/bin" ]; then
        BIN_DIR="$HOME/.local/bin"
    else
        BIN_DIR="$HOME/.local/bin"
        mkdir -p "$BIN_DIR"
    fi
else
    PLATFORM="Linux"
    if [ -w "/usr/local/bin" ]; then
        BIN_DIR="/usr/local/bin"
    elif [ -w "$HOME/.local/bin" ]; then
        BIN_DIR="$HOME/.local/bin"
    else
        BIN_DIR="$HOME/.local/bin"
        mkdir -p "$BIN_DIR"
    fi
fi

echo "Detected platform: $PLATFORM"

echo "Building Mori..."
cd "$SCRIPT_DIR" && go build -o "$BIN_NAME" .

echo "Installing to $BIN_DIR/$BIN_NAME..."
rm -f "$BIN_DIR/$BIN_NAME"
cp "$SCRIPT_DIR/$BIN_NAME" "$BIN_DIR/$BIN_NAME"
chmod +x "$BIN_DIR/$BIN_NAME"

if [ -n "$BASH_VERSION" ]; then
    SHELL_RC="$HOME/.bashrc"
elif [ -n "$ZSH_VERSION" ]; then
    SHELL_RC="$HOME/.zshrc"
else
    echo "Unsupported shell. Please add the function manually."
    exit 1
fi

WT_FUNC="
$BIN_NAME() {
    local target_dir=\$(\"$BIN_NAME\" \"\$@\")
    if [ -d \"\$target_dir\" ]; then
        cd \"\$target_dir\"
    fi
}
"

if ! grep -q "$BIN_NAME()" "$SHELL_RC" 2>/dev/null; then
    echo "" >> "$SHELL_RC"
    echo "# Mori - Git worktree manager" >> "$SHELL_RC"
    echo "$WT_FUNC" >> "$SHELL_RC"
    echo "Added function to $SHELL_RC"
else
    echo "$BIN_NAME function already exists in $SHELL_RC"
fi

if [[ ":$PATH:" != *":$BIN_DIR:"* ]]; then
    echo "Add $BIN_DIR to your PATH if not already present"
fi

echo "Done! Restart your shell or run: source $SHELL_RC"
