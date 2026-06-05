package tui

import (
	"fmt"
	"image/color"
	"sort"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"github.com/trebaud/mori/internal"
	"github.com/trebaud/mori/internal/github"
	"github.com/trebaud/mori/internal/insights"
)

const prColMinWidth = 110

// View is the Elm view function — it dispatches to a layout based on mode and
// terminal width. All rendering below is pure: model in, string out.
func (m model) View() tea.View {
	totalWidth := m.width
	if totalWidth == 0 {
		totalWidth = 140
	}

	if m.width > 0 && (m.width < minViewWidth || m.height < minViewHeight) {
		v := tea.NewView(m.viewTooSmall())
		v.AltScreen = true
		return v
	}

	var base string
	if m.showHelp {
		base = m.viewHelp(totalWidth)
	} else {
		base = m.viewListOnly(totalWidth)
	}

	out := base
	switch {
	case m.mode == modeCreate || m.mode == modeCreating:
		out = m.applyOverlay(base, totalWidth)
	case m.showInsights && totalWidth < splitMinWidth:
		// Narrow terminals fall back to the floating overlay.
		out = m.applyInsightsOverlay(base, totalWidth)
	}

	v := tea.NewView(out)
	v.AltScreen = true
	return v
}

func (m model) viewTooSmall() string {
	msg := fmt.Sprintf("terminal too small (%d×%d) — need at least %d×%d", m.width, m.height, minViewWidth, minViewHeight)
	if m.height < 2 {
		return msg
	}
	pad := (m.height - 1) / 2
	return strings.Repeat("\n", pad) + " " + mutedStyle.Render(msg)
}

// --- Top-level layouts ---

func (m model) viewListOnly(width int) string {
	top := m.renderTopBar(width)
	warn := m.renderMissingToolsWarning(width)

	var framed string
	if m.showInsights && width >= splitMinWidth {
		framed = m.renderSplitColumns(width)
	} else {
		list := m.renderWorktreeList(width - 2)
		framed = renderFrame(list, width, "worktrees")
	}

	footer := m.renderBelowList(width) + m.renderFooter(width-2)
	return "\n" + top + "\n" + warn + "\n" + framed + "\n" + footer + "\n"
}

// renderSplitColumns renders the worktree list and insights panel side by side.
func (m model) renderSplitColumns(width int) string {
	innerH := m.listInnerHeight()

	leftW := width * 55 / 100
	rightW := width - leftW - 1
	if leftW < 55 {
		leftW = 55
	}
	if rightW < 38 {
		rightW = 38
	}

	list := m.renderWorktreeListWithHeight(leftW-2, innerH)
	listFrame := renderFrame(list, leftW, "worktrees")
	frameH := lipgloss.Height(listFrame)

	insightsFrame := m.renderInsightsSidePanel(rightW, frameH, m.insightsTitle())

	return joinColumns(listFrame, insightsFrame)
}

// renderInsightsSidePanel renders the insights content into a frame of exactly
// outerH rows. Layout from top to bottom: tab strip, scrollable content, fixed
// keybind footer. Tab content scrolls with [ / ] when it overflows.
func (m model) renderInsightsSidePanel(width, outerH int, title string) string {
	panelW := width - 4
	innerH := outerH - 2 // frame top + bottom
	if panelW < 10 {
		panelW = 10
	}
	if innerH < 4 {
		innerH = 4
	}

	tabStrip := m.renderInsightsTabStrip(panelW)
	footer := m.renderInsightsFooterStrip(panelW)
	// Reserve one row for tab strip, one for footer.
	contentH := innerH - 2
	if contentH < 2 {
		contentH = 2
	}

	fullContent := m.renderInsightsPanel(panelW)
	lines := strings.Split(fullContent, "\n")
	for len(lines) > 0 && lines[len(lines)-1] == "" {
		lines = lines[:len(lines)-1]
	}

	totalLines := len(lines)
	canScroll := totalLines > contentH
	visibleH := contentH
	if canScroll {
		visibleH = contentH - 1
	}

	maxOffset := totalLines - visibleH
	if maxOffset < 0 {
		maxOffset = 0
	}
	offset := m.insightsScrollOffset
	if offset > maxOffset {
		offset = maxOffset
	}
	if offset < 0 {
		offset = 0
	}

	end := offset + visibleH
	if end > totalLines {
		end = totalLines
	}
	visible := append([]string{}, lines[offset:end]...)
	for len(visible) < visibleH {
		visible = append(visible, "")
	}

	var sb strings.Builder
	sb.WriteString(tabStrip)
	sb.WriteString("\n")
	sb.WriteString(strings.Join(visible, "\n"))
	if canScroll {
		sb.WriteString("\n")
		var arrows []string
		if offset > 0 {
			arrows = append(arrows, "↑")
		}
		if offset < maxOffset {
			arrows = append(arrows, "↓")
		}
		sb.WriteString("   " + dimStyle.Render(strings.Join(arrows, "")+" [ / ] scroll"))
	}
	sb.WriteString("\n")
	sb.WriteString(footer)

	return renderFrame(sb.String(), width, title)
}

// insightsTabNames is the canonical tab order; index matches the [1-5] hotkey.
var insightsTabNames = []string{"overview", "activity", "git", "todos", "cost"}

// renderInsightsTabStrip draws the per-tab navigation row. The active tab is
// bold; others are muted. Tab names are prefixed with a numeric hint so a new
// user discovers the hotkey without consulting the footer.
func (m model) renderInsightsTabStrip(width int) string {
	tab := m.insightsTab
	if tab < 0 || tab >= len(insightsTabNames) {
		tab = 0
	}
	var parts []string
	for i, name := range insightsTabNames {
		label := fmt.Sprintf("%d %s", i+1, name)
		if i == tab {
			parts = append(parts, selectedStyle.Render(label))
		} else {
			parts = append(parts, mutedStyle.Render(label))
		}
	}
	return " " + strings.Join(parts, dimStyle.Render("  "))
}

// renderInsightsFooterStrip is the always-visible keybind row at the bottom of
// the insights frame. Action keys ([c]/[p]/[l]/[K]) only do something while the
// panel is open — see handleNormalKey.
func (m model) renderInsightsFooterStrip(width int) string {
	hints := []string{
		dimStyle.Render("[1-5]") + mutedStyle.Render(" tab"),
		dimStyle.Render("[j/k]") + mutedStyle.Render(" wt"),
		dimStyle.Render("[c]") + mutedStyle.Render(" copy"),
		dimStyle.Render("[p]") + mutedStyle.Render(" pr"),
		dimStyle.Render("[l]") + mutedStyle.Render(" log"),
		dimStyle.Render("[K]") + mutedStyle.Render(" kill"),
		dimStyle.Render("[esc]") + mutedStyle.Render(" close"),
	}
	line := strings.Join(hints, dimStyle.Render("  "))
	if lipgloss.Width(line) > width-1 {
		// Drop the rarer hints first so something always fits.
		short := []string{
			dimStyle.Render("[1-5]") + mutedStyle.Render(" tab"),
			dimStyle.Render("[j/k]") + mutedStyle.Render(" wt"),
			dimStyle.Render("[c/p/l/K]") + mutedStyle.Render(" act"),
			dimStyle.Render("[esc]") + mutedStyle.Render(" close"),
		}
		line = strings.Join(short, dimStyle.Render("  "))
	}
	return " " + line
}

// joinColumns concatenates two frame strings side by side with a single space separator.
func joinColumns(left, right string) string {
	ls := strings.Split(left, "\n")
	rs := strings.Split(right, "\n")
	n := len(ls)
	if len(rs) > n {
		n = len(rs)
	}
	var b strings.Builder
	for i := 0; i < n; i++ {
		if i > 0 {
			b.WriteString("\n")
		}
		l, r := "", ""
		if i < len(ls) {
			l = ls[i]
		}
		if i < len(rs) {
			r = rs[i]
		}
		b.WriteString(l + " " + r)
	}
	return b.String()
}

// renderInsightsCard builds the floating insights window for narrow terminals.
func (m model) renderInsightsCard(totalWidth, totalHeight int) string {
	floatW := insightsFloatW
	floatH := insightsFloatH
	if floatW > totalWidth-4 {
		floatW = totalWidth - 4
	}
	if floatH > totalHeight-4 {
		floatH = totalHeight - 4
	}
	if floatW < 40 {
		floatW = 40
	}
	if floatH < 10 {
		floatH = 10
	}

	return m.renderInsightsSidePanel(floatW, floatH, m.insightsTitle())
}

// insightsTitle returns the frame title shown above the insights panel:
// "insights · branch · session-title" (with title trimmed to fit).
func (m model) insightsTitle() string {
	wt := m.selectedWorktree()
	if wt == nil {
		return "agent insights"
	}
	parts := []string{"insights", wt.Branch}
	if t := wt.Insights.SessionTitle; t != "" {
		if len(t) > 32 {
			t = t[:31] + "…"
		}
		parts = append(parts, t)
	} else if wt.Insights.Slug != "" {
		parts = append(parts, wt.Insights.Slug)
	}
	return strings.Join(parts, " · ")
}

// applyInsightsOverlay composites the floating insights card over the dimmed
// base view, centered horizontally and vertically.
func (m model) applyInsightsOverlay(base string, totalWidth int) string {
	card := m.renderInsightsCard(totalWidth, m.height)
	cardW := lipgloss.Width(card)
	cardH := lipgloss.Height(card)
	baseH := lipgloss.Height(base)

	x := (totalWidth - cardW) / 2
	if x < 0 {
		x = 0
	}
	y := (baseH - cardH) / 2
	if y < 2 {
		y = 2
	}

	dimmed := dimBackground(base)
	baseLayer := lipgloss.NewLayer(dimmed).Z(0)
	cardLayer := lipgloss.NewLayer(card).X(x).Y(y).Z(1)
	return lipgloss.NewCompositor(baseLayer, cardLayer).Render()
}

func (m model) renderMissingToolsWarning(width int) string {
	if len(m.missingTools) == 0 {
		return ""
	}
	msg := "⚠  " + strings.Join(m.missingTools, ", ") + " not found in PATH — some features unavailable"
	return " " + waitingStyle.Render(msg)
}

// renderBelowList renders the thin inline status/search line that always sits
// between the list and the footer. It stays one row tall regardless of mode so
// the footer never shifts; the "new worktree" and "agent prompt" prompts are
// drawn as floating overlays in applyOverlay instead.
func (m model) renderBelowList(width int) string {
	_ = width
	return m.renderInputLine() + "\n"
}

// --- Chrome: top bar, footer, input line, frame ---

func (m model) renderTopBar(width int) string {
	brand := titleStyle.Render("◆ MORI")
	branch := mutedStyle.Render("on ") + textStyle.Render(m.currentBranch)

	w, q, i, _ := m.statusCounts()
	var pills []string
	if w > 0 {
		pills = append(pills, workingStyle.Render(fmt.Sprintf("● %d working", w)))
	}
	if q > 0 {
		pills = append(pills, waitingStyle.Render(fmt.Sprintf("◐ %d waiting", q)))
	}
	if i > 0 {
		pills = append(pills, idleStyle.Render(fmt.Sprintf("○ %d idle", i)))
	}
	right := strings.Join(pills, mutedStyle.Render("  ·  "))

	left := brand + "  " + branch
	return padBetween(left, right, width-2)
}

func (m model) renderInputLine() string {
	switch m.mode {
	case modeSearch:
		return " " + titleStyle.Render("/") + " " + m.textInput.View()
	case modeConfirmDelete:
		if m.deleteTarget < len(m.filtered) {
			wt := m.worktrees[m.filtered[m.deleteTarget]]
			return " " + errorStyle.Render("delete "+wt.Branch+"? ") + mutedStyle.Render("[y/N]")
		}
		return ""
	default:
		if m.statusMsg != nil && time.Now().Before(m.statusMsg.expires) {
			switch {
			case m.statusMsg.isLoading:
				return " " + mutedStyle.Render("⋯ "+m.statusMsg.text)
			case m.statusMsg.isError:
				return " " + errorStyle.Render("✗ "+m.statusMsg.text)
			default:
				return " " + successStyle.Render("✓ "+m.statusMsg.text)
			}
		}
		return ""
	}
}

// renderCreateCard is the floating "new worktree" card, drawn over the list by
// applyOverlay. The frame is sized by content; positioning happens in the
// overlay.
func (m model) renderCreateCard(width int) string {
	cardW := width - 4
	if cardW > 72 {
		cardW = 72
	}
	if cardW < 32 {
		cardW = 32
	}

	prompt := titleStyle.Render("›")
	inputLine := " " + prompt + "  " + m.textInput.View()
	hint := " " + dimStyle.Render("branch off ") + mutedStyle.Render(m.currentBranch) +
		dimStyle.Render("  ·  leave empty for a random name")
	keys := " " + mutedStyle.Render("[enter] create  [esc] cancel")

	var content strings.Builder
	content.WriteString("\n")
	content.WriteString(inputLine)
	content.WriteString("\n\n")
	content.WriteString(hint)
	content.WriteString("\n")
	content.WriteString(keys)
	content.WriteString("\n")

	return renderFrame(content.String(), cardW, "new worktree")
}

// spinnerFrames are braille dots used to animate steps in progress.
var spinnerFrames = []string{"⠋", "⠙", "⠹", "⠸", "⠼", "⠴", "⠦", "⠧", "⠇", "⠏"}

// renderCreatingCard is the floating progress card shown while a worktree is
// being created. It lists each setup step with its command and a live state
// glyph (spinner, check, cross, or pending dot).
func (m model) renderCreatingCard(width int) string {
	cardW := width - 4
	if cardW > 96 {
		cardW = 96
	}
	if cardW < 40 {
		cardW = 40
	}

	innerW := cardW - 2
	cmdW := innerW - 4
	if cmdW < 8 {
		cmdW = 8
	}

	header := " " + mutedStyle.Render("creating ") + textStyle.Render(m.creatingBranch)

	var content strings.Builder
	content.WriteString("\n")
	content.WriteString(header)
	content.WriteString("\n\n")

	spin := spinnerFrames[m.animFrame%len(spinnerFrames)]

	for _, step := range m.creatingSteps {
		var glyph string
		var nameStyle lipgloss.Style
		switch step.state {
		case stepRunning:
			glyph = workingStyle.Render(spin)
			nameStyle = textStyle.Bold(true)
		case stepSucceeded:
			glyph = successStyle.Render("✓")
			nameStyle = mutedStyle
		case stepFailed:
			glyph = errorStyle.Render("✗")
			nameStyle = errorStyle
		default:
			glyph = mutedStyle.Render("○")
			nameStyle = mutedStyle
		}

		content.WriteString(" " + glyph + " " + nameStyle.Render(step.name) + "\n")
		cmdLine := step.cmd
		if lipgloss.Width(cmdLine) > cmdW {
			cmdLine = cmdLine[:cmdW-1] + "…"
		}
		content.WriteString("   " + dimStyle.Render(cmdLine) + "\n")
	}

	content.WriteString("\n")

	return renderFrame(content.String(), cardW, "new worktree")
}

// applyOverlay composites a floating prompt card on top of the base view,
// keeping the worktree list visible (and dimmed) underneath. The card is
// horizontally centered and vertically anchored inside the list area so it
// never covers the top bar or the footer.
func (m model) applyOverlay(base string, width int) string {
	var card string
	switch m.mode {
	case modeCreate:
		card = m.renderCreateCard(width)
	case modeCreating:
		card = m.renderCreatingCard(width)
	default:
		return base
	}

	cardW := lipgloss.Width(card)
	cardH := lipgloss.Height(card)

	baseH := lipgloss.Height(base)

	x := (width - cardW) / 2
	if x < 0 {
		x = 0
	}

	// Anchor the card vertically inside the list region: leave the top bar
	// (rows 0–2) and the bottom chrome (footer + status, ~3 rows) clear.
	const topReserve = 3
	const bottomReserve = 3
	avail := baseH - topReserve - bottomReserve
	y := topReserve
	if avail > cardH {
		y = topReserve + (avail-cardH)/2
	}
	if y+cardH > baseH-1 {
		y = baseH - 1 - cardH
	}
	if y < 0 {
		y = 0
	}

	dimmed := dimBackground(base)
	baseLayer := lipgloss.NewLayer(dimmed).Z(0)
	cardLayer := lipgloss.NewLayer(card).X(x).Y(y).Z(1)
	return lipgloss.NewCompositor(baseLayer, cardLayer).Render()
}

// dimBackground softens the base view by re-applying the SGR faint attribute
// after every reset, so every styled span on every line ends up dimmed. The
// effect approximates a "blur" behind the floating prompt card.
func dimBackground(s string) string {
	const faintOn = "\x1b[2m"
	const reset = "\x1b[0m"
	// Re-apply faint after every full reset embedded in styled content, then
	// frame each line with an outer faint+reset so unstyled segments dim too.
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

func (m model) renderFooter(width int) string {
	if m.mode == modeSearch {
		return " " + mutedStyle.Render("[enter] apply  [esc] clear  [↑/↓] navigate")
	}

	insightsHint := "[tab] insights"
	if m.showInsights {
		insightsHint = "[tab] close  [1-5] tabs"
	}
	// Keep the always-visible footer to ~5 essentials. Delete, message,
	// archive, and refresh are documented under [?] help.
	left := mutedStyle.Render("[enter] open  " + insightsHint + "  [n] new  [?] help  [q] quit")

	var indicators []string
	indicators = append(indicators, mutedStyle.Render("sort ")+textStyle.Render(m.sortMode.String()))
	if m.statusFilter != filterAll {
		indicators = append(indicators, mutedStyle.Render("filter ")+textStyle.Render(m.statusFilter.String()))
	}
	if m.showArchive {
		indicators = append(indicators, mutedStyle.Render("archived"))
	}
	right := strings.Join(indicators, dimStyle.Render("  ·  "))

	return padBetween(left, right, width)
}

// renderFrame draws a rounded-border frame around content with an optional inline title.
func renderFrame(content string, width int, title string) string {
	innerW := width - 2
	if innerW < 4 {
		innerW = 4
	}

	var top string
	if title != "" {
		leadDashes := 2
		titleText := " " + title + " "
		titleWidth := lipgloss.Width(titleText)
		tail := innerW - leadDashes - titleWidth
		if tail < 1 {
			tail = 1
		}
		top = borderStyle.Render("╭"+strings.Repeat("─", leadDashes)) +
			titleText +
			borderStyle.Render(strings.Repeat("─", tail)+"╮")
	} else {
		top = borderStyle.Render("╭" + strings.Repeat("─", innerW) + "╮")
	}
	bottom := borderStyle.Render("╰" + strings.Repeat("─", innerW) + "╯")

	lines := strings.Split(content, "\n")
	var out strings.Builder
	out.WriteString(top + "\n")
	for _, ln := range lines {
		pad := innerW - lipgloss.Width(ln)
		if pad < 0 {
			pad = 0
		}
		out.WriteString(borderStyle.Render("│") + ln + strings.Repeat(" ", pad) + borderStyle.Render("│") + "\n")
	}
	out.WriteString(bottom)
	return out.String()
}

// --- Worktree list ---

func colWidths(width int) (branchW, activityW, statusW, contextW, prW int) {
	activityW, statusW, contextW = 10, 12, 10
	if width > 100 {
		activityW = 12
	}
	if width >= prColMinWidth {
		prW = 8
	}
	separators := 3
	if prW > 0 {
		separators = 4
	}
	branchW = width - 2 - activityW - statusW - contextW - prW - separators
	if branchW < 20 {
		branchW = 20
	}
	if branchW > 60 {
		branchW = 60
	}
	return
}

func (m model) renderWorktreeList(width int) string {
	return m.renderWorktreeListWithHeight(width, m.listInnerHeight())
}

// renderWorktreeListWithHeight renders the list with a fixed inner height. The
// header and divider take two lines; the remainder is a viewport that scrolls
// around the cursor. Empty lines pad to the target height so the surrounding
// frame doesn't change size as worktrees come and go.
func (m model) renderWorktreeListWithHeight(width, innerH int) string {
	var s strings.Builder

	branchW, activityW, statusW, contextW, prW := colWidths(width)

	header := "  " +
		mutedStyle.Width(branchW).Render("branch") + " " +
		mutedStyle.Width(activityW).Render("activity") + " " +
		mutedStyle.Width(statusW).Render("status") + " " +
		mutedStyle.Width(contextW).Render("context")
	if prW > 0 {
		header += " " + mutedStyle.Width(prW).Render("pr")
	}
	s.WriteString(header + "\n")
	s.WriteString(dimStyle.Render(strings.Repeat("─", width)) + "\n")

	rows := innerH - 2
	if rows < 1 {
		rows = 1
	}

	if len(m.filtered) == 0 {
		s.WriteString("\n  " + mutedStyle.Render("no matching worktrees") + "\n")
		// Pad to height so the frame stays a fixed size.
		written := 1
		for i := written; i < rows; i++ {
			s.WriteString("\n")
		}
		return s.String()
	}

	offset := m.scrollOffset
	if offset < 0 {
		offset = 0
	}
	if offset > len(m.filtered)-1 {
		offset = max(0, len(m.filtered)-1)
	}
	end := offset + rows
	if end > len(m.filtered) {
		end = len(m.filtered)
	}

	for i := offset; i < end; i++ {
		s.WriteString(m.renderWorktreeRow(i, m.filtered[i], width, branchW, activityW, statusW, contextW, prW) + "\n")
	}

	// Pad with blank rows so the frame keeps a fixed height even when there
	// are fewer worktrees than the viewport can show.
	for i := end - offset; i < rows; i++ {
		s.WriteString("\n")
	}

	return s.String()
}

func (m model) renderWorktreeRow(cursorIdx, wtIdx, rowW, branchW, activityW, statusW, contextW, prW int) string {
	wt := m.worktrees[wtIdx]
	selected := m.cursor == cursorIdx

	trunc := func(s string, w int) string {
		if lipgloss.Width(s) > w {
			if w <= 1 {
				return s[:w]
			}
			return s[:w-1] + "…"
		}
		return s
	}

	branchLabel := wt.Branch
	if wt.IsMain {
		branchLabel = "★ " + branchLabel
	}
	if m.archived[wt.Branch] {
		branchLabel = "◌ " + branchLabel
	}
	branchLabel = trunc(branchLabel, branchW)

	activity := "—"
	if !wt.Insights.LastActivity.IsZero() {
		activity = relativeTime(wt.Insights.LastActivity)
	}

	effStatus := m.effectiveStatus(wt)
	statusText := statusIcon(effStatus) + " " + strings.ToLower(string(effStatus))
	contextText := renderInlineContextRaw(wt.Insights)

	// Each span sets both fg AND bg (when selected) so terminal resets
	// between spans don't leave un-highlighted gaps.
	bg := func(st lipgloss.Style) lipgloss.Style {
		if selected {
			return st.Background(colRowBg)
		}
		return st
	}
	sep := bg(lipgloss.NewStyle()).Render(" ")

	var accentBar string
	switch {
	case selected:
		accentBar = bg(lipgloss.NewStyle().Foreground(colAccent)).Render("▌")
	case effStatus == insights.StatusWorking || effStatus == insights.StatusWait:
		accentBar = lipgloss.NewStyle().Foreground(statusColor(effStatus)).Render("▏")
	default:
		accentBar = " "
	}

	var branchStyleCell, activityStyleCell, statusCellStyle, contextCellStyle lipgloss.Style
	if selected {
		branchStyleCell = lipgloss.NewStyle().Foreground(colText).Bold(true).Width(branchW)
		activityStyleCell = lipgloss.NewStyle().Foreground(colMuted).Width(activityW)
	} else {
		branchStyleCell = lipgloss.NewStyle().Foreground(colText).Width(branchW)
		activityStyleCell = lipgloss.NewStyle().Foreground(colDim).Width(activityW)
	}
	statusCellStyle = statusStyle(effStatus).Width(statusW)
	contextCellStyle = contextFgStyle(wt.Insights).Width(contextW)

	branchCell := bg(branchStyleCell).Render(branchLabel)
	activityCell := bg(activityStyleCell).Render(activity)
	statusCell := bg(statusCellStyle).Render(statusText)
	contextCell := bg(contextCellStyle).Render(contextText)

	row := accentBar + sep + branchCell + sep + activityCell + sep + statusCell + sep + contextCell
	if prW > 0 {
		row += sep + bg(prCellStyle(wt.PR).Width(prW)).Render(prCellText(wt.PR))
	}

	if selected {
		cur := lipgloss.Width(row)
		if cur < rowW {
			row += bg(lipgloss.NewStyle()).Render(strings.Repeat(" ", rowW-cur))
		}
	}
	return row
}

func renderInlineContextRaw(ins insights.Insights) string {
	if ins.InputTokens <= 0 {
		return "—"
	}
	maxTokens := contextMaxTokens(ins.Model)
	percent := float64(ins.InputTokens) / float64(maxTokens)
	if percent > 1 {
		percent = 1
	}
	return fmt.Sprintf("%d%%", int(percent*100))
}

func prCellText(pr *github.PRInfo) string {
	if pr == nil || pr.Number == 0 {
		return "—"
	}
	return fmt.Sprintf("%s#%d", prGlyph(pr), pr.Number)
}

func prCellStyle(pr *github.PRInfo) lipgloss.Style {
	if pr == nil || pr.Number == 0 {
		return mutedStyle
	}
	switch pr.State {
	case github.PRStateOpen:
		return successStyle
	case github.PRStateDraft:
		return mutedStyle
	case github.PRStateMerged:
		return titleStyle
	case github.PRStateClosed:
		return errorStyle
	}
	return mutedStyle
}

func prGlyph(pr *github.PRInfo) string {
	switch pr.State {
	case github.PRStateOpen:
		return "●"
	case github.PRStateDraft:
		return "◐"
	case github.PRStateMerged:
		return "✔"
	case github.PRStateClosed:
		return "✕"
	}
	return "·"
}

func contextFgStyle(ins insights.Insights) lipgloss.Style {
	if ins.InputTokens <= 0 {
		return mutedStyle
	}
	maxTokens := contextMaxTokens(ins.Model)
	percent := float64(ins.InputTokens) / float64(maxTokens)
	switch {
	case percent >= 0.8:
		return barHighStyle
	case percent >= 0.5:
		return barMedStyle
	default:
		return barLowStyle
	}
}

// --- Insights panel ---

// renderInsightsPanel dispatches to the per-tab renderer based on
// m.insightsTab. The frame chrome (tab strip + footer) is added by
// renderInsightsSidePanel — each tab body is the scrollable middle.
func (m model) renderInsightsPanel(width int) string {
	wt := m.selectedWorktree()
	if wt == nil {
		return "\n  " + mutedStyle.Render("no worktree selected") + "\n"
	}
	switch m.insightsTab {
	case 1:
		return m.renderInsightsActivity(*wt, width)
	case 2:
		return m.renderInsightsGit(*wt, width)
	case 3:
		return m.renderInsightsTodos(*wt, width)
	case 4:
		return m.renderInsightsCost(*wt, width)
	default:
		return m.renderInsightsOverview(*wt, width)
	}
}

// renderInsightsOverview is the "at a glance" tab: status, model, cost, the
// pending question (if any) or last prompt, the running todo, attach hint.
// Heavy detail (full todo list, commits, tool mix) lives on dedicated tabs.
func (m model) renderInsightsOverview(wt internal.Worktree, width int) string {
	var s strings.Builder
	effStatus := m.effectiveStatus(wt)

	// Header: status pill + model/mode/cost on the right.
	pill := renderStatusPillWithDuration(effStatus, wt.Insights.LastActivity)
	var rightParts []string
	if wt.Insights.Model != "" {
		rightParts = append(rightParts, renderModelTier(wt.Insights.Model))
	}
	if wt.Insights.PlanModeActive {
		rightParts = append(rightParts, waitingStyle.Render("PLAN"))
	}
	if wt.Insights.Mode != "" && wt.Insights.Mode != "default" {
		rightParts = append(rightParts, mutedStyle.Render(wt.Insights.Mode))
	}
	if wt.Insights.CostUSD > 0 {
		rightParts = append(rightParts, successStyle.Render(fmt.Sprintf("$%.2f", wt.Insights.CostUSD)))
	}
	right := strings.Join(rightParts, mutedStyle.Render(" · "))

	s.WriteString("\n")
	s.WriteString(padBetween(pill, right, width) + "\n")

	// Status detail — error with tool name, running tool, or last-active time.
	var detail string
	switch {
	case wt.Insights.HasError && wt.Insights.ErrorDetail.Tool != "":
		msg := wt.Insights.ErrorDetail.Msg
		if msg == "" {
			msg = "tool errored"
		}
		detail = errorStyle.Render("⚠ "+wt.Insights.ErrorDetail.Tool+": ") + mutedStyle.Render(truncate(msg, width-12))
	case wt.Insights.HasError:
		detail = errorStyle.Render("⚠ last tool errored")
	case wt.Insights.LastTool != "" && effStatus == insights.StatusWorking:
		detail = mutedStyle.Render("running ") + textStyle.Render(wt.Insights.LastTool)
	case effStatus == insights.StatusIdle && !wt.Insights.LastActivity.IsZero():
		detail = mutedStyle.Render("last active " + relativeTime(wt.Insights.LastActivity))
	}
	if detail != "" {
		s.WriteString(" " + detail + "\n")
	}

	// Context bar with burn-rate warning.
	s.WriteString("\n")
	s.WriteString(renderContextRow(wt.Insights, width-2) + "\n")

	// Pending question (when waiting) takes priority over the last prompt.
	if wt.Insights.PendingQuestion != "" {
		s.WriteString("\n")
		s.WriteString(" " + headingStyle.Render("waiting on you") + "\n")
		for _, line := range wrapText(wt.Insights.PendingQuestion, width-3) {
			s.WriteString("   " + waitingStyle.Render(line) + "\n")
		}
	} else if wt.Insights.LastPrompt != "" {
		s.WriteString("\n")
		s.WriteString(" " + headingStyle.Render("prompt") + "\n")
		prompt := stripPromptBoilerplate(wt.Insights.LastPrompt)
		for _, line := range wrapText(prompt, width-3) {
			s.WriteString("   " + textStyle.Render(line) + "\n")
		}
	}

	// Compact todo summary — full list lives on the "todos" tab.
	if n := len(wt.Insights.Todos); n > 0 {
		done := 0
		var inProgress *insights.TodoItem
		for i := range wt.Insights.Todos {
			t := &wt.Insights.Todos[i]
			if t.Status == "completed" {
				done++
			}
			if t.Status == "in_progress" && inProgress == nil {
				inProgress = t
			}
		}
		counter := mutedStyle.Render(fmt.Sprintf("%d/%d", done, n))
		s.WriteString("\n")
		s.WriteString(" " + headingStyle.Render("progress") + "  " + counter + "\n")
		if inProgress != nil {
			spin := workingStyle.Render(spinnerFrames[m.animFrame%len(spinnerFrames)])
			text := inProgress.ActiveForm
			if text == "" {
				text = inProgress.Content
			}
			s.WriteString("   " + spin + " " + textStyle.Bold(true).Render(truncate(text, width-6)) + "\n")
		}
	}

	return s.String()
}

// renderInsightsActivity is the "what is the agent doing" tab: which file it
// just touched, the chronological feed of edits and sub-agent spawns, the
// tool-mix summary, and a slash-command breadcrumb trail.
func (m model) renderInsightsActivity(wt internal.Worktree, width int) string {
	var s strings.Builder
	ins := wt.Insights

	s.WriteString("\n")

	// Pinned: most recently edited file.
	if len(ins.FilesTouched) > 0 {
		f := ins.FilesTouched[0]
		s.WriteString(" " + headingStyle.Render("editing") + "\n")
		s.WriteString("   " + textStyle.Bold(true).Render(truncate(shortPath(f.Path), width-6)) + "\n")
	}

	// Files touched (top 6 after the pinned one).
	if len(ins.FilesTouched) > 1 {
		s.WriteString("\n")
		s.WriteString(" " + headingStyle.Render("files touched") + "\n")
		max := 6
		if len(ins.FilesTouched) < max+1 {
			max = len(ins.FilesTouched) - 1
		}
		for i := 1; i <= max; i++ {
			f := ins.FilesTouched[i]
			suffix := ""
			if f.Edits > 1 {
				suffix = mutedStyle.Render(fmt.Sprintf("  ×%d", f.Edits))
			}
			s.WriteString("   " + mutedStyle.Render("·") + " " + textStyle.Render(truncate(shortPath(f.Path), width-10)) + suffix + "\n")
		}
	}

	// Tool mix — running counts grouped into common buckets.
	if len(ins.ToolCounts) > 0 {
		s.WriteString("\n")
		s.WriteString(" " + headingStyle.Render("tool mix") + "\n")
		s.WriteString("   " + renderToolMix(ins.ToolCounts) + "\n")
	}

	// Sub-agents — running first, then resolved.
	if len(ins.SubAgents) > 0 {
		s.WriteString("\n")
		s.WriteString(" " + headingStyle.Render("sub-agents") + "\n")
		// Show last 5 in reverse insertion order.
		start := len(ins.SubAgents) - 5
		if start < 0 {
			start = 0
		}
		for i := len(ins.SubAgents) - 1; i >= start; i-- {
			sa := ins.SubAgents[i]
			var glyph string
			if sa.Running {
				glyph = workingStyle.Render(spinnerFrames[m.animFrame%len(spinnerFrames)])
			} else {
				glyph = successStyle.Render("✓")
			}
			label := sa.Type
			if sa.Description != "" {
				label += " · " + sa.Description
			}
			s.WriteString("   " + glyph + " " + textStyle.Render(truncate(label, width-6)) + "\n")
		}
	}

	// Slash commands.
	if len(ins.LastSlashCommands) > 0 {
		s.WriteString("\n")
		s.WriteString(" " + headingStyle.Render("recent slash commands") + "\n")
		s.WriteString("   " + mutedStyle.Render(strings.Join(ins.LastSlashCommands, "  ·  ")) + "\n")
	}

	if s.Len() == 1 { // only the leading newline
		s.WriteString("  " + mutedStyle.Render("no tool activity yet") + "\n")
	}

	return s.String()
}

// renderInsightsGit is the git tab: branch ahead/behind, working-tree diff,
// pull-request state, and the recent commit list.
func (m model) renderInsightsGit(wt internal.Worktree, width int) string {
	var s strings.Builder
	ins := wt.Insights
	s.WriteString("\n")

	any := false
	if ins.AheadBehind != "" {
		s.WriteString(kvRow(" branch", textStyle.Render(ins.AheadBehind), width) + "\n")
		any = true
	}
	if ins.DiffShortstat != "" {
		s.WriteString(kvRow(" diff", textStyle.Render(ins.DiffShortstat), width) + "\n")
		any = true
	}

	if wt.PR != nil && wt.PR.Number > 0 {
		s.WriteString(renderPRSection(wt.PR, width))
		any = true
	}

	if len(ins.GitLog) > 0 {
		s.WriteString("\n")
		s.WriteString(" " + headingStyle.Render("recent commits") + "\n")
		for _, entry := range ins.GitLog {
			line := entry
			if lipgloss.Width(line) > width-5 {
				line = line[:width-6] + "…"
			}
			s.WriteString("   " + mutedStyle.Render("•") + " " + textStyle.Render(line) + "\n")
		}
		any = true
	}

	if !any {
		s.WriteString("  " + mutedStyle.Render("clean working tree, no commits yet") + "\n")
	}
	return s.String()
}

// renderInsightsTodos is the full todo list with statuses — what the overview
// tab summarizes in one row.
func (m model) renderInsightsTodos(wt internal.Worktree, width int) string {
	var s strings.Builder
	s.WriteString("\n")

	if len(wt.Insights.Todos) == 0 {
		s.WriteString("  " + mutedStyle.Render("no todos yet") + "\n")
		return s.String()
	}

	done := 0
	for _, t := range wt.Insights.Todos {
		if t.Status == "completed" {
			done++
		}
	}
	counter := mutedStyle.Render(fmt.Sprintf("%d/%d", done, len(wt.Insights.Todos)))
	s.WriteString(" " + headingStyle.Render("progress") + "  " + counter + "\n\n")

	spin := spinnerFrames[m.animFrame%len(spinnerFrames)]
	labelW := width - 6
	for _, todo := range wt.Insights.Todos {
		var glyph, label string
		switch todo.Status {
		case "completed":
			glyph = successStyle.Render("✓")
			label = mutedStyle.Render(truncate(todo.Content, labelW))
		case "in_progress":
			glyph = workingStyle.Render(spin)
			text := todo.ActiveForm
			if text == "" {
				text = todo.Content
			}
			label = textStyle.Bold(true).Render(truncate(text, labelW))
		default:
			glyph = dimStyle.Render("○")
			label = dimStyle.Render(truncate(todo.Content, labelW))
		}
		s.WriteString("   " + glyph + " " + label + "\n")
	}
	return s.String()
}

// renderInsightsCost is the cost tab: total spend, model-tier breakdown,
// burn rate, projected cost, turn count, context bar, token sparkline.
func (m model) renderInsightsCost(wt internal.Worktree, width int) string {
	var s strings.Builder
	ins := wt.Insights
	s.WriteString("\n")

	// Total cost row.
	if ins.CostUSD > 0 {
		s.WriteString(kvRow(" total", successStyle.Render(fmt.Sprintf("$%.2f", ins.CostUSD)), width) + "\n")
	} else {
		s.WriteString(kvRow(" total", mutedStyle.Render("$0.00"), width) + "\n")
	}

	// Per-tier cost — only show tiers with non-zero spend.
	if len(ins.CostByTier) > 0 {
		for _, tier := range []string{"opus", "sonnet", "haiku"} {
			if c, ok := ins.CostByTier[tier]; ok && c > 0 {
				s.WriteString(kvRow("   "+tier, textStyle.Render(fmt.Sprintf("$%.2f", c)), width) + "\n")
			}
		}
	}

	// Burn rate + projection.
	if rate, proj := costRateAndProjection(ins); rate > 0 {
		label := fmt.Sprintf("$%.2f/hr · proj $%.2f", rate, proj)
		s.WriteString(kvRow(" rate", textStyle.Render(label), width) + "\n")
	}

	// Turn count.
	if ins.TotalTurns > 0 {
		s.WriteString(kvRow(" turns", textStyle.Render(fmt.Sprintf("%d", ins.TotalTurns)), width) + "\n")
	}

	// Context bar.
	s.WriteString("\n")
	s.WriteString(renderContextRow(ins, width-2) + "\n")

	// Token velocity sparkline.
	if len(ins.TokenSamples) >= 2 {
		s.WriteString("\n")
		s.WriteString(" " + headingStyle.Render("token trend") + "\n")
		s.WriteString("   " + renderSparkline(ins.TokenSamples, width-6) + "\n")
	}

	// Session age.
	if !ins.SessionStart.IsZero() {
		age := time.Since(ins.SessionStart)
		s.WriteString("\n")
		s.WriteString(kvRow(" session age", mutedStyle.Render(formatDuration(age)), width) + "\n")
	}

	return s.String()
}

// truncate clips s to at most maxW visual columns, appending "…" if clipped.
func truncate(s string, maxW int) string {
	if lipgloss.Width(s) <= maxW {
		return s
	}
	// Walk runes so we don't slice mid-codepoint.
	w := 0
	for i, r := range s {
		rw := 1
		if r > 0x7F {
			rw = 2 // conservative estimate for wide chars
		}
		if w+rw > maxW-1 {
			return s[:i] + "…"
		}
		w += rw
	}
	return s
}

func insightsSessionLabel(ins insights.Insights) string {
	// Prefer the AI-generated title when present — it's the most human-readable
	// of the three. Fall back to user-supplied slug, then the JSONL filename's
	// first 8 chars.
	if ins.SessionTitle != "" {
		return textStyle.Render(ins.SessionTitle)
	}
	if ins.Slug != "" {
		return textStyle.Render(ins.Slug)
	}
	if ins.SessionID != "" {
		s := ins.SessionID
		if len(s) > 8 {
			s = s[:8]
		}
		return mutedStyle.Render(s)
	}
	return mutedStyle.Render("—")
}

func renderPRSection(pr *github.PRInfo, width int) string {
	var s strings.Builder
	s.WriteString("\n")
	s.WriteString(" " + headingStyle.Render("pull request") + "\n")

	stateLabel := strings.ToLower(string(pr.State))
	badge := prCellStyle(pr).Render(prGlyph(pr) + " #" + fmt.Sprintf("%d", pr.Number))
	titleW := width - lipgloss.Width(badge) - lipgloss.Width(stateLabel) - 8
	if titleW < 4 {
		titleW = 4
	}
	title := pr.Title
	if lipgloss.Width(title) > titleW {
		title = title[:titleW-1] + "…"
	}
	s.WriteString("   " + badge + " " + mutedStyle.Render(stateLabel) + " " + mutedStyle.Render("·") + " " + textStyle.Render(title) + "\n")

	url := pr.URL
	if lipgloss.Width(url) > width-4 {
		url = url[:width-5] + "…"
	}
	s.WriteString("   " + dimStyle.Render(url) + "\n")
	return s.String()
}

func kvRow(label, value string, width int) string {
	labelCell := mutedStyle.Render(label)
	lw := lipgloss.Width(labelCell)
	vw := lipgloss.Width(value)
	gap := width - lw - vw - 1
	if gap < 1 {
		gap = 1
	}
	return labelCell + strings.Repeat(" ", gap) + value + " "
}

func renderContextRow(ins insights.Insights, width int) string {
	var percent float64
	var label string
	if ins.InputTokens > 0 {
		maxTokens := contextMaxTokens(ins.Model)
		percent = float64(ins.InputTokens) / float64(maxTokens)
		tokenK := ins.InputTokens / 1000
		label = fmt.Sprintf("%d%% · %dk/%dk", int(percent*100), tokenK, maxTokens/1000)
	} else {
		const maxSize int64 = 10 * 1024 * 1024
		if maxSize > 0 {
			percent = float64(ins.SessionSize) / float64(maxSize)
		}
		label = fmt.Sprintf("%d%%", int(percent*100))
	}

	// Burn-rate warning: when context is past 75% AND climbing meaningfully
	// across recent samples, flag a /compact hint so the user has time to act
	// before the next big tool result blows the window.
	var warning string
	if percent >= 0.75 && contextRising(ins.TokenSamples) {
		warning = " " + errorStyle.Render("⚠ /compact?")
	}

	labelW := lipgloss.Width(label) + lipgloss.Width(warning)
	barW := width - labelW - 3
	if barW < 8 {
		barW = 8
	}
	bar := renderSmoothBar(percent, barW)
	return " " + bar + " " + mutedStyle.Render(label) + warning
}

// contextRising returns true if the last sample is meaningfully larger than
// the median of recent samples — a heuristic for "growing fast" without
// rate-of-change math.
func contextRising(samples []int) bool {
	if len(samples) < 3 {
		return false
	}
	last := samples[len(samples)-1]
	// Pick the median-ish baseline from the prior samples.
	prior := append([]int{}, samples[:len(samples)-1]...)
	sort.Ints(prior)
	mid := prior[len(prior)/2]
	if mid == 0 {
		return false
	}
	// At least 5% increase over the median.
	return last > mid+mid/20
}

// renderSmoothBar renders a horizontal progress bar with 8x sub-cell precision.
func renderSmoothBar(percent float64, width int) string {
	if percent < 0 {
		percent = 0
	}
	if percent > 1 {
		percent = 1
	}

	partials := []string{"", "▏", "▎", "▍", "▌", "▋", "▊", "▉"}
	totalEighths := int(percent * float64(width) * 8)
	fullCells := totalEighths / 8
	remainder := totalEighths % 8
	if fullCells > width {
		fullCells = width
		remainder = 0
	}

	var fg color.Color
	switch {
	case percent >= 0.8:
		fg = colDanger
	case percent >= 0.5:
		fg = colWarn
	default:
		fg = colSuccess
	}
	filledStyle := lipgloss.NewStyle().Foreground(fg)
	trackStyle := lipgloss.NewStyle().Foreground(colFaint)

	var b strings.Builder
	b.WriteString(filledStyle.Render(strings.Repeat("█", fullCells)))
	remain := width - fullCells
	if remainder > 0 && remain > 0 {
		b.WriteString(filledStyle.Render(partials[remainder]))
		remain--
	}
	if remain > 0 {
		b.WriteString(trackStyle.Render(strings.Repeat("─", remain)))
	}
	return b.String()
}

// --- Overlays: help and message prompt ---

func (m model) viewHelp(width int) string {
	w := 60
	if width < w+4 {
		w = width - 4
	}

	var content strings.Builder

	bindings := []struct {
		section string
		keys    []struct{ key, desc string }
	}{
		{"navigation", []struct{ key, desc string }{
			{"j/k, ↑/↓", "move cursor"},
			{"g / G", "jump to first / last"},
			{"ctrl+d/u", "half-page down / up"},
			{"w", "jump to next working/waiting"},
			{"enter, o", "open claude (attaches bg session if one exists)"},
			{"ctrl+b d", "detach from claude — leaves the bg session running"},
			{"tab, i", "toggle insights panel"},
			{"1-5", "switch insights tab (overview/activity/git/todos/cost)"},
			{"[ / ]", "scroll insights up / down"},
			{"esc", "back out of overlays / filters"},
			{"q, ctrl+c", "quit"},
		}},
		{"actions", []struct{ key, desc string }{
			{"n", "create new worktree"},
			{"D", "delete worktree (force)"},
			{"y", "yank worktree path"},
			{"r", "refresh insights now"},
			{"p", "open PR (insights) / refresh PR status"},
			{"c", "copy last prompt (insights only)"},
			{"l", "copy log path (insights only)"},
			{"?", "toggle this help"},
		}},
		{"search & sort", []struct{ key, desc string }{
			{"/", "search by branch/path"},
			{"s", "cycle sort"},
			{"f", "cycle status filter"},
			{"esc", "clear filter / cancel"},
		}},
		{"archive", []struct{ key, desc string }{
			{"x", "archive / unarchive"},
			{"X", "toggle show archived"},
		}},
	}

	for i, section := range bindings {
		if i > 0 {
			content.WriteString("\n")
		}
		content.WriteString(headingStyle.Render(section.section) + "\n")
		for _, b := range section.keys {
			keyCell := selectedStyle.Render(b.key)
			pad := 14 - lipgloss.Width(keyCell)
			if pad < 1 {
				pad = 1
			}
			content.WriteString("  " + keyCell + strings.Repeat(" ", pad) + mutedStyle.Render(b.desc) + "\n")
		}
	}

	framed := renderFrame(content.String(), w+2, "keybindings")
	return "\n" + m.renderTopBar(width) + "\n\n" + framed + "\n" + mutedStyle.Render(" [?] close  [q] quit") + "\n"
}

