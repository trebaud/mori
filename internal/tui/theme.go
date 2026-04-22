package tui

import (
	"image/color"

	"charm.land/lipgloss/v2"

	"github.com/trebaud/mori/internal/agent"
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

func statusIcon(status agent.StatusType) string {
	switch status {
	case agent.StatusWorking:
		return "●"
	case agent.StatusWait:
		return "◐"
	case agent.StatusIdle:
		return "○"
	default:
		return "·"
	}
}

func statusStyle(status agent.StatusType) lipgloss.Style {
	switch status {
	case agent.StatusWorking:
		return workingStyle
	case agent.StatusWait:
		return waitingStyle
	case agent.StatusIdle:
		return idleStyle
	default:
		return noneStyle
	}
}

// statusColor returns the bare color for a status, used for accent bars.
func statusColor(status agent.StatusType) color.Color {
	switch status {
	case agent.StatusWorking:
		return colWarn
	case agent.StatusWait:
		return colInfo
	case agent.StatusIdle:
		return colSuccess
	default:
		return colFaint
	}
}
