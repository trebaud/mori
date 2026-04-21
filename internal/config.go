package internal

// Step represents a named shell command to run as a hook.
type Step struct {
	Name string `json:"name"`
	Cmd  string `json:"cmd"`
}

// Config holds mori configuration loaded from settings files.
type Config struct {
	PostCreate []Step `json:"post_create"`
}

// HookResult records the outcome of a single post-create hook step.
type HookResult struct {
	Name    string
	Success bool
}
