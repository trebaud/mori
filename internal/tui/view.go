package tui

import (
	"fmt"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"github.com/trebaud/mori/v2/internal"
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
	lines = append(lines, m.renderRows(width)...)
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

// renderRows renders the scrolling viewport of worktree rows, padded to a
// fixed height so the footer never shifts as worktrees come and go.
func (m model) renderRows(width int) []string {
	height := m.listHeight()
	var lines []string

	if len(m.filtered) == 0 {
		lines = append(lines, m.renderEmpty(height)...)
	} else {
		cols := m.rowColumns(width)
		end := min(m.scrollOffset+m.visibleRows(), len(m.filtered))
		for i := m.scrollOffset; i < end; i++ {
			lines = append(lines, m.renderRow(i, width, cols))
		}
		if hint := scrollHint(m.scrollOffset, len(m.filtered)-end); hint != "" {
			lines = append(lines, "  "+dimStyle.Render(hint))
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

// --- Rows ---

// rowPalette is the style set one row draws with. The selected row lays a
// faint tint under its full width, and a background only covers what a style
// actually paints — so every span and every run of padding on the row has to
// come from this set, or the tint comes out full of holes.
type rowPalette struct {
	name, meta, head, dirty, clean, fill lipgloss.Style
	nameMatch                            lipgloss.Style
}

func newRowPalette(selected bool) rowPalette {
	p := rowPalette{
		name:  textStyle,
		meta:  mutedStyle,
		head:  dimStyle,
		dirty: dirtyStyle,
		clean: cleanStyle,
		fill:  lipgloss.NewStyle(),
	}
	p.nameMatch = markMatch(p.name)
	if !selected {
		return p
	}

	tint := func(st lipgloss.Style) lipgloss.Style { return st.Background(colRowBg) }
	p.name = tint(p.name.Bold(true))
	p.meta = tint(p.meta)
	p.head = tint(p.head)
	p.dirty = tint(p.dirty)
	p.clean = tint(p.clean)
	p.nameMatch = tint(markMatch(p.name))
	p.fill = rowStyle
	return p
}

// rowColumns holds the width of each column. A zero width hides the column:
// a narrow terminal sheds HEAD first, then the ahead/behind counts, then the
// age, rather than squeezing the branch name down to nothing.
type rowColumns struct {
	branch, state, sync, age, head int
	slack                          int // spare columns, held before the age
}

// minBranchWidth is the narrowest a branch column may get before the row
// starts dropping columns to its right.
const minBranchWidth = 18

// fixedWidth is what every column except the branch costs, gaps included.
func (c rowColumns) fixedWidth() int {
	w := 0
	for _, col := range []struct{ gap, width int }{
		{1, c.state}, {1, c.sync}, {2, c.age}, {2, c.head},
	} {
		if col.width > 0 {
			w += col.gap + col.width
		}
	}
	return w
}

// rowColumns measures the columns against every worktree on show, not just
// the visible slice, so they stay put while the list scrolls.
//
// The branch takes only the width it needs. Whatever is left over is held as
// slack before the age, which pins the age and HEAD to the right edge — so a
// wide terminal reads as name-on-the-left, time-on-the-right rather than one
// column stretched across a void.
func (m model) rowColumns(width int) rowColumns {
	c := rowColumns{}
	natural := 0
	for _, i := range m.filtered {
		wt := m.worktrees[i]
		natural = max(natural, lipgloss.Width(m.rowLabel(wt)))
		c.state = max(c.state, lipgloss.Width(gitStateText(wt)))
		c.sync = max(c.sync, lipgloss.Width(syncText(wt)))
		c.age = max(c.age, lipgloss.Width(relativeTime(wt.LastCommit)))
		c.head = max(c.head, lipgloss.Width(wt.Head))
	}

	// The bar costs 2 columns and the row keeps a 1-column right margin.
	avail := func() int { return width - 3 - c.fixedWidth() }
	for _, drop := range []*int{&c.head, &c.sync, &c.age} {
		if avail() >= minBranchWidth {
			break
		}
		*drop = 0
	}
	c.branch = max(6, min(natural, avail()))
	c.slack = max(0, avail()-c.branch)
	return c
}

// rowLabel is what the branch column shows for a worktree.
func (m model) rowLabel(wt internal.Worktree) string {
	if m.archived[wt.Branch] {
		return "◌ " + wt.Label()
	}
	return wt.Label()
}

// gitStateText and syncText are the plain forms of the two git columns. The
// columns are measured on these, then rendered from them.
func gitStateText(wt internal.Worktree) string {
	if wt.Dirty > 0 {
		return fmt.Sprintf("● %d changed", wt.Dirty)
	}
	return "○ clean"
}

func syncText(wt internal.Worktree) string {
	switch {
	case wt.Ahead > 0 && wt.Behind > 0:
		return fmt.Sprintf("↑%d ↓%d", wt.Ahead, wt.Behind)
	case wt.Ahead > 0:
		return fmt.Sprintf("↑%d", wt.Ahead)
	case wt.Behind > 0:
		return fmt.Sprintf("↓%d", wt.Behind)
	}
	return ""
}

// renderRow draws one worktree as a single row of aligned columns:
//
//	▌ feat/parser         ● 3 changed ↑2     12m ago  a1b2c3d
func (m model) renderRow(idx, width int, c rowColumns) string {
	wt := m.worktrees[m.filtered[idx]]
	selected := idx == m.cursor
	p := newRowPalette(selected)

	// The accent bar marks the selection; the tint behind it carries across
	// the row so the whole line reads as one.
	bar := p.fill.Render("  ")
	if selected {
		bar = selectedStyle.Background(colRowBg).Render("▌") + p.fill.Render(" ")
	}

	label := truncate(m.rowLabel(wt), c.branch)
	row := bar + highlightMatch(label, strings.TrimSpace(m.textInput.Value()), p.name, p.nameMatch) +
		p.fill.Render(strings.Repeat(" ", c.branch-lipgloss.Width(label)))

	stateStyle := p.clean
	if wt.Dirty > 0 {
		stateStyle = p.dirty
	}
	row += cell(gitStateText(wt), c.state, 1, false, stateStyle, p.fill)
	row += cell(syncText(wt), c.sync, 1, false, p.meta, p.fill)
	row += cell(relativeTime(wt.LastCommit), c.age, 2+c.slack, true, p.meta, p.fill)
	row += cell(wt.Head, c.head, 2, false, p.head, p.fill)

	// Pad to the full width so the tint runs edge to edge. Measuring the row
	// rather than adding the columns up keeps this right whichever columns
	// the width shed.
	if pad := width - lipgloss.Width(row); pad > 0 {
		row += p.fill.Render(strings.Repeat(" ", pad))
	}
	return row
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
