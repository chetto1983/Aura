package tools

import (
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/chetto1983/aura/internal/sandbox/usersandbox"
)

// B-16: fs_grep / fs_glob sweep an arbitrary tree, so a `path:/` (or any huge tree) could scan a
// whole filesystem and wedge a turn. The sweep must stop early on the node-count cap and flag
// truncation; a sweep that completes under the cap is unaffected. These run over the fakeBox
// harness: the sweep happens inside the box now, and the cap is applied to what comes back.
//
// The budget half of the hidden-cache guarantee — pruning BEFORE the node cap counts, so a
// 66k-file /root/.cache cannot exhaust it — lives at the producer and is pinned by
// TestBoxSweepsPruneAtTheProducer, which asserts the find predicate the box actually receives. The
// results half is pinned here.

// boxPathLines renders n absolute box paths the way `find` prints them.
func boxPathLines(root string, n int) string {
	var b strings.Builder
	for i := range n {
		fmt.Fprintf(&b, "%s/file_%05d.txt\n", root, i)
	}
	return b.String()
}

// boxTextFrames renders n framed files, all carrying the same body.
func boxTextFrames(t *testing.T, cmd string, n int, body string) []byte {
	t.Helper()
	pairs := make([]string, 0, 2*n)
	for i := range n {
		pairs = append(pairs, fmt.Sprintf("file_%05d.txt", i), body)
	}
	return boxFrames(t, cmd, boxWorkspaceRoot, pairs...)
}

func TestFSGrepNodeCapTruncates(t *testing.T) {
	t.Setenv(envFSWalkNodeCap, "20")
	be := &fakeBox{respond: func(cmd string) usersandbox.ExecResult {
		return usersandbox.ExecResult{Stdout: boxTextFrames(t, cmd, 100, "needle here\n")} // far over the cap
	}}
	ctx := ctxWith(t, "sess-grep-cap", "call-grep-cap")
	// A pattern that matches NOTHING, so maxResults never trips — only the node budget can stop it.
	res, err := (&FSGrep{Router: routerWith(be)}).
		Execute(ctx, mustJSON(t, map[string]any{"pattern": "this-matches-no-line-anywhere"}))
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if !strings.Contains(res.Preview, walkTruncatedMarker) {
		t.Fatalf("over-cap sweep must flag truncation, got: %q", res.Preview)
	}
}

func TestFSGlobNodeCapTruncates(t *testing.T) {
	t.Setenv(envFSWalkNodeCap, "20")
	be := boxRespondingWith(boxPathLines(boxWorkspaceRoot, 100))
	ctx := ctxWith(t, "sess-glob-cap", "call-glob-cap")
	// A pattern that matches NOTHING by name, so maxResults never trips.
	res, err := (&FSGlob{Router: routerWith(be)}).
		Execute(ctx, mustJSON(t, map[string]any{"pattern": "**/*.no-such-extension"}))
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if !strings.Contains(res.Preview, walkTruncatedMarker) {
		t.Fatalf("over-cap sweep must flag truncation, got: %q", res.Preview)
	}
	// The cap must reach the box as the enumeration bound, not only be checked afterwards.
	if len(be.execs) != 1 || !strings.Contains(be.execs[0].Command, "head -n 21") {
		t.Fatalf("enumeration must be bounded at cap+1 inside the box: %#v", be.execs)
	}
}

func TestFSGrepUnderCapNoTruncation(t *testing.T) {
	t.Setenv(envFSWalkNodeCap, "1000")
	be := &fakeBox{respond: func(cmd string) usersandbox.ExecResult {
		return usersandbox.ExecResult{Stdout: boxTextFrames(t, cmd, 10, "needle here\n")}
	}}
	ctx := ctxWith(t, "sess-grep-ok", "call-grep-ok")
	res, err := (&FSGrep{Router: routerWith(be)}).Execute(ctx, mustJSON(t, map[string]any{"pattern": "needle"}))
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if strings.Contains(res.Preview, walkTruncatedMarker) {
		t.Fatalf("under-cap sweep must NOT flag truncation, got: %q", res.Preview)
	}
	if !strings.Contains(res.Preview, "needle") {
		t.Fatalf("under-cap sweep should still return matches, got: %q", res.Preview)
	}
}

func TestFSGlobUnderCapNoTruncation(t *testing.T) {
	t.Setenv(envFSWalkNodeCap, "1000")
	be := boxRespondingWith(boxPathLines(boxWorkspaceRoot, 10))
	ctx := ctxWith(t, "sess-glob-ok", "call-glob-ok")
	res, err := (&FSGlob{Router: routerWith(be)}).Execute(ctx, mustJSON(t, map[string]any{"pattern": "**/*.txt"}))
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if strings.Contains(res.Preview, walkTruncatedMarker) {
		t.Fatalf("under-cap sweep must NOT flag truncation, got: %q", res.Preview)
	}
	if !strings.Contains(res.Preview, "file_00000.txt") {
		t.Fatalf("under-cap sweep should still return matches, got: %q", res.Preview)
	}
}

// Regression (the appliance /root bug): a hidden cache subdir must not hide the operator's own
// top-level files in the RESULTS. See the file header for the half of this guarantee the box arm
// does not yet reproduce.
func TestFSGlobPrunesHiddenCacheFromResults(t *testing.T) {
	be := boxRespondingWith(
		boxWorkspaceRoot + "/.cache/blob_1\n" +
			boxWorkspaceRoot + "/.cache/deep/blob_2\n" +
			boxWorkspaceRoot + "/test_aura_note.txt\n")
	ctx := ctxWith(t, "sess-glob-hidden", "call-glob-hidden")
	res, err := (&FSGlob{Router: routerWith(be)}).Execute(ctx, mustJSON(t, map[string]any{"pattern": "test_aura*"}))
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if !strings.Contains(res.Preview, "test_aura_note.txt") {
		t.Fatalf("top-level file must be found past the hidden cache, got: %q", res.Preview)
	}
	if strings.Contains(res.Preview, ".cache") {
		t.Fatalf("hidden cache must be pruned from results, got: %q", res.Preview)
	}
}

// fs_grep parity: the hidden-cache prune reaches a top-level content match too.
func TestFSGrepPrunesHiddenCacheFromResults(t *testing.T) {
	be := &fakeBox{respond: func(cmd string) usersandbox.ExecResult {
		return usersandbox.ExecResult{Stdout: boxFrames(t, cmd, boxWorkspaceRoot,
			".cache/blob_1", "unique_marker_zzz\n",
			"test_aura_note.txt", "unique_marker_zzz\n",
		)}
	}}
	ctx := ctxWith(t, "sess-grep-hidden", "call-grep-hidden")
	res, err := (&FSGrep{Router: routerWith(be)}).
		Execute(ctx, mustJSON(t, map[string]any{"pattern": "unique_marker_zzz"}))
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if !strings.Contains(res.Preview, "test_aura_note.txt") {
		t.Fatalf("top-level match must be found past the hidden cache, got: %q", res.Preview)
	}
	if strings.Contains(res.Preview, ".cache") {
		t.Fatalf("hidden cache must be pruned from results, got: %q", res.Preview)
	}
}

// An explicitly-targeted hidden root IS searched — the prune is computed on the ROOT-RELATIVE
// path, so `path: /workspace/.cache` still finds files inside it.
func TestFSGlobExplicitHiddenRootIsSearched(t *testing.T) {
	be := boxRespondingWith(boxPathLines(boxWorkspaceRoot+"/.cache", 5))
	ctx := ctxWith(t, "sess-glob-hidden-root", "call-glob-hidden-root")
	res, err := (&FSGlob{Router: routerWith(be)}).
		Execute(ctx, mustJSON(t, map[string]any{"pattern": "**/*.txt", "path": "/workspace/.cache"}))
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if !strings.Contains(res.Preview, "file_00000.txt") {
		t.Fatalf("an explicit hidden root must be searched, got: %q", res.Preview)
	}
}

// A single line over the 1 MiB bufio.Scanner cap stops Scan with bufio.ErrTooLong; grepContent
// must surface it (it checks scanner.Err()) instead of returning a partial scan as if exhaustive.
func TestFSGrepLongLineSurfacesScanStop(t *testing.T) {
	huge := strings.Repeat("a", 2<<20) // 2 MiB, no newline, no NUL (not "binary")
	be := &fakeBox{respond: func(cmd string) usersandbox.ExecResult {
		return usersandbox.ExecResult{Stdout: boxFrames(t, cmd, boxWorkspaceRoot, "big.txt", huge)}
	}}
	ctx := ctxWith(t, "sess-grep-long", "call-grep-long")
	res, err := (&FSGrep{Router: routerWith(be)}).
		Execute(ctx, mustJSON(t, map[string]any{"pattern": "zzz-no-such-match"}))
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if !strings.Contains(res.Preview, "not fully searched") {
		t.Fatalf("a >1MiB line must surface a scan-stopped note, got: %q", res.Preview)
	}
}

// AURA_FS_WALK_TIMEOUT_MS still bounds the sweep: it is applied to the box exec's context, which
// is the only place a bound can live now that there is no per-entry host walk to count.
func TestBoxSweepDeadlineHonoursTheWalkTimeoutKnob(t *testing.T) {
	t.Setenv(envFSWalkTimeoutMs, "50")
	ctx, cancel := boxSweepDeadline(t.Context())
	defer cancel()
	deadline, ok := ctx.Deadline()
	if !ok {
		t.Fatal("box sweep ran with no deadline — a path:/ sweep would be bounded only by the turn")
	}
	if remaining := time.Until(deadline); remaining <= 0 || remaining > 200*time.Millisecond {
		t.Fatalf("deadline in %v, want it to track the 50ms knob", remaining)
	}
}
