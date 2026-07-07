package tools

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// AG-046: fs_grep glob honors ** path semantics like fs_glob, so a model reusing
// `**/*.go` does not silently get zero matches.
func TestFSGrepGlobSupportsDoubleStar(t *testing.T) {
	dir := t.TempDir()
	sub := filepath.Join(dir, "pkg", "deep")
	if err := os.MkdirAll(sub, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(sub, "code.go"), []byte("needle here\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	ctx := ctxWith(t, "sess-grep", "call-grep")
	res, err := (&FSGrep{}).Execute(ctx, mustJSON(t, fsGrepArgs{Pattern: "needle", Path: dir, Glob: "**/*.go"}))
	if err != nil {
		t.Fatalf("grep: %v", err)
	}
	if !strings.Contains(res.Preview, "needle") {
		t.Fatalf("**/*.go grep found no matches (AG-046): %q", res.Preview)
	}
}

// AG-046: the plain `*.go` basename glob still works for grep.
func TestFSGrepGlobBasenameStillWorks(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "a.go"), []byte("needle\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "b.txt"), []byte("needle\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	ctx := ctxWith(t, "sess-grep2", "call-grep2")
	res, err := (&FSGrep{}).Execute(ctx, mustJSON(t, fsGrepArgs{Pattern: "needle", Path: dir, Glob: "*.go"}))
	if err != nil {
		t.Fatalf("grep: %v", err)
	}
	if !strings.Contains(res.Preview, "a.go") || strings.Contains(res.Preview, "b.txt") {
		t.Fatalf("*.go basename glob wrong (AG-046): %q", res.Preview)
	}
}

// AG-019: send_file fails closed in a non-CLI context when the workspace root is
// empty, instead of silently allowing unrestricted host delivery.
func TestSendFileFailsClosedWhenRootEmptyNonCLI(t *testing.T) {
	path := filepath.Join(t.TempDir(), "leak.txt")
	if err := os.WriteFile(path, []byte("secret"), 0o644); err != nil {
		t.Fatal(err)
	}
	raw, _ := json.Marshal(map[string]string{"path": path})
	res, err := (&SendFile{RequireWorkspace: true}).Execute(ctxWith(t, "sess-sf-fc", "call-sf-fc"), raw)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if res.Meta != nil {
		t.Fatal("non-CLI send_file with empty root must NOT deliver (fail closed)")
	}
	if !strings.Contains(res.Preview, "workspace") {
		t.Fatalf("fail-closed result must mention the missing workspace fence: %q", res.Preview)
	}
}

// mutableTool lets a test change a tool's description in place to simulate an MCP
// reconnect re-advertising the same tool name with a new description.
type mutableTool struct {
	name string
	desc string
}

func (m *mutableTool) Spec() Spec {
	return Spec{Name: m.name, Summary: "s", Description: m.desc, Parameters: json.RawMessage(`{"type":"object"}`), Deferred: true}
}

func (*mutableTool) Execute(context.Context, json.RawMessage) (ToolResult, error) {
	return ToolResult{}, nil
}

// AG-020 (WR-01): when an existing tool's description changes and InvalidateIndex
// fires, the semantic bank re-embeds that tool so its vector is no longer stale.
func TestToolSearch_DescriptionChangeReEmbeds(t *testing.T) {
	reg := NewRegistry()
	mt := &mutableTool{name: "alpha", desc: "original alpha capability"}
	reg.Register(mt)
	reg.Register(bm25Tool{name: "beta", desc: "beta gadget"})
	emb := &bagEmbedder{}
	ts := &ToolSearch{Registry: reg, Embed: emb}
	ctx := ctxWith(t, "sess-rehash", "call-rehash")

	if _, err := ts.Execute(ctx, []byte(`{"query":"alpha capability"}`)); err != nil {
		t.Fatalf("first Execute: %v", err)
	}
	emb.mu.Lock()
	afterFirst := emb.embedded
	emb.mu.Unlock()

	// Change alpha's description (a reconnect re-advertised it), then invalidate.
	mt.desc = "completely rewritten zephyr quasar tool"
	ts.InvalidateIndex()

	if _, err := ts.Execute(ctx, []byte(`{"query":"zephyr quasar"}`)); err != nil {
		t.Fatalf("post-change Execute: %v", err)
	}
	emb.mu.Lock()
	delta := emb.embedded - afterFirst
	emb.mu.Unlock()
	// A description change must trigger at least one re-embed of the changed tool
	// (plus the query embed). A stale append-only bank would embed only the query.
	if delta < 2 {
		t.Fatalf("description change did not re-embed the tool (AG-020): delta=%d", delta)
	}
}

// AG-015: finished background shells are reclaimed on Evict (session end), not
// only on the next start.
func TestBackgroundShells_EvictReclaimsFinished(t *testing.T) {
	b := NewBackgroundShells(nil)
	id, err := b.start(context.Background(), "exit 0", t.TempDir(), nil)
	if err != nil {
		t.Fatalf("start: %v", err)
	}
	// Wait for the reaper to finish the shell.
	waitShellDone(t, b, id)
	b.Evict("any-session")
	if _, ok := b.get(id); ok {
		t.Fatal("Evict did not reclaim the finished background shell (AG-015)")
	}
}

// AG-017: byte-exact incremental paging across writes — each snapshot returns
// ONLY the new bytes since the prior read, with no overlap or gap, and the buffer
// is compacted in one step so readOff never drifts.
func TestBackgroundShellIncrementalPagingByteExact(t *testing.T) {
	sh := &bgShell{bufCap: 64}
	if _, err := sh.Write([]byte("alpha")); err != nil {
		t.Fatalf("write1: %v", err)
	}
	chunk, _ := sh.snapshot(nil)
	if chunk != "alpha" {
		t.Fatalf("first snapshot = %q, want alpha", chunk)
	}
	if _, err := sh.Write([]byte("bravo")); err != nil {
		t.Fatalf("write2: %v", err)
	}
	chunk, _ = sh.snapshot(nil)
	if chunk != "bravo" {
		t.Fatalf("second snapshot = %q, want only the new bytes 'bravo'", chunk)
	}
	// An empty snapshot after a full drain returns no bytes and leaves state clean.
	chunk, _ = sh.snapshot(nil)
	if chunk != "" || len(sh.buf) != 0 || sh.readOff != 0 {
		t.Fatalf("drained snapshot = %q buf=%q readOff=%d, want empty/clean", chunk, sh.buf, sh.readOff)
	}
}

func waitShellDone(t *testing.T, b *BackgroundShells, id string) {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for {
		sh, ok := b.get(id)
		if !ok {
			return
		}
		_, status := sh.snapshot(nil)
		if status != "running" {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("background shell %s still running after deadline", id)
		}
		time.Sleep(20 * time.Millisecond)
	}
}

// AG-018: the approval digest is normalized so /tmp and /tmp/ (trailing slash)
// hash identically — an operator who approved a command does not get re-prompted
// because the model resent it with a cosmetically-different cwd.
func TestShellApprovalDigestNormalizesCwd(t *testing.T) {
	a := ShellApprovalDigest("rm -rf x", "/tmp")
	b := ShellApprovalDigest("rm -rf x", "/tmp/")
	if a != b {
		t.Fatalf("approval digest differs for /tmp vs /tmp/ (AG-018): %s vs %s", a, b)
	}
}

// AG-050: read_tool_output rejects a sidecar runDir that is not absolute (the
// WithToolCallContext invariant is now asserted, not assumed).
func TestReadToolOutput_RejectsRelativeRunDir(t *testing.T) {
	ctx := ctxWithRunDir("sess-rel", "call-rel", "relative/run/dir")
	_, err := (ReadToolOutput{}).Execute(ctx, []byte(`{"tool_call_id":"call-rel"}`))
	if err == nil {
		t.Fatal("read_tool_output with a relative runDir err = nil, want invariant rejection (AG-050)")
	}
	if !strings.Contains(strings.ToLower(err.Error()), "absolute") {
		t.Fatalf("relative-runDir err = %v, want absolute-path invariant reason", err)
	}
}
