package tui

import (
	"fmt"
	"strings"
	"time"

	"charm.land/lipgloss/v2"

	"github.com/trebaud/mori/internal/insights"
)

// Layout and refresh timing.
const (
	tickFast      = 2 * time.Second
	tickSlow      = 10 * time.Second
	minViewWidth  = 60
	minViewHeight = 12
	splitMinWidth = 120 // minimum terminal width for the split list+insights layout
)

// Insights floating window dimensions (outer frame including border).
const (
	insightsFloatW = 76
	insightsFloatH = 32
)

// Status-message bucket durations.
const (
	statusInfoDuration  = 2500 * time.Millisecond
	statusErrorDuration = 4 * time.Second
	statusLoadingMax    = 30 * time.Second
)

func contextMaxTokens(model string) int {
	switch insights.ModelTier(model) {
	case "opus":
		return 1_000_000
	default:
		return 200_000
	}
}

func relativeTime(t time.Time) string {
	d := time.Since(t)
	switch {
	case d < time.Minute:
		return "just now"
	case d < time.Hour:
		m := int(d.Minutes())
		if m == 1 {
			return "1m ago"
		}
		return fmt.Sprintf("%dm ago", m)
	case d < 24*time.Hour:
		h := int(d.Hours())
		if h == 1 {
			return "1h ago"
		}
		return fmt.Sprintf("%dh ago", h)
	default:
		days := int(d.Hours() / 24)
		if days == 1 {
			return "1d ago"
		}
		return fmt.Sprintf("%dd ago", days)
	}
}

func wrapText(text string, width int) []string {
	if lipgloss.Width(text) <= width {
		return []string{text}
	}

	var lines []string
	words := strings.Fields(text)
	currentLine := ""

	for _, word := range words {
		projected := lipgloss.Width(word)
		if currentLine != "" {
			projected += lipgloss.Width(currentLine) + 1
		}
		if projected <= width {
			if currentLine != "" {
				currentLine += " "
			}
			currentLine += word
		} else {
			if currentLine != "" {
				lines = append(lines, currentLine)
			}
			currentLine = word
		}
	}
	if currentLine != "" {
		lines = append(lines, currentLine)
	}

	return lines
}

// padBetween places left and right on the same line, separated to fill width.
func padBetween(left, right string, width int) string {
	lw := lipgloss.Width(left)
	rw := lipgloss.Width(right)
	gap := width - lw - rw
	if gap < 1 {
		gap = 1
	}
	return " " + left + strings.Repeat(" ", gap) + right + " "
}
