package tools

import (
	"archive/tar"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"strings"
	"testing"

	"github.com/chetto1983/aura/internal/config"
	"github.com/chetto1983/aura/internal/sandbox/usersandbox"
)

// fakeBoxBackend is a usersandbox.Backend double for the routed-tool unit tests. It records the
// exec/copy calls and can be told to fail Resolve (the fail-CLOSED path), return a canned exec
// result, or return/fail an artifact copy-out. It also implements the structural CopyArtifactsOut /
// CopyFileIn capabilities the router resolves for send_file / fs_write.
type fakeBoxBackend struct {
	resolveErr error

	execResult usersandbox.ExecResult
	execErr    error
	execCalls  []usersandbox.ExecRequest

	writeErr error
	writes   []fakeBoxWrite

	artifact     []byte // tar stream CopyArtifactsOut returns
	artifactErr  error
	copyOutCalls []string
}

type fakeBoxWrite struct {
	path    string
	content []byte
}

func (f *fakeBoxBackend) Resolve(_ context.Context, spec usersandbox.SandboxSpec) (usersandbox.BoxHandle, error) {
	if f.resolveErr != nil {
		return usersandbox.BoxHandle{}, f.resolveErr
	}
	return usersandbox.BoxHandle{ContainerID: "c-" + spec.IdentityID, IdentityID: spec.IdentityID}, nil
}

func (f *fakeBoxBackend) Exec(_ context.Context, _ usersandbox.BoxHandle, req usersandbox.ExecRequest) (usersandbox.ExecResult, error) {
	f.execCalls = append(f.execCalls, req)
	if f.execErr != nil {
		return usersandbox.ExecResult{}, f.execErr
	}
	return f.execResult, nil
}

func (f *fakeBoxBackend) Suspend(context.Context, usersandbox.BoxHandle) error { return nil }
func (f *fakeBoxBackend) Resume(context.Context, usersandbox.BoxHandle) error  { return nil }
func (f *fakeBoxBackend) Stop(context.Context, usersandbox.BoxHandle) error    { return nil }

func (f *fakeBoxBackend) CopyArtifactsOut(_ context.Context, _ usersandbox.BoxHandle, boxPath string) (io.ReadCloser, error) {
	f.copyOutCalls = append(f.copyOutCalls, boxPath)
	if f.artifactErr != nil {
		return nil, f.artifactErr
	}
	return io.NopCloser(bytes.NewReader(f.artifact)), nil
}

func (f *fakeBoxBackend) CopyFileIn(_ context.Context, _ usersandbox.BoxHandle, boxPath string, content []byte, _ int64) error {
	if f.writeErr != nil {
		return f.writeErr
	}
	f.writes = append(f.writes, fakeBoxWrite{path: boxPath, content: append([]byte(nil), content...)})
	return nil
}

// newStrictBoxRouter wraps a backend in a server_production (strict) router so Route contains into
// the box (routed=true) keyed on the seeded `local` identity for a bare ctx.
func newStrictBoxRouter(be usersandbox.Backend) *usersandbox.SandboxRouter {
	return usersandbox.NewSandboxRouter(be, config.ProfileServerProduction, config.SandboxConfig{
		Image: "aura-sandbox:test", CPULimit: 1, MemoryLimit: 1 << 30, PidsLimit: 128,
	})
}

// tarOneFile builds a one-entry tar (the CopyArtifactsOut stream shape) for a staged artifact.
func tarOneFile(t *testing.T, name string, content []byte) []byte {
	t.Helper()
	var buf bytes.Buffer
	tw := tar.NewWriter(&buf)
	if err := tw.WriteHeader(&tar.Header{Name: name, Mode: 0o644, Size: int64(len(content)), Typeflag: tar.TypeReg}); err != nil {
		t.Fatalf("tar header: %v", err)
	}
	if _, err := tw.Write(content); err != nil {
		t.Fatalf("tar write: %v", err)
	}
	if err := tw.Close(); err != nil {
		t.Fatalf("tar close: %v", err)
	}
	return buf.Bytes()
}

// TestShellExec_FailClosedNoHostFallback: under a strict profile a box that fails to Resolve makes
// shell_exec DENY with the sandbox_unavailable result — the host os/exec path is NEVER reached
// (the backend's Exec is never called; the output carries no host command result). D-09/GATE-01.
func TestShellExec_FailClosedNoHostFallback(t *testing.T) {
	be := &fakeBoxBackend{resolveErr: errors.New("dockerd down")}
	tool := &ShellExec{Router: newStrictBoxRouter(be)}
	ctx := ctxWith(t, "sess-fc", "call-fc")

	res, err := tool.Execute(ctx, json.RawMessage(`{"command":"echo LEAKED-TO-HOST"}`))
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if !strings.Contains(res.Preview, "sandbox_unavailable") {
		t.Fatalf("want sandbox_unavailable deny result, got: %q", res.Preview)
	}
	if strings.Contains(res.Preview, "LEAKED-TO-HOST") {
		t.Fatalf("host fallback executed — the command ran on the host: %q", res.Preview)
	}
	if len(be.execCalls) != 0 {
		t.Fatalf("backend.Exec must not be called when Resolve failed; got %d calls", len(be.execCalls))
	}
}

// TestShellExec_RoutedRunsInBox: under a strict profile the command runs INSIDE the box via
// backend.Exec, using POSIX /bin/sh with PLAIN pwd (never `pwd -W`), and the box $PWD is tracked
// from the cwd marker onto the footer/meta.
func TestShellExec_RoutedRunsInBox(t *testing.T) {
	be := &fakeBoxBackend{execResult: usersandbox.ExecResult{
		Stdout:   []byte("hello-box\n\n__AURA_CWD__ /workspace/sub\n"),
		ExitCode: 0,
	}}
	tool := &ShellExec{Router: newStrictBoxRouter(be)}
	ctx := ctxWith(t, "sess-box", "call-box")

	res, err := tool.Execute(ctx, json.RawMessage(`{"command":"echo hello-box"}`))
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if len(be.execCalls) != 1 {
		t.Fatalf("want exactly one box exec, got %d", len(be.execCalls))
	}
	cmd := be.execCalls[0].Command
	if !strings.Contains(cmd, "echo hello-box") {
		t.Fatalf("box command missing user command: %q", cmd)
	}
	if strings.Contains(cmd, "pwd -W") {
		t.Fatalf("routed box command must use PLAIN pwd, not `pwd -W` (Pitfall 6): %q", cmd)
	}
	if !strings.Contains(res.Preview, "hello-box") {
		t.Fatalf("preview missing box stdout: %q", res.Preview)
	}
	if res.Meta == nil || (*res.Meta)["cwd"] != "/workspace/sub" {
		t.Fatalf("meta cwd not tracked from the box marker: %#v", res.Meta)
	}
}

// TestShellExec_NilRouterHostUnchanged: a nil router keeps the host os/exec path byte-for-byte
// (dev/local_trusted), identical to today's behavior.
func TestShellExec_NilRouterHostUnchanged(t *testing.T) {
	tool := &ShellExec{}
	ctx := ctxWith(t, "sess-host", "call-host")
	res, err := tool.Execute(ctx, json.RawMessage(`{"command":"echo host-direct"}`))
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if !strings.Contains(res.Preview, "host-direct") {
		t.Fatalf("nil-router host path missing stdout: %q", res.Preview)
	}
}

// TestSnippetUse_StrictRendersSandboxPath: skill action=use renders the IN-BOX SandboxPath under a
// strict profile and the HostPath under a lenient one — and calls NO backend.Exec (action=use only
// chooses the path for the subsequent routed shell_exec).
func TestSnippetUse_StrictRendersSandboxPath(t *testing.T) {
	loader := newFakeLoader()
	loader.snippets = map[string]fakeSnippet{
		"calc": {
			instructions: "Adds two numbers.",
			hostPath:     "/host/export/calc/calc.py",
			sandboxPath:  "/skills/calc/calc.py",
			interpreter:  "python3",
		},
	}
	be := &fakeBoxBackend{}
	ctx := ctxWith(t, "sess-snip", "call-snip")

	strict := &SkillTool{Loader: loader, SandboxRouter: newStrictBoxRouter(be)}
	res, err := strict.Execute(ctx, json.RawMessage(`{"action":"use","name":"calc"}`))
	if err != nil {
		t.Fatalf("strict use: %v", err)
	}
	if !strings.Contains(res.Preview, "/skills/calc/calc.py") {
		t.Fatalf("strict snippet use must render the SandboxPath: %q", res.Preview)
	}
	if strings.Contains(res.Preview, "/host/export/calc/calc.py") {
		t.Fatalf("strict snippet use must NOT render the host path: %q", res.Preview)
	}

	lenient := &SkillTool{Loader: loader} // nil SandboxRouter → Strict()==false → host path
	res2, err := lenient.Execute(ctx, json.RawMessage(`{"action":"use","name":"calc"}`))
	if err != nil {
		t.Fatalf("lenient use: %v", err)
	}
	if !strings.Contains(res2.Preview, "/host/export/calc/calc.py") {
		t.Fatalf("lenient snippet use must render the host path: %q", res2.Preview)
	}
	if len(be.execCalls) != 0 {
		t.Fatalf("action=use must call NO backend.Exec; got %d", len(be.execCalls))
	}
}

// TestSendFile_StrictCopiesArtifactOut: a routed send_file stages the box artifact out via
// CopyArtifactsOut and delivers the staged host-side copy; a box copy-out failure denies.
func TestSendFile_StrictCopiesArtifactOut(t *testing.T) {
	be := &fakeBoxBackend{artifact: tarOneFile(t, "out.xlsx", []byte("XLSXDATA"))}
	tool := &SendFile{Router: newStrictBoxRouter(be)}
	ctx := ctxWith(t, "sess-sf", "call-sf")

	res, err := tool.Execute(ctx, json.RawMessage(`{"path":"/workspace/out.xlsx","caption":"results"}`))
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if res.Meta == nil {
		t.Fatalf("want an artifact descriptor Meta, got nil (preview %q)", res.Preview)
	}
	art, _ := (*res.Meta)["artifact"].(map[string]any)
	if art == nil || art["filename"] != "out.xlsx" {
		t.Fatalf("descriptor filename wrong: %#v", res.Meta)
	}
	if len(be.copyOutCalls) != 1 || be.copyOutCalls[0] != "/workspace/out.xlsx" {
		t.Fatalf("CopyArtifactsOut not called for the box path: %#v", be.copyOutCalls)
	}

	// Box copy-out failure → deny (never a host delivery).
	beFail := &fakeBoxBackend{artifactErr: errors.New("copy failed")}
	toolFail := &SendFile{Router: newStrictBoxRouter(beFail)}
	resFail, err := toolFail.Execute(ctx, json.RawMessage(`{"path":"/workspace/out.xlsx"}`))
	if err != nil {
		t.Fatalf("Execute (fail): %v", err)
	}
	if !strings.Contains(resFail.Preview, "sandbox_unavailable") {
		t.Fatalf("want deny on copy-out failure, got: %q", resFail.Preview)
	}
}

// TestSendFile_RoutedWorkspaceFence: a box path OUTSIDE /workspace is rejected by the pre-copy
// prefix-check (never copied out); a box path INSIDE /workspace is staged out and delivered.
func TestSendFile_RoutedWorkspaceFence(t *testing.T) {
	be := &fakeBoxBackend{artifact: tarOneFile(t, "ok.txt", []byte("OK"))}
	tool := &SendFile{Router: newStrictBoxRouter(be)}
	ctx := ctxWith(t, "sess-fence", "call-fence")

	outside, err := tool.Execute(ctx, json.RawMessage(`{"path":"/etc/passwd"}`))
	if err != nil {
		t.Fatalf("Execute (outside): %v", err)
	}
	if !strings.Contains(outside.Preview, "outside_workspace_unsupported") {
		t.Fatalf("want an outside-workspace reject for a non-/workspace box path: %q", outside.Preview)
	}
	if len(be.copyOutCalls) != 0 {
		t.Fatalf("an out-of-workspace box path must NOT be copied out; got %#v", be.copyOutCalls)
	}

	inside, err := tool.Execute(ctx, json.RawMessage(`{"path":"/workspace/ok.txt"}`))
	if err != nil {
		t.Fatalf("Execute (inside): %v", err)
	}
	if inside.Meta == nil {
		t.Fatalf("an in-workspace box artifact must be delivered: %q", inside.Preview)
	}
}

// TestFSRead_FailClosedNoHostFallback + routed read: a strict fs_read reads via the box; a Resolve
// failure denies without a host os.ReadFile.
func TestFSRead_RoutedReadsInBoxAndFailsClosed(t *testing.T) {
	be := &fakeBoxBackend{execResult: usersandbox.ExecResult{Stdout: []byte("box-file-body\n"), ExitCode: 0}}
	tool := &FSRead{Router: newStrictBoxRouter(be)}
	ctx := ctxWith(t, "sess-fr", "call-fr")
	res, err := tool.Execute(ctx, json.RawMessage(`{"path":"/workspace/data.txt"}`))
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if !strings.Contains(res.Preview, "box-file-body") {
		t.Fatalf("routed fs_read missing box content: %q", res.Preview)
	}
	if len(be.execCalls) != 1 || !strings.Contains(be.execCalls[0].Command, "head -c") {
		t.Fatalf("routed fs_read must read via a bounded head -c exec: %#v", be.execCalls)
	}

	beFail := &fakeBoxBackend{resolveErr: errors.New("no box")}
	toolFail := &FSRead{Router: newStrictBoxRouter(beFail)}
	resFail, err := toolFail.Execute(ctx, json.RawMessage(`{"path":"/etc/hostname"}`))
	if err != nil {
		t.Fatalf("Execute (fail): %v", err)
	}
	if !strings.Contains(resFail.Preview, "sandbox_unavailable") {
		t.Fatalf("want deny on Resolve failure: %q", resFail.Preview)
	}
}

// TestFSWrite_RoutedWritesInBox: a strict fs_write copies content into the box (never host), and
// refuses a write into the /skills mount.
func TestFSWrite_RoutedWritesInBoxAndSkillsFence(t *testing.T) {
	be := &fakeBoxBackend{}
	tool := &FSWrite{Router: newStrictBoxRouter(be)}
	ctx := ctxWith(t, "sess-fw", "call-fw")

	res, err := tool.Execute(ctx, json.RawMessage(`{"path":"/workspace/out.txt","content":"hello"}`))
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if !strings.Contains(res.Preview, "wrote 5 bytes to /workspace/out.txt") {
		t.Fatalf("routed fs_write preview wrong: %q", res.Preview)
	}
	if len(be.writes) != 1 || be.writes[0].path != "/workspace/out.txt" || string(be.writes[0].content) != "hello" {
		t.Fatalf("routed fs_write did not copy into the box: %#v", be.writes)
	}

	skills, err := tool.Execute(ctx, json.RawMessage(`{"path":"/skills/evil/evil.py","content":"x"}`))
	if err == nil {
		t.Fatalf("write into /skills must error (gated authoring), got result: %q", skills.Preview)
	}
	if !strings.Contains(err.Error(), "skills") {
		t.Fatalf("want a skills-fence error, got: %v", err)
	}
}
