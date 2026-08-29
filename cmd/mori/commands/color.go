package commands

import (
	"os"

	"github.com/charmbracelet/colorprofile"
)

// progress is where the commands stream their step-by-step output. It wraps
// stderr in a colorprofile writer, which downgrades or strips the color in
// whatever is written to it based on the terminal and the environment —
// NO_COLOR, CLICOLOR, CLICOLOR_FORCE and TERM. Escape sequences that aren't
// color, like the cursor moves the spinner uses, pass through untouched.
//
// Anything written to stdout is a path a caller captures, and stays plain.
var progress = colorprofile.NewWriter(os.Stderr, os.Environ())
