package github

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"sync"
	"time"
)

type PRState string

const (
	PRStateOpen   PRState = "OPEN"
	PRStateDraft  PRState = "DRAFT"
	PRStateMerged PRState = "MERGED"
	PRStateClosed PRState = "CLOSED"
)

type PRInfo struct {
	Number    int
	State     PRState
	IsDraft   bool
	Title     string
	URL       string
	FetchedAt time.Time
}

const (
	cacheTTL     = 5 * time.Minute
	fetchTimeout = 3 * time.Second
)

var (
	errGhMissing = errors.New("gh CLI not found")

	ghPath = resolveGhPath()
)

func resolveGhPath() string {
	if p := os.Getenv("MORI_GH_PATH"); p != "" {
		return p
	}
	p, _ := exec.LookPath("gh")
	return p
}

// IsAvailable returns true when a usable gh binary was located at startup.
func IsAvailable() bool { return ghPath != "" }

type entry struct {
	info      *PRInfo
	fetchedAt time.Time
	inflight  bool
}

var (
	mu    sync.Mutex
	cache = map[string]*entry{}
)

// Lookup returns the cached PRInfo for a branch without spawning gh.
// Returns nil if the branch has never been fetched or fetched-with-no-PR
// (the sentinel is preserved in the cache; nil here means "unknown").
func Lookup(branch string) *PRInfo {
	mu.Lock()
	defer mu.Unlock()
	e, ok := cache[branch]
	if !ok {
		return nil
	}
	return e.info
}

// Fetch returns the cached PRInfo if fresh; otherwise spawns gh and
// updates the cache. The returned info may be nil if gh is unavailable
// or the call failed.
func Fetch(branch string) (*PRInfo, error) {
	mu.Lock()
	if e, ok := cache[branch]; ok && time.Since(e.fetchedAt) < cacheTTL {
		mu.Unlock()
		return e.info, nil
	}
	mu.Unlock()
	return Refresh(branch)
}

// Refresh ignores the TTL and re-fetches PR state for a branch.
func Refresh(branch string) (*PRInfo, error) {
	if !IsAvailable() {
		return nil, errGhMissing
	}

	mu.Lock()
	if e, ok := cache[branch]; ok && e.inflight {
		mu.Unlock()
		return e.info, nil
	}
	if cache[branch] == nil {
		cache[branch] = &entry{}
	}
	cache[branch].inflight = true
	mu.Unlock()

	defer func() {
		mu.Lock()
		if e, ok := cache[branch]; ok {
			e.inflight = false
		}
		mu.Unlock()
	}()

	info, err := runGh(branch)
	if err != nil {
		return nil, err
	}

	mu.Lock()
	cache[branch] = &entry{info: info, fetchedAt: time.Now()}
	mu.Unlock()
	return info, nil
}

// InvalidateAll clears the entire PR cache. Next Fetch/Refresh will re-fetch.
func InvalidateAll() {
	mu.Lock()
	cache = map[string]*entry{}
	mu.Unlock()
}

func runGh(branch string) (*PRInfo, error) {
	ctx, cancel := context.WithTimeout(context.Background(), fetchTimeout)
	defer cancel()

	cmd := exec.CommandContext(ctx, ghPath,
		"pr", "list",
		"--head", branch,
		"--state", "all",
		"--json", "number,state,isDraft,url,title",
		"--limit", "1",
	)
	out, err := cmd.Output()
	if err != nil {
		return nil, err
	}

	var raw []struct {
		Number  int    `json:"number"`
		State   string `json:"state"`
		IsDraft bool   `json:"isDraft"`
		URL     string `json:"url"`
		Title   string `json:"title"`
	}
	if err := json.Unmarshal(out, &raw); err != nil {
		return nil, err
	}

	if len(raw) == 0 {
		// Sentinel: fetched, no PR exists.
		return &PRInfo{Number: 0, FetchedAt: time.Now()}, nil
	}

	r := raw[0]
	state := PRState(r.State)
	if state == PRStateOpen && r.IsDraft {
		state = PRStateDraft
	}
	return &PRInfo{
		Number:    r.Number,
		State:     state,
		IsDraft:   r.IsDraft,
		Title:     r.Title,
		URL:       r.URL,
		FetchedAt: time.Now(),
	}, nil
}
