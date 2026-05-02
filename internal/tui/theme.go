package tui

import (
	"image/color"
	"os"

	"charm.land/lipgloss/v2"

	"github.com/trebaud/mori/internal/insights"
)

// Palette — semantic colors only. We default to ANSI 16 indices so the user's
// terminal theme controls actual appearance. Layered backgrounds use explicit
// hex values selected per light/dark detection in ApplyTheme.
var (
	colAccent  color.Color = lipgloss.Color("5") // magenta
	colSuccess color.Color = lipgloss.Color("2") // green
	colWarn    color.Color = lipgloss.Color("3") // yellow
	colDanger  color.Color = lipgloss.Color("1") // red
	colInfo    color.Color = lipgloss.Color("6") // cyan

	// Text-tone colors. colText leaves the foreground at the terminal default;
	// colMuted/colDim/colFaint use ANSI 8 so the terminal theme decides the
	// actual gray. ApplyTheme overrides background/border with adaptive hex
	// pairs so they're visible on light themes.
	colText   color.Color = lipgloss.NoColor{}
	colMuted  color.Color = lipgloss.ANSIColor(8)
	colDim    color.Color = lipgloss.ANSIColor(8)
	colFaint  color.Color = lipgloss.ANSIColor(8)
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

	workingStyle lipgloss.Style
	waitingStyle lipgloss.Style
	idleStyle    lipgloss.Style
	noneStyle    lipgloss.Style

	errorStyle   lipgloss.Style
	successStyle lipgloss.Style

	barHighStyle lipgloss.Style
	barMedStyle  lipgloss.Style
	barLowStyle  lipgloss.Style

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
	colFaint = colBorder

	titleStyle = lipgloss.NewStyle().Foreground(colAccent).Bold(true)
	textStyle = lipgloss.NewStyle()
	mutedStyle = lipgloss.NewStyle().Foreground(colMuted)
	dimStyle = lipgloss.NewStyle().Faint(true)
	headingStyle = lipgloss.NewStyle().Foreground(colMuted).Bold(true)
	selectedStyle = lipgloss.NewStyle().Foreground(colAccent).Bold(true)

	// Status semantics: working is the desired healthy state (green); waiting
	// is the one that needs the user's attention, so it gets the warn color;
	// idle is informational; none is muted.
	workingStyle = lipgloss.NewStyle().Foreground(colSuccess).Bold(true)
	waitingStyle = lipgloss.NewStyle().Foreground(colWarn).Bold(true)
	idleStyle = lipgloss.NewStyle().Foreground(colInfo)
	noneStyle = lipgloss.NewStyle().Faint(true)

	errorStyle = lipgloss.NewStyle().Foreground(colDanger)
	successStyle = lipgloss.NewStyle().Foreground(colSuccess)

	barHighStyle = lipgloss.NewStyle().Foreground(colDanger)
	barMedStyle = lipgloss.NewStyle().Foreground(colWarn)
	barLowStyle = lipgloss.NewStyle().Foreground(colSuccess)

	borderStyle = lipgloss.NewStyle().Foreground(colBorder)
}

// DetectAndApplyTheme inspects the terminal background once and applies the
// matching theme. Falls back to dark on any detection error.
func DetectAndApplyTheme() {
	ApplyTheme(lipgloss.HasDarkBackground(os.Stdin, os.Stdout))
}

func statusIcon(status insights.StatusType) string {
	switch status {
	case insights.StatusWorking:
		return "●"
	case insights.StatusWait:
		return "◐"
	case insights.StatusIdle:
		return "○"
	default:
		return "·"
	}
}

func statusStyle(status insights.StatusType) lipgloss.Style {
	switch status {
	case insights.StatusWorking:
		return workingStyle
	case insights.StatusWait:
		return waitingStyle
	case insights.StatusIdle:
		return idleStyle
	default:
		return noneStyle
	}
}

// statusColor returns the bare color for a status, used for accent bars.
func statusColor(status insights.StatusType) color.Color {
	switch status {
	case insights.StatusWorking:
		return colSuccess
	case insights.StatusWait:
		return colWarn
	case insights.StatusIdle:
		return colInfo
	default:
		return colFaint
	}
}
