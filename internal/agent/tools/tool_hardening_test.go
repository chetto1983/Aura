package tools

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/chetto1983/aura/internal/sandbox/usersandbox"
)

// AG-046: fs_grep glob honors ** path semantics like fs_glob, so a model reusing `**/*.go` does
// not silently get zero matches. globMatch is shared, and the box arm feeds it the same two
// arguments (root-relative path, basename) the deleted host walk did.
func TestFSGrepGlobSupportsDoubleStar(t *testing.T) {
	be := &fakeBox{respond: func(cmd string) usersandbox.ExecResult {
		return usersandbox.ExecResult{Stdout: boxFrames(t, cmd, boxWorkspaceRoot, "pkg/deep/code.go", "needle here\n")}
	}}
	ctx := ctxWith(t, "sess-grep", "call-grep")
	res, err := (&FSGrep{Router: routerWith(be)}).
		Execute(ctx, mustJSON(t, fsGrepArgs{Pattern: "needle", Glob: "**/*.go"}))
	if err != nil {
		t.Fatalf("grep: %v", err)
	}
	if !strings.Contains(res.Preview, "needle") {
		t.Fatalf("**/*.go grep found no matches (AG-046): %q", res.Preview)
	}
}

// AG-046: the plain `*.go` basename glob still works for grep.
func TestFSGrepGlobBasenameStillWorks(t *testing.T) {
	be := &fakeBox{respond: func(cmd string) usersandbox.ExecResult {
		return usersandbox.ExecResult{Stdout: boxFrames(t, cmd, boxWorkspaceRoot, "a.go", "needle\n", "b.txt", "needle\n")}
	}}
	ctx := ctxWith(t, "sess-grep2", "call-grep2")
	res, err := (&FSGrep{Router: routerWith(be)}).
		Execute(ctx, mustJSON(t, fsGrepArgs{Pattern: "needle", Glob: "*.go"}))
	if err != nil {
		t.Fatalf("grep: %v", err)
	}
	if !strings.Contains(res.Preview, "a.go") || strings.Contains(res.Preview, "b.txt") {
		t.Fatalf("*.go basename glob wrong (AG-046): %q", res.Preview)
	}
}

// mutableTool lets a test change a tool's advertised capability in place to
// simulate an MCP reconnect re-advertising the same tool name with new wording.
type mutableTool struct {
	name    string
	summary string
}

func (m *mutableTool) Spec() Spec {
	return Spec{Name: m.name, Summary: m.summary, Description: "usage prose", Parameters: json.RawMessage(`{"type":"object"}`), Deferred: true}
}

func (*mutableTool) Execute(context.Context, json.RawMessage) (ToolResult, error) {
	return ToolResult{}, nil
}

// AG-020 (WR-01): when a registered tool changes its spec and InvalidateIndex
// fires, the rebuilt index reflects the NEW text — the old one is not served from a
// cache. On a reconnect an MCP server can re-advertise the same name with different
// wording, and a stale entry would keep answering for words the tool no longer has.
func TestToolSearch_SpecChangeIsReindexed(t *testing.T) {
	reg := NewRegistry()
	mt := &mutableTool{name: "alpha", summary: "original alpha capability"}
	reg.Register(mt)
	reg.Register(bm25Tool{name: "beta", summary: "beta gadget"})
	ts := &ToolSearch{Registry: reg}
	ctx := ctxWith(t, "sess-rehash", "call-rehash")

	if _, err := ts.Execute(ctx, []byte(`{"query":"alpha capability"}`)); err != nil {
		t.Fatalf("first Execute: %v", err)
	}

	mt.summary = "completely rewritten zephyr quasar tool"
	ts.InvalidateIndex()

	res, err := ts.Execute(ctx, []byte(`{"query":"zephyr quasar"}`))
	if err != nil {
		t.Fatalf("post-change Execute: %v", err)
	}
	if !strings.Contains(res.Preview, "## alpha") {
		t.Fatalf("the rewritten spec was not reindexed: %q", res.Preview)
	}
	// The retired wording no longer retrieves it. The query omits the NAME on
	// purpose: a tool stays reachable by name forever, so only the capability words
	// can show whether the old text is still in the index.
	if res, err = ts.Execute(ctx, []byte(`{"query":"original capability"}`)); err != nil {
		t.Fatalf("stale-word Execute: %v", err)
	}
	if strings.Contains(res.Preview, "## alpha") {
		t.Fatalf("the stale wording still retrieves the tool: %q", res.Preview)
	}
}

func TestBackgroundShells_EvictReclaimsFinished(t *testing.T) {
	b := NewBackgroundShells(nil)
	const id = "job-evict"
	registerJob(t, b, context.Background(), id).finish(&bgBoxExit{code: 0})
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
