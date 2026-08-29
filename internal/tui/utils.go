package tui

import (
	"fmt"
	"strings"
	"time"

	"charm.land/lipgloss/v2"
)

// relativeTime renders a timestamp as a short "12m ago" style string.
func relativeTime(t time.Time) string {
	if t.IsZero() {
		return "—"
	}
	d := time.Since(t)
	switch {
	case d < time.Minute:
		return "just now"
	case d < time.Hour:
		return fmt.Sprintf("%dm ago", int(d.Minutes()))
	case d < 24*time.Hour:
		return fmt.Sprintf("%dh ago", int(d.Hours()))
	case d < 365*24*time.Hour:
		return fmt.Sprintf("%dd ago", int(d.Hours()/24))
	default:
		return fmt.Sprintf("%dy ago", int(d.Hours()/24/365))
	}
}

// truncate shortens s to at most w display columns, marking the cut with an
// ellipsis. Truncation is rune-aware so multi-byte names don't get mangled.
func truncate(s string, w int) string {
	if w <= 0 {
		return ""
	}
	if lipgloss.Width(s) <= w {
		return s
	}
	runes := []rune(s)
	for len(runes) > 0 && lipgloss.Width(string(runes))+1 > w {
		runes = runes[:len(runes)-1]
	}
	return string(runes) + "…"
}

// padBetween places left and right on one line of exactly the given width,
// pushing them to opposite edges with a one-column margin on each side. When
// the two cannot both fit, right is dropped — it always carries the secondary
// information. Callers must pass a left that fits on its own.
func padBetween(left, right string, width int) string {
	return padBetweenFill(left, right, width, lipgloss.NewStyle())
}

// padBetweenFill is padBetween with the margins and the gap drawn in fill.
// A tinted row has to paint its own whitespace, or the background shows
// through as a gap between the two columns.
func padBetweenFill(left, right string, width int, fill lipgloss.Style) string {
	inner := width - 2
	lw, rw := lipgloss.Width(left), lipgloss.Width(right)
	if rw > 0 && lw+rw+1 > inner {
		right, rw = "", 0
	}
	gap := inner - lw - rw
	if gap < 1 {
		gap = 1
	}
	return fill.Render(" ") + left + fill.Render(strings.Repeat(" ", gap)) + right + fill.Render(" ")
}

// rule draws the hairline that separates the header from the list.
func rule(width int) string {
	if width < 3 {
		return ""
	}
	return " " + borderStyle.Render(strings.Repeat("─", width-2)) + " "
}

// --- Key hints ---

// keyHint is one "[k] label" pair in a footer or a card's action row.
type keyHint struct{ key, label string }

// hintsWidth is the rendered width of a hint row, measured on the plain text.
func hintsWidth(hints []keyHint) int {
	w := 0
	for i, h := range hints {
		if i > 0 {
			w += 2
		}
		// "[" + key + "]" + " " + label
		w += lipgloss.Width(h.key) + lipgloss.Width(h.label) + 4
	}
	return w
}

// renderHints draws a hint row, keys in the foreground and labels muted so the
// keys are what the eye lands on.
func renderHints(hints []keyHint) string {
	var b strings.Builder
	for i, h := range hints {
		if i > 0 {
			b.WriteString("  ")
		}
		b.WriteString(dimStyle.Render("[") + keyStyle.Render(h.key) + dimStyle.Render("]") +
			" " + mutedStyle.Render(h.label))
	}
	return b.String()
}

// firstHintsThatFit picks the first hint row no wider than width, falling back
// to the last (shortest) candidate. Used to shed hints on narrow terminals.
func firstHintsThatFit(width int, sets ...[]keyHint) []keyHint {
	for _, s := range sets {
		if hintsWidth(s) <= width {
			return s
		}
	}
	return sets[len(sets)-1]
}

// firstThatFits returns the first candidate no wider than width, or the last
// one when none fit. Used to shrink key hints on narrow terminals.
func firstThatFits(width int, candidates ...string) string {
	for _, c := range candidates {
		if lipgloss.Width(c) <= width {
			return c
		}
	}
	return candidates[len(candidates)-1]
}

// wrapText breaks text into lines of at most width display columns.
func wrapText(text string, width int) []string {
	if lipgloss.Width(text) <= width {
		return []string{text}
	}
	var lines []string
	line := ""
	for _, word := range strings.Fields(text) {
		projected := lipgloss.Width(word)
		if line != "" {
			projected += lipgloss.Width(line) + 1
		}
		if projected <= width {
			if line != "" {
				line += " "
			}
			line += word
			continue
		}
		if line != "" {
			lines = append(lines, line)
		}
		line = word
	}
	if line != "" {
		lines = append(lines, line)
	}
	return lines
}

// renderFrame draws a rounded border around content, with an optional title
// set into the top edge.
func renderFrame(content string, width int, title string) string {
	innerW := width - 2
	if innerW < 4 {
		innerW = 4
	}

	top := borderStyle.Render("╭" + strings.Repeat("─", innerW) + "╮")
	if title != "" {
		titleText := " " + title + " "
		tail := innerW - 2 - lipgloss.Width(titleText)
		if tail < 1 {
			tail = 1
		}
		top = borderStyle.Render("╭──") + headingStyle.Render(titleText) +
			borderStyle.Render(strings.Repeat("─", tail)+"╮")
	}

	var out strings.Builder
	out.WriteString(top + "\n")
	for _, ln := range strings.Split(content, "\n") {
		pad := innerW - lipgloss.Width(ln)
		if pad < 0 {
			pad = 0
		}
		out.WriteString(borderStyle.Render("│") + ln + strings.Repeat(" ", pad) + borderStyle.Render("│") + "\n")
	}
	out.WriteString(borderStyle.Render("╰" + strings.Repeat("─", innerW) + "╯"))
	return out.String()
}

// dimBackground softens the base view by re-applying the SGR faint attribute
// after every reset, so every styled span ends up dimmed behind an overlay.
func dimBackground(s string) string {
	const faintOn = "\x1b[2m"
	const reset = "\x1b[0m"
	s = strings.ReplaceAll(s, reset, reset+faintOn)
	lines := strings.Split(s, "\n")
	for i, ln := range lines {
		if ln == "" {
			continue
		}
		lines[i] = faintOn + ln + reset
	}
	return strings.Join(lines, "\n")
}

// overlay composites a floating card centered over the dimmed base view.
func overlay(base, card string, width int) string {
	cardW := lipgloss.Width(card)
	cardH := lipgloss.Height(card)
	baseH := lipgloss.Height(base)

	x := max(0, (width-cardW)/2)
	y := max(0, (baseH-cardH)/2)
	if y+cardH > baseH {
		y = max(0, baseH-cardH)
	}

	return lipgloss.NewCompositor(
		lipgloss.NewLayer(dimBackground(base)).Z(0),
		lipgloss.NewLayer(card).X(x).Y(y).Z(1),
	).Render()
}
