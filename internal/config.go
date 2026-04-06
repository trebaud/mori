package internal

import (
	"encoding/json"
	"fmt"
	"os"
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
