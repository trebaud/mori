package tui

import (
	"image/color"

	"charm.land/lipgloss/v2"

	"github.com/trebaud/mori/internal/insights"
)

// Palette — one accent + functional colors; everything else is grayscale.
var (
	colAccent  = lipgloss.Color("205")
	colSuccess = lipgloss.Color("78")
	colWarn    = lipgloss.Color("214")
	colDanger  = lipgloss.Color("203")
	colInfo    = lipgloss.Color("117")

	colText   = lipgloss.Color("252")
	colMuted  = lipgloss.Color("245")
	colDim    = lipgloss.Color("240")
	colFaint  = lipgloss.Color("238")
	colRowBg  = lipgloss.Color("237")
	colBorder = lipgloss.Color("238")
)

var (
	titleStyle    = lipgloss.NewStyle().Foreground(colAccent).Bold(true)
	textStyle     = lipgloss.NewStyle().Foreground(colText)
	mutedStyle    = lipgloss.NewStyle().Foreground(colMuted)
	dimStyle      = lipgloss.NewStyle().Foreground(colDim)
	headingStyle  = lipgloss.NewStyle().Foreground(colMuted).Bold(true)
	selectedStyle = lipgloss.NewStyle().Foreground(colAccent).Bold(true)

	workingStyle = lipgloss.NewStyle().Foreground(colWarn).Bold(true)
	waitingStyle = lipgloss.NewStyle().Foreground(colInfo).Bold(true)
	idleStyle    = lipgloss.NewStyle().Foreground(colSuccess)
	noneStyle    = lipgloss.NewStyle().Foreground(colDim)

	errorStyle   = lipgloss.NewStyle().Foreground(colDanger)
	successStyle = lipgloss.NewStyle().Foreground(colSuccess)

	barHighStyle = lipgloss.NewStyle().Foreground(colDanger)
	barMedStyle  = lipgloss.NewStyle().Foreground(colWarn)
	barLowStyle  = lipgloss.NewStyle().Foreground(colSuccess)

	borderStyle = lipgloss.NewStyle().Foreground(colBorder)
)

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
		return colWarn
	case insights.StatusWait:
		return colInfo
	case insights.StatusIdle:
		return colSuccess
	default:
		return colFaint
	}
}
