package insights

import (
	"bufio"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/trebaud/mori/internal/git"
)

type StatusType string

const (
	StatusWorking StatusType = "WORKING"
	StatusIdle    StatusType = "IDLE"
	StatusWait    StatusType = "WAITING"
	StatusNone    StatusType = "NONE"
)

// TodoItem is one entry from a TodoWrite tool call.
type TodoItem struct {
	Content    string
	ActiveForm string
	Status     string // "pending" | "in_progress" | "completed"
}

// FileEdit records a file path that the agent edited during the session, with
// the number of edit-class tool calls against it. Order in Insights.FilesTouched
// is most-recently-edited first.
type FileEdit struct {
	Path  string
	Edits int
}

// SubAgentRun is one Task tool_use call. Running stays true until the matching
// tool_result lands in a later user message.
type SubAgentRun struct {
	Type        string
	Description string
	Running     bool
}

// ErrorDetail captures the most recent tool_result that came back with
// is_error=true, along with the originating tool's name.
type ErrorDetail struct {
	Tool string
	Msg  string
}

type Insights struct {
	Status        StatusType
	LastActivity  time.Time
	SessionSize   int64
	CurrentTask   string
	SessionTitle  string // AI-generated title from ai-title records
	LastPrompt    string // Most recent user prompt from last-prompt records
	Todos         []TodoItem
	SessionID     string
	Slug          string
	Model         string
	Mode          string
	LastTool      string
	CostUSD       float64
	TurnDurationS int
	MessageCount  int
	GitLog        []string
	Available     bool

	InputTokens int    // latest input token count for context estimation
	HasError    bool   // last tool_result had is_error
	AheadBehind string // e.g. "+3/-0" relative to main

	// Phase-1 additions: latent JSONL signal.
	FilesTouched      []FileEdit
	ToolCounts        map[string]int     // per-tool call count, key is tool name (Edit/Read/Bash/mcp__.../Task/...)
	SubAgents         []SubAgentRun      // Task spawns in order seen
	LastSlashCommands []string           // last 3 slash commands user typed (e.g. "/loop")
	TotalTurns        int                // count of system/turn_duration entries
	ErrorDetail       ErrorDetail        // last failed tool's name + first error line
	PlanModeActive    bool               // EnterPlanMode without matching ExitPlanMode
	PendingQuestion   string             // unanswered AskUserQuestion text
	LastCompactedAt   time.Time          // wall time of the most recent compaction marker
	CostByTier        map[string]float64 // cost split by ModelTier (opus/sonnet/haiku)

	// Phase-3 additions: derived metrics.
	SessionStart  time.Time // timestamp of first JSONL entry, used for cost/hour
	DiffShortstat string    // git diff --shortstat HEAD, e.g. "3 files · +127/-44"
	LogPath       string    // path to the underlying JSONL file
	TokenSamples  []int     // ring buffer of recent InputTokens samples (per-turn)
}

func ClaudeProjectPath(worktreePath string) string {
	encoded := strings.ReplaceAll(worktreePath, "/", "-")
	encoded = strings.ReplaceAll(encoded, ".", "-")
	return encoded
}

type cachedInsights struct {
	file    string
	modTime time.Time
	size    int64
	headRef string

	insights Insights
}

var cache = make(map[string]*cachedInsights)

func GetInsights(worktreePath string) Insights {
	file, modTime := newestSessionFile(worktreePath)
	headRef := git.HeadRef(worktreePath)

	if prev := cache[worktreePath]; prev.unchanged(file, modTime, headRef) {
		return prev.insights
	}

	result := buildInsights(worktreePath, file, modTime, headRef)
	cache[worktreePath] = &cachedInsights{
		file: file, modTime: modTime, size: result.SessionSize,
		headRef: headRef, insights: result,
	}
	return result
}

func (c *cachedInsights) unchanged(file string, modTime time.Time, headRef string) bool {
	if c == nil || c.file != file || c.headRef != headRef {
		return false
	}
	return file == "" || (c.modTime == modTime && c.size == fileSize(file))
}

func newestSessionFile(worktreePath string) (string, time.Time) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", time.Time{}
	}
	entries, err := os.ReadDir(filepath.Join(home, ".claude", "projects", ClaudeProjectPath(worktreePath)))
	if err != nil {
		return "", time.Time{}
	}

	var best string
	var bestTime time.Time
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".jsonl") {
			continue
		}
		info, err := e.Info()
		if err != nil {
			continue
		}
		if info.ModTime().After(bestTime) {
			bestTime = info.ModTime()
			best = filepath.Join(filepath.Dir(best), e.Name())
		}
	}
	if best == "" {
		return "", time.Time{}
	}
	dir := filepath.Join(home, ".claude", "projects", ClaudeProjectPath(worktreePath))
	return filepath.Join(dir, best), bestTime
}

func fileSize(path string) int64 {
	info, err := os.Stat(path)
	if err != nil {
		return 0
	}
	return info.Size()
}

func gitData(worktreePath, headRef string) (logLines []string, aheadBehind, diffStat string) {
	prev := cache[worktreePath]
	if prev != nil && prev.headRef == headRef && headRef != "" {
		return prev.insights.GitLog, prev.insights.AheadBehind, prev.insights.DiffShortstat
	}
	return git.Log(worktreePath, 5), git.AheadBehind(worktreePath), git.DiffShortstat(worktreePath)
}

func buildInsights(worktreePath, file string, modTime time.Time, headRef string) Insights {
	gitLog, aheadBehind, diffStat := gitData(worktreePath, headRef)

	if file == "" {
		return Insights{Status: StatusNone, Available: true, GitLog: gitLog, AheadBehind: aheadBehind, DiffShortstat: diffStat}
	}

	parsed := scanSession(file)

	// A session reads as WORKING when its last turn is a user message — i.e. the
	// agent is mid-response. But a live agent appends to its JSONL continuously,
	// so if the file hasn't changed in a while the session was abandoned (the
	// user quit before the agent replied) and we'd otherwise show WORKING
	// forever. Downgrade stale WORKING sessions to IDLE.
	if parsed.status == StatusWorking && time.Since(modTime) > workingStaleAfter {
		parsed.status = StatusIdle
	}

	// Token sample ring buffer: append latest tokens to the previous cache.
	var samples []int
	if prev := cache[worktreePath]; prev != nil {
		samples = append(samples, prev.insights.TokenSamples...)
	}
	if parsed.inputTokens > 0 {
		// Only append when the count changed — sequential calls inside the
		// same turn shouldn't pollute the trend.
		if n := len(samples); n == 0 || samples[n-1] != parsed.inputTokens {
			samples = append(samples, parsed.inputTokens)
		}
	}
	if len(samples) > 20 {
		samples = samples[len(samples)-20:]
	}

	return Insights{
		Status:        parsed.status,
		LastActivity:  modTime,
		SessionSize:   fileSize(file),
		CurrentTask:   parsed.task,
		SessionTitle:  parsed.sessionTitle,
		LastPrompt:    parsed.lastPrompt,
		Todos:         parsed.todos,
		SessionID:     strings.TrimSuffix(filepath.Base(file), ".jsonl"),
		Slug:          parsed.slug,
		Model:         parsed.model,
		Mode:          parsed.mode,
		LastTool:      parsed.lastTool,
		CostUSD:       parsed.cost,
		TurnDurationS: parsed.turnDurationS,
		MessageCount:  parsed.messageCount,
		InputTokens:   parsed.inputTokens,
		HasError:      parsed.hasError,
		GitLog:        gitLog,
		AheadBehind:   aheadBehind,
		Available:     true,

		FilesTouched:      parsed.filesTouched,
		ToolCounts:        parsed.toolCounts,
		SubAgents:         parsed.subAgents,
		LastSlashCommands: parsed.lastSlashCommands,
		TotalTurns:        parsed.totalTurns,
		ErrorDetail:       parsed.errorDetail,
		PlanModeActive:    parsed.planModeActive,
		PendingQuestion:   parsed.pendingQuestion,
		LastCompactedAt:   parsed.lastCompactedAt,
		CostByTier:        parsed.costByTier,

		SessionStart:  parsed.sessionStart,
		DiffShortstat: diffStat,
		LogPath:       file,
		TokenSamples:  samples,
	}
}

type sessionData struct {
	status        StatusType
	task          string
	sessionTitle  string
	lastPrompt    string
	todos         []TodoItem
	slug          string
	model         string
	mode          string
	lastTool      string
	cost          float64
	turnDurationS int
	messageCount  int
	inputTokens   int
	hasError      bool

	filesTouched      []FileEdit
	toolCounts        map[string]int
	subAgents         []SubAgentRun
	lastSlashCommands []string
	totalTurns        int
	errorDetail       ErrorDetail
	planModeActive    bool
	pendingQuestion   string
	lastCompactedAt   time.Time
	costByTier        map[string]float64
	sessionStart      time.Time
}

const maxScanBytes = 10 * 1024 * 1024 // 10 MB tail limit for large session files

// workingStaleAfter is how long a session can sit on a trailing user turn
// before we stop treating it as WORKING. Time-to-first-token after a prompt is
// seconds even with extended thinking, so anything older is an abandoned
// session rather than a live one.
const workingStaleAfter = 5 * time.Minute

func scanSession(path string) sessionData {
	file, err := os.Open(path)
	if err != nil {
		return sessionData{status: StatusIdle}
	}
	defer file.Close()

	// For very large files, only read the tail to avoid excessive memory usage.
	// Cost will be approximate but the process won't get OOM-killed.
	if info, err := file.Stat(); err == nil && info.Size() > maxScanBytes {
		file.Seek(info.Size()-maxScanBytes, 0)
		// Discard the first partial line after seeking.
		buf := bufio.NewReader(file)
		buf.ReadBytes('\n')
		scanner := bufio.NewScanner(buf)
		scanner.Buffer(make([]byte, 1024*1024), 1024*1024)
		return scanFromReader(scanner)
	}

	scanner := bufio.NewScanner(file)
	scanner.Buffer(make([]byte, 1024*1024), 1024*1024)
	return scanFromReader(scanner)
}

func scanFromReader(scanner *bufio.Scanner) sessionData {
	var result sessionData
	var lastConvType string // only "user" or "assistant" — metadata entries don't affect status
	var lastAssistantLine string
	var lastUserTask string

	// Track tool_use ↔ tool_result pairs across entries so we can attribute
	// errors back to the tool name and detect when sub-agents / questions resolve.
	toolUseByID := map[string]string{}     // id → tool name
	subAgentByID := map[string]int{}       // id → index in result.subAgents
	askQuestionByID := map[string]string{} // id → question text (unanswered)
	askOrder := []string{}                 // insertion-ordered ids so we pick the latest unanswered question at the end

	var entry struct {
		Type       string `json:"type"`
		Subtype    string `json:"subtype"`
		Slug       string `json:"slug"`
		AiTitle    string `json:"aiTitle"`
		LastPrompt string `json:"lastPrompt"`
		Timestamp  string `json:"timestamp"`
		Message    struct {
			Model   string          `json:"model"`
			Role    string          `json:"role"`
			Content json.RawMessage `json:"content"`
			Usage   struct {
				InputTokens              int `json:"input_tokens"`
				OutputTokens             int `json:"output_tokens"`
				CacheCreationInputTokens int `json:"cache_creation_input_tokens"`
				CacheReadInputTokens     int `json:"cache_read_input_tokens"`
			} `json:"usage"`
		} `json:"message"`
		PermissionMode string `json:"permissionMode"`
		DurationMs     int    `json:"durationMs"`
		MessageCount   int    `json:"messageCount"`
	}

	for scanner.Scan() {
		line := scanner.Text()
		if err := json.Unmarshal([]byte(line), &entry); err != nil {
			continue
		}

		ts := parseTimestamp(entry.Timestamp)
		if result.sessionStart.IsZero() && !ts.IsZero() {
			result.sessionStart = ts
		}

		if entry.Slug != "" {
			result.slug = entry.Slug
		}

		switch entry.Type {
		case "ai-title":
			if entry.AiTitle != "" {
				result.sessionTitle = entry.AiTitle
			}
		case "last-prompt":
			if entry.LastPrompt != "" {
				result.lastPrompt = entry.LastPrompt
			}
		case "permission-mode":
			if entry.PermissionMode != "" {
				result.mode = entry.PermissionMode
			}
		case "user":
			lastConvType = "user"
			if entry.PermissionMode != "" {
				result.mode = entry.PermissionMode
			}
			ci := parseContent(entry.Message.Content)
			if ci.text != "" {
				lastUserTask = ci.text
				// User messages land in the JSONL immediately; the matching
				// `last-prompt` metadata record is only written at end-of-turn.
				// Use the user message so the prompt panel updates right away.
				result.lastPrompt = ci.text
				if cmd := slashCommand(ci.text); cmd != "" {
					result.lastSlashCommands = appendSlashCommand(result.lastSlashCommands, cmd)
				}
			}
			// tool_results sit in user messages — they resolve prior tool_uses.
			for _, tr := range ci.toolResults {
				name := toolUseByID[tr.ToolUseID]
				if tr.IsError {
					result.hasError = true
					result.errorDetail = ErrorDetail{Tool: name, Msg: tr.FirstLine}
				} else if name == "" || name != "Task" {
					// A clean result clears the error flag only if it isn't from
					// a sub-agent that may itself have failed downstream.
					result.hasError = false
				}
				if idx, ok := subAgentByID[tr.ToolUseID]; ok {
					result.subAgents[idx].Running = false
					delete(subAgentByID, tr.ToolUseID)
				}
				if _, ok := askQuestionByID[tr.ToolUseID]; ok {
					delete(askQuestionByID, tr.ToolUseID)
				}
			}
		case "assistant":
			lastConvType = "assistant"
			lastAssistantLine = line
			if entry.Message.Model != "" {
				result.model = entry.Message.Model
			}
			ci := parseContent(entry.Message.Content)

			for _, tu := range ci.toolUses {
				if result.toolCounts == nil {
					result.toolCounts = make(map[string]int)
				}
				result.toolCounts[tu.Name]++
				result.lastTool = tu.Name
				if tu.ID != "" {
					toolUseByID[tu.ID] = tu.Name
				}

				switch tu.Name {
				case "Edit", "Write", "MultiEdit", "NotebookEdit":
					if p := extractFilePath(tu.Input); p != "" {
						result.filesTouched = recordFileEdit(result.filesTouched, p)
					}
				case "Task":
					sa := extractSubAgent(tu.Input)
					sa.Running = true
					result.subAgents = append(result.subAgents, sa)
					if tu.ID != "" {
						subAgentByID[tu.ID] = len(result.subAgents) - 1
					}
				case "AskUserQuestion":
					if q := extractFirstQuestion(tu.Input); q != "" && tu.ID != "" {
						askQuestionByID[tu.ID] = q
						askOrder = append(askOrder, tu.ID)
					}
				case "ExitPlanMode":
					result.planModeActive = false
				}

				if tu.Name == "EnterPlanMode" {
					result.planModeActive = true
				}
			}

			if todos := parseTodos(entry.Message.Content); todos != nil {
				result.todos = todos
			}

			if entry.Message.Usage.InputTokens > 0 {
				result.inputTokens = entry.Message.Usage.InputTokens +
					entry.Message.Usage.CacheCreationInputTokens +
					entry.Message.Usage.CacheReadInputTokens
			}

			tier := ModelTier(entry.Message.Model)
			p := pricing[tier]
			u := entry.Message.Usage
			turnCost := float64(u.InputTokens) * p.input / 1_000_000
			turnCost += float64(u.OutputTokens) * p.output / 1_000_000
			turnCost += float64(u.CacheCreationInputTokens) * p.cacheWrite / 1_000_000
			turnCost += float64(u.CacheReadInputTokens) * p.cacheRead / 1_000_000
			result.cost += turnCost
			if turnCost > 0 {
				if result.costByTier == nil {
					result.costByTier = make(map[string]float64)
				}
				result.costByTier[tier] += turnCost
			}
		case "system":
			switch entry.Subtype {
			case "turn_duration":
				result.turnDurationS = entry.DurationMs / 1000
				result.messageCount = entry.MessageCount
				result.totalTurns++
			case "compaction", "compact", "summary":
				if !ts.IsZero() {
					result.lastCompactedAt = ts
				}
			}
		}
	}

	if lastUserTask != "" {
		if len(lastUserTask) > 80 {
			result.task = lastUserTask[:80] + "..."
		} else {
			result.task = lastUserTask
		}
	}

	// Pick the most recent still-unanswered AskUserQuestion as PendingQuestion.
	for i := len(askOrder) - 1; i >= 0; i-- {
		if q, ok := askQuestionByID[askOrder[i]]; ok {
			result.pendingQuestion = q
			break
		}
	}

	// Derive status from the last conversation turn (user/assistant only).
	// Metadata entries like ai-title, last-prompt, attachment, queue-operation
	// are written around conversation entries and must not override the status.
	switch lastConvType {
	case "user":
		result.status = StatusWorking
	case "assistant":
		if strings.Contains(lastAssistantLine, "AskUserQuestion") || result.pendingQuestion != "" {
			result.status = StatusWait
		} else {
			result.status = StatusIdle
		}
	default:
		result.status = StatusIdle
	}

	return result
}

type toolUseBlock struct {
	ID    string
	Name  string
	Input json.RawMessage
}

type toolResultBlock struct {
	ToolUseID string
	IsError   bool
	FirstLine string
}

type contentInfo struct {
	text        string // first text block (or plain string content)
	toolUses    []toolUseBlock
	toolResults []toolResultBlock
}

func parseContent(raw json.RawMessage) contentInfo {
	var info contentInfo

	// Try plain string first (user messages can be a simple string).
	var s string
	if json.Unmarshal(raw, &s) == nil && s != "" {
		info.text = s
		return info
	}

	// Otherwise parse as block array once.
	var blocks []struct {
		Type      string          `json:"type"`
		Text      string          `json:"text"`
		Name      string          `json:"name"`
		ID        string          `json:"id"`
		Input     json.RawMessage `json:"input"`
		ToolUseID string          `json:"tool_use_id"`
		Content   json.RawMessage `json:"content"`
		IsError   bool            `json:"is_error"`
	}
	if json.Unmarshal(raw, &blocks) != nil {
		return info
	}

	for _, b := range blocks {
		switch b.Type {
		case "text":
			if info.text == "" && b.Text != "" {
				info.text = b.Text
			}
		case "tool_use":
			if b.Name != "" {
				info.toolUses = append(info.toolUses, toolUseBlock{
					ID: b.ID, Name: b.Name, Input: b.Input,
				})
			}
		case "tool_result":
			tr := toolResultBlock{ToolUseID: b.ToolUseID, IsError: b.IsError}
			if b.IsError {
				tr.FirstLine = firstLineOfResultContent(b.Content)
			}
			info.toolResults = append(info.toolResults, tr)
		}
	}
	return info
}

// firstLineOfResultContent extracts the first non-empty line of a tool_result's
// content, which can be either a plain string or a [{type: "text", text: ...}]
// block array.
func firstLineOfResultContent(raw json.RawMessage) string {
	var s string
	if json.Unmarshal(raw, &s) == nil && s != "" {
		return firstLine(s)
	}
	var blocks []struct {
		Type string `json:"type"`
		Text string `json:"text"`
	}
	if json.Unmarshal(raw, &blocks) == nil {
		for _, b := range blocks {
			if b.Type == "text" && b.Text != "" {
				return firstLine(b.Text)
			}
		}
	}
	return ""
}

func firstLine(s string) string {
	s = strings.TrimSpace(s)
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		s = s[:i]
	}
	if len(s) > 120 {
		s = s[:120] + "…"
	}
	return s
}

// parseTodos extracts the todo list from the last TodoWrite tool_use block in
// an assistant message's content. Returns nil if no TodoWrite is found.
func parseTodos(raw json.RawMessage) []TodoItem {
	var blocks []struct {
		Type  string          `json:"type"`
		Name  string          `json:"name"`
		Input json.RawMessage `json:"input"`
	}
	if json.Unmarshal(raw, &blocks) != nil {
		return nil
	}
	var result []TodoItem
	for _, b := range blocks {
		if b.Type != "tool_use" || b.Name != "TodoWrite" {
			continue
		}
		var input struct {
			Todos []struct {
				Content    string `json:"content"`
				ActiveForm string `json:"activeForm"`
				Status     string `json:"status"`
			} `json:"todos"`
		}
		if json.Unmarshal(b.Input, &input) != nil {
			continue
		}
		result = make([]TodoItem, len(input.Todos))
		for i, t := range input.Todos {
			result[i] = TodoItem{
				Content:    t.Content,
				ActiveForm: t.ActiveForm,
				Status:     t.Status,
			}
		}
	}
	return result
}

func extractFilePath(raw json.RawMessage) string {
	var input struct {
		FilePath     string `json:"file_path"`
		NotebookPath string `json:"notebook_path"`
	}
	if json.Unmarshal(raw, &input) != nil {
		return ""
	}
	if input.FilePath != "" {
		return input.FilePath
	}
	return input.NotebookPath
}

func extractSubAgent(raw json.RawMessage) SubAgentRun {
	var input struct {
		SubagentType string `json:"subagent_type"`
		Description  string `json:"description"`
	}
	_ = json.Unmarshal(raw, &input)
	return SubAgentRun{Type: input.SubagentType, Description: input.Description}
}

func extractFirstQuestion(raw json.RawMessage) string {
	var input struct {
		Questions []struct {
			Question string `json:"question"`
		} `json:"questions"`
	}
	if json.Unmarshal(raw, &input) != nil {
		return ""
	}
	if len(input.Questions) == 0 {
		return ""
	}
	return input.Questions[0].Question
}

// recordFileEdit increments the edit counter for path and moves it to the
// front of the slice so callers can take the head for "recently edited".
func recordFileEdit(files []FileEdit, path string) []FileEdit {
	for i, f := range files {
		if f.Path == path {
			f.Edits++
			// Move to front so most-recently-edited comes first.
			out := make([]FileEdit, 0, len(files))
			out = append(out, f)
			out = append(out, files[:i]...)
			out = append(out, files[i+1:]...)
			return out
		}
	}
	return append([]FileEdit{{Path: path, Edits: 1}}, files...)
}

// appendSlashCommand keeps the last 3 slash commands in chronological order,
// dropping duplicates that appear consecutively.
func appendSlashCommand(list []string, cmd string) []string {
	if n := len(list); n > 0 && list[n-1] == cmd {
		return list
	}
	list = append(list, cmd)
	if len(list) > 3 {
		list = list[len(list)-3:]
	}
	return list
}

// slashCommand returns the leading slash command (e.g. "/loop") in a user
// message, or "" if the message doesn't start with one.
func slashCommand(text string) string {
	t := strings.TrimSpace(text)
	if !strings.HasPrefix(t, "/") {
		return ""
	}
	cmd := strings.SplitN(t, " ", 2)[0]
	// Strip anything past a newline too.
	if i := strings.IndexAny(cmd, "\r\n\t"); i >= 0 {
		cmd = cmd[:i]
	}
	if len(cmd) <= 1 || len(cmd) > 40 {
		return ""
	}
	return cmd
}

func parseTimestamp(s string) time.Time {
	if s == "" {
		return time.Time{}
	}
	if t, err := time.Parse(time.RFC3339Nano, s); err == nil {
		return t
	}
	if t, err := time.Parse(time.RFC3339, s); err == nil {
		return t
	}
	return time.Time{}
}

type modelPricing struct {
	input      float64
	output     float64
	cacheWrite float64
	cacheRead  float64
}

var pricing = map[string]modelPricing{
	"opus":   {input: 15.0, output: 75.0, cacheWrite: 18.75, cacheRead: 1.50},
	"sonnet": {input: 3.0, output: 15.0, cacheWrite: 3.75, cacheRead: 0.30},
	"haiku":  {input: 0.80, output: 4.0, cacheWrite: 1.0, cacheRead: 0.08},
}

// ModelTier returns the pricing tier name for a given model string.
func ModelTier(model string) string {
	m := strings.ToLower(model)
	if strings.Contains(m, "opus") {
		return "opus"
	}
	if strings.Contains(m, "haiku") {
		return "haiku"
	}
	return "sonnet"
}
