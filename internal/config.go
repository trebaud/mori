package internal

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
)

type Step struct {
	Name string `json:"name"`
	Cmd  string `json:"cmd"`
}

type Config struct {
	PostCreate []Step `json:"post_create"`
}

// Load reads config from ~/.mori/settings.json (global) and {repoRoot}/.mori.json (project).
// Project config replaces global entirely for post_create commands.
func Load(repoRoot string) Config {
	var cfg Config

	// Global config
	if home, err := os.UserHomeDir(); err == nil {
		globalPath := filepath.Join(home, ".mori", "settings.json")
		if parsed, ok := readConfig(globalPath); ok && parsed.PostCreate != nil {
			cfg.PostCreate = parsed.PostCreate
		}
	}

	// Project config (replaces global)
	if repoRoot != "" {
		projectPath := filepath.Join(repoRoot, ".mori.json")
		if parsed, ok := readConfig(projectPath); ok && parsed.PostCreate != nil {
			cfg.PostCreate = parsed.PostCreate
		}
	}

	return cfg
}


// HookResult records the outcome of a single post-create hook step.
type HookResult struct {
	Name    string
	Success bool
}

// RunPostCreateHooks executes each post-create step in the given directory.
// Returns results for all steps (both successes and failures).
func RunPostCreateHooks(dir string, steps []Step) []HookResult {
	var results []HookResult
	for _, step := range steps {
		cmd := exec.Command("sh", "-c", step.Cmd)
		cmd.Dir = dir
		results = append(results, HookResult{
			Name:    step.Name,
			Success: cmd.Run() == nil,
		})
	}
	return results
}

func readConfig(path string) (Config, bool) {
	data, err := os.ReadFile(path)
	if err != nil {
		return Config{}, false
	}

	var cfg Config
	if err := json.Unmarshal(data, &cfg); err != nil {
		fmt.Fprintf(os.Stderr, "    \033[1;33m⚠\033[0m Warning: failed to parse %s: %v\n", path, err)
		return Config{}, false
	}
	return cfg, true
}
