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
	if m.width > 0 && (m.width < minViewWidth || m.height < minViewHeight) {
		return decorate(tea.NewView(m.viewTooSmall()))
	}

	// Below splitMinWidth this is the whole layout and it stops growing at
	// maxContentWidth, because past that a branch name and its age drift to
	// opposite ends of the screen with a void between them. Above it, the
	// spare columns go to a pane instead of into that void.
	width := m.contentWidth()

	base := m.viewList()
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
	case modeDetail:
		out = overlay(base, m.renderDetailCard(width), width)
	}

	return decorate(tea.NewView(out))
}

// decorate sets what every frame of the UI asks of the terminal. Focus
// reporting is the one that matters: a window nobody is looking at does not
// need a git query every fifteen seconds. Terminals that do not report it
// simply never send a BlurMsg, and mori keeps polling as it always did.
func decorate(v tea.View) tea.View {
	v.AltScreen = true
	v.ReportFocus = true
	// Cell motion, not all motion: mori wants clicks and the wheel, and has
	// nothing to do with a bare mouse move.
	v.MouseMode = tea.MouseModeCellMotion
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

func (m model) viewList() string {
	listW := m.listWidth()

	// A blank line under the brand, then the column labels: the labels are
	// the only separator the list needs, and they earn their row by naming
	// what each column of glyphs and numbers means.
	header := ""
	if len(m.filtered) > 0 {
		header = m.renderColumnHeader(m.rowColumns(listW))
	}
	body := append([]string{header}, m.renderRows(listW)...)

	// The top bar and the footer span both columns; only the list and the
	// pane sit side by side.
	if m.splitView() {
		body = joinColumns(body, m.renderPane(m.paneWidth(), len(body)), listW)
	}

	lines := []string{"", m.renderTopBar(m.contentWidth()), ""}
	lines = append(lines, body...)
	if !m.splitView() {
		lines = append(lines, m.renderSelectedPath(listW))
	}
	lines = append(lines, m.renderStatusLine(), m.renderFooter(m.contentWidth()))
	return strings.Join(lines, "\n")
}

// joinColumns sets the pane beside the list, line for line. The list has
// already been padded to listW by the row renderer; the header and the empty
// state have not, so every line is padded again here.
func joinColumns(left, right []string, listW int) []string {
	out := make([]string, 0, max(len(left), len(right)))
	gap := strings.Repeat(" ", paneGap)
	for i := 0; i < max(len(left), len(right)); i++ {
		var l, r string
		if i < len(left) {
			l = left[i]
		}
		if i < len(right) {
			r = right[i]
		}
		out = append(out, padRight(l, listW)+gap+r)
	}
	return out
}

// renderSelectedPath is the one-column layout's home for the full path of the
// worktree under the cursor. It sits at a fixed row above the status line —
// under the row it belongs to, it moved every line below the cursor by one on
// every keypress, and the list rippled as you scrolled.
func (m model) renderSelectedPath(width int) string {
	wt := m.selectedWorktree()
	if wt == nil {
		return ""
	}
	path := truncate(wt.DisplayPath, max(0, width-2))
	// A query that hit the path and not the branch left the row with nothing
	// lit at all, which read as a filter that had stopped working. Mark it
	// here, where the path actually is.
	return " " + highlightQuery(path, m.query(), pathStyle, markMatch(pathStyle))
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
// fixed height so the footer never shifts as worktrees come and go. Every
// worktree is exactly one line, selected or not.
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
	switch {
	case m.loading:
		// Nothing is known yet — not that there is nothing. Saying the
		// clearing is empty before looking would be a lie half the time.
		mark = successStyle.Render(m.spinner())
		msg, hint = "counting the trees…", nil
	case m.query() != "":
		mark = mutedStyle.Render("∅")
		msg = "nothing grows under “" + truncate(m.query(), max(8, width-24)) + "”"
		hint = []keyHint{{key: "esc", label: "clear the filter"}}
	}

	block := []string{mark, "", mutedStyle.Render(msg)}
	if hint != nil {
		block = append(block, "", renderHints(hint))
	}

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

// rowPalette is the style set one row draws with. Selection is carried by the
// caret in the gutter and by the branch name taking the accent — the other
// columns read the same selected or not, so a moving cursor never repaints
// half the list.
type rowPalette struct {
	name, meta, head, dirty, clean lipgloss.Style
	nameMatch                      lipgloss.Style
}

func newRowPalette(selected bool) rowPalette {
	p := rowPalette{
		name:  textStyle,
		meta:  mutedStyle,
		head:  dimStyle,
		dirty: dirtyStyle,
		clean: cleanStyle,
	}
	if selected {
		p.name = selectedStyle
	}
	p.nameMatch = markMatch(p.name)
	return p
}

// rowColumns holds the width of each column. A zero width hides the column:
// a narrow terminal sheds the commit subject first, then the ahead/behind
// counts, then the age, rather than squeezing the branch name down to nothing.
type rowColumns struct {
	branch, state, sync, age, subject int
	slack                             int // spare columns, held after the branch
}

// minBranchWidth is the narrowest a branch column may get before the row
// starts dropping columns to its right.
const minBranchWidth = 18

// minSubjectWidth is the least a commit subject can be given and still say
// something. Under it the column is dropped and the row keeps the width.
const minSubjectWidth = 24

// Column labels. They are measured into the column widths alongside the
// content, so a label never gets truncated by the rows beneath it.
const (
	labelBranch  = "branch"
	labelChanges = "changes"
	labelSync    = "sync"
	// The age is the worktree's, not the commit's — which is worth spelling
	// out now that the column beside it carries a commit.
	labelAge     = "created"
	labelSubject = "last commit"
)

// fixedWidth is what every column except the branch costs, gaps included.
func (c rowColumns) fixedWidth() int {
	w := 0
	for _, col := range []struct{ gap, width int }{
		{1, c.state}, {1, c.sync}, {2, c.age}, {2, c.subject},
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
// The branch takes only the width it needs. What is left over goes to the
// commit subject, which is the column that can use it — a sentence reads
// better long. When there is not enough left for a subject worth reading, the
// spare columns are held as slack after the branch instead, which pushes the
// remaining columns against the right edge as one block rather than leaving
// them adrift in the middle.
func (m model) rowColumns(width int) rowColumns {
	c := rowColumns{
		state:   len(labelChanges),
		sync:    len(labelSync),
		age:     len(labelAge),
		subject: minSubjectWidth,
	}
	natural := len(labelBranch)
	for _, i := range m.filtered {
		wt := m.worktrees[i]
		natural = max(natural, lipgloss.Width(m.rowLabel(wt)))
		c.state = max(c.state, lipgloss.Width(gitStateText(wt)))
		c.sync = max(c.sync, lipgloss.Width(syncText(wt)))
		c.age = max(c.age, lipgloss.Width(relativeTime(wt.Age())))
	}

	// The bar costs 2 columns and the row keeps a 1-column right margin.
	avail := func() int { return width - 3 - c.fixedWidth() }
	for _, drop := range []*int{&c.subject, &c.sync, &c.age} {
		if avail() >= minBranchWidth {
			break
		}
		*drop = 0
	}
	c.branch = max(6, min(natural, avail()))

	// Hand the leftovers to the subject if it is still standing, otherwise
	// hold them after the branch.
	if spare := avail() - c.branch; c.subject > 0 {
		c.subject += max(0, spare)
	} else {
		c.slack = max(0, spare)
	}
	return c
}

// renderColumnHeader labels the grid. It is built from the same cells as a
// worktree row — same widths, same gaps, same alignment — so the labels sit
// exactly over the values they name.
func (m model) renderColumnHeader(c rowColumns) string {
	row := cell(labelBranch, c.branch, gutterWidth, false, columnStyle)
	row += cell(labelChanges, c.state, 1+c.slack, true, columnStyle)
	row += cell(labelSync, c.sync, 1, true, columnStyle)
	row += cell(labelAge, c.age, 2, true, columnStyle)
	row += cell(labelSubject, c.subject, 2, false, columnStyle)
	return row
}

// hereGlyph marks the worktree mori was launched from.
const hereGlyph = "◆"

// rowLabel is what the branch column shows for a worktree. The glyphs go in
// front of the name rather than in the gutter, which belongs to the cursor —
// and they are measured into the column width with everything else, so a
// marked row still lines up.
func (m model) rowLabel(wt internal.Worktree) string {
	switch {
	case m.isHere(wt):
		return hereGlyph + " " + wt.Label()
	case m.archived[wt.Branch]:
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

// syncStyled draws what syncText measures, picking the behind count out of
// the muted run around it. Being behind is the one thing on a row that is
// about work someone else did, and the one thing you can act on without
// opening the worktree.
func syncStyled(wt internal.Worktree, base lipgloss.Style) string {
	ahead := base.Render(fmt.Sprintf("↑%d", wt.Ahead))
	behind := behindStyle.Render(fmt.Sprintf("↓%d", wt.Behind))
	switch {
	case wt.Ahead > 0 && wt.Behind > 0:
		return ahead + " " + behind
	case wt.Ahead > 0:
		return ahead
	case wt.Behind > 0:
		return behind
	}
	return ""
}

// cursorGlyph marks the selected row, and gutterWidth is the column it and
// the header's leading gap both reserve for it.
const (
	cursorGlyph = ">"
	gutterWidth = 2
)

// renderRow draws one worktree as a row of aligned columns:
//
//	> feat/parser          ● 3  ↑2   12m  size the pane's columns instead of…
//
// One line, always. The full path lives at a fixed spot below the list, or in
// the side pane — anywhere but here, where it used to push every row beneath
// the cursor down a line and back again as the selection moved.
func (m model) renderRow(idx, width int, c rowColumns) string {
	wt := m.worktrees[m.filtered[idx]]
	selected := idx == m.cursor
	p := newRowPalette(selected)

	gutter := strings.Repeat(" ", gutterWidth)
	if selected {
		gutter = cursorStyle.Render(cursorGlyph) + strings.Repeat(" ", gutterWidth-1)
	}

	label := truncate(m.rowLabel(wt), c.branch)
	name := highlightQuery(label, m.query(), p.name, p.nameMatch)
	if m.sweepBranch != "" && wt.Branch == m.sweepBranch {
		name = renderSweep(label, m.sweepFrame, p.name)
	}
	row := gutter + name + strings.Repeat(" ", c.branch-lipgloss.Width(label))

	stateStyle := p.clean
	if wt.Dirty > 0 {
		stateStyle = p.dirty
	}
	row += cell(gitStateText(wt), c.state, 1+c.slack, true, stateStyle)
	row += styledCell(syncText(wt), syncStyled(wt, p.meta), c.sync, 1, true)
	row += cell(relativeTime(wt.Age()), c.age, 2, true, p.meta)
	row += cell(wt.Subject, c.subject, 2, false, p.head)

	return padRight(row, width)
}

// renderSweep draws the one-pass highlight over a freshly created worktree's
// name: a window of sweepWindow columns, lit in the match style, travelling
// from just before the name to just past it over sweepFrames steps. The step
// is derived from the name's length rather than fixed, so a long branch and a
// short one take the same moment to settle.
func renderSweep(label string, frame int, base lipgloss.Style) string {
	runes := []rune(label)
	span := len(runes) + sweepWindow
	start := frame*span/sweepFrames - sweepWindow

	lo, hi := max(0, start), min(len(runes), start+sweepWindow)
	if lo >= hi {
		return base.Render(label)
	}
	return base.Render(string(runes[:lo])) +
		markMatch(base).Render(string(runes[lo:hi])) +
		base.Render(string(runes[hi:]))
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
		// The primary verb leads: enter is what the whole session is for.
		hints = append(hints, keyHint{key: "enter", label: "cd"})
		hints = append(hints, keyHint{key: "y", label: "copy path", prio: 1})
	}
	hints = append(hints, keyHint{key: "n", label: "new"})
	if len(m.filtered) > 0 {
		hints = append(hints, keyHint{key: "d", label: "delete", prio: 1})
		detail := keyHint{key: "i", label: "details", prio: 2}
		if m.width >= splitMinWidth {
			detail.label = "fold pane"
			if !m.paneOpen {
				detail.label = "show pane"
			}
		}
		hints = append(hints, detail)
		if wt := m.selectedWorktree(); wt != nil && m.archived[wt.Branch] {
			hints = append(hints, keyHint{key: "x", label: "unarchive", prio: 3})
		}
	}
	if m.undo != nil {
		hints = append(hints, keyHint{key: "u", label: "undo delete", prio: 1})
	}
	if m.query() != "" {
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

	// padBetween keeps a one-column margin each side and a one-column gap
	// before whatever sits on the right, so the hint row gets width-3.
	left := renderHints(fitHints(width-3, m.footerHints()))

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
	base := m.createBase
	if base == "" {
		base = m.baseBranch
	}
	c.WriteString(" " + dimStyle.Render("branches off ") + mutedStyle.Render(base) + "\n")
	// Wrapped, not truncated: on a narrow card this note grows a line rather
	// than trailing off mid-sentence.
	note, noteStyle := "leave it empty and mori will name it for you", dimStyle
	if p := m.branchProblem(strings.TrimSpace(m.textInput.Value())); p != "" {
		note, noteStyle = "⚠ "+p, warnStyle
	}
	for _, ln := range wrapText(note, w-3) {
		c.WriteString(" " + noteStyle.Render(ln) + "\n")
	}
	c.WriteString("\n")

	problem := m.branchProblem(strings.TrimSpace(m.textInput.Value()))
	hints := []keyHint{{key: "enter", label: "create"}}
	if problem != "" {
		// Nothing to press but escape until the name is a name.
		hints = nil
	}
	// The toggle is only worth naming when there is another base to toggle to.
	if wt := m.selectedWorktree(); wt != nil && wt.Branch != "" && wt.Branch != m.baseBranch {
		other := wt.Branch
		if base == other {
			other = m.baseBranch
		}
		hints = append(hints, keyHint{key: "tab", label: "off " + truncate(other, 20)})
	}
	hints = append(hints, keyHint{key: "esc", label: "cancel"})
	c.WriteString(" " + renderHints(hints) + "\n")

	return renderFrame(c.String(), w, "new worktree")
}

// spinnerFrames are braille dots used to animate anything in flight.
var spinnerFrames = []string{"⠋", "⠙", "⠹", "⠸", "⠼", "⠴", "⠦", "⠧", "⠇", "⠏"}

// spinner is the current frame. Everything that spins spins together.
func (m model) spinner() string {
	return spinnerFrames[m.animFrame%len(spinnerFrames)]
}

// failedStepOutputLines is how much of a failing step's output the card
// shows. The tail, not the head: a build log says what went wrong last.
const failedStepOutputLines = 8

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
		for _, ln := range tailLines(step.output, m.outputBudget()) {
			c.WriteString("   " + mutedStyle.Render(truncate(ln, cmdW)) + "\n")
		}
	}
	c.WriteString("\n")

	// While the steps run there is no way out but ctrl+c, so the card says
	// nothing. Once one has failed the card is a report, and it says how to
	// put it down.
	if m.creatingDone {
		c.WriteString(" " + renderHints([]keyHint{{key: "esc", label: "dismiss"}}) + "\n")
	}

	return renderFrame(c.String(), w, "new worktree")
}

// outputBudget is how many lines of a failed step the card can afford. The
// compositor clips whatever hangs past the bottom of the terminal, border and
// all, so the card sizes its one growable part rather than trusting the room
// is there.
func (m model) outputBudget() int {
	if m.height <= 0 {
		return failedStepOutputLines
	}
	// The card's fixed parts: two border rows, the heading and the blank
	// lines around it, the dismiss row, and two lines per step.
	spare := m.height - 8 - 2*len(m.creatingSteps)
	return min(failedStepOutputLines, max(2, spare))
}

// tailLines is the last n lines of s, or none when s is empty.
func tailLines(s string, n int) []string {
	if strings.TrimSpace(s) == "" {
		return nil
	}
	lines := strings.Split(strings.TrimRight(s, "\n"), "\n")
	if len(lines) > n {
		lines = lines[len(lines)-n:]
	}
	return lines
}

func (m model) renderDeleteCard(width int) string {
	w := cardWidth(width, createCardMaxWidth)
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
		for _, ln := range wrapText(fmt.Sprintf("⚠ %d uncommitted %s will be lost — u cannot bring those back", wt.Dirty, noun), w-4) {
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

	// A clean worktree is a checkout away from coming back, so enter is
	// enough. A dirty one asks for the name: the pause is the point.
	hints := []keyHint{{key: "enter", label: "delete"}, {key: "esc", label: "cancel"}}
	if m.deleteNeedsName {
		c.WriteString(" " + titleStyle.Render("❯") + "  " + m.textInput.View() + "\n\n")
		if strings.TrimSpace(m.textInput.Value()) != wt.Label() {
			hints = []keyHint{{key: "esc", label: "cancel"}}
		}
	}
	c.WriteString(" " + renderHints(hints) + "\n")

	return renderFrame(c.String(), w, "delete worktree")
}

// detailCardMaxWidth caps the detail pane. Wider than the other cards: it
// carries commit subjects, which are sentences rather than labels.
const detailCardMaxWidth = 76

// A detail pane holds more than a short terminal has rows for, and the
// compositor clips whatever hangs past the bottom — including the border. So
// every line carries a drop tier and the pane sheds lines until it fits:
// whitespace first, then the fields the row it floats over already showed,
// then history from the oldest commit up. The heading goes last of the
// history, so a pane never labels an empty section.
const (
	dropNever   = 0
	dropHeading = 1
	// dropCommit is offset by the commit's index, so the oldest goes first.
	// detailCommitLimit keeps that ladder clear of dropField above it.
	dropCommit  = 2
	dropField   = 20
	dropPadding = 30
)

// detailLine is one rendered line of the pane plus its drop tier.
type detailLine struct {
	text string
	drop int
}

// detailBody describes a worktree in full: the path enter would hand back,
// the git state the row only had glyphs for, and the tail of its history —
// enough to tell two similar branches apart without leaving mori to run git
// log. Both the floating card and the side pane are this, framed differently.
func (m model) detailBody(wt internal.Worktree, innerW int) []detailLine {
	lines := []detailLine{
		{"", dropPadding},
		{" " + selectedStyle.Render(truncate(wt.Label(), innerW)), dropNever},
		{" " + highlightQuery(truncate(wt.DisplayPath, innerW), m.query(), pathStyle, markMatch(pathStyle)), dropNever},
		{"", dropPadding},
	}
	for _, f := range m.detailFields(wt) {
		lines = append(lines, detailLine{
			" " + mutedStyle.Render(f.label) + strings.Repeat(" ", max(1, detailLabelWidth-len(f.label))) +
				textStyle.Render(truncate(f.value, innerW-detailLabelWidth)), f.drop,
		})
	}
	lines = append(lines, detailLine{"", dropPadding})
	return append(lines, m.detailHistoryLines(innerW)...)
}

// renderDetailCard floats the detail over the list, for terminals too narrow
// to set it beside one.
func (m model) renderDetailCard(width int) string {
	w := cardWidth(width, detailCardMaxWidth)
	wt := m.selectedWorktree()
	if wt == nil {
		return renderFrame("\n "+mutedStyle.Render("nothing selected")+"\n", w, "worktree")
	}
	// The frame, plus the one-column margin every line keeps on each side, so
	// a truncated commit subject does not end against the border.
	innerW := w - 4

	lines := m.detailBody(*wt, innerW)
	lines = append(lines, detailLine{"", dropPadding})
	lines = append(lines, detailLine{" " + renderHints([]keyHint{
		{key: "enter", label: "cd"}, {key: "j/k", label: "next"},
		{key: "y", label: "copy path"}, {key: "esc", label: "close"},
	}), dropNever})
	lines = append(lines, detailLine{"", dropPadding})

	// Two of the budget go to the border rows, one more keeps the card off the
	// very edge of the terminal it floats over.
	budget := 21
	if m.height > 0 {
		budget = m.height - 3
	}
	return renderFrame(strings.Join(fitDetailLines(lines, budget), "\n"), w, "worktree")
}

// renderPane is the same detail set beside the list rather than over it, as
// tall as the list is and following the cursor. It needs no action row: every
// key it would name is already in the footer below it.
func (m model) renderPane(w, height int) []string {
	inner := height - 2 // the pane's own border rows
	wt := m.selectedWorktree()

	var body []string
	if wt == nil {
		body = []string{"", " " + mutedStyle.Render("nothing to describe")}
	} else {
		body = fitDetailLines(m.detailBody(*wt, w-4), inner)
	}

	// The frame is as tall as its content, and the content is padded to the
	// list beside it — a pane that stopped short would leave the layout with
	// two different bottom edges.
	for len(body) < inner {
		body = append(body, "")
	}
	return strings.Split(renderFrame(strings.Join(body[:max(0, inner)], "\n"), w, "worktree"), "\n")
}

// fitDetailLines drops whole tiers, highest first, until the pane fits the
// rows it has — the same shape as fitHints, one axis over.
func fitDetailLines(lines []detailLine, budget int) []string {
	maxDrop := 0
	for _, ln := range lines {
		maxDrop = max(maxDrop, ln.drop)
	}
	for cap := maxDrop; ; cap-- {
		kept := make([]string, 0, len(lines))
		for _, ln := range lines {
			if ln.drop <= cap {
				kept = append(kept, ln.text)
			}
		}
		if len(kept) <= budget || cap == 0 {
			return kept
		}
	}
}

// detailLabelWidth is the label column of the field block.
const detailLabelWidth = 9

// detailField is one label/value pair of the pane's field block.
type detailField struct {
	label, value string
	drop         int
}

// detailFields spells out the facts the row's columns carry as glyphs. head,
// changes and sync are why the pane was opened; the timestamps below them are
// context, and go first when the terminal is short.
func (m model) detailFields(wt internal.Worktree) []detailField {
	head := wt.Head
	if head == "" {
		head = "—"
	}
	if wt.Detached {
		head += "  (detached)"
	}

	changes := "clean"
	if wt.Dirty > 0 {
		noun := "files"
		if wt.Dirty == 1 {
			noun = "file"
		}
		changes = fmt.Sprintf("%d uncommitted %s", wt.Dirty, noun)
	}

	sync := "in sync with " + m.baseBranch
	switch {
	case wt.Ahead > 0 && wt.Behind > 0:
		sync = fmt.Sprintf("%d ahead, %d behind %s", wt.Ahead, wt.Behind, m.baseBranch)
	case wt.Ahead > 0:
		sync = fmt.Sprintf("%d ahead of %s", wt.Ahead, m.baseBranch)
	case wt.Behind > 0:
		sync = fmt.Sprintf("%d behind %s", wt.Behind, m.baseBranch)
	}

	fields := []detailField{
		{"head", head, dropNever},
		{labelChanges, changes, dropNever},
		{labelSync, sync, dropNever},
		{"created", since(wt.Created), dropField},
		{"commit", since(wt.LastCommit), dropField},
	}
	switch {
	case m.isHere(wt):
		fields = append(fields, detailField{"state", "you are here", dropField})
	case m.archived[wt.Branch]:
		fields = append(fields, detailField{"state", "archived", dropField})
	}
	return fields
}

// since spells a timestamp out for the detail pane, where there is room for
// the word the age column drops.
func since(t time.Time) string {
	if t.IsZero() {
		return "unknown"
	}
	return relativeTime(t) + " ago"
}

// detailHistoryLines is the tail of the branch's log, one commit per line:
// sha, age, subject. While the log is in flight — or when there is none — the
// section is a single line in place of the heading, so the pane never labels
// a section it has nothing to put in.
func (m model) detailHistoryLines(innerW int) []detailLine {
	switch {
	case m.detailLoading:
		return []detailLine{{" " + successStyle.Render(m.spinner()) + " " +
			mutedStyle.Render("reading history…"), dropHeading}}
	case m.detailErr != nil:
		return []detailLine{{" " + errorStyle.Render(truncate("✗ "+m.detailErr.Error(), innerW)), dropHeading}}
	case len(m.detailCommits) == 0:
		return []detailLine{{" " + mutedStyle.Render("no commits yet"), dropHeading}}
	}

	// Both prefix columns are measured, never assumed: git abbreviates %h to
	// whatever keeps a sha unambiguous in that repository, so a big one prints
	// ten columns where a fresh one prints seven. A guessed width here is a
	// subject three columns too wide, and the frame has no say in it.
	shaW, ageW := 0, 3
	for _, cm := range m.detailCommits {
		shaW = max(shaW, lipgloss.Width(cm.SHA))
		ageW = max(ageW, lipgloss.Width(relativeTime(cm.When)))
	}
	// The sha, the two gaps around the age column, and the age column itself.
	subjectW := max(4, innerW-shaW-4-ageW)

	lines := []detailLine{{" " + columnStyle.Render("history"), dropHeading}}
	for i, cm := range m.detailCommits {
		age := relativeTime(cm.When)
		lines = append(lines, detailLine{
			" " + dimStyle.Render(cm.SHA) + strings.Repeat(" ", shaW-lipgloss.Width(cm.SHA)) +
				"  " + mutedStyle.Render(age) + strings.Repeat(" ", ageW-lipgloss.Width(age)) +
				"  " + textStyle.Render(truncate(cm.Subject, subjectW)),
			dropCommit + i,
		})
	}
	return lines
}

// --- Help ---

var helpSections = []struct {
	title string
	keys  [][2]string
}{
	{"navigation", [][2]string{
		{"enter", "cd into the worktree and quit"},
		{"j/k, ↑/↓", "move between worktrees"},
		{"g / G", "jump to first / last"},
		{"ctrl+d/u", "page down / up"},
		{"esc", "back out of filter, archive, help"},
		{"q, ctrl+c", "quit"},
	}},
	{"worktrees", [][2]string{
		{"y", "copy the path to the clipboard"},
		{"n", "create a worktree off the default branch"},
		{"N", "create one off the branch under the cursor"},
		{"d", "delete the selected worktree"},
		{"u", "restore the last deleted worktree"},
		{"i, tab", "inspect, or fold the side pane away"},
		{"r", "refresh git state now"},
	}},
	{"marks", [][2]string{
		{hereGlyph, "the worktree you are standing in"},
		{"◌", "archived"},
		{"● n", "n files with uncommitted changes"},
		{"↑/↓ n", "commits ahead of / behind the base branch"},
	}},
	{"view", [][2]string{
		{"/", "fuzzy filter, best matches first"},
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
