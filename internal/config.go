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

// HookResult records the outcome of a single post-create hook step, including
// whatever it wrote. A failed step used to report only its name, which told
// you mori had broken rather than that your lockfile was stale.
type HookResult struct {
	Name    string
	Success bool
	Output  string
}

// HookCallbacks lets callers observe each step as it starts and completes,
// so they can render live progress (e.g. a spinner per step). output is the
// step's combined stdout and stderr, and is only worth showing when the step
// failed.
type HookCallbacks struct {
	OnStart    func(name string)
	OnComplete func(name string, success bool, output string)
}
