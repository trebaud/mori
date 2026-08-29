package tui

import (
	"fmt"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"github.com/trebaud/mori/internal"
)

// View is the Elm view function — model in, string out. The base layer is
// always the worktree list (or the help screen); prompts float on top of it.
func (m model) View() tea.View {
	width := m.width
	if width == 0 {
		width = 100
	}

	if m.width > 0 && (m.width < minViewWidth || m.height < minViewHeight) {
		v := tea.NewView(m.viewTooSmall())
		v.AltScreen = true
		return v
	}

	// Past maxContentWidth a card's branch name and its age drift to opposite
	// ends of the screen with a void between them, so the layout stops growing
	// and stays left-aligned in the terminal.
	width = min(width, maxContentWidth)

	base := m.viewList(width)
	if m.showHelp {
		base = m.viewHelp(width)
	}

	out := base
	switch m.mode {
	case modeCreate:
		out = overlay(base, m.renderCreateCard(width), width)
	case modeCreating:
		out = overlay(base, m.renderCreatingCard(width), width)
	case modeConfirmDelete:
		out = overlay(base, m.renderDeleteCard(width), width)
	}

	v := tea.NewView(out)
	v.AltScreen = true
	return v
}

func (m model) viewTooSmall() string {
	msg := fmt.Sprintf("terminal too small (%d×%d) — need at least %d×%d",
		m.width, m.height, minViewWidth, minViewHeight)
	if m.height < 2 {
		return msg
	}
	return strings.Repeat("\n", (m.height-1)/2) + " " + mutedStyle.Render(msg)
}

// --- Base layout ---

func (m model) viewList(width int) string {
	lines := []string{"", m.renderTopBar(width), rule(width)}
	lines = append(lines, m.renderCards(width)...)
	lines = append(lines, m.renderStatusLine(), m.renderFooter(width))
	return strings.Join(lines, "\n")
}

func (m model) renderTopBar(width int) string {
	const brand = "◆ MORI"
	// The brand, two spaces, and the line's own margins are fixed overhead.
	label := truncate(m.repoLabel, max(0, width-len(brand)-4))
	left := titleStyle.Render(brand) + "  " + mutedStyle.Render(label)

	n := len(m.filtered)
	summary := fmt.Sprintf("%d worktrees", n)
	if n == 1 {
		summary = "1 worktree"
	}
	right := mutedStyle.Render(summary)
	if d := m.dirtyCount(); d > 0 {
		right += mutedStyle.Render(" · ") + dirtyStyle.Render(fmt.Sprintf("%d dirty", d))
	}

	return padBetween(left, right, width)
}

// renderCards renders the scrolling viewport of worktree cards, padded to a
// fixed height so the footer never shifts as worktrees come and go.
func (m model) renderCards(width int) []string {
	height := m.listHeight()
	var lines []string

	if len(m.filtered) == 0 {
		lines = append(lines, m.renderEmpty(height)...)
	} else {
		end := min(m.scrollOffset+m.visibleCards(), len(m.filtered))
		for i := m.scrollOffset; i < end; i++ {
			lines = append(lines, m.renderCard(i, width)...)
		}
		// Reuse the last card's spacer row for the scroll hint so the
		// viewport height stays exactly visibleCards() cards tall.
		if hint := scrollHint(m.scrollOffset, len(m.filtered)-end); hint != "" {
			lines[len(lines)-1] = "  " + dimStyle.Render(hint)
		}
	}

	for len(lines) < height {
		lines = append(lines, "")
	}
	return lines[:height]
}

// scrollHint describes off-screen worktrees above and below the viewport.
func scrollHint(above, below int) string {
	var parts []string
	if above > 0 {
		parts = append(parts, fmt.Sprintf("↑ %d above", above))
	}
	if below > 0 {
		parts = append(parts, fmt.Sprintf("↓ %d more", below))
	}
	return strings.Join(parts, " · ")
}

// renderEmpty draws the empty state a third of the way down the list area,
// where the eye already is, rather than pinned under the header.
func (m model) renderEmpty(height int) []string {
	msg, hint := "no worktrees yet", []keyHint{{key: "n", label: "create one"}}
	if m.textInput.Value() != "" {
		msg = "no worktrees match “" + m.textInput.Value() + "”"
		hint = []keyHint{{key: "esc", label: "clear the filter"}}
	}

	lines := make([]string, 0, height)
	for i := 0; i < max(0, (height-2)/3); i++ {
		lines = append(lines, "")
	}
	return append(lines, "  "+mutedStyle.Render(msg), "", "  "+renderHints(hint))
}

// cardStyles is the style set one card draws with. The selected card lays a
// faint tint under its whole row, and a background only covers what a style
// actually paints — so every span, separator and run of padding on the row has
// to come from this set, or the tint comes out full of holes.
type cardStyles struct {
	name, path, meta, head, dirty, clean, fill lipgloss.Style
	nameMatch, pathMatch                       lipgloss.Style
}

func newCardStyles(selected bool) cardStyles {
	cs := cardStyles{
		name:  textStyle,
		path:  mutedStyle,
		meta:  mutedStyle,
		head:  dimStyle,
		dirty: dirtyStyle,
		clean: cleanStyle,
		fill:  lipgloss.NewStyle(),
	}
	cs.nameMatch, cs.pathMatch = markMatch(cs.name), markMatch(cs.path)
	if !selected {
		return cs
	}

	tint := func(st lipgloss.Style) lipgloss.Style { return st.Background(colRowBg) }
	cs.name = tint(cs.name.Bold(true))
	cs.path = tint(cs.path)
	cs.meta = tint(cs.meta)
	cs.head = tint(cs.head)
	cs.dirty = tint(cs.dirty)
	cs.clean = tint(cs.clean)
	cs.nameMatch = tint(markMatch(cs.name))
	cs.pathMatch = tint(cs.pathMatch)
	cs.fill = rowStyle
	return cs
}

// renderCard draws one worktree as three rows plus a trailing blank row:
//
//	▌ feat/parser                              12m ago
//	│   ~/.mori/worktrees/mori/feat-parser     a1b2c3d
//	│   ● 3 files changed · 2 ahead
func (m model) renderCard(idx, width int) []string {
	wt := m.worktrees[m.filtered[idx]]
	selected := idx == m.cursor
	cs := newCardStyles(selected)

	// The accent bar carries the selection down all three rows; the tint
	// behind it is what makes the card read as one block.
	bar, cont := cs.fill.Render("  "), cs.fill.Render("  ")
	if selected {
		bar = selectedStyle.Background(colRowBg).Render("▌") + cs.fill.Render(" ")
		cont = selectedStyle.Background(colRowBg).Render("│") + cs.fill.Render(" ")
	}

	label := wt.Label()
	if m.archived[wt.Branch] {
		label = "◌ " + label
	}

	// Every row is padded to the full width so the tint runs edge to edge, and
	// so the age and HEAD line up down the list: the bar costs 2 columns on row
	// 1 and 3 on rows 2 and 3, and padBetween adds a margin on each side.
	query := strings.TrimSpace(m.textInput.Value())

	age := relativeTime(wt.LastCommit)
	labelW := width - 4 - lipgloss.Width(age) - 1
	first := bar + padBetweenFill(
		highlightMatch(truncate(label, labelW), query, cs.name, cs.nameMatch),
		cs.meta.Render(age), width-2, cs.fill)

	pathW := width - 5 - lipgloss.Width(wt.Head) - 1
	second := cont + cs.fill.Render(" ") + padBetweenFill(
		highlightMatch(truncate(wt.DisplayPath, pathW), query, cs.path, cs.pathMatch),
		cs.head.Render(wt.Head), width-3, cs.fill)

	third := cont + cs.fill.Render(" ") + padBetweenFill(
		m.renderGitState(wt, cs), "", width-3, cs.fill)

	return []string{first, second, third, ""}
}

// renderGitState is the "● 3 files changed · 2 ahead" line of a card.
func (m model) renderGitState(wt internal.Worktree, cs cardStyles) string {
	var parts []string

	if wt.Dirty > 0 {
		noun := "files"
		if wt.Dirty == 1 {
			noun = "file"
		}
		parts = append(parts, cs.dirty.Render(fmt.Sprintf("● %d %s changed", wt.Dirty, noun)))
	} else {
		parts = append(parts, cs.clean.Render("○ clean"))
	}

	switch {
	case wt.Ahead > 0 && wt.Behind > 0:
		parts = append(parts, cs.meta.Render(fmt.Sprintf("%d ahead", wt.Ahead)),
			cs.meta.Render(fmt.Sprintf("%d behind", wt.Behind)))
	case wt.Ahead > 0:
		parts = append(parts, cs.meta.Render(fmt.Sprintf("%d ahead", wt.Ahead)))
	case wt.Behind > 0:
		parts = append(parts, cs.meta.Render(fmt.Sprintf("%d behind", wt.Behind)))
	}

	return strings.Join(parts, cs.meta.Render(" · "))
}

// renderStatusLine is the single row between the list and the footer. It holds
// the search prompt while filtering, and transient status messages otherwise.
func (m model) renderStatusLine() string {
	if m.mode == modeSearch {
		return " " + titleStyle.Render("/") + " " + m.textInput.View()
	}
	if m.statusMsg == nil || time.Now().After(m.statusMsg.expires) {
		return ""
	}
	switch {
	case m.statusMsg.isLoading:
		return " " + mutedStyle.Render("⋯ "+m.statusMsg.text)
	case m.statusMsg.isError:
		return " " + errorStyle.Render("✗ "+m.statusMsg.text)
	default:
		return " " + successStyle.Render("✓ "+m.statusMsg.text)
	}
}

// footerHints is the footer's key row for the current state. It lists only
// what is actionable right now: no [enter] cd with nothing to cd into, and
// [esc] clear filter only while a filter is narrowing the list.
func (m model) footerHints() []keyHint {
	hints := []keyHint{}
	if len(m.filtered) > 0 {
		hints = append(hints, keyHint{key: "enter", label: "cd"})
	}
	hints = append(hints, keyHint{key: "n", label: "new"})
	if len(m.filtered) > 0 {
		hints = append(hints, keyHint{key: "d", label: "delete", prio: 1})
		if wt := m.selectedWorktree(); wt != nil && m.archived[wt.Branch] {
			hints = append(hints, keyHint{key: "x", label: "unarchive", prio: 3})
		}
	}
	if m.textInput.Value() != "" {
		hints = append(hints, keyHint{key: "esc", label: "clear filter"})
	} else if len(m.filtered) > 0 {
		hints = append(hints, keyHint{key: "/", label: "filter", prio: 2})
	}
	return append(hints, keyHint{key: "?", label: "help"}, keyHint{key: "q", label: "quit"})
}

func (m model) renderFooter(width int) string {
	if m.mode == modeSearch {
		return " " + renderHints(fitHints(width-2, []keyHint{
			{key: "enter", label: "apply"},
			{key: "esc", label: "clear"},
			{key: "↑/↓", label: "navigate", prio: 1},
		}))
	}

	left := renderHints(fitHints(width-2, m.footerHints()))

	var indicators []string
	if m.sortMode != internal.SortDefault {
		indicators = append(indicators, mutedStyle.Render("sort ")+textStyle.Render(m.sortMode.String()))
	}
	if m.showArchive {
		indicators = append(indicators, mutedStyle.Render("archived"))
	}
	return padBetween(left, strings.Join(indicators, mutedStyle.Render("  ·  ")), width)
}

// --- Floating cards ---

// createCardMaxWidth caps the "new worktree" card; the text input is sized
// against it in syncInputWidth.
const createCardMaxWidth = 64

// cardWidth clamps an overlay card to a comfortable reading width.
func cardWidth(width, maxW int) int {
	w := width - 8
	if w > maxW {
		w = maxW
	}
	if w < 32 {
		w = 32
	}
	return w
}

func (m model) renderCreateCard(width int) string {
	w := cardWidth(width, createCardMaxWidth)

	var c strings.Builder
	c.WriteString("\n")
	c.WriteString(" " + titleStyle.Render("›") + "  " + m.textInput.View() + "\n\n")
	c.WriteString(" " + dimStyle.Render("branches off ") + mutedStyle.Render(m.baseBranch) + "\n")
	c.WriteString(" " + dimStyle.Render("leave empty for a random name") + "\n\n")
	c.WriteString(" " + renderHints([]keyHint{{key: "enter", label: "create"}, {key: "esc", label: "cancel"}}) + "\n")

	return renderFrame(c.String(), w, "new worktree")
}

// spinnerFrames are braille dots used to animate a running step.
var spinnerFrames = []string{"⠋", "⠙", "⠹", "⠸", "⠼", "⠴", "⠦", "⠧", "⠇", "⠏"}

func (m model) renderCreatingCard(width int) string {
	w := cardWidth(width, 88)
	cmdW := w - 6

	var c strings.Builder
	c.WriteString("\n")
	c.WriteString(" " + mutedStyle.Render("creating ") + textStyle.Render(m.creatingBranch) + "\n\n")

	spin := spinnerFrames[m.animFrame%len(spinnerFrames)]
	for _, step := range m.creatingSteps {
		var glyph string
		nameStyle := mutedStyle
		switch step.state {
		case stepRunning:
			glyph = successStyle.Render(spin)
			nameStyle = textStyle.Bold(true)
		case stepSucceeded:
			glyph = successStyle.Render("✓")
		case stepFailed:
			glyph = errorStyle.Render("✗")
			nameStyle = errorStyle
		default:
			glyph = mutedStyle.Render("○")
		}
		c.WriteString(" " + glyph + " " + nameStyle.Render(step.name) + "\n")
		c.WriteString("   " + dimStyle.Render(truncate(step.cmd, cmdW)) + "\n")
	}
	c.WriteString("\n")

	return renderFrame(c.String(), w, "new worktree")
}

func (m model) renderDeleteCard(width int) string {
	w := cardWidth(width, 64)
	if m.deleteTarget >= len(m.filtered) {
		return renderFrame("\n "+mutedStyle.Render("nothing to delete")+"\n", w, "delete worktree")
	}
	wt := m.worktrees[m.filtered[m.deleteTarget]]

	var c strings.Builder
	c.WriteString("\n")
	c.WriteString(" " + textStyle.Render("Remove ") + selectedStyle.Render(wt.Label()) + textStyle.Render("?") + "\n")
	c.WriteString(" " + mutedStyle.Render(truncate(wt.DisplayPath, w-4)) + "\n\n")

	if wt.Dirty > 0 {
		noun := "files"
		if wt.Dirty == 1 {
			noun = "file"
		}
		for _, ln := range wrapText(fmt.Sprintf("⚠ %d uncommitted %s will be lost", wt.Dirty, noun), w-4) {
			c.WriteString(" " + warnStyle.Render(ln) + "\n")
		}
		c.WriteString("\n")
	}
	if wt.Ahead > 0 {
		for _, ln := range wrapText(fmt.Sprintf("⚠ %d commit(s) not on %s", wt.Ahead, m.baseBranch), w-4) {
			c.WriteString(" " + warnStyle.Render(ln) + "\n")
		}
		c.WriteString("\n")
	}

	c.WriteString(" " + renderHints([]keyHint{{key: "y", label: "delete"}, {key: "esc", label: "cancel"}}) + "\n")

	return renderFrame(c.String(), w, "delete worktree")
}

// --- Help ---

var helpSections = []struct {
	title string
	keys  [][2]string
}{
	{"navigation", [][2]string{
		{"j/k, ↑/↓", "move between worktrees"},
		{"g / G", "jump to first / last"},
		{"ctrl+d/u", "page down / up"},
		{"esc", "back out of filter, archive, help"},
		{"q, ctrl+c", "quit"},
	}},
	{"worktrees", [][2]string{
		{"enter, o", "pick worktree — prints its path on exit"},
		{"n", "create a worktree"},
		{"d", "delete the selected worktree"},
		{"y", "yank the path to the clipboard"},
		{"r", "refresh git state now"},
	}},
	{"view", [][2]string{
		{"/", "filter by branch or path"},
		{"s", "cycle sort (default, recent, name)"},
		{"x", "archive / unarchive"},
		{"X", "show or hide archived worktrees"},
		{"?", "toggle this help"},
	}},
}

func (m model) viewHelp(width int) string {
	w := min(64, width-4)

	var c strings.Builder
	for i, section := range helpSections {
		if i > 0 {
			c.WriteString("\n")
		}
		c.WriteString(" " + headingStyle.Render(section.title) + "\n")
		for _, kv := range section.keys {
			key := selectedStyle.Render(kv[0])
			pad := max(1, 14-lipgloss.Width(key))
			c.WriteString("  " + key + strings.Repeat(" ", pad) + mutedStyle.Render(kv[1]) + "\n")
		}
	}

	return strings.Join([]string{
		"",
		m.renderTopBar(width),
		"",
		renderFrame(c.String(), w, "keybindings"),
		"",
		" " + renderHints([]keyHint{{key: "?", label: "close"}, {key: "q", label: "quit"}}),
	}, "\n")
}
