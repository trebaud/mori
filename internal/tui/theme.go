package tui

import (
	"image/color"
	"os"

	"charm.land/lipgloss/v2"
)

// Palette — semantic colors only. We default to ANSI 16 indices so the user's
// terminal theme controls the actual appearance. Layered backgrounds and
// borders get explicit hex values chosen per light/dark detection in ApplyTheme.
var (
	colAccent  color.Color = lipgloss.Color("5") // magenta
	colSuccess color.Color = lipgloss.Color("2") // green
	colWarn    color.Color = lipgloss.Color("3") // yellow
	colDanger  color.Color = lipgloss.Color("1") // red

	colText   color.Color = lipgloss.NoColor{} // terminal default foreground
	colMuted  color.Color = lipgloss.ANSIColor(8)
	colRowBg  color.Color = lipgloss.Color("237")
	colBorder color.Color = lipgloss.ANSIColor(8)
)

var (
	titleStyle    lipgloss.Style
	textStyle     lipgloss.Style
	mutedStyle    lipgloss.Style
	dimStyle      lipgloss.Style
	headingStyle  lipgloss.Style
	selectedStyle lipgloss.Style

	dirtyStyle lipgloss.Style
	cleanStyle lipgloss.Style

	errorStyle   lipgloss.Style
	successStyle lipgloss.Style
	warnStyle    lipgloss.Style

	borderStyle lipgloss.Style
)

func init() {
	ApplyTheme(true)
}

// ApplyTheme rebuilds the style table for the current terminal background.
// Call once at startup after detecting whether the terminal is dark or light;
// the package keeps a sensible dark default until then.
func ApplyTheme(isDark bool) {
	ld := lipgloss.LightDark(isDark)

	// 239 (dark) sits ~5 steps above common terminal backgrounds so the
	// selection reads even on themes that tint bg around 234. 254 (light)
	// is the symmetric near-bg gray for light terminals.
	colRowBg = ld(lipgloss.Color("254"), lipgloss.Color("239"))
	colBorder = ld(lipgloss.Color("250"), lipgloss.Color("238"))

	titleStyle = lipgloss.NewStyle().Foreground(colAccent).Bold(true)
	textStyle = lipgloss.NewStyle()
	mutedStyle = lipgloss.NewStyle().Foreground(colMuted)
	dimStyle = lipgloss.NewStyle().Faint(true)
	headingStyle = lipgloss.NewStyle().Foreground(colMuted).Bold(true)
	selectedStyle = lipgloss.NewStyle().Foreground(colAccent).Bold(true)

	// A worktree with uncommitted work is the one that needs attention.
	dirtyStyle = lipgloss.NewStyle().Foreground(colWarn)
	cleanStyle = lipgloss.NewStyle().Foreground(colMuted)

	errorStyle = lipgloss.NewStyle().Foreground(colDanger)
	successStyle = lipgloss.NewStyle().Foreground(colSuccess)
	warnStyle = lipgloss.NewStyle().Foreground(colWarn)

	borderStyle = lipgloss.NewStyle().Foreground(colBorder)
}

// DetectAndApplyTheme queries the terminal for its background color once and
// applies the matching theme. Falls back to dark on any detection error.
func DetectAndApplyTheme(tty *os.File) {
	ApplyTheme(lipgloss.HasDarkBackground(tty, tty))
}
