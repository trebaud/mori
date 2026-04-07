package agent

import (
	"bufio"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/moosecode/mori/internal/git"
)

type StatusType string

const (
	StatusWorking StatusType = "WORKING"
	StatusIdle    StatusType = "IDLE"
	StatusWait    StatusType = "WAITING"
	StatusNone    StatusType = "NONE"
)

type Insights struct {
	Status        StatusType
	LastActivity  time.Time
	SessionSize   int64
	CurrentTask   string
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
}

func ClaudeProjectPath(worktreePath string) string {
	encoded := strings.ReplaceAll(worktreePath, "/", "-")
	encoded = strings.ReplaceAll(encoded, ".", "-")
	return encoded
}

// insightsCache stores per-worktree cached insights to avoid redundant I/O.
type insightsCache struct {
	// JSONL cache key
	file    string
	modTime time.Time
	size    int64

	// Git cache key
	headRef string

	// Cached result
	insights Insights
}

var cache = make(map[string]*insightsCache)

func GetInsights(worktreePath string) Insights {
	home, err := os.UserHomeDir()
	if err != nil {
		return Insights{Status: StatusNone, Available: false}
	}

	projectDir := filepath.Join(home, ".claude", "projects", ClaudeProjectPath(worktreePath))

	entries, err := os.ReadDir(projectDir)
	if err != nil {
		return Insights{Status: StatusNone, Available: false}
	}

	var newestFile string
	var newestModTime time.Time

	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".jsonl") {
			continue
		}
		info, err := e.Info()
		if err != nil {
			continue
		}
		if info.ModTime().After(newestModTime) {
			newestModTime = info.ModTime()
			newestFile = filepath.Join(projectDir, e.Name())
		}
	}

	if newestFile == "" {
		// No session file — only need git data, still cacheable by HEAD ref
		prev := cache[worktreePath]
		headRef := git.HeadRef(worktreePath)
		if prev != nil && prev.file == "" && prev.headRef == headRef {
			return prev.insights
		}
		result := Insights{Status: StatusNone, Available: true, GitLog: git.Log(worktreePath, 5)}
		cache[worktreePath] = &insightsCache{headRef: headRef, insights: result}
		return result
	}

	info, _ := os.Stat(newestFile)
	sessionSize := int64(0)
	if info != nil {
		sessionSize = info.Size()
	}

	// Check cache: skip re-parse if file unchanged AND HEAD unchanged.
	headRef := git.HeadRef(worktreePath)
	prev := cache[worktreePath]
	if prev != nil && prev.file == newestFile && prev.modTime == newestModTime && prev.size == sessionSize && prev.headRef == headRef {
		return prev.insights
	}

	sessionID := strings.TrimSuffix(filepath.Base(newestFile), ".jsonl")
	parsed := scanSession(newestFile)

	// Reuse cached git data if HEAD hasn't changed.
	var gitLog []string
	var aheadBehind string
	if prev != nil && prev.headRef == headRef && prev.headRef != "" {
		gitLog = prev.insights.GitLog
		aheadBehind = prev.insights.AheadBehind
	} else {
		gitLog = git.Log(worktreePath, 5)
		aheadBehind = git.AheadBehind(worktreePath)
	}

	result := Insights{
		Status:        parsed.status,
		LastActivity:  newestModTime,
		SessionSize:   sessionSize,
		CurrentTask:   parsed.task,
		SessionID:     sessionID,
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
	}

	cache[worktreePath] = &insightsCache{
		file:     newestFile,
		modTime:  newestModTime,
		size:     sessionSize,
		headRef:  headRef,
		insights: result,
	}
	return result
}

type sessionData struct {
	status        StatusType
	task          string
	slug          string
	model         string
	mode          string
	lastTool      string
	cost          float64
	turnDurationS int
	messageCount  int
	inputTokens   int
	hasError      bool
}

func scanSession(path string) sessionData {
	file, err := os.Open(path)
	if err != nil {
		return sessionData{status: StatusIdle}
	}
	defer file.Close()

	var result sessionData
	var lastType string
	var lastLine string
	var lastUserTask string

	scanner := bufio.NewScanner(file)
	scanner.Buffer(make([]byte, 1024*1024), 1024*1024)

	var entry struct {
		Type    string `json:"type"`
		Subtype string `json:"subtype"`
		Slug    string `json:"slug"`
		Message struct {
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
		lastLine = scanner.Text()
		if err := json.Unmarshal([]byte(lastLine), &entry); err != nil {
			continue
		}

		lastType = entry.Type

		if entry.Slug != "" {
			result.slug = entry.Slug
		}

		switch entry.Type {
		case "permission-mode":
			if entry.PermissionMode != "" {
				result.mode = entry.PermissionMode
			}
		case "user":
			if entry.PermissionMode != "" {
				result.mode = entry.PermissionMode
			}
			ci := parseContent(entry.Message.Content)
			if ci.text != "" {
				lastUserTask = ci.text
			}
		case "assistant":
			if entry.Message.Model != "" {
				result.model = entry.Message.Model
			}
			ci := parseContent(entry.Message.Content)
			result.lastTool = ci.toolName
			result.hasError = ci.hasError

			if entry.Message.Usage.InputTokens > 0 {
				result.inputTokens = entry.Message.Usage.InputTokens +
					entry.Message.Usage.CacheCreationInputTokens +
					entry.Message.Usage.CacheReadInputTokens
			}

			p := pricing[ModelTier(entry.Message.Model)]
			u := entry.Message.Usage
			result.cost += float64(u.InputTokens) * p.input / 1_000_000
			result.cost += float64(u.OutputTokens) * p.output / 1_000_000
			result.cost += float64(u.CacheCreationInputTokens) * p.cacheWrite / 1_000_000
			result.cost += float64(u.CacheReadInputTokens) * p.cacheRead / 1_000_000
		case "system":
			if entry.Subtype == "turn_duration" {
				result.turnDurationS = entry.DurationMs / 1000
				result.messageCount = entry.MessageCount
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

	switch lastType {
	case "user":
		result.status = StatusWorking
	case "assistant":
		if strings.Contains(lastLine, "AskUserQuestion") {
			result.status = StatusWait
		} else {
			result.status = StatusIdle
		}
	default:
		result.status = StatusIdle
	}

	return result
}

type contentInfo struct {
	text     string // first text block (or plain string content)
	toolName string // last tool_use name
	hasError bool   // any tool_result with is_error
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
		Type    string `json:"type"`
		Text    string `json:"text"`
		Name    string `json:"name"`
		IsError bool   `json:"is_error"`
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
				info.toolName = b.Name // keep overwriting — we want the last one
			}
		case "tool_result":
			if b.IsError {
				info.hasError = true
			}
		}
	}
	return info
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

