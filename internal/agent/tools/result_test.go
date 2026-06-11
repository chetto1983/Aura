package tools

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"unicode/utf8"

	"pgregory.net/rapid"
)

const testCap = 2048

func ctxWith(t *testing.T, sessionID, toolCallID string) context.Context {
	t.Helper()
	return WithToolCallContext(context.Background(), sessionID, toolCallID, t.TempDir(), testCap)
}

func ctxWithRunDir(sessionID, toolCallID, runDir string) context.Context {
	return WithToolCallContext(context.Background(), sessionID, toolCallID, runDir, testCap)
}

// Test 1 (Req#6 large): a >cap output truncates the preview, spills the full
// bytes to a sidecar, and the history content (preview+footer) stays ≤ ~2 KiB.
func TestNewResult_LargeSpills(t *testing.T) {
	runDir := t.TempDir()
	content := strings.Repeat("a", 100_000)
	ctx := ctxWithRunDir("sess-1", "call-1", runDir)

	res, err := NewResult(ctx, content)
	if err != nil {
		t.Fatalf("NewResult: %v", err)
	}
	if !res.Truncated {
		t.Fatal("want Truncated=true for a 100KB output")
	}
	if res.Bytes != 100_000 {
		t.Fatalf("Bytes = %d, want 100000", res.Bytes)
	}
	// History content is preview (≤cap) + a short footer pointer; well under ~2 KiB + a small footer.
	if len(res.Preview) > testCap+512 {
		t.Fatalf("history content %d bytes, want ≤ cap+footer", len(res.Preview))
	}
	if !strings.Contains(res.Preview, "read_tool_output") {
		t.Fatalf("preview missing read_tool_output footer pointer: %q", res.Preview)
	}
	full, err := os.ReadFile(res.FullPath)
	if err != nil {
		t.Fatalf("read sidecar: %v", err)
	}
	if len(full) != 100_000 {
		t.Fatalf("sidecar holds %d bytes, want 100000", len(full))
	}
}

// Test 2 (Req#6 small): a ≤cap output writes no sidecar and the history content
// equals the raw preview, FullPath empty.
func TestNewResultReservingTail_LargeKeepsFooter(t *testing.T) {
	runDir := t.TempDir()
	body := strings.Repeat("body-", testCap)
	footer := "\n[exit code 7]\n[aura_shell {\"exit_code\":7}]"
	ctx := ctxWithRunDir("sess-tail", "call-tail", runDir)

	res, err := NewResultReservingTail(ctx, body, footer)
	if err != nil {
		t.Fatalf("NewResultReservingTail: %v", err)
	}
	if !res.Truncated {
		t.Fatal("want Truncated=true for body larger than preview cap")
	}
	if !strings.Contains(res.Preview, "[exit code 7]") || !strings.Contains(res.Preview, `"exit_code":7`) {
		t.Fatalf("preview dropped reserved footer: %q", res.Preview)
	}
	if !strings.Contains(res.Preview, "read_tool_output") {
		t.Fatalf("preview missing read_tool_output pointer: %q", res.Preview)
	}
	full, err := os.ReadFile(res.FullPath)
	if err != nil {
		t.Fatalf("read sidecar: %v", err)
	}
	if string(full) != body+footer {
		t.Fatal("sidecar must hold the full body plus reserved footer")
	}
}

func TestNewResult_SmallNoSidecar(t *testing.T) {
	runDir := t.TempDir()
	content := "small output"
	ctx := ctxWithRunDir("sess-2", "call-2", runDir)

	res, err := NewResult(ctx, content)
	if err != nil {
		t.Fatalf("NewResult: %v", err)
	}
	if res.Truncated {
		t.Fatal("want Truncated=false for a small output")
	}
	if res.FullPath != "" {
		t.Fatalf("FullPath = %q, want empty", res.FullPath)
	}
	if res.Preview != content {
		t.Fatalf("Preview = %q, want raw content", res.Preview)
	}
	if res.Bytes != len(content) {
		t.Fatalf("Bytes = %d, want %d", res.Bytes, len(content))
	}
	// No file written anywhere under the run dir.
	convDir := filepath.Join(runDir, "conversations")
	if _, err := os.Stat(convDir); !os.IsNotExist(err) {
		t.Fatalf("conversations dir created for a small output: %v", err)
	}
}

// TestNewResult_ExactlyCapNoSidecar: content whose length is EXACTLY the cap is
// the boundary of the `total <= tc.cap` short-circuit. It must NOT truncate and
// must NOT write a sidecar — a `<=`→`<` mutant would spill at the boundary.
func TestNewResult_ExactlyCapNoSidecar(t *testing.T) {
	runDir := t.TempDir()
	content := strings.Repeat("c", testCap) // len == cap exactly
	ctx := ctxWithRunDir("sess-cap", "call-cap", runDir)

	res, err := NewResult(ctx, content)
	if err != nil {
		t.Fatalf("NewResult: %v", err)
	}
	if res.Truncated {
		t.Fatalf("content of exactly cap bytes (%d) must NOT truncate", testCap)
	}
	if res.FullPath != "" {
		t.Fatalf("FullPath = %q, want empty (no sidecar at the cap boundary)", res.FullPath)
	}
	if res.Preview != content {
		t.Fatal("Preview must equal the raw content at the cap boundary")
	}
	convDir := filepath.Join(runDir, "conversations")
	if _, err := os.Stat(convDir); !os.IsNotExist(err) {
		t.Fatalf("conversations dir created at the cap boundary: %v", err)
	}
}

// Test 3 (Req#6 property, rapid): for any string + any cap, truncatePreview
// output is valid UTF-8, ≤ cap bytes, and a prefix of the input.
func TestTruncatePreview_Property(t *testing.T) {
	rapid.Check(t, func(rt *rapid.T) {
		content := rapid.String().Draw(rt, "content")
		capBytes := rapid.IntRange(0, len(content)+16).Draw(rt, "cap")
		got := truncatePreview(content, capBytes)
		if !utf8.ValidString(got) {
			rt.Fatalf("truncated preview is not valid UTF-8: %q", got)
		}
		if len(got) > capBytes {
			rt.Fatalf("len(got)=%d > cap=%d", len(got), capBytes)
		}
		if !strings.HasPrefix(content, got) {
			rt.Fatalf("got %q is not a prefix of %q", got, content)
		}
	})
}

// Test 3b: a multi-byte rune straddling the cap boundary is never split.
func TestTruncatePreview_RuneBoundary(t *testing.T) {
	// "é" is 2 bytes (0xC3 0xA9). Repeat so the cap lands mid-rune.
	content := strings.Repeat("é", 100) // 200 bytes
	got := truncatePreview(content, 51) // odd cap → must back off to an even boundary
	if !utf8.ValidString(got) {
		t.Fatalf("split a multi-byte rune: %q", got)
	}
	if len(got) > 51 {
		t.Fatalf("len=%d > 51", len(got))
	}
}

// Test 4 (Req#8): the sidecar path stays inside the session directory, carries
// the provider id plus an opaque suffix, and is created lazily on first persist.
func TestSidecarLayout(t *testing.T) {
	runDir := t.TempDir()
	ctx := ctxWithRunDir("sess-XYZ", "call-ABC", runDir)
	res, err := NewResult(ctx, strings.Repeat("z", 5000))
	if err != nil {
		t.Fatalf("NewResult: %v", err)
	}
	wantDir := filepath.Join(runDir, "conversations", "sess-XYZ")
	if filepath.Dir(res.FullPath) != wantDir {
		t.Fatalf("FullPath dir = %q, want %q", filepath.Dir(res.FullPath), wantDir)
	}
	if !strings.HasSuffix(res.FullPath, ".result") {
		t.Fatalf("FullPath = %q, want .result suffix", res.FullPath)
	}
	if !strings.Contains(filepath.Base(res.FullPath), "call-ABC-") {
		t.Fatalf("FullPath base = %q, want provider id plus opaque suffix", filepath.Base(res.FullPath))
	}
	if _, err := os.Stat(res.FullPath); err != nil {
		t.Fatalf("sidecar not created lazily: %v", err)
	}
}

func TestSidecarFilePermissionsAreOwnerOnly(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Go on Windows does not expose owner-only ACLs through FileMode.Perm")
	}
	runDir := t.TempDir()
	ctx := ctxWithRunDir("sess-perm", "call-perm", runDir)
	res, err := NewResult(ctx, strings.Repeat("x", testCap+100))
	if err != nil {
		t.Fatalf("NewResult: %v", err)
	}
	info, err := os.Stat(res.FullPath)
	if err != nil {
		t.Fatalf("stat sidecar: %v", err)
	}
	if got := info.Mode().Perm(); got != 0o600 {
		t.Fatalf("sidecar permissions = %#o, want 0600", got)
	}
}

func TestSidecarIDRejectsWindowsADSColon(t *testing.T) {
	runDir := t.TempDir()
	ctx := ctxWithRunDir("sess", "call:ads", runDir)
	_, err := NewResult(ctx, strings.Repeat("p", testCap+100))
	if err == nil {
		t.Fatal("want an error for a colon in tool_call_id")
	}
	if !strings.Contains(err.Error(), "invalid character") {
		t.Fatalf("want invalid character error, got %q", err.Error())
	}
}

func TestNewResult_ReusedProviderToolCallIDDoesNotOverwriteSidecar(t *testing.T) {
	runDir := t.TempDir()
	ctx1 := WithToolCallContext(context.Background(), "sess-reuse", "call-reused", runDir, testCap)
	ctx2 := WithToolCallContext(context.Background(), "sess-reuse", "call-reused", runDir, testCap)

	res1, err := NewResult(ctx1, strings.Repeat("a", testCap+100))
	if err != nil {
		t.Fatalf("NewResult first: %v", err)
	}
	res2, err := NewResult(ctx2, strings.Repeat("b", testCap+100))
	if err != nil {
		t.Fatalf("NewResult second: %v", err)
	}
	if res1.FullPath == res2.FullPath {
		t.Fatalf("reused provider tool_call_id overwrote sidecar path %q", res1.FullPath)
	}
	got1, err := os.ReadFile(res1.FullPath)
	if err != nil || !strings.HasPrefix(string(got1), "aaa") {
		t.Fatalf("first sidecar not preserved, bytes=%q err=%v", string(got1[:min(len(got1), 8)]), err)
	}
	got2, err := os.ReadFile(res2.FullPath)
	if err != nil || !strings.HasPrefix(string(got2), "bbb") {
		t.Fatalf("second sidecar not preserved, bytes=%q err=%v", string(got2[:min(len(got2), 8)]), err)
	}
}

// Test 5 (D-29): a sidecar write failure degrades clean — Preview carries a
// "full output unavailable" note and NO error is returned (turn continues).
func TestNewResult_WriteFailureDegrades(t *testing.T) {
	// Point run_dir at a regular FILE so MkdirAll under it fails.
	tmp := t.TempDir()
	blocker := filepath.Join(tmp, "notadir")
	if err := os.WriteFile(blocker, []byte("x"), 0o644); err != nil {
		t.Fatalf("setup: %v", err)
	}
	ctx := ctxWithRunDir("sess-5", "call-5", blocker)

	res, err := NewResult(ctx, strings.Repeat("q", 5000))
	if err != nil {
		t.Fatalf("NewResult returned a terminal error, want clean degrade: %v", err)
	}
	if !res.Truncated {
		t.Fatal("want Truncated=true")
	}
	if res.FullPath != "" {
		t.Fatalf("FullPath = %q, want empty on write failure", res.FullPath)
	}
	if !strings.Contains(res.Preview, "full output unavailable") {
		t.Fatalf("preview missing degradation note: %q", res.Preview)
	}
}

// Test 7 (T-03-07 path traversal): ids containing `..`/`../..`/path separators
// are rejected before filepath.Join — NewResult returns an error and NO file is
// written outside <run_dir>/conversations/.
func TestSidecarPathTraversal(t *testing.T) {
	cases := []struct {
		name              string
		sessionID, callID string
	}{
		{"session dotdot", "..", "call-1"},
		{"session dotdot nested", "../..", "call-1"},
		{"session fwd slash", "a/b", "call-1"},
		{"session back slash", `a\b`, "call-1"},
		{"callid dotdot", "sess", ".."},
		{"callid dotdot escape", "sess", "../../../etc/passwd"},
		{"callid fwd slash", "sess", "a/b"},
		{"callid back slash", "sess", `a\b`},
		// Leading separator at index 0: kills a loop `i:=0`→`i:=1` mutant that
		// would skip the first byte of the id.
		{"session leading fwd slash", "/abc", "call-1"},
		{"session leading back slash", `\abc`, "call-1"},
		{"callid leading fwd slash", "sess", "/abc"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			runDir := t.TempDir()
			ctx := ctxWithRunDir(tc.sessionID, tc.callID, runDir)
			// Force the spill path (>cap) so id validation gates the write.
			_, err := NewResult(ctx, strings.Repeat("p", 5000))
			if err == nil {
				t.Fatal("want an error for a traversal-shaped id, got nil")
			}
			// No file escaped: nothing exists outside the run dir's conversations prefix.
			// Specifically, the parent of run_dir holds no .result file.
			parent := filepath.Dir(runDir)
			entries, _ := os.ReadDir(parent)
			for _, e := range entries {
				if strings.HasSuffix(e.Name(), ".result") {
					t.Fatalf("traversal escaped: %s in %s", e.Name(), parent)
				}
			}
		})
	}
}

// Test 6 (migration): TextResponse + ToolSearch satisfy the new signature and
// preserve their behavior.
func TestTextResponse_Migrated(t *testing.T) {
	res, err := TextResponse{}.Execute(context.Background(), []byte(`{"text":"hello"}`))
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if res.Preview != "hello" {
		t.Fatalf("Preview = %q, want hello", res.Preview)
	}
	if res.Truncated {
		t.Fatal("text_response must not spill")
	}
	if _, err := (TextResponse{}).Execute(context.Background(), []byte(`{}`)); err == nil {
		t.Fatal("want error for empty text")
	}
}

func TestToolSearch_Migrated(t *testing.T) {
	reg := NewRegistry()
	reg.Register(TextResponse{})
	ts := &ToolSearch{Registry: reg}
	ctx := ctxWith(t, "sess-ts", "call-ts")

	res, err := ts.Execute(ctx, []byte(`{"query":"nonexistent"}`))
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	// Contract updated with amendment #49, retargeted #52/D-41: the no-result reply
	// carries the fixed orientation tail pointing at the always-on find-skills skill
	// (the deleted `action=catalog` routing is gone; capability gaps route into the
	// host-terminal skills loop).
	if res.Preview != noMatchOrientation {
		t.Fatalf("Preview = %q, want the noMatchOrientation constant", res.Preview)
	}
	if _, err := ts.Execute(ctx, []byte(`{"query":""}`)); err == nil {
		t.Fatal("want error for empty query")
	}
}

// Compile-time assertions that the migrated tools satisfy the Tool interface.
var (
	_ Tool = TextResponse{}
	_ Tool = (*ToolSearch)(nil)
)
