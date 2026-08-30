package tui

import (
	"fmt"
	"strings"
	"time"

	"charm.land/lipgloss/v2"
)

// relativeTime renders a timestamp as a bare "12m" age. The column is
// labelled, so every row repeating "ago" only adds width and noise.
func relativeTime(t time.Time) string {
	if t.IsZero() {
		return "—"
	}
	d := time.Since(t)
	switch {
	case d < time.Minute:
		return "now"
	case d < time.Hour:
		return fmt.Sprintf("%dm", int(d.Minutes()))
	case d < 24*time.Hour:
		return fmt.Sprintf("%dh", int(d.Hours()))
	case d < 365*24*time.Hour:
		return fmt.Sprintf("%dd", int(d.Hours()/24))
	default:
		return fmt.Sprintf("%dy", int(d.Hours()/24/365))
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

// center indents s so it sits in the middle of width columns. The measurement
// ignores styling, so a colored line centers on the text a reader sees.
func center(s string, width int) string {
	return strings.Repeat(" ", max(0, (width-lipgloss.Width(s))/2)) + s
}

// highlightMatch renders s in base, with the first case-insensitive run of
// query drawn in match. The match style underlines as well as colors, so the
// hit is still visible with color stripped or on a monochrome terminal.
//
// s must already be truncated: a hit cut off by truncation simply highlights
// the part that survived.
func highlightMatch(s, query string, base, match lipgloss.Style) string {
	if query == "" {
		return base.Render(s)
	}
	i := strings.Index(strings.ToLower(s), strings.ToLower(query))
	if i < 0 {
		return base.Render(s)
	}
	end := i + len(query)
	return base.Render(s[:i]) + match.Render(s[i:end]) + base.Render(s[end:])
}

// cell renders one column of a list row: gap columns of padding, then text
// truncated to w and padded out to it, aligned right when right is set. A
// zero width hides the column and its gap with it. The padding comes from
// fill so a tinted row stays continuous.
func cell(text string, w, gap int, right bool, st, fill lipgloss.Style) string {
	if w <= 0 {
		return ""
	}
	t := truncate(text, w)
	pad := fill.Render(strings.Repeat(" ", max(0, w-lipgloss.Width(t))))
	out := fill.Render(strings.Repeat(" ", gap))
	if right {
		return out + pad + st.Render(t)
	}
	return out + st.Render(t) + pad
}

// --- Key hints ---

// keyHint is one "[k] label" pair in a footer or a card's action row. prio
// orders what a narrow terminal sheds first: 0 never drops, higher goes first.
type keyHint struct {
	key, label string
	prio       int
}

// hintSeparator is the gap between hints. Wider than the space inside a
// single hint, so a row reads as pairs rather than one run of words.
const hintSeparator = "   "

// hintsWidth is the rendered width of a hint row, measured on the plain text.
func hintsWidth(hints []keyHint) int {
	w := 0
	for i, h := range hints {
		if i > 0 {
			w += len(hintSeparator)
		}
		// key + " " + label
		w += lipgloss.Width(h.key) + lipgloss.Width(h.label) + 1
	}
	return w
}

// renderHints draws a hint row as bare "key label" pairs — the key in the
// terminal's own foreground, its label muted. Weight and color do the work
// brackets used to, which keeps the footer from reading as punctuation.
func renderHints(hints []keyHint) string {
	var b strings.Builder
	for i, h := range hints {
		if i > 0 {
			b.WriteString(hintSeparator)
		}
		b.WriteString(keyStyle.Render(h.key) + " " + mutedStyle.Render(h.label))
	}
	return b.String()
}

// fitHints drops the least important hints until the row fits, so a footer
// assembled from the current state still degrades gracefully on a narrow
// terminal. The prio-0 hints are kept whether they fit or not — a footer
// without [q] quit is worse than one that wraps.
func fitHints(width int, hints []keyHint) []keyHint {
	maxPrio := 0
	for _, h := range hints {
		maxPrio = max(maxPrio, h.prio)
	}
	for cap := maxPrio; cap >= 0; cap-- {
		kept := make([]keyHint, 0, len(hints))
		for _, h := range hints {
			if h.prio <= cap {
				kept = append(kept, h)
			}
		}
		if hintsWidth(kept) <= width || cap == 0 {
			return kept
		}
	}
	return hints
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
		top = borderStyle.Render("╭──") + titleStyle.Render(titleText) +
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
