package tools

import (
	"context"
	"fmt"
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

// Test 2 (Req#7): an unknown tool_call_id hard-fails with an error (not empty,
// not panic) — D-15.
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
}

// Test 3 (Req#7 defaults): offset omitted -> 0; limit omitted -> defaultReadLimit.
func TestReadToolOutput_Defaults(t *testing.T) {
	content := strings.Repeat("k", 10_000)
	ctx := seedSidecar(t, "sess-r3", "call-r3", content)

	res, err := ReadToolOutput{}.Execute(ctx, []byte(`{"tool_call_id":"call-r3"}`))
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	// Default window is defaultReadLimit bytes starting at 0.
	if !strings.Contains(res.Preview, fmt.Sprintf("showing bytes 0-%d of 10000", defaultReadLimit)) {
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
func TestReadToolOutput_PathTraversal(t *testing.T) {
	runDir := t.TempDir()
	ctx := ctxWithRunDir("sess-r5", "agent-call", runDir)
	for _, id := range []string{"..", "../../etc/passwd", "a/b", `a\b`} {
		args := fmt.Sprintf(`{"tool_call_id":%q}`, id)
		if _, err := (ReadToolOutput{}).Execute(ctx, []byte(args)); err == nil {
			t.Fatalf("want error for traversal id %q", id)
		}
	}
}

// Negative offset is rejected, not a panic.
func TestReadToolOutput_NegativeOffset(t *testing.T) {
	runDir := t.TempDir()
	ctx := ctxWithRunDir("sess-r6", "call-r6", runDir)
	if _, err := (ReadToolOutput{}).Execute(ctx, []byte(`{"tool_call_id":"call-r6","offset":-5}`)); err == nil {
		t.Fatal("want error for negative offset")
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
