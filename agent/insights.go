package agent

import (
	"bufio"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

type AgentStatusType string

const (
	StatusWorking AgentStatusType = "WORKING"
	StatusIdle   AgentStatusType = "IDLE"
	StatusWait   AgentStatusType = "WAITING"
	StatusNone  AgentStatusType = "NONE"
)

type AgentStatus struct {
	Status        AgentStatusType
	LastActivity  time.Time
	SessionSize   int64
	CurrentTask   string
	SessionID     string
	CostUSD       float64
	GitLog        []string
	Available     bool
}

func ClaudeProjectPath(worktreePath string) string {
	encoded := strings.ReplaceAll(worktreePath, "/", "-")
	encoded = strings.ReplaceAll(encoded, ".", "-")
	return encoded
}

func GetInsights(worktreePath string) AgentStatus {
	home, err := os.UserHomeDir()
	if err != nil {
		return AgentStatus{Status: StatusNone, Available: false}
	}

	projectDir := filepath.Join(home, ".claude", "projects", ClaudeProjectPath(worktreePath))

	entries, err := os.ReadDir(projectDir)
	if err != nil {
		return AgentStatus{Status: StatusNone, Available: false}
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
		return AgentStatus{Status: StatusNone, Available: true, GitLog: getGitLog(worktreePath)}
	}

	info, _ := os.Stat(newestFile)
	sessionSize := int64(0)
	if info != nil {
		sessionSize = info.Size()
	}

	lastLine := readLastLine(newestFile)
	status := StatusIdle
	task := ""

	if lastLine != "" {
		status = parseStatusFromLine(lastLine)
		task = extractTaskFromLine(lastLine)
	}

	sessionID := strings.TrimSuffix(filepath.Base(newestFile), ".jsonl")
	cost := computeCost(newestFile)

	return AgentStatus{
		Status:       status,
		LastActivity: newestModTime,
		SessionSize:  sessionSize,
		CurrentTask:  task,
		SessionID:    sessionID,
		CostUSD:      cost,
		GitLog:       getGitLog(worktreePath),
		Available:    true,
	}
}

func readLastLine(path string) string {
	file, err := os.Open(path)
	if err != nil {
		return ""
	}
	defer file.Close()

	var lastLine string
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		lastLine = scanner.Text()
	}
	return lastLine
}

func parseStatusFromLine(line string) AgentStatusType {
	var entry struct {
		Type string `json:"type"`
	}
	if err := json.Unmarshal([]byte(line), &entry); err != nil {
		return StatusIdle
	}

	switch entry.Type {
	case "user":
		return StatusWorking
	case "assistant":
		if strings.Contains(line, "AskUserQuestion") {
			return StatusWait
		}
		return StatusIdle
	default:
		return StatusIdle
	}
}

func extractTaskFromLine(line string) string {
	var entry struct {
		Message struct {
			Content []struct {
				Text string `json:"text"`
			} `json:"content"`
		} `json:"message"`
	}
	if err := json.Unmarshal([]byte(line), &entry); err != nil {
		return ""
	}

	if len(entry.Message.Content) > 0 && len(entry.Message.Content[0].Text) > 0 {
		text := entry.Message.Content[0].Text
		if len(text) > 60 {
			return text[:60] + "..."
		}
		return text
	}
	return ""
}

// Pricing per million tokens (USD) — Opus 4.6 / Sonnet 4.6 / Haiku 4.5
type modelPricing struct {
	input       float64
	output      float64
	cacheWrite  float64
	cacheRead   float64
}

var pricing = map[string]modelPricing{
	"opus": {input: 15.0, output: 75.0, cacheWrite: 18.75, cacheRead: 1.50},
	"sonnet": {input: 3.0, output: 15.0, cacheWrite: 3.75, cacheRead: 0.30},
	"haiku":  {input: 0.80, output: 4.0, cacheWrite: 1.0, cacheRead: 0.08},
}

func modelTier(model string) string {
	m := strings.ToLower(model)
	if strings.Contains(m, "opus") {
		return "opus"
	}
	if strings.Contains(m, "haiku") {
		return "haiku"
	}
	return "sonnet"
}

func computeCost(path string) float64 {
	file, err := os.Open(path)
	if err != nil {
		return 0
	}
	defer file.Close()

	var total float64
	scanner := bufio.NewScanner(file)
	scanner.Buffer(make([]byte, 1024*1024), 1024*1024)

	var entry struct {
		Type    string `json:"type"`
		Message struct {
			Model string `json:"model"`
			Usage struct {
				InputTokens              int `json:"input_tokens"`
				OutputTokens             int `json:"output_tokens"`
				CacheCreationInputTokens int `json:"cache_creation_input_tokens"`
				CacheReadInputTokens     int `json:"cache_read_input_tokens"`
			} `json:"usage"`
		} `json:"message"`
	}

	for scanner.Scan() {
		line := scanner.Bytes()
		if err := json.Unmarshal(line, &entry); err != nil {
			continue
		}
		if entry.Type != "assistant" {
			continue
		}
		p := pricing[modelTier(entry.Message.Model)]
		u := entry.Message.Usage
		total += float64(u.InputTokens) * p.input / 1_000_000
		total += float64(u.OutputTokens) * p.output / 1_000_000
		total += float64(u.CacheCreationInputTokens) * p.cacheWrite / 1_000_000
		total += float64(u.CacheReadInputTokens) * p.cacheRead / 1_000_000
	}
	return total
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
