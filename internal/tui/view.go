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
	// A blank line under the brand, then the column labels: the labels are
	// the only separator the list needs, and they earn their row by naming
	// what each column of glyphs and numbers means.
	header := ""
	if len(m.filtered) > 0 {
		header = m.renderColumnHeader(m.rowColumns(width))
	}

	lines := []string{"", m.renderTopBar(width), "", header}
	lines = append(lines, m.renderRows(width)...)
	lines = append(lines, m.renderStatusLine(), m.renderFooter(width))
	return strings.Join(lines, "\n")
}

// brand is the wordmark. The asterism is three marks in a clearing — a very
// small forest, which is what "mori" means and what a set of worktrees is.
const brand = "⁂ mori"

func (m model) renderTopBar(width int) string {
	// The brand, two spaces, and the line's own margins are fixed overhead.
	label := truncate(m.repoLabel, max(0, width-lipgloss.Width(brand)-4))
	left := brandStyle.Render(brand) + "  " + mutedStyle.Render(label)

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
		lines = append(lines, m.renderEmpty(width, height)...)
	} else {
		cols := m.rowColumns(width)
		end := min(m.scrollOffset+m.visibleRows(), len(m.filtered))
		for i := m.scrollOffset; i < end; i++ {
			lines = append(lines, m.renderRow(i, width, cols))
		}
		if hint := scrollHint(m.scrollOffset, len(m.filtered)-end); hint != "" {
			lines = append(lines, "  "+columnStyle.Render(hint))
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
		parts = append(parts, fmt.Sprintf("↓ %d below", below))
	}
	return strings.Join(parts, " · ")
}

// renderEmpty draws the empty state centered across the list area and a third
// of the way down it, where the eye already is rather than pinned under the
// header. Nothing to list means nothing else to align to, so this is the one
// place the layout leaves its columns behind.
func (m model) renderEmpty(width, height int) []string {
	mark := brandStyle.Render("⁂")
	msg, hint := "this clearing is empty", []keyHint{{key: "n", label: "plant a worktree"}}
	if m.textInput.Value() != "" {
		mark = mutedStyle.Render("∅")
		msg = "nothing grows under “" + truncate(m.textInput.Value(), max(8, width-24)) + "”"
		hint = []keyHint{{key: "esc", label: "clear the filter"}}
	}

	block := []string{mark, "", mutedStyle.Render(msg), "", renderHints(hint)}

	lines := make([]string, 0, height)
	for i := 0; i < max(0, (height-len(block))/3); i++ {
		lines = append(lines, "")
	}
	for _, ln := range block {
		lines = append(lines, center(ln, width))
	}
	return lines
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
	slack                          int // spare columns, held after the branch
}

// minBranchWidth is the narrowest a branch column may get before the row
// starts dropping columns to its right.
const minBranchWidth = 18

// Column labels. They are measured into the column widths alongside the
// content, so a label never gets truncated by the rows beneath it.
const (
	labelBranch  = "branch"
	labelChanges = "changes"
	labelSync    = "sync"
	labelAge     = "age"
	labelHead    = "head"
)

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
// slack directly after it, which pushes every other column against the right
// edge as one block — so a wide terminal reads as name on the left, git state
// on the right, rather than four columns adrift in the middle.
func (m model) rowColumns(width int) rowColumns {
	c := rowColumns{
		state: len(labelChanges),
		sync:  len(labelSync),
		age:   len(labelAge),
		head:  len(labelHead),
	}
	natural := len(labelBranch)
	for _, i := range m.filtered {
		wt := m.worktrees[i]
		natural = max(natural, lipgloss.Width(m.rowLabel(wt)))
		c.state = max(c.state, lipgloss.Width(gitStateText(wt)))
		c.sync = max(c.sync, lipgloss.Width(syncText(wt)))
		c.age = max(c.age, lipgloss.Width(relativeTime(wt.Age())))
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

// renderColumnHeader labels the grid. It is built from the same cells as a
// worktree row — same widths, same gaps, same alignment — so the labels sit
// exactly over the values they name.
func (m model) renderColumnHeader(c rowColumns) string {
	fill := lipgloss.NewStyle()
	row := cell(labelBranch, c.branch, 2, false, columnStyle, fill)
	row += cell(labelChanges, c.state, 1+c.slack, true, columnStyle, fill)
	row += cell(labelSync, c.sync, 1, true, columnStyle, fill)
	row += cell(labelAge, c.age, 2, true, columnStyle, fill)
	row += cell(labelHead, c.head, 2, false, columnStyle, fill)
	return row
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
//
// A clean worktree is the common case and the uninteresting one, so it says
// so with a single dot: the eye should catch the rows carrying work, not read
// the word "clean" five times on the way down.
func gitStateText(wt internal.Worktree) string {
	if wt.Dirty > 0 {
		return fmt.Sprintf("● %d", wt.Dirty)
	}
	return "·"
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
//	▎ feat/parser                      ● 3  ↑2   12m  a1b2c3d
func (m model) renderRow(idx, width int, c rowColumns) string {
	wt := m.worktrees[m.filtered[idx]]
	selected := idx == m.cursor
	p := newRowPalette(selected)

	// The accent bar marks the selection; the tint behind it carries across
	// the row so the whole line reads as one.
	bar := p.fill.Render("  ")
	if selected {
		bar = barStyle.Background(colRowBg).Render("▎") + p.fill.Render(" ")
	}

	label := truncate(m.rowLabel(wt), c.branch)
	row := bar + highlightMatch(label, strings.TrimSpace(m.textInput.Value()), p.name, p.nameMatch) +
		p.fill.Render(strings.Repeat(" ", c.branch-lipgloss.Width(label)))

	stateStyle := p.clean
	if wt.Dirty > 0 {
		stateStyle = p.dirty
	}
	row += cell(gitStateText(wt), c.state, 1+c.slack, true, stateStyle, p.fill)
	row += cell(syncText(wt), c.sync, 1, true, p.meta, p.fill)
	row += cell(relativeTime(wt.Age()), c.age, 2, true, p.meta, p.fill)
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
		return " " + successStyle.Render(m.spinner()) + " " + mutedStyle.Render(m.statusMsg.text)
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
	c.WriteString(" " + titleStyle.Render("❯") + "  " + m.textInput.View() + "\n\n")
	c.WriteString(" " + dimStyle.Render("branches off ") + mutedStyle.Render(m.baseBranch) + "\n")
	// Wrapped, not truncated: on a narrow card this note grows a line rather
	// than trailing off mid-sentence.
	for _, ln := range wrapText("leave it empty and mori will name it for you", w-3) {
		c.WriteString(" " + dimStyle.Render(ln) + "\n")
	}
	c.WriteString("\n")
	c.WriteString(" " + renderHints([]keyHint{{key: "enter", label: "create"}, {key: "esc", label: "cancel"}}) + "\n")

	return renderFrame(c.String(), w, "new worktree")
}

// spinnerFrames are braille dots used to animate anything in flight.
var spinnerFrames = []string{"⠋", "⠙", "⠹", "⠸", "⠼", "⠴", "⠦", "⠧", "⠇", "⠏"}

// spinner is the current frame. Everything that spins spins together.
func (m model) spinner() string {
	return spinnerFrames[m.animFrame%len(spinnerFrames)]
}

func (m model) renderCreatingCard(width int) string {
	w := cardWidth(width, 88)
	cmdW := w - 6

	var c strings.Builder
	c.WriteString("\n")
	c.WriteString(" " + mutedStyle.Render("creating ") + textStyle.Render(m.creatingBranch) + "\n\n")

	spin := m.spinner()
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
			glyph = dimStyle.Render("·")
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
	c.WriteString(" " + textStyle.Render("remove ") + selectedStyle.Render(wt.Label()) + textStyle.Render("?") + "\n")
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
			pad := max(1, 14-lipgloss.Width(kv[0]))
			c.WriteString("  " + keyStyle.Render(kv[0]) + strings.Repeat(" ", pad) +
				mutedStyle.Render(kv[1]) + "\n")
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
