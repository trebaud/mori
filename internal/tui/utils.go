package tui

import (
	"fmt"
	"path/filepath"
	"sort"
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
	case d < 10*time.Second:
		return "just now"
	case d < time.Minute:
		return fmt.Sprintf("%ds ago", int(d.Seconds()))
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

// formatDuration is the "X ran for Y" variant of relativeTime — bare numbers
// without an "ago" suffix.
func formatDuration(d time.Duration) string {
	switch {
	case d < time.Minute:
		return fmt.Sprintf("%ds", int(d.Seconds()))
	case d < time.Hour:
		return fmt.Sprintf("%dm", int(d.Minutes()))
	case d < 24*time.Hour:
		h := d.Hours()
		if h < 10 {
			return fmt.Sprintf("%.1fh", h)
		}
		return fmt.Sprintf("%dh", int(h))
	default:
		return fmt.Sprintf("%dd", int(d.Hours()/24))
	}
}

// shortPath collapses a long absolute path so it reads at a glance. Files
// under the user's worktree show as "{repo}/{relpath}"; everything else falls
// back to the basename plus a parent.
func shortPath(p string) string {
	if p == "" {
		return ""
	}
	parts := strings.Split(p, string(filepath.Separator))
	if len(parts) <= 3 {
		return p
	}
	// Keep the last 3 path segments: repo/dir/file.
	tail := parts[len(parts)-3:]
	return "…/" + strings.Join(tail, "/")
}

// stripPromptBoilerplate trims polite preambles and code fences from a user
// prompt so the truncated preview shows actual intent.
func stripPromptBoilerplate(s string) string {
	s = strings.TrimSpace(s)
	for _, prefix := range []string{"Please ", "please ", "Can you ", "can you ", "Could you ", "could you "} {
		if strings.HasPrefix(s, prefix) {
			s = strings.TrimSpace(s[len(prefix):])
		}
	}
	// Drop leading fenced code block delimiter so the preview shows the first
	// useful line of text rather than ``` followed by a language tag.
	if strings.HasPrefix(s, "```") {
		if idx := strings.IndexByte(s, '\n'); idx >= 0 {
			s = strings.TrimSpace(s[idx+1:])
		}
	}
	return s
}

// renderModelTier returns a color-coded tier badge: opus warm, sonnet cool,
// haiku green. Helps the cost row register at a glance.
func renderModelTier(model string) string {
	tier := insights.ModelTier(model)
	style := mutedStyle
	switch tier {
	case "opus":
		style = lipgloss.NewStyle().Foreground(colDanger).Bold(true)
	case "sonnet":
		style = lipgloss.NewStyle().Foreground(colInfo)
	case "haiku":
		style = lipgloss.NewStyle().Foreground(colSuccess)
	}
	return style.Render(tier)
}

// renderStatusPillWithDuration renders the status pill with an inline
// duration suffix ("idle 2m", "working 45s") so the user can tell a 10-second
// pause from a 2-hour stall at a glance.
func renderStatusPillWithDuration(status insights.StatusType, last time.Time) string {
	glyph := statusIcon(status)
	text := strings.ToLower(string(status))
	if !last.IsZero() && status != insights.StatusNone {
		text += " · " + formatDuration(time.Since(last))
	}
	return statusStyle(status).Padding(0, 1).Render(glyph + " " + text)
}

// toolMixBucket categorizes a tool name into one of the display buckets.
func toolMixBucket(name string) string {
	switch name {
	case "Edit", "Write", "MultiEdit", "NotebookEdit":
		return "Edit"
	case "Read", "NotebookRead":
		return "Read"
	case "Bash":
		return "Bash"
	case "Grep", "Glob":
		return "Search"
	case "Task":
		return "Task"
	case "WebFetch", "WebSearch":
		return "Web"
	case "TodoWrite":
		return "Todo"
	case "AskUserQuestion":
		return "Ask"
	case "EnterPlanMode", "ExitPlanMode":
		return "Plan"
	}
	if strings.HasPrefix(name, "mcp__") {
		return "MCP"
	}
	return name
}

// renderToolMix collapses raw tool counts into a "Edit 12 · Read 30 · Bash 4"
// line, ordered by count desc.
func renderToolMix(counts map[string]int) string {
	if len(counts) == 0 {
		return ""
	}
	buckets := make(map[string]int)
	for name, n := range counts {
		buckets[toolMixBucket(name)] += n
	}
	type pair struct {
		name string
		n    int
	}
	pairs := make([]pair, 0, len(buckets))
	for name, n := range buckets {
		pairs = append(pairs, pair{name, n})
	}
	sort.Slice(pairs, func(i, j int) bool {
		if pairs[i].n != pairs[j].n {
			return pairs[i].n > pairs[j].n
		}
		return pairs[i].name < pairs[j].name
	})
	max := 6
	if len(pairs) < max {
		max = len(pairs)
	}
	parts := make([]string, 0, max)
	for i := 0; i < max; i++ {
		parts = append(parts, textStyle.Render(pairs[i].name)+" "+mutedStyle.Render(fmt.Sprintf("%d", pairs[i].n)))
	}
	return strings.Join(parts, mutedStyle.Render(" · "))
}

// renderSparkline draws a single-row braille sparkline of samples scaled to width.
func renderSparkline(samples []int, width int) string {
	if width < 2 || len(samples) == 0 {
		return ""
	}
	bars := []rune{'▁', '▂', '▃', '▄', '▅', '▆', '▇', '█'}
	// Downsample to width if needed.
	pts := samples
	if len(pts) > width {
		out := make([]int, width)
		for i := 0; i < width; i++ {
			start := i * len(pts) / width
			end := (i + 1) * len(pts) / width
			if end <= start {
				end = start + 1
			}
			sum := 0
			for j := start; j < end && j < len(pts); j++ {
				sum += pts[j]
			}
			out[i] = sum / (end - start)
		}
		pts = out
	}
	minV, maxV := pts[0], pts[0]
	for _, v := range pts {
		if v < minV {
			minV = v
		}
		if v > maxV {
			maxV = v
		}
	}
	span := maxV - minV
	var b strings.Builder
	for _, v := range pts {
		idx := 0
		if span > 0 {
			idx = (v - minV) * (len(bars) - 1) / span
		}
		b.WriteRune(bars[idx])
	}
	return textStyle.Render(b.String())
}

// costRateAndProjection returns the running $/hour spend and the projected
// total cost if the current rate persists until the session is 1 hour old.
// Both are 0 when we can't tell yet (session younger than 30 seconds, or no
// cost recorded).
func costRateAndProjection(ins insights.Insights) (rate, projection float64) {
	if ins.CostUSD <= 0 || ins.SessionStart.IsZero() {
		return 0, 0
	}
	elapsed := time.Since(ins.SessionStart)
	if elapsed < 30*time.Second {
		return 0, 0
	}
	rate = ins.CostUSD / elapsed.Hours()
	// Projection assumes another hour at the same rate.
	projection = ins.CostUSD + rate
	return
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
