package internal

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
)

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

// RunPostCreateHooks executes each post-create step in the given directory.
// Returns results for all steps (both successes and failures).
// If cb is non-nil, OnStart and OnComplete fire around each step so callers
// can stream progress instead of waiting for the whole batch to finish.
func RunPostCreateHooks(dir string, steps []Step, cb *HookCallbacks) []HookResult {
	var results []HookResult
	for _, step := range steps {
		if cb != nil && cb.OnStart != nil {
			cb.OnStart(step.Name)
		}
		cmd := exec.Command("sh", "-c", step.Cmd)
		cmd.Dir = dir
		success := cmd.Run() == nil
		if cb != nil && cb.OnComplete != nil {
			cb.OnComplete(step.Name, success)
		}
		results = append(results, HookResult{
			Name:    step.Name,
			Success: success,
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
