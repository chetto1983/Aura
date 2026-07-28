package tools

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// seedSidecar spills a >cap output via NewResult so a sidecar file exists for
// read_tool_output to page, returning the ctx (carrying session_id+run_dir) that
// read_tool_output reuses to resolve the same path.
func seedSidecar(t *testing.T, sessionID, callID, content string) context.Context {
	t.Helper()
	runDir := t.TempDir()
	ctx := ctxWithRunDir(sessionID, callID, runDir)
	res, err := NewResult(ctx, content)
	if err != nil {
		t.Fatalf("seed NewResult: %v", err)
	}
	if !res.Truncated {
		t.Fatalf("seed content not large enough to spill (%d bytes)", len(content))
	}
	return ctx
}

func sidecarIDFromPreview(t *testing.T, preview string) string {
	t.Helper()
	const marker = `read_tool_output(tool_call_id="`
	start := strings.Index(preview, marker)
	if start < 0 {
		t.Fatalf("preview missing read_tool_output pointer: %q", preview)
	}
	start += len(marker)
	end := strings.IndexByte(preview[start:], '"')
	if end < 0 {
		t.Fatalf("preview has unterminated tool_call_id pointer: %q", preview)
	}
	return preview[start : start+end]
}

// Test 1 (Req#7): offset=50000 limit=100 returns the correct 100-byte slice of
// the 100KB fixture with a byte-based next-offset footer.
func TestReadToolOutput_ByteSlice(t *testing.T) {
	// Build a 100KB fixture whose bytes encode their own index mod 10 so we can
	// assert the exact slice content.
	var b strings.Builder
	for i := 0; i < 100_000; i++ {
		b.WriteByte(byte('0' + i%10))
	}
	content := b.String()
	ctx := seedSidecar(t, "sess-r1", "call-r1", content)

	args := fmt.Sprintf(`{"tool_call_id":%q,"offset":50000,"limit":100}`, "call-r1")
	res, err := ReadToolOutput{}.Execute(ctx, []byte(args))
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	want := content[50000:50100]
	if !strings.HasPrefix(res.Preview, want) {
		t.Fatalf("slice mismatch:\n got %q\nwant prefix %q", res.Preview[:min(120, len(res.Preview))], want)
	}
	if !strings.Contains(res.Preview, "showing bytes 50000-50100 of 100000, next offset 50100") {
		t.Fatalf("footer wrong: %q", res.Preview)
	}
}

func TestReadToolOutput_FreshContextUsesOpaqueFooterID(t *testing.T) {
	runDir := t.TempDir()
	content := strings.Repeat("f", 10_000)
	writeCtx := ctxWithRunDir("sess-fresh", "call-fresh", runDir)
	spilled, err := NewResult(writeCtx, content)
	if err != nil {
		t.Fatalf("NewResult: %v", err)
	}
	spillID := sidecarIDFromPreview(t, spilled.Preview)
	if spillID == "call-fresh" {
		t.Fatalf("footer must expose opaque sidecar id, got provider id %q", spillID)
	}

	readCtx := ctxWithRunDir("sess-fresh", "read-call", runDir)
	args := fmt.Sprintf(`{"tool_call_id":%q,"offset":4096,"limit":10}`, spillID)
	res, err := ReadToolOutput{}.Execute(readCtx, []byte(args))
	if err != nil {
		t.Fatalf("Execute with fresh context: %v", err)
	}
	if res.Bytes != 10 || !strings.HasPrefix(res.Preview, "ffffffffff") {
		t.Fatalf("fresh-context read returned bytes=%d preview %.40q", res.Bytes, res.Preview)
	}
}

// Test 2 (Req#7): an unknown tool_call_id hard-fails with an error (not empty,
// not panic) — D-15. Asserts the SPECIFIC "no output for tool_call_id" message (not
// just that the id appears): removing the IsNotExist-branch return falls through to
// the generic "read sidecar" wrap, which can still surface the id via %w — so a bare
// id-substring check would pass on that mutant. Pinning the no-output phrasing kills it.
func TestReadToolOutput_UnknownID(t *testing.T) {
	runDir := t.TempDir()
	ctx := ctxWithRunDir("sess-r2", "agent-call", runDir)
	res, err := ReadToolOutput{}.Execute(ctx, []byte(`{"tool_call_id":"never-spilled"}`))
	if err == nil {
		t.Fatalf("want an error for an unknown id, got result %+v", res)
	}
	if !strings.Contains(err.Error(), "never-spilled") {
		t.Fatalf("error should name the id: %v", err)
	}
	if !strings.Contains(err.Error(), "no output for tool_call_id") {
		t.Fatalf("want the IsNotExist 'no output' message, got %q", err.Error())
	}
}

// Test 3 (Req#7 defaults): offset omitted -> 0; limit omitted -> defaultReadLimit.
func TestReadToolOutput_Defaults(t *testing.T) {
	// Sized OFF the constant, not off a literal: the fixture only exercises the default
	// window if it is larger than the window, and the window is a tunable.
	total := defaultReadLimit * 2
	content := strings.Repeat("k", total)
	ctx := seedSidecar(t, "sess-r3", "call-r3", content)

	res, err := ReadToolOutput{}.Execute(ctx, []byte(`{"tool_call_id":"call-r3"}`))
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	// Default window is defaultReadLimit bytes starting at 0.
	if !strings.Contains(res.Preview, fmt.Sprintf("showing bytes 0-%d of %d", defaultReadLimit, total)) {
		t.Fatalf("default window footer wrong: %q", res.Preview)
	}
}

// Test (Req#7 clamp / T-03-09): offset past EOF clamps to total, no panic.
func TestReadToolOutput_OffsetPastEOF(t *testing.T) {
	content := strings.Repeat("m", 5000)
	ctx := seedSidecar(t, "sess-r4", "call-r4", content)
	res, err := ReadToolOutput{}.Execute(ctx, []byte(`{"tool_call_id":"call-r4","offset":999999,"limit":100}`))
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if !strings.Contains(res.Preview, "showing bytes 5000-5000 of 5000, next offset 5000") {
		t.Fatalf("clamp footer wrong: %q", res.Preview)
	}
}

// T-03-07: a traversal-shaped tool_call_id is rejected before filepath.Join.
// Asserts the SPECIFIC validateID phrasing ('invalid character') so removing
// the sidecarPath-error return — which would fall through to os.ReadFile and a
// generic "read sidecar" error — is killed by the message check, not just err!=nil.
func TestReadToolOutput_PathTraversal(t *testing.T) {
	runDir := t.TempDir()
	ctx := ctxWithRunDir("sess-r5", "agent-call", runDir)
	for _, id := range []string{"..", "../../etc/passwd", "a/b", `a\b`} {
		args := fmt.Sprintf(`{"tool_call_id":%q}`, id)
		_, err := (ReadToolOutput{}).Execute(ctx, []byte(args))
		if err == nil {
			t.Fatalf("want error for traversal id %q", id)
		}
		if !strings.Contains(err.Error(), "invalid character") {
			t.Fatalf("traversal id %q: want a validateID invalid-character rejection, got %q", id, err.Error())
		}
	}
}

// Negative offset is rejected, not a panic. The sidecar IS seeded so the
// negative-offset guard is the branch actually reached (with no sidecar, the read
// fails first and the guard's removal would survive). Asserts the SPECIFIC "is
// negative" message so removing the guard — which then falls through to data[-5:],
// a slice-bounds panic OR a different error — is killed by the message check.
func TestReadToolOutput_NegativeOffset(t *testing.T) {
	ctx := seedSidecar(t, "sess-r6", "call-r6", strings.Repeat("n", 10_000))
	_, err := (ReadToolOutput{}).Execute(ctx, []byte(`{"tool_call_id":"call-r6","offset":-5}`))
	if err == nil {
		t.Fatal("want error for negative offset")
	}
	if !strings.Contains(err.Error(), "is negative") {
		t.Fatalf("want the negative-offset error, got %q", err.Error())
	}
	// Boundary: offset == -1 is the largest negative value and MUST still be
	// rejected (`< 0`, not `< -1`). A `< 0`→`< -1` mutant would let -1 through
	// and index data[-1:].
	_, err = (ReadToolOutput{}).Execute(ctx, []byte(`{"tool_call_id":"call-r6","offset":-1}`))
	if err == nil {
		t.Fatal("want error for offset == -1 (smallest negative)")
	}
	if !strings.Contains(err.Error(), "is negative") {
		t.Fatalf("want the negative-offset error for -1, got %q", err.Error())
	}
}

// TestReadToolOutput_LimitOneNotDefaulted: limit == 1 is positive and MUST be
// honoured verbatim (`<= 0`, not `<= 1`); a `<= 0`→`<= 1` mutant would silently
// widen a 1-byte request to the default window.
func TestReadToolOutput_LimitOneNotDefaulted(t *testing.T) {
	content := strings.Repeat("w", 10_000)
	ctx := seedSidecar(t, "sess-r7", "call-r7", content)
	res, err := ReadToolOutput{}.Execute(ctx, []byte(`{"tool_call_id":"call-r7","offset":0,"limit":1}`))
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	// Exactly 1 byte returned, footer reports the 0-1 window — not the default window.
	if res.Bytes != 1 {
		t.Fatalf("Bytes = %d, want 1 (limit=1 must not default)", res.Bytes)
	}
	if !strings.Contains(res.Preview, "showing bytes 0-1 of 10000, next offset 1") {
		t.Fatalf("limit-1 window footer wrong: %q", res.Preview)
	}
}

// TestReadToolOutputDefaultWindowValue is the ONE place the number is written down. Every
// other assertion derives from the constant, so retuning the window touches this test and
// nothing else — but an off-by-one mutation of defaultReadLimit still dies here, which is
// what the old hard-coded literal existed for (D-27/A4). Scattering the literal instead
// bought the same mutation coverage and a test whose NAME went stale on the first retune.
func TestReadToolOutputDefaultWindowValue(t *testing.T) {
	if defaultReadLimit != 30000 {
		t.Fatalf("defaultReadLimit = %d, want 30000 — retuning it is a deliberate contract change: update this pin and say why in the commit", defaultReadLimit)
	}
}

// TestReadToolOutput_OmittedLimitUsesTheDefaultWindow pins that an omitted limit returns
// exactly one default window and advertises the next offset. It used to be named after the
// value (…Is2048) and assert the literal — so retuning the window renamed a passing test
// into a lie. The contract is "one window, correctly reported", not the number.
func TestReadToolOutput_OmittedLimitUsesTheDefaultWindow(t *testing.T) {
	total := defaultReadLimit * 2
	content := strings.Repeat("d", total)
	ctx := seedSidecar(t, "sess-r8", "call-r8", content)
	res, err := ReadToolOutput{}.Execute(ctx, []byte(`{"tool_call_id":"call-r8"}`))
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if res.Bytes != defaultReadLimit {
		t.Fatalf("default window = %d bytes, want exactly defaultReadLimit (%d)", res.Bytes, defaultReadLimit)
	}
	want := fmt.Sprintf("showing bytes 0-%d of %d, next offset %d", defaultReadLimit, total, defaultReadLimit)
	if !strings.Contains(res.Preview, want) {
		t.Fatalf("default window footer = %q, want it to contain %q", res.Preview, want)
	}
}

func TestReadToolOutput_ClampsHugeLimit(t *testing.T) {
	content := strings.Repeat("z", maxReadToolOutputLimit*2)
	ctx := seedSidecar(t, "sess-limit", "call-limit", content)
	res, err := ReadToolOutput{}.Execute(ctx, []byte(`{"tool_call_id":"call-limit","limit":999999999}`))
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if res.Bytes != maxReadToolOutputLimit {
		t.Fatalf("Bytes = %d, want clamp %d", res.Bytes, maxReadToolOutputLimit)
	}
	if !strings.Contains(res.Preview, "limit clamped") {
		t.Fatalf("preview must mention clamp, got %q", res.Preview)
	}
}

func TestReadToolOutput_ReadsOnlyRequestedWindow(t *testing.T) {
	runDir := t.TempDir()
	ctx := ctxWithRunDir("sess-window", "call-window", runDir)
	path, err := sidecarPath(runDir, "sess-window", "call-window")
	if err != nil {
		t.Fatalf("sidecarPath: %v", err)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	f, err := os.Create(path)
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if _, err := f.WriteString(strings.Repeat("a", 10_000)); err != nil {
		t.Fatalf("write head: %v", err)
	}
	if _, err := f.WriteString("TARGET"); err != nil {
		t.Fatalf("write target: %v", err)
	}
	if err := f.Truncate(10 << 20); err != nil {
		t.Fatalf("sparse truncate: %v", err)
	}
	_ = f.Close()

	res, err := ReadToolOutput{}.Execute(ctx, []byte(`{"tool_call_id":"call-window","offset":10000,"limit":6}`))
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if !strings.HasPrefix(res.Preview, "TARGET") {
		t.Fatalf("want bounded window TARGET, got %.40q", res.Preview)
	}
}

func TestReadToolOutput_Deferred(t *testing.T) {
	if (ReadToolOutput{}).Spec().Deferred {
		t.Fatal("read_tool_output must be Deferred:false")
	}
	desc := (ReadToolOutput{}).Spec().Description + string((ReadToolOutput{}).Spec().Parameters)
	if !strings.Contains(strings.ToLower(desc), "byte") {
		t.Fatal("read_tool_output schema must mention bytes")
	}
}

var _ Tool = ReadToolOutput{}
