package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/chetto1983/aura/internal/config"
	"github.com/chetto1983/aura/internal/sandbox/usersandbox"
)

// The routed branches of fs_edit / fs_glob / fs_grep run only under a strict profile with a
// live box, and the docker_integration tier that exercises them contributes ZERO coverage
// (CLAUDE.md): it is absent from the gate's tag matrix, so every one of those statements
// counts as uncovered no matter how green that tier is.
//
// They do not need a daemon, though. usersandbox.Backend is an exported five-verb interface
// and SandboxRouter.Exec delegates straight to it, so a fake backend puts the whole routed
// path under test — and gives a SHARPER test than the container one: canned Exec output pins
// the exact command composed and the exact parsing of what comes back, which a live box can
// only observe end-to-end.

// fakeBox is a usersandbox.Backend plus the structural fileWriter capability the router's
// WriteFile needs. It records every exec and answers from a scripted responder.
type fakeBox struct {
	execs    []usersandbox.ExecRequest
	respond  func(cmd string) usersandbox.ExecResult
	written  map[string]string
	resolveE error
	execE    error
	writeE   error
}

func (f *fakeBox) Resolve(context.Context, usersandbox.SandboxSpec) (usersandbox.BoxHandle, error) {
	if f.resolveE != nil {
		return usersandbox.BoxHandle{}, f.resolveE
	}
	return usersandbox.BoxHandle{ContainerID: "box-1", IdentityID: "id-1"}, nil
}

func (f *fakeBox) Exec(_ context.Context, _ usersandbox.BoxHandle, req usersandbox.ExecRequest) (usersandbox.ExecResult, error) {
	f.execs = append(f.execs, req)
	if f.execE != nil {
		return usersandbox.ExecResult{}, f.execE
	}
	if f.respond == nil {
		return usersandbox.ExecResult{}, nil
	}
	return f.respond(req.Command), nil
}

func (f *fakeBox) Suspend(context.Context, usersandbox.BoxHandle) error { return nil }
func (f *fakeBox) Resume(context.Context, usersandbox.BoxHandle) error  { return nil }
func (f *fakeBox) Stop(context.Context, usersandbox.BoxHandle) error    { return nil }

// CopyFileIn satisfies the router's optional fileWriter capability (fs_edit's write-back).
func (f *fakeBox) CopyFileIn(_ context.Context, _ usersandbox.BoxHandle, boxPath string, content []byte, _ int64) error {
	if f.writeE != nil {
		return f.writeE
	}
	if f.written == nil {
		f.written = map[string]string{}
	}
	f.written[boxPath] = string(content)
	return nil
}

func routerWith(be usersandbox.Backend) *usersandbox.SandboxRouter {
	return usersandbox.NewSandboxRouter(be, config.ProfileSingleUserHardened, config.SandboxConfig{
		Image: "aura-sandbox:latest", CPULimit: 1, MemoryLimit: 1 << 30, PidsLimit: 128, IdleTTLSec: 1800,
	})
}

// nulFrames builds the sweep output shape boxReadFiles parses: \0name\0content per file.
func nulFrames(pairs ...string) []byte {
	var b strings.Builder
	for i := 0; i+1 < len(pairs); i += 2 {
		b.WriteString("\x00" + pairs[i] + "\x00" + pairs[i+1])
	}
	return []byte(b.String())
}

func boxCtx(t *testing.T) context.Context {
	t.Helper()
	return WithToolCallContext(t.Context(), "session", "toolcall", t.TempDir(), 4096)
}

// hostDecoy is a host tree the routed tool is configured with. Nothing under it may appear in
// a routed result — the containment fs_read/fs_write already had.
func hostDecoy(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "hostonly.go"), []byte("HOST-DECOY needle\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	return dir
}

func TestFSGrepRoutedSweepsTheBoxAndParsesFrames(t *testing.T) {
	be := &fakeBox{respond: func(string) usersandbox.ExecResult {
		return usersandbox.ExecResult{Stdout: nulFrames(
			"./sub/app.go", "alpha\nport := 8080\n",
			"./node_modules/pkg/i.js", "port := 9999\n", // pruned during decode
			"./readme.md", "no match here\n",
		)}
	}}
	dir := hostDecoy(t)
	res, err := (&FSGrep{WorkspaceRoot: dir, Router: routerWith(be)}).
		Execute(boxCtx(t), json.RawMessage(`{"pattern":"port := \\d+"}`))
	if err != nil {
		t.Fatalf("routed fs_grep: %v", err)
	}
	if !strings.Contains(res.Preview, "sub/app.go:2: port := 8080") {
		t.Errorf("match not reported with box-relative path + line: %q", res.Preview)
	}
	if strings.Contains(res.Preview, "node_modules") {
		t.Errorf("pruned directory leaked into results: %q", res.Preview)
	}
	if strings.Contains(res.Preview, "hostonly.go") || strings.Contains(res.Preview, "HOST-DECOY") {
		t.Errorf("HOST tree reached by a routed grep: %q", res.Preview)
	}
	// The sweep must target the box workspace, not the host root it was configured with.
	if len(be.execs) != 1 || !strings.Contains(be.execs[0].Command, "'/workspace'") {
		t.Fatalf("sweep command = %+v, want one exec rooted at /workspace", be.execs)
	}
	if strings.Contains(be.execs[0].Command, dir) {
		t.Errorf("host path leaked into the box command: %q", be.execs[0].Command)
	}
	// The PATTERN must never be handed to the box — matching stays on Go's RE2 so a routed
	// search cannot answer differently from a host search.
	if strings.Contains(be.execs[0].Command, "port :=") {
		t.Errorf("pattern was pushed into the box shell: %q", be.execs[0].Command)
	}
}

func TestFSGrepRoutedReportsNoMatches(t *testing.T) {
	be := &fakeBox{respond: func(string) usersandbox.ExecResult {
		return usersandbox.ExecResult{Stdout: nulFrames("./a.txt", "nothing relevant\n")}
	}}
	res, err := (&FSGrep{WorkspaceRoot: hostDecoy(t), Router: routerWith(be)}).
		Execute(boxCtx(t), json.RawMessage(`{"pattern":"ZZZ"}`))
	if err != nil {
		t.Fatalf("routed fs_grep: %v", err)
	}
	if !strings.Contains(res.Preview, "[no matches]") {
		t.Errorf("preview = %q, want the no-matches marker", res.Preview)
	}
}

func TestFSGlobRoutedListsBoxTreeAndPrunes(t *testing.T) {
	be := &fakeBox{respond: func(string) usersandbox.ExecResult {
		return usersandbox.ExecResult{Stdout: []byte(strings.Join([]string{
			"/workspace/main.go",
			"/workspace/sub/app.go",
			"/workspace/vendor/dep/x.go", // pruned
			"/workspace/notes.md",        // pattern miss
		}, "\n") + "\n")}
	}}
	res, err := (&FSGlob{WorkspaceRoot: hostDecoy(t), Router: routerWith(be)}).
		Execute(boxCtx(t), json.RawMessage(`{"pattern":"**/*.go"}`))
	if err != nil {
		t.Fatalf("routed fs_glob: %v", err)
	}
	for _, want := range []string{"main.go", "sub/app.go"} {
		if !strings.Contains(res.Preview, want) {
			t.Errorf("missing %q in %q", want, res.Preview)
		}
	}
	if strings.Contains(res.Preview, "vendor") {
		t.Errorf("vendor not pruned: %q", res.Preview)
	}
	if strings.Contains(res.Preview, "notes.md") {
		t.Errorf("pattern miss listed: %q", res.Preview)
	}
	if strings.Contains(res.Preview, "hostonly.go") {
		t.Errorf("HOST tree reached by a routed glob: %q", res.Preview)
	}
}

func TestFSEditRoutedReadsBoxAppliesEditAndWritesBack(t *testing.T) {
	be := &fakeBox{respond: func(cmd string) usersandbox.ExecResult {
		if !strings.HasPrefix(cmd, "head -c ") {
			t.Errorf("fs_edit must read through the bounded head -c exec, got %q", cmd)
		}
		return usersandbox.ExecResult{Stdout: []byte("port := 8080\nname := \"aura\"\n")}
	}}
	dir := hostDecoy(t)
	hostTwin := filepath.Join(dir, "app.go")
	original := []byte("port := 8080\n")
	if err := os.WriteFile(hostTwin, original, 0o644); err != nil {
		t.Fatal(err)
	}

	res, err := (&FSEdit{WorkspaceRoot: dir, Router: routerWith(be)}).Execute(boxCtx(t), json.RawMessage(
		`{"path":"/workspace/app.go","old_string":"port := 8080","new_string":"port := 9090"}`))
	if err != nil {
		t.Fatalf("routed fs_edit: %v", err)
	}
	if !strings.Contains(res.Preview, "replaced 1 occurrence") {
		t.Errorf("result = %q", res.Preview)
	}
	got, ok := be.written["/workspace/app.go"]
	if !ok {
		t.Fatalf("nothing written back into the box: %+v", be.written)
	}
	if !strings.Contains(got, "port := 9090") || strings.Contains(got, "port := 8080") {
		t.Errorf("box write = %q, want the replacement applied", got)
	}
	after, err := os.ReadFile(hostTwin)
	if err != nil {
		t.Fatal(err)
	}
	if string(after) != string(original) {
		t.Errorf("routed fs_edit modified the HOST twin — containment breached: %q", after)
	}
}

func TestFSEditRoutedRefusesTheSkillsMount(t *testing.T) {
	be := &fakeBox{}
	_, err := (&FSEdit{WorkspaceRoot: hostDecoy(t), Router: routerWith(be)}).Execute(boxCtx(t), json.RawMessage(
		`{"path":"/skills/calc/calc.py","old_string":"a","new_string":"b"}`))
	if err == nil || !strings.Contains(err.Error(), "skills") {
		t.Fatalf("err = %v, want a refusal naming the skills mount", err)
	}
	if len(be.execs) != 0 {
		t.Errorf("the fence must refuse BEFORE touching the box, got %+v", be.execs)
	}
}

// A box failure must DENY, never silently fall back to the host (D-09/GATE-01). Each routed
// tool is checked at both seams that can fail: resolving the box, and the exec/write itself.
func TestRoutedFSToolsFailClosedOnBoxFailure(t *testing.T) {
	deny := func(t *testing.T, res ToolResult, err error, tool string) {
		t.Helper()
		if err != nil {
			t.Fatalf("%s: a box failure is a DENY result, not a Go error: %v", tool, err)
		}
		var payload map[string]string
		if uerr := json.Unmarshal([]byte(res.Preview), &payload); uerr != nil {
			t.Fatalf("%s: deny preview is not json: %q", tool, res.Preview)
		}
		if payload["error"] != "sandbox_unavailable" || payload["tool"] != tool {
			t.Fatalf("%s: payload = %+v, want sandbox_unavailable for this tool", tool, payload)
		}
		if !strings.Contains(payload["message"], "NOT run on the host") {
			t.Errorf("%s: the deny must say the host was not used: %q", tool, payload["message"])
		}
	}

	for _, tc := range []struct {
		name string
		be   *fakeBox
	}{
		{"box cannot be resolved", &fakeBox{resolveE: fmt.Errorf("dockerd down")}},
		{"exec fails inside the box", &fakeBox{execE: fmt.Errorf("exec refused")}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dir := hostDecoy(t)
			r := routerWith(tc.be)
			ctx := boxCtx(t)

			res, err := (&FSGrep{WorkspaceRoot: dir, Router: r}).Execute(ctx, json.RawMessage(`{"pattern":"needle"}`))
			deny(t, res, err, "fs_grep")

			res, err = (&FSGlob{WorkspaceRoot: dir, Router: r}).Execute(ctx, json.RawMessage(`{"pattern":"**/*.go"}`))
			deny(t, res, err, "fs_glob")

			res, err = (&FSEdit{WorkspaceRoot: dir, Router: r}).Execute(ctx, json.RawMessage(
				`{"path":"/workspace/app.go","old_string":"a","new_string":"b"}`))
			deny(t, res, err, "fs_edit")
		})
	}

	// The write-back seam: the read succeeds, the box refuses the write.
	be := &fakeBox{
		respond: func(string) usersandbox.ExecResult {
			return usersandbox.ExecResult{Stdout: []byte("a\n")}
		},
		writeE: fmt.Errorf("copy refused"),
	}
	res, err := (&FSEdit{WorkspaceRoot: hostDecoy(t), Router: routerWith(be)}).Execute(boxCtx(t), json.RawMessage(
		`{"path":"/workspace/app.go","old_string":"a","new_string":"b"}`))
	deny(t, res, err, "fs_edit")
}
