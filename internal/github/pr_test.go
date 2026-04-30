package github

import (
	"os"
	"path/filepath"
	"testing"
)

const stubPR = `#!/bin/sh
cat <<'JSON'
[{"number":7,"state":"MERGED","isDraft":false,"url":"https://example.com/pr/7","title":"hello"}]
JSON
`

const stubEmpty = `#!/bin/sh
echo '[]'
`

const stubDraft = `#!/bin/sh
cat <<'JSON'
[{"number":3,"state":"OPEN","isDraft":true,"url":"https://example.com/pr/3","title":"wip"}]
JSON
`

func writeStub(t *testing.T, body string) string {
	t.Helper()
	dir := t.TempDir()
	p := filepath.Join(dir, "gh")
	if err := os.WriteFile(p, []byte(body), 0o755); err != nil {
		t.Fatal(err)
	}
	return p
}

// resetForTest swaps ghPath and clears the cache; returns a restore func.
func resetForTest(t *testing.T, path string) func() {
	t.Helper()
	prev := ghPath
	ghPath = path
	mu.Lock()
	cache = map[string]*entry{}
	mu.Unlock()
	return func() {
		ghPath = prev
		mu.Lock()
		cache = map[string]*entry{}
		mu.Unlock()
	}
}

func TestFetch_MergedPR(t *testing.T) {
	defer resetForTest(t, writeStub(t, stubPR))()

	info, err := Fetch("any-branch")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if info == nil {
		t.Fatal("expected PRInfo, got nil")
	}
	if info.Number != 7 || info.State != PRStateMerged || info.Title != "hello" {
		t.Errorf("unexpected info: %+v", info)
	}
}

func TestFetch_DraftSynthesized(t *testing.T) {
	defer resetForTest(t, writeStub(t, stubDraft))()

	info, err := Fetch("feat")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if info.State != PRStateDraft {
		t.Errorf("expected DRAFT, got %q", info.State)
	}
	if !info.IsDraft {
		t.Error("expected IsDraft=true")
	}
}

func TestFetch_NoPRSentinel(t *testing.T) {
	defer resetForTest(t, writeStub(t, stubEmpty))()

	info, err := Fetch("orphan")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if info == nil {
		t.Fatal("expected sentinel PRInfo, got nil")
	}
	if info.Number != 0 {
		t.Errorf("expected sentinel Number=0, got %d", info.Number)
	}
}

func TestFetch_CacheHit(t *testing.T) {
	stubPath := writeStub(t, stubPR)
	defer resetForTest(t, stubPath)()

	if _, err := Fetch("b"); err != nil {
		t.Fatal(err)
	}
	// Replace stub with one that would fail if invoked again.
	if err := os.WriteFile(stubPath, []byte("#!/bin/sh\nexit 99\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	info, err := Fetch("b")
	if err != nil {
		t.Fatalf("expected cache hit, got error: %v", err)
	}
	if info.Number != 7 {
		t.Errorf("expected cached value, got %+v", info)
	}
}

func TestRefresh_BypassesCache(t *testing.T) {
	stubPath := writeStub(t, stubPR)
	defer resetForTest(t, stubPath)()

	if _, err := Fetch("b"); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(stubPath, []byte(stubEmpty), 0o755); err != nil {
		t.Fatal(err)
	}
	info, err := Refresh("b")
	if err != nil {
		t.Fatal(err)
	}
	if info.Number != 0 {
		t.Errorf("expected sentinel after refresh, got %+v", info)
	}
}

func TestFetch_GhMissing(t *testing.T) {
	defer resetForTest(t, "")()

	info, err := Fetch("b")
	if err == nil {
		t.Error("expected error when gh is missing")
	}
	if info != nil {
		t.Errorf("expected nil info, got %+v", info)
	}
}

func TestLookup_BeforeFetch(t *testing.T) {
	defer resetForTest(t, writeStub(t, stubPR))()

	if got := Lookup("never-fetched"); got != nil {
		t.Errorf("expected nil before fetch, got %+v", got)
	}
}

func TestInvalidateAll(t *testing.T) {
	defer resetForTest(t, writeStub(t, stubPR))()

	if _, err := Fetch("b"); err != nil {
		t.Fatal(err)
	}
	if Lookup("b") == nil {
		t.Fatal("expected cached entry")
	}
	InvalidateAll()
	if Lookup("b") != nil {
		t.Error("expected cache cleared after InvalidateAll")
	}
}
