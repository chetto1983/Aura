package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// B-16: fs_grep / fs_glob walk a directory tree with no node-count or time
// budget, so a path:/ (or any huge tree) can scan the whole disk and wedge a
// turn. The walk must stop early on a node-count cap and flag truncation; a walk
// that completes under the cap is unaffected.

// buildWideTree writes n empty files under dir and returns dir.
func buildWideTree(t *testing.T, dir string, n int) {
	t.Helper()
	for i := range n {
		p := filepath.Join(dir, fmt.Sprintf("file_%05d.txt", i))
		if err := os.WriteFile(p, []byte("needle here\n"), 0o600); err != nil {
			t.Fatalf("write %s: %v", p, err)
		}
	}
}

func TestFSGrepNodeCapTruncates(t *testing.T) {
	t.Setenv(envFSWalkNodeCap, "20")
	dir := t.TempDir()
	buildWideTree(t, dir, 100) // far over the 20-node cap

	tool := &FSGrep{WorkspaceRoot: dir}
	ctx := ctxWith(t, "sess-grep-cap", "call-grep-cap")
	// A pattern that matches NOTHING, so maxResults never trips — only the node
	// budget can stop the walk.
	res, err := tool.Execute(ctx, mustJSON(t, map[string]any{"pattern": "this-matches-no-line-anywhere"}))
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if !strings.Contains(res.Preview, walkTruncatedMarker) {
		t.Fatalf("over-cap walk must flag truncation, got: %q", res.Preview)
	}
}

func TestFSGlobNodeCapTruncates(t *testing.T) {
	t.Setenv(envFSWalkNodeCap, "20")
	dir := t.TempDir()
	buildWideTree(t, dir, 100)

	tool := &FSGlob{WorkspaceRoot: dir}
	ctx := ctxWith(t, "sess-glob-cap", "call-glob-cap")
	// A pattern that matches NOTHING by name, so maxResults never trips.
	res, err := tool.Execute(ctx, mustJSON(t, map[string]any{"pattern": "**/*.no-such-extension"}))
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if !strings.Contains(res.Preview, walkTruncatedMarker) {
		t.Fatalf("over-cap walk must flag truncation, got: %q", res.Preview)
	}
}

func TestFSGrepUnderCapNoTruncation(t *testing.T) {
	t.Setenv(envFSWalkNodeCap, "1000")
	dir := t.TempDir()
	buildWideTree(t, dir, 10) // well under the cap

	tool := &FSGrep{WorkspaceRoot: dir}
	ctx := ctxWith(t, "sess-grep-ok", "call-grep-ok")
	res, err := tool.Execute(ctx, mustJSON(t, map[string]any{"pattern": "needle"}))
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if strings.Contains(res.Preview, walkTruncatedMarker) {
		t.Fatalf("under-cap walk must NOT flag truncation, got: %q", res.Preview)
	}
	if !strings.Contains(res.Preview, "needle") {
		t.Fatalf("under-cap walk should still return matches, got: %q", res.Preview)
	}
}

func TestFSGlobUnderCapNoTruncation(t *testing.T) {
	t.Setenv(envFSWalkNodeCap, "1000")
	dir := t.TempDir()
	buildWideTree(t, dir, 10)

	tool := &FSGlob{WorkspaceRoot: dir}
	ctx := ctxWith(t, "sess-glob-ok", "call-glob-ok")
	res, err := tool.Execute(ctx, mustJSON(t, map[string]any{"pattern": "**/*.txt"}))
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if strings.Contains(res.Preview, walkTruncatedMarker) {
		t.Fatalf("under-cap walk must NOT flag truncation, got: %q", res.Preview)
	}
	if !strings.Contains(res.Preview, "file_00000.txt") {
		t.Fatalf("under-cap walk should still return matches, got: %q", res.Preview)
	}
}

// The deadline cap stops a walk that runs past its budget even when the node cap
// is high. A zero/expired deadline trips immediately.
func TestFSGrepDeadlineCapTruncates(t *testing.T) {
	t.Setenv(envFSWalkNodeCap, "1000000")
	t.Setenv(envFSWalkTimeoutMs, "1") // 1ms: any non-trivial tree blows it
	dir := t.TempDir()
	buildWideTree(t, dir, 200)

	tool := &FSGrep{WorkspaceRoot: dir}
	ctx := ctxWith(t, "sess-grep-deadline", "call-grep-deadline")
	res, err := tool.Execute(ctx, mustJSON(t, map[string]any{"pattern": "no-match-pattern-xyz"}))
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if !strings.Contains(res.Preview, walkTruncatedMarker) {
		t.Fatalf("deadline-exceeded walk must flag truncation, got: %q", res.Preview)
	}
}

// Regression (the appliance /root bug): a home-style tree whose hidden cache subdir
// dwarfs the node budget must NOT hide the operator's own top-level files. The walk
// prunes hidden subdirs, so it reaches the target without exhausting the budget —
// the agent found `test_aura*` files were invisible to fs_glob while `shell_exec ls`
// saw them, because /root/.cache (66k files) blew the node cap first.
func TestFSGlobSkipsHiddenCacheAndFindsTopLevel(t *testing.T) {
	t.Setenv(envFSWalkNodeCap, "50") // small: the hidden cache alone would blow it
	dir := t.TempDir()
	cache := filepath.Join(dir, ".cache")
	if err := os.MkdirAll(cache, 0o755); err != nil {
		t.Fatal(err)
	}
	buildWideTree(t, cache, 200) // 200 files in a hidden cache, far over the 50 cap
	if err := os.WriteFile(filepath.Join(dir, "test_aura_note.txt"), []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}

	tool := &FSGlob{WorkspaceRoot: dir}
	ctx := ctxWith(t, "sess-glob-hidden", "call-glob-hidden")
	res, err := tool.Execute(ctx, mustJSON(t, map[string]any{"pattern": "test_aura*"}))
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if !strings.Contains(res.Preview, "test_aura_note.txt") {
		t.Fatalf("top-level file must be found past the hidden cache, got: %q", res.Preview)
	}
	if strings.Contains(res.Preview, walkTruncatedMarker) {
		t.Fatalf("hidden cache should be pruned so the budget is not exhausted, got: %q", res.Preview)
	}
}

// fs_grep parity: the hidden-cache prune reaches a top-level content match too.
func TestFSGrepSkipsHiddenCacheAndFindsTopLevel(t *testing.T) {
	t.Setenv(envFSWalkNodeCap, "50")
	dir := t.TempDir()
	cache := filepath.Join(dir, ".cache")
	if err := os.MkdirAll(cache, 0o755); err != nil {
		t.Fatal(err)
	}
	buildWideTree(t, cache, 200)
	if err := os.WriteFile(filepath.Join(dir, "test_aura_note.txt"), []byte("unique_marker_zzz\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	tool := &FSGrep{WorkspaceRoot: dir}
	ctx := ctxWith(t, "sess-grep-hidden", "call-grep-hidden")
	res, err := tool.Execute(ctx, mustJSON(t, map[string]any{"pattern": "unique_marker_zzz"}))
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if !strings.Contains(res.Preview, "test_aura_note.txt") {
		t.Fatalf("top-level match must be found past the hidden cache, got: %q", res.Preview)
	}
	if strings.Contains(res.Preview, walkTruncatedMarker) {
		t.Fatalf("hidden cache should be pruned so the budget is not exhausted, got: %q", res.Preview)
	}
}

// An explicitly-targeted hidden root IS searched — the prune never applies to the
// walk root — so `path: <dir>/.cache` still finds files inside it.
func TestFSGlobExplicitHiddenRootIsSearched(t *testing.T) {
	t.Setenv(envFSWalkNodeCap, "1000")
	dir := t.TempDir()
	cache := filepath.Join(dir, ".cache")
	if err := os.MkdirAll(cache, 0o755); err != nil {
		t.Fatal(err)
	}
	buildWideTree(t, cache, 5)

	tool := &FSGlob{WorkspaceRoot: dir}
	ctx := ctxWith(t, "sess-glob-hidden-root", "call-glob-hidden-root")
	res, err := tool.Execute(ctx, mustJSON(t, map[string]any{"pattern": "**/*.txt", "path": cache}))
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if !strings.Contains(res.Preview, "file_00000.txt") {
		t.Fatalf("an explicit hidden root must be searched, got: %q", res.Preview)
	}
}

// A single line over the 1 MiB bufio.Scanner cap stops Scan with bufio.ErrTooLong;
// grepFile must surface it (checked scanner.Err()) instead of silently returning a
// partial file scan as if exhaustive.
func TestFSGrepLongLineSurfacesScanStop(t *testing.T) {
	dir := t.TempDir()
	huge := make([]byte, 2<<20) // 2 MiB, no newline, no NUL (not "binary")
	for i := range huge {
		huge[i] = 'a'
	}
	if err := os.WriteFile(filepath.Join(dir, "big.txt"), huge, 0o600); err != nil {
		t.Fatal(err)
	}

	tool := &FSGrep{WorkspaceRoot: dir}
	ctx := ctxWith(t, "sess-grep-long", "call-grep-long")
	res, err := tool.Execute(ctx, mustJSON(t, map[string]any{"pattern": "zzz-no-such-match"}))
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if !strings.Contains(res.Preview, "not fully searched") {
		t.Fatalf("a >1MiB line must surface a scan-stopped note, got: %q", res.Preview)
	}
}

// A ctx whose deadline is already passed stops the walk at once (budget respects
// an already-threaded ctx deadline, not only its own timer).
func TestWalkBudgetRespectsCtxDeadline(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel() // already cancelled
	b := newWalkBudget(ctx)
	if !b.exceeded() {
		t.Fatal("a cancelled ctx should make the walk budget report exceeded")
	}
	_ = json.RawMessage(nil)
}
