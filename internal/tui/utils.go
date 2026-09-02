package tui

import (
	"fmt"
	"strings"
	"time"

	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
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
	inner := width - 2
	lw, rw := lipgloss.Width(left), lipgloss.Width(right)
	if rw > 0 && lw+rw+1 > inner {
		right, rw = "", 0
	}
	gap := inner - lw - rw
	if gap < 1 {
		gap = 1
	}
	return " " + left + strings.Repeat(" ", gap) + right + " "
}

// padRight pads s out to width display columns, so every line of the list is
// the same length whatever columns it ended up carrying.
func padRight(s string, width int) string {
	if pad := width - lipgloss.Width(s); pad > 0 {
		return s + strings.Repeat(" ", pad)
	}
	return s
}

// center indents s so it sits in the middle of width columns. The measurement
// ignores styling, so a colored line centers on the text a reader sees.
func center(s string, width int) string {
	return strings.Repeat(" ", max(0, (width-lipgloss.Width(s))/2)) + s
}

// --- Fuzzy matching ---
//
// A filter is a guess at a name half-remembered, so `parse` should find
// `feat/parser` and `fp` should find it too. The matcher takes the query's
// runes in order but not necessarily together, and scores what it finds so
// the list can put the likeliest first.

// Scoring weights. A run of adjacent matches is what makes a hit feel like
// the right one, so it counts for far more than a scattering of the same
// letters; a hit that starts a word counts for more than one in the middle;
// and every rune skipped along the way costs.
const (
	scoreAdjacent = 10
	scoreBoundary = 8
	scoreLeading  = 4
	penaltyGap    = 1
)

// isBoundary reports whether the rune at index i in runes starts a word.
// Branch names are punctuated with these, which is what makes `fp` a
// reasonable way to ask for `feat/parser`.
func isBoundary(runes []rune, i int) bool {
	if i == 0 {
		return true
	}
	switch runes[i-1] {
	case '/', '-', '_', '.', ' ':
		return true
	}
	return false
}

// fuzzyMatch finds query's runes in s, in order, case-insensitively. It
// returns the index of every matched rune and a score, or ok=false when the
// query is not there at all. An empty query matches everything, scoring zero.
//
// The scan is greedy — the first place each rune fits — which can miss the
// prettiest alignment of a query against a long name, but it is one pass and
// this runs on every keystroke against every worktree.
func fuzzyMatch(s, query string) (positions []int, score int, ok bool) {
	if query == "" {
		return nil, 0, true
	}
	hay, needle := []rune(strings.ToLower(s)), []rune(strings.ToLower(query))

	prev := -2
	q := 0
	for i := 0; i < len(hay) && q < len(needle); i++ {
		if hay[i] != needle[q] {
			continue
		}
		switch {
		case i == prev+1:
			score += scoreAdjacent
		case isBoundary(hay, i):
			score += scoreBoundary
		}
		if i < 4 {
			score += scoreLeading - i
		}
		score -= penaltyGap * max(0, i-prev-1)
		positions = append(positions, i)
		prev = i
		q++
	}
	if q < len(needle) {
		return nil, 0, false
	}
	return positions, score, true
}

// highlightRunes renders s in base with the runes at the given indices drawn
// in match. The match style underlines as well as colors, so the hit is still
// visible with color stripped or on a monochrome terminal.
//
// s must already be truncated, and the indices are into it: a hit cut off by
// truncation simply highlights the part that survived.
func highlightRunes(s string, positions []int, base, match lipgloss.Style) string {
	if len(positions) == 0 {
		return base.Render(s)
	}
	hit := make(map[int]bool, len(positions))
	for _, i := range positions {
		hit[i] = true
	}

	// Runs, not runes: one style span per stretch keeps the escape sequences
	// down and lets a multi-rune hit underline as one mark.
	var b strings.Builder
	runes := []rune(s)
	for i := 0; i < len(runes); {
		j := i
		for j < len(runes) && hit[j] == hit[i] {
			j++
		}
		st := base
		if hit[i] {
			st = match
		}
		b.WriteString(st.Render(string(runes[i:j])))
		i = j
	}
	return b.String()
}

// highlightQuery is highlightRunes over a fresh match, for text that was not
// what the filter was scored against — a path under a query that matched the
// branch, say, where there may be nothing to mark at all.
func highlightQuery(s, query string, base, match lipgloss.Style) string {
	positions, _, ok := fuzzyMatch(s, query)
	if !ok {
		return base.Render(s)
	}
	return highlightRunes(s, positions, base, match)
}

// cell renders one column of a list row: gap columns of padding, then text
// truncated to w and padded out to it, aligned right when right is set. A
// zero width hides the column and its gap with it.
func cell(text string, w, gap int, right bool, st lipgloss.Style) string {
	if w <= 0 {
		return ""
	}
	t := truncate(text, w)
	pad := strings.Repeat(" ", max(0, w-lipgloss.Width(t)))
	out := strings.Repeat(" ", gap)
	if right {
		return out + pad + st.Render(t)
	}
	return out + st.Render(t) + pad
}

// styledCell is cell for content that carries styles of its own: text is what
// the column is measured and padded on, rendered is what is drawn. It does
// not truncate — the caller must have measured the column wide enough.
func styledCell(text, rendered string, w, gap int, right bool) string {
	if w <= 0 {
		return ""
	}
	pad := strings.Repeat(" ", max(0, w-lipgloss.Width(text)))
	out := strings.Repeat(" ", gap)
	if right {
		return out + pad + rendered
	}
	return out + rendered + pad
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
		// The frame is the last word on width: a line wider than the card is
		// clipped here rather than allowed to run past the right border. Cut
		// with ansi.Truncate, which counts display columns and leaves the
		// escape sequences around them intact.
		if lipgloss.Width(ln) > innerW {
			ln = ansi.Truncate(ln, innerW, "…")
		}
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
