package tui

import (
	"github.com/moosecode/mori/agent"
)

type Worktree struct {
	Path          string
	Branch        string
	RelativePath  string
	IsMain        bool
	ClaudeSession bool
	ClaudeStale   bool
	Insights      agent.AgentStatus
}
