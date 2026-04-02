package agent

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

type Session struct {
	PID int    `json:"pid"`
	CWD string `json:"cwd"`
}

func CheckSession(path string) (active bool, stale bool) {
	home, err := os.UserHomeDir()
	if err != nil {
		return false, false
	}

	sessionsDir := filepath.Join(home, ".claude", "sessions")
	entries, err := os.ReadDir(sessionsDir)
	if err != nil {
		return false, false
	}

	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".json") {
			continue
		}

		data, err := os.ReadFile(filepath.Join(sessionsDir, e.Name()))
		if err != nil {
			continue
		}

		var sess Session
		if err := json.Unmarshal(data, &sess); err != nil {
			continue
		}

		if sess.CWD == path {
			if isRunning(sess.PID) {
				return true, false
			}
			return false, true
		}
	}
	return false, false
}

func isRunning(pid int) bool {
	return exec.Command("kill", "-0", fmt.Sprintf("%d", pid)).Run() == nil
}
