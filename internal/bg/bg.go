// Package bg wraps Claude Code's background-session feature (`claude --bg`)
// and the on-disk state files it maintains under ~/.claude/jobs/<id>.
//
// Mori uses it for two things:
//   - dispatching async prompts (the [m] flow) by shelling out to
//     `claude --bg "<prompt>"` and parsing the short id printed on stdout;
//   - reading per-worktree liveness/state by scanning the jobs directory and
//     matching each session's cwd back to a mori worktree path.
package bg

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"
)

// Session is a snapshot of one ~/.claude/jobs/<id>/state.json file.
type Session struct {
	ID        string    // short id (daemonShort), e.g. "677919a0"
	SessionID string    // full UUID
	Cwd       string    // working directory the session was launched in
	State     string    // working | input | idle | done | failed | stopped
	Tempo     string    // working | idle — finer-grained activity flag
	Detail    string    // human-readable summary (shown in agent view)
	Intent    string    // original prompt
	InFlight  int       // tasks currently running
	UpdatedAt time.Time
	CreatedAt time.Time
}

// Live reports whether the supervisor still considers the session active.
// Stopped/failed/done sessions persist on disk so users can resume them,
// but mori treats them as inert.
func (s Session) Live() bool {
	switch s.State {
	case "done", "failed", "stopped":
		return false
	}
	return true
}

// Working reports whether Claude is actively running tools or generating a
// response for this session.
func (s Session) Working() bool {
	return s.State == "working" || s.Tempo == "working" || s.InFlight > 0
}

// NeedsInput reports whether the session is blocked on the user.
func (s Session) NeedsInput() bool {
	return s.State == "input"
}

// jobsDir returns the directory where the supervisor stores per-session state.
// Respects CLAUDE_CONFIG_DIR if set (matches the daemon's own behavior).
func jobsDir() string {
	if dir := os.Getenv("CLAUDE_CONFIG_DIR"); dir != "" {
		return filepath.Join(dir, "jobs")
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return filepath.Join(home, ".claude", "jobs")
}

// List scans jobs/*/state.json. Entries that fail to parse are skipped.
func List() []Session {
	dir := jobsDir()
	if dir == "" {
		return nil
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil
	}
	var out []Session
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		s, ok := readSession(filepath.Join(dir, e.Name(), "state.json"))
		if ok {
			out = append(out, s)
		}
	}
	return out
}

// FindByCwd returns the most recently updated session whose cwd equals path,
// or nil. Stopped/failed sessions are returned too — callers can filter via
// Session.Live() when they only care about active ones.
func FindByCwd(path string) *Session {
	var matches []Session
	for _, s := range List() {
		if s.Cwd == path {
			matches = append(matches, s)
		}
	}
	if len(matches) == 0 {
		return nil
	}
	// Prefer live over inert; among each group, prefer the most recently updated.
	sort.Slice(matches, func(i, j int) bool {
		li, lj := matches[i].Live(), matches[j].Live()
		if li != lj {
			return li
		}
		return matches[i].UpdatedAt.After(matches[j].UpdatedAt)
	})
	return &matches[0]
}

// ByCwd builds a path→Session map over a list of worktree paths, returning
// only the best match per path. It scans the jobs directory once, which is
// much cheaper than calling FindByCwd in a loop.
func ByCwd(paths []string) map[string]*Session {
	want := make(map[string]bool, len(paths))
	for _, p := range paths {
		want[p] = true
	}
	all := List()
	out := make(map[string]*Session)
	for i := range all {
		s := all[i]
		if !want[s.Cwd] {
			continue
		}
		cur, ok := out[s.Cwd]
		if !ok {
			out[s.Cwd] = &s
			continue
		}
		// Prefer live; break ties on UpdatedAt.
		if (!cur.Live() && s.Live()) ||
			(cur.Live() == s.Live() && s.UpdatedAt.After(cur.UpdatedAt)) {
			out[s.Cwd] = &s
		}
	}
	return out
}

func readSession(path string) (Session, bool) {
	data, err := os.ReadFile(path)
	if err != nil {
		return Session{}, false
	}
	var raw struct {
		State        string `json:"state"`
		Tempo        string `json:"tempo"`
		Detail       string `json:"detail"`
		Intent       string `json:"intent"`
		SessionID    string `json:"sessionId"`
		DaemonShort  string `json:"daemonShort"`
		Cwd          string `json:"cwd"`
		CreatedAt    string `json:"createdAt"`
		UpdatedAt    string `json:"updatedAt"`
		InFlight     struct {
			Tasks int `json:"tasks"`
		} `json:"inFlight"`
	}
	if json.Unmarshal(data, &raw) != nil {
		return Session{}, false
	}
	if raw.DaemonShort == "" {
		// Fall back to the directory name when daemonShort is missing.
		raw.DaemonShort = filepath.Base(filepath.Dir(path))
	}
	parseTime := func(s string) time.Time {
		t, _ := time.Parse(time.RFC3339Nano, s)
		return t
	}
	return Session{
		ID:        raw.DaemonShort,
		SessionID: raw.SessionID,
		Cwd:       raw.Cwd,
		State:     raw.State,
		Tempo:     raw.Tempo,
		Detail:    raw.Detail,
		Intent:    raw.Intent,
		InFlight:  raw.InFlight.Tasks,
		CreatedAt: parseTime(raw.CreatedAt),
		UpdatedAt: parseTime(raw.UpdatedAt),
	}, true
}

var bgIDRe = regexp.MustCompile(`backgrounded[ \t]+·[ \t]+([a-z0-9]+)`)

// Launch dispatches a new background session via `claude --bg <prompt>` in
// dir and returns the short id printed on stdout.
func Launch(claudePath, dir, prompt string, extraArgs ...string) (string, error) {
	if claudePath == "" {
		p, err := exec.LookPath("claude")
		if err != nil {
			return "", fmt.Errorf("claude not found in PATH")
		}
		claudePath = p
	}

	args := []string{"--bg"}
	args = append(args, extraArgs...)
	args = append(args, prompt)

	cmd := exec.Command(claudePath, args...)
	cmd.Dir = dir
	// Force a clean stdin so `claude --bg` can't block on terminal prompts.
	devNull, err := os.OpenFile(os.DevNull, os.O_RDONLY, 0)
	if err == nil {
		cmd.Stdin = devNull
		defer devNull.Close()
	}

	out, err := cmd.CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("claude --bg failed: %w (%s)", err, strings.TrimSpace(string(out)))
	}

	scanner := bufio.NewScanner(strings.NewReader(string(out)))
	for scanner.Scan() {
		line := scanner.Text()
		if m := bgIDRe.FindStringSubmatch(line); m != nil {
			return m[1], nil
		}
	}
	return "", fmt.Errorf("could not parse session id from claude --bg output:\n%s", string(out))
}

// Logs returns the recent terminal output for the session.
func Logs(claudePath, id string) (string, error) {
	if claudePath == "" {
		p, err := exec.LookPath("claude")
		if err != nil {
			return "", fmt.Errorf("claude not found in PATH")
		}
		claudePath = p
	}
	cmd := exec.Command(claudePath, "logs", id)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("claude logs: %w", err)
	}
	return string(out), nil
}

// AttachCommand returns an *exec.Cmd configured to attach interactively.
// Caller is responsible for wiring stdin/stdout/stderr and calling Run.
func AttachCommand(claudePath, id string) (*exec.Cmd, error) {
	if claudePath == "" {
		p, err := exec.LookPath("claude")
		if err != nil {
			return nil, fmt.Errorf("claude not found in PATH")
		}
		claudePath = p
	}
	return exec.Command(claudePath, "attach", id), nil
}

// Stop terminates a session via `claude stop <id>`. Used for cleanup.
func Stop(claudePath, id string) error {
	if claudePath == "" {
		p, err := exec.LookPath("claude")
		if err != nil {
			return err
		}
		claudePath = p
	}
	return exec.Command(claudePath, "stop", id).Run()
}
