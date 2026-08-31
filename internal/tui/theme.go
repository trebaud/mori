package tui

import (
	"image/color"
	"os"

	"charm.land/lipgloss/v2"
)

// Palette — semantic colors only. We default to ANSI 16 indices so the user's
// terminal theme controls the actual appearance. Nothing in the list paints a
// background; only the border gets an explicit value, chosen per light/dark
// detection in ApplyTheme.
var (
	colAccent  color.Color = lipgloss.Color("5") // magenta
	colSuccess color.Color = lipgloss.Color("2") // green
	colWarn    color.Color = lipgloss.Color("3") // yellow
	colDanger  color.Color = lipgloss.Color("1") // red

	colText   color.Color = lipgloss.NoColor{} // terminal default foreground
	colMuted  color.Color = lipgloss.ANSIColor(8)
	colBorder color.Color = lipgloss.ANSIColor(8)
)

var (
	titleStyle    lipgloss.Style
	brandStyle    lipgloss.Style
	textStyle     lipgloss.Style
	mutedStyle    lipgloss.Style
	dimStyle      lipgloss.Style
	headingStyle  lipgloss.Style
	columnStyle   lipgloss.Style
	selectedStyle lipgloss.Style
	keyStyle      lipgloss.Style
	cursorStyle   lipgloss.Style
	pathStyle     lipgloss.Style

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

	colBorder = ld(lipgloss.Color("250"), lipgloss.Color("238"))

	titleStyle = lipgloss.NewStyle().Foreground(colAccent).Bold(true)
	// The wordmark is set in the accent without bold: at this size the color
	// alone carries it, and bold on a two-glyph mark reads as shouting.
	brandStyle = lipgloss.NewStyle().Foreground(colAccent)
	textStyle = lipgloss.NewStyle()
	mutedStyle = lipgloss.NewStyle().Foreground(colMuted)
	dimStyle = lipgloss.NewStyle().Faint(true)
	headingStyle = lipgloss.NewStyle().Foreground(colMuted).Bold(true)
	// Column labels name the grid without competing with it, so they sit a
	// step quieter than the muted text underneath them.
	columnStyle = lipgloss.NewStyle().Foreground(colMuted).Faint(true)
	selectedStyle = lipgloss.NewStyle().Foreground(colAccent).Bold(true)
	// The caret in the left gutter is the entire selection marker. A tint
	// behind the row would fight whatever the user's terminal theme already
	// paints there; one glyph and a colored name cannot.
	cursorStyle = lipgloss.NewStyle().Foreground(colAccent).Bold(true)
	// The selected worktree's full path, set under its name. It is a caption
	// to the row, not another column, so it sits lighter than the row above.
	pathStyle = lipgloss.NewStyle().Foreground(colMuted).Faint(true)

	// Key hints read as "key, then what it does": the key sits at the
	// terminal's own foreground, bold enough to scan down, while its label
	// stays muted. No brackets — weight separates them.
	keyStyle = lipgloss.NewStyle().Foreground(colText).Bold(true)

	// A worktree with uncommitted work is the one that needs attention; a
	// clean one says so as quietly as it can and still be there.
	dirtyStyle = lipgloss.NewStyle().Foreground(colWarn)
	cleanStyle = lipgloss.NewStyle().Foreground(colMuted).Faint(true)

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

// markMatch decorates a style so the run of text a filter matched stands out
// from the text around it. It underlines as well as colors, so the hit is
// still visible under NO_COLOR and on monochrome terminals.
func markMatch(st lipgloss.Style) lipgloss.Style {
	return st.Foreground(colAccent).Underline(true)
}
