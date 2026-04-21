package agent

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

func gitData(worktreePath, headRef string) ([]string, string) {
	prev := cache[worktreePath]
	if prev != nil && prev.headRef == headRef && headRef != "" {
		return prev.insights.GitLog, prev.insights.AheadBehind
	}
	return git.Log(worktreePath, 5), git.AheadBehind(worktreePath)
}

func buildInsights(worktreePath, file string, modTime time.Time, headRef string) Insights {
	gitLog, aheadBehind := gitData(worktreePath, headRef)

	if file == "" {
		return Insights{Status: StatusNone, Available: true, GitLog: gitLog, AheadBehind: aheadBehind}
	}

	parsed := scanSession(file)
	return Insights{
		Status:        parsed.status,
		LastActivity:  modTime,
		SessionSize:   fileSize(file),
		CurrentTask:   parsed.task,
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
	}
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

const maxScanBytes = 10 * 1024 * 1024 // 10 MB tail limit for large session files

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
	var lastType string
	var lastLine string
	var lastUserTask string

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

