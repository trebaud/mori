package tui

import (
	"fmt"
	"image/color"
	"strings"
	"time"

	"charm.land/bubbles/v2/key"
	"charm.land/bubbles/v2/textarea"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

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
	switch {
	case m.showHelp:
		base = m.viewHelp(totalWidth)
	case m.showInsights && totalWidth >= sideByMinWidth:
		base = m.viewSideBySide(totalWidth)
	case m.showInsights && totalWidth >= listOnlyMinWidth:
		base = m.viewStacked(totalWidth)
	default:
		base = m.viewListOnly(totalWidth)
	}

	out := base
	if m.mode == modeCreate || m.mode == modeMessage {
		out = m.applyOverlay(base, totalWidth)
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
	innerW := width - 2

	top := m.renderTopBar(width)
	warn := m.renderMissingToolsWarning(width)
	list := m.renderWorktreeList(innerW)
	framed := renderFrame(list, width, "worktrees")
	nyan := m.renderNyanBanner(width)

	footer := m.renderBelowList(width) + m.renderFooter(width-2)

	return "\n" + top + "\n" + warn + "\n" + framed + "\n" + nyan + "\n" + footer + "\n"
}

func (m model) viewStacked(width int) string {
	innerW := width - 2

	top := m.renderTopBar(width)
	warn := m.renderMissingToolsWarning(width)

	// Render the insights panel first to know how tall it is, then give the
	// list whatever vertical space is left so the footer stays anchored.
	insights := m.renderInsightsPanel(innerW - 2)
	insightsH := lipgloss.Height(insights)
	if insightsH < 1 {
		insightsH = 1
	}

	// top blank + topbar + blank + 2×(frame top+bottom) + nyan banner + status + footer + spacer
	const reserved = 10
	listH := m.height - reserved - insightsH
	if listH < 4 {
		listH = 4
	}
	list := m.renderWorktreeListWithHeight(innerW, listH)
	listFrame := renderFrame(list, width, "worktrees")

	insightsFrame := renderFrame(insights, width, "agent insights")

	nyan := m.renderNyanBanner(width)
	footer := m.renderBelowList(width) + m.renderFooter(width-2)

	return "\n" + top + "\n" + warn + "\n" + listFrame + "\n" + insightsFrame + "\n" + nyan + "\n" + footer + "\n"
}

func (m model) viewSideBySide(width int) string {
	rightW := insightsPaneWidth(width)
	leftW := width - rightW - 1

	leftContent := m.renderWorktreeList(leftW - 2)
	rightContent := m.renderInsightsPanel(rightW - 2)

	leftContent, rightContent = padToSameHeight(leftContent, rightContent)

	leftFrame := renderFrame(leftContent, leftW, "worktrees")
	rightFrame := renderFrame(rightContent, rightW, "agent insights")

	joined := lipgloss.JoinHorizontal(lipgloss.Top, leftFrame, " ", rightFrame)

	top := m.renderTopBar(width)
	warn := m.renderMissingToolsWarning(width)
	nyan := m.renderNyanBanner(width)
	footer := m.renderBelowList(width) + m.renderFooter(width-2)

	return "\n" + top + "\n" + warn + "\n" + joined + "\n" + nyan + "\n" + footer + "\n"
}

func (m model) renderMissingToolsWarning(width int) string {
	if len(m.missingTools) == 0 {
		return ""
	}
	msg := "⚠  " + strings.Join(m.missingTools, ", ") + " not found in PATH — some features unavailable"
	return " " + waitingStyle.Render(msg)
}

// renderNyanBanner is a fixed-height 1-row slot between the frame border and
// the footer. It always occupies exactly one line so the layout never shifts.
// When no agent is active it returns a blank line; when working it shows a
// small cat marching right across a rainbow trail.
func (m model) renderNyanBanner(width int) string {
	if width < 8 {
		return strings.Repeat(" ", width)
	}

	if !m.hasActiveAgent() {
		return strings.Repeat(" ", width)
	}

	// ANSI rainbow: red, yellow, green, cyan, blue, magenta
	trailColors := []color.Color{
		lipgloss.Color("1"),
		lipgloss.Color("3"),
		lipgloss.Color("2"),
		lipgloss.Color("6"),
		lipgloss.Color("4"),
		lipgloss.Color("5"),
	}
	n := len(trailColors)

	// 2-frame walk cycle
	cats := [2]string{"=^.^=", "=^-^="}
	cat := cats[m.animFrame%len(cats)]
	catW := lipgloss.Width(cat)

	maxX := width - catW
	if maxX < 1 {
		maxX = 1
	}
	catX := (m.animFrame * 4) % (maxX + 1)

	var b strings.Builder

	// Rainbow trail in 4-char color blocks.
	for block := 0; block*4 < catX; block++ {
		start := block * 4
		end := start + 4
		if end > catX {
			end = catX
		}
		s := lipgloss.NewStyle().Foreground(trailColors[(block+m.animFrame)%n])
		b.WriteString(s.Render(strings.Repeat("─", end-start)))
	}

	// Cat
	b.WriteString(workingStyle.Render(cat))

	// Trailing spaces to fill the reserved row width.
	if right := width - catX - catW; right > 0 {
		b.WriteString(strings.Repeat(" ", right))
	}

	return b.String()
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

// renderMessageCard is the floating multi-line "agent prompt" card.
func (m model) renderMessageCard(width int) string {
	cardW := width - 4
	if cardW > 100 {
		cardW = 100
	}
	if cardW < 34 {
		cardW = 34
	}

	wt := m.selectedWorktree()
	var target string
	if wt != nil {
		target = mutedStyle.Render("target ") + textStyle.Render(wt.Branch)
		if wt.Insights.SessionID != "" {
			target += mutedStyle.Render("  ·  resumes existing session")
		} else {
			target += mutedStyle.Render("  ·  starts new session")
		}
		target += mutedStyle.Render("  ·  --dangerously-skip-permissions")
	}

	keys := " " + mutedStyle.Render("[enter] launch  [shift+enter] newline  [ctrl+v] paste  [esc] cancel")

	var content strings.Builder
	if target != "" {
		content.WriteString(" " + target + "\n")
		content.WriteString(dimStyle.Render(strings.Repeat("─", cardW-2)) + "\n")
	}
	content.WriteString(m.messageInput.View())
	content.WriteString("\n")
	content.WriteString(keys)
	content.WriteString("\n")

	return renderFrame(content.String(), cardW, "agent prompt")
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
	case modeMessage:
		card = m.renderMessageCard(width)
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
		insightsHint = "[tab] hide"
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

	statusText := statusIcon(wt.Insights.Status) + " " + strings.ToLower(string(wt.Insights.Status))
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
	case wt.Insights.Status == insights.StatusWorking || wt.Insights.Status == insights.StatusWait:
		accentBar = lipgloss.NewStyle().Foreground(statusColor(wt.Insights.Status)).Render("▏")
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
	statusCellStyle = statusStyle(wt.Insights.Status).Width(statusW)
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

func (m model) renderInsightsPanel(width int) string {
	wt := m.selectedWorktree()
	if wt == nil {
		return "\n  " + mutedStyle.Render("no worktree selected") + "\n"
	}

	var s strings.Builder

	// Header: status pill + model/cost on the right.
	pill := renderStatusPill(wt.Insights.Status)
	var rightParts []string
	if wt.Insights.Model != "" {
		rightParts = append(rightParts, textStyle.Render(insights.ModelTier(wt.Insights.Model)))
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

	// Status detail — last tool, error, or relative time.
	var detail string
	switch {
	case wt.Insights.HasError:
		detail = errorStyle.Render("⚠ last tool errored")
	case wt.Insights.LastTool != "" && wt.Insights.Status == insights.StatusWorking:
		detail = mutedStyle.Render("running ") + textStyle.Render(wt.Insights.LastTool)
	case wt.Insights.Status == insights.StatusIdle && !wt.Insights.LastActivity.IsZero():
		detail = mutedStyle.Render("last active " + relativeTime(wt.Insights.LastActivity))
	}
	if detail != "" {
		s.WriteString(" " + detail + "\n")
	}

	// Context bar.
	s.WriteString("\n")
	s.WriteString(renderContextRow(wt.Insights, width-2) + "\n")

	// Key/value rows.
	s.WriteString("\n")
	s.WriteString(kvRow(" session", insightsSessionLabel(wt.Insights), width) + "\n")
	if wt.Insights.AheadBehind != "" {
		s.WriteString(kvRow(" branch", textStyle.Render(wt.Insights.AheadBehind), width) + "\n")
	}
	if wt.PR != nil && wt.PR.Number > 0 {
		s.WriteString(renderPRSection(wt.PR, width))
	}
	if wt.Insights.Status == insights.StatusWorking && (wt.Insights.TurnDurationS > 0 || wt.Insights.MessageCount > 0) {
		turn := fmt.Sprintf("%ds · %d msgs", wt.Insights.TurnDurationS, wt.Insights.MessageCount)
		s.WriteString(kvRow(" turn", textStyle.Render(turn), width) + "\n")
	}

	// Task.
	task := wt.Insights.CurrentTask
	if task == "" {
		task = "—"
	}
	s.WriteString("\n")
	s.WriteString(" " + headingStyle.Render("task") + "\n")
	for _, line := range wrapText(task, width-3) {
		s.WriteString("   " + textStyle.Render(line) + "\n")
	}

	// Recent commits.
	if len(wt.Insights.GitLog) > 0 {
		s.WriteString("\n")
		s.WriteString(" " + headingStyle.Render("recent commits") + "\n")
		for _, entry := range wt.Insights.GitLog {
			line := entry
			if lipgloss.Width(line) > width-5 {
				line = line[:width-6] + "…"
			}
			s.WriteString("   " + mutedStyle.Render("•") + " " + textStyle.Render(line) + "\n")
		}
	}

	return s.String()
}

func insightsSessionLabel(ins insights.Insights) string {
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

func renderStatusPill(status insights.StatusType) string {
	glyph := statusIcon(status)
	text := strings.ToLower(string(status))
	return statusStyle(status).Padding(0, 1).Render(glyph + " " + text)
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

	labelW := lipgloss.Width(label)
	barW := width - labelW - 3
	if barW < 8 {
		barW = 8
	}
	bar := renderSmoothBar(percent, barW)
	return " " + bar + " " + mutedStyle.Render(label)
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
			{"enter, o", "open claude code in worktree"},
			{"tab, i", "toggle insights panel"},
			{"esc", "back out of overlays / filters"},
			{"q, ctrl+c", "quit"},
		}},
		{"actions", []struct{ key, desc string }{
			{"n", "create new worktree"},
			{"D", "delete worktree (force)"},
			{"y", "yank (copy) worktree path"},
			{"m", "launch agent (claude --dangerously-skip-permissions, background)"},
			{"r", "refresh insights now"},
			{"p", "refresh PR status"},
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

// newMessageTextarea builds the multi-line prompt editor used by the "m" command.
// Enter submits; newlines go via shift+enter, alt+enter, or ctrl+j.
func newMessageTextarea() textarea.Model {
	ta := textarea.New()
	ta.Prompt = "│ "
	ta.ShowLineNumbers = false
	ta.CharLimit = 4000
	ta.MaxHeight = 0
	ta.SetHeight(8)
	ta.Placeholder = "Describe what the agent should do…"

	km := textarea.DefaultKeyMap()
	km.InsertNewline = key.NewBinding(
		key.WithKeys("shift+enter", "alt+enter", "ctrl+j"),
		key.WithHelp("shift+enter", "newline"),
	)
	ta.KeyMap = km
	return ta
}
