package agent

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

type StatusType string

const (
	StatusWorking StatusType = "WORKING"
	StatusIdle    StatusType = "IDLE"
	StatusWait    StatusType = "WAITING"
	StatusNone    StatusType = "NONE"
)

type Status struct {
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

func GetInsights(worktreePath string) Status {
	home, err := os.UserHomeDir()
	if err != nil {
		return Status{Status: StatusNone, Available: false}
	}

	projectDir := filepath.Join(home, ".claude", "projects", ClaudeProjectPath(worktreePath))

	entries, err := os.ReadDir(projectDir)
	if err != nil {
		return Status{Status: StatusNone, Available: false}
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
		return Status{Status: StatusNone, Available: true, GitLog: getGitLog(worktreePath)}
	}

	info, _ := os.Stat(newestFile)
	sessionSize := int64(0)
	if info != nil {
		sessionSize = info.Size()
	}

	sessionID := strings.TrimSuffix(filepath.Base(newestFile), ".jsonl")
	parsed := scanSession(newestFile)

	return Status{
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
		GitLog:        getGitLog(worktreePath),
		AheadBehind:   getAheadBehind(worktreePath),
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
			text := extractTextFromContent(entry.Message.Content)
			if text != "" {
				lastUserTask = text
			}
		case "assistant":
			if entry.Message.Model != "" {
				result.model = entry.Message.Model
			}
			result.lastTool = extractToolName(entry.Message.Content)
			result.hasError = checkToolError(entry.Message.Content)

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

func extractTextFromContent(raw json.RawMessage) string {
	var s string
	if json.Unmarshal(raw, &s) == nil && s != "" {
		return s
	}
	var blocks []struct {
		Type string `json:"type"`
		Text string `json:"text"`
	}
	if json.Unmarshal(raw, &blocks) == nil {
		for _, b := range blocks {
			if b.Type == "text" && b.Text != "" {
				return b.Text
			}
		}
	}
	return ""
}

func extractToolName(raw json.RawMessage) string {
	var blocks []struct {
		Type string `json:"type"`
		Name string `json:"name"`
	}
	if json.Unmarshal(raw, &blocks) == nil {
		for i := len(blocks) - 1; i >= 0; i-- {
			if blocks[i].Type == "tool_use" && blocks[i].Name != "" {
				return blocks[i].Name
			}
		}
	}
	return ""
}

func checkToolError(raw json.RawMessage) bool {
	var blocks []struct {
		Type    string `json:"type"`
		IsError bool   `json:"is_error"`
	}
	if json.Unmarshal(raw, &blocks) == nil {
		for _, b := range blocks {
			if b.Type == "tool_result" && b.IsError {
				return true
			}
		}
	}
	return false
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

func getDefaultBranch(repo string) string {
	out, err := exec.Command("git", "-C", repo, "symbolic-ref", "refs/remotes/origin/HEAD").Output()
	if err == nil {
		parts := strings.Split(strings.TrimSpace(string(out)), "/")
		if len(parts) > 0 {
			return parts[len(parts)-1]
		}
	}
	for _, name := range []string{"main", "master"} {
		if exec.Command("git", "-C", repo, "rev-parse", "--verify", "--quiet", name).Run() == nil {
			return name
		}
	}
	return "main"
}

func getGitLog(worktreePath string) []string {
	out, err := exec.Command("git", "-C", worktreePath, "log", "--oneline", "--pretty=format:%ar: %s", "-n", "5").Output()
	if err != nil {
		return nil
	}
	lines := strings.Split(strings.TrimSpace(string(out)), "\n")
	if len(lines) == 1 && lines[0] == "" {
		return nil
	}
	return lines
}

func getAheadBehind(worktreePath string) string {
	mainBranch := getDefaultBranch(worktreePath)

	out, err := exec.Command("git", "-C", worktreePath, "rev-list", "--left-right", "--count", mainBranch+"...HEAD").Output()
	if err != nil {
		return ""
	}

	parts := strings.Fields(strings.TrimSpace(string(out)))
	if len(parts) != 2 {
		return ""
	}

	behind, ahead := parts[0], parts[1]
	if ahead == "0" && behind == "0" {
		return ""
	}
	return fmt.Sprintf("+%s/-%s", ahead, behind)
}
