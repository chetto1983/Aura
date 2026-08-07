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

// TestShellExec_NilRouterDenies replaces TestShellExec_NilRouterHostUnchanged, which pinned the
// removed behaviour: a nil router used to mean "run it on the host". A router is no longer
// optional, so an absent one is a failure mode and must DENY. This is the invariant a future
// regression re-introducing a host arm would trip.
func TestShellExec_NilRouterDenies(t *testing.T) {
	tool := &ShellExec{}
	ctx := ctxWith(t, "sess-host", "call-host")
	res, err := tool.Execute(ctx, json.RawMessage(`{"command":"echo LEAKED-TO-HOST"}`))
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if !strings.Contains(res.Preview, "sandbox_unavailable") {
		t.Fatalf("a router-less shell_exec must DENY, got: %q", res.Preview)
	}
	if strings.Contains(res.Preview, "LEAKED-TO-HOST") {
		t.Fatalf("the command reached a host shell: %q", res.Preview)
	}
}

// AG-018. A cwd the box cannot chdir into is the model's own argument mistake, and with one
// execution path it lands on executeInBox's blanket infra-error branch, which answers
// sandbox_unavailable — "the sandbox runtime is down and an operator must restore it". That is
// advice a model can act on only by retrying the same broken call forever. The deleted host arm
// caught it with an os.Stat BEFORE the call; a box path cannot be stat'ed from here, so the box is
// asked directly — and only once the real exec has already failed, so the common case pays nothing.

// cwdProbeBackend refuses every real command the way a daemon refuses a bad WorkingDir, and answers
// the `[ -d … ]` probe with a scripted result.
type cwdProbeBackend struct {
	fakeBoxBackend
	probeExit int
	probeErr  error
	probes    []string
}

func (b *cwdProbeBackend) Exec(_ context.Context, _ usersandbox.BoxHandle, req usersandbox.ExecRequest) (usersandbox.ExecResult, error) {
	b.execCalls = append(b.execCalls, req)
	if strings.HasPrefix(req.Command, "[ -d ") {
		b.probes = append(b.probes, req.Command)
		if b.probeErr != nil {
			return usersandbox.ExecResult{}, b.probeErr
		}
		return usersandbox.ExecResult{ExitCode: b.probeExit}, nil
	}
	return usersandbox.ExecResult{}, errors.New("OCI runtime exec failed: chdir to cwd: no such file or directory")
}

func TestShellExec_BadCwdIsAnArgumentErrorNotAnOutage(t *testing.T) {
	be := &cwdProbeBackend{probeExit: 1}
	tool := &ShellExec{Router: newStrictBoxRouter(be)}

	raw, _ := json.Marshal(shellExecArgs{Command: "ls", Cwd: "/workspace/nope"})
	res, err := tool.Execute(ctxWith(t, "sess-badcwd", "call-1"), raw)
	if err == nil {
		t.Fatalf("a bad cwd must be a self-correctable error, got result: %q", res.Preview)
	}
	if !strings.Contains(err.Error(), "/workspace/nope") || !strings.Contains(err.Error(), "not a directory") {
		t.Fatalf("error must name the offending directory: %v", err)
	}
	if strings.Contains(err.Error(), "sandbox") && strings.Contains(err.Error(), "down") {
		t.Fatalf("a mistyped cwd must not read as an outage: %v", err)
	}
	if len(be.probes) != 1 || !strings.Contains(be.probes[0], shellQuoteArg("/workspace/nope")) {
		t.Fatalf("the probe must ask the box about the QUOTED dir: %#v", be.probes)
	}
}

// The classifier must not swallow real outages: when the box says the directory is fine, an exec
// failure is still infra and still DENIES. Without this the AG-018 fix would be a hole in GATE-01.
func TestShellExec_ExecFailureWithAGoodCwdStillDenies(t *testing.T) {
	for _, tc := range []struct {
		name string
		be   *cwdProbeBackend
	}{
		{"dir exists", &cwdProbeBackend{probeExit: 0}},
		{"box cannot even answer the probe", &cwdProbeBackend{probeErr: errors.New("dockerd gone")}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			tool := &ShellExec{Router: newStrictBoxRouter(tc.be)}
			raw, _ := json.Marshal(shellExecArgs{Command: "ls", Cwd: "/workspace/fine"})
			res, err := tool.Execute(ctxWith(t, "sess-outage", "call-1"), raw)
			if err != nil {
				t.Fatalf("an infra failure is a deny RESULT, not a Go error: %v", err)
			}
			if !strings.Contains(res.Preview, "sandbox_unavailable") {
				t.Fatalf("want the fail-CLOSED deny, got: %q", res.Preview)
			}
		})
	}
}

// A tracked cwd is the ONLY cwd source once there is one execution path, so a directory the session
// cd'd into and later deleted would resolve on every subsequent call and wedge the session for good.
// It is dropped instead, and the error says so.
func TestShellExec_StaleTrackedCwdIsResetNotWedged(t *testing.T) {
	be := &cwdProbeBackend{probeExit: 1}
	tool := &ShellExec{Router: newStrictBoxRouter(be)}
	ctx := ctxWith(t, "sess-stale", "call-1")
	tool.storeCwd(ctx, "/workspace/gone")

	_, err := tool.Execute(ctx, json.RawMessage(`{"command":"ls"}`))
	if err == nil {
		t.Fatal("a vanished tracked cwd must surface as an error")
	}
	if !strings.Contains(err.Error(), "/workspace/gone") {
		t.Fatalf("error must name the vanished directory: %v", err)
	}
	if got := tool.boxWorkdir(ctx, ""); got != "" {
		t.Fatalf("tracked cwd = %q after the reset, want it dropped so the next call starts at the box default", got)
	}
}

// The background arm resolves the same directory and had the same misclassification.
func TestShellExec_BackgroundBadCwdIsAnArgumentError(t *testing.T) {
	be := &cwdProbeBackend{probeExit: 1}
	tool := &ShellExec{
		Router:     newStrictBoxRouter(be),
		Background: NewBackgroundShells(newStrictBoxRouter(be)),
	}
	raw, _ := json.Marshal(shellExecArgs{Command: "sleep 60", Cwd: "/workspace/nope", Background: true})
	res, err := tool.Execute(ctxWith(t, "sess-bgcwd", "call-1"), raw)
	if err == nil {
		t.Fatalf("a background call with a bad cwd must error, got: %q", res.Preview)
	}
	if !strings.Contains(err.Error(), "/workspace/nope") {
		t.Fatalf("error must name the offending directory: %v", err)
	}
}

// TestSnippetUse_RendersSandboxPath: skill action=use renders the IN-BOX SandboxPath — the only
// path shell_exec can reach — and calls NO backend.Exec (action=use only names the path for the
// subsequent shell_exec).
func TestSnippetUse_RendersSandboxPath(t *testing.T) {
	loader := newFakeLoader()
	loader.snippets = map[string]fakeSnippet{
		"calc": {
			instructions: "Adds two numbers.",
			sandboxPath:  "/skills/calc/calc.py",
			interpreter:  "python3",
		},
	}
	be := &fakeBoxBackend{}
	ctx := ctxWith(t, "sess-snip", "call-snip")

	tool := &SkillTool{Loader: loader}
	res, err := tool.Execute(ctx, json.RawMessage(`{"action":"use","name":"calc"}`))
	if err != nil {
		t.Fatalf("use: %v", err)
	}
	if !strings.Contains(res.Preview, "python3 '/skills/calc/calc.py'") {
		t.Fatalf("snippet use must render the quoted in-box path: %q", res.Preview)
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

// Routed read: a strict read_file reads via the box; a Resolve failure denies without ever
// falling back to a host os.ReadFile.
func TestReadFile_RoutedReadsInBoxAndFailsClosed(t *testing.T) {
	be := &fakeBoxBackend{execResult: usersandbox.ExecResult{Stdout: []byte("box-file-body\n"), ExitCode: 0}}
	tool := &ReadFile{Router: newStrictBoxRouter(be)}
	ctx := ctxWith(t, "sess-fr", "call-fr")
	res, err := tool.Execute(ctx, json.RawMessage(`{"path":"/workspace/data.txt"}`))
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if !strings.Contains(res.Preview, "box-file-body") {
		t.Fatalf("routed read_file missing box content: %q", res.Preview)
	}
	if len(be.execCalls) != 1 || !strings.Contains(be.execCalls[0].Command, "head -c") {
		t.Fatalf("routed read_file must read via a bounded head -c exec: %#v", be.execCalls)
	}

	beFail := &fakeBoxBackend{resolveErr: errors.New("no box")}
	toolFail := &ReadFile{Router: newStrictBoxRouter(beFail)}
	resFail, err := toolFail.Execute(ctx, json.RawMessage(`{"path":"/etc/hostname"}`))
	if err != nil {
		t.Fatalf("Execute (fail): %v", err)
	}
	if !strings.Contains(resFail.Preview, "sandbox_unavailable") {
		t.Fatalf("want deny on Resolve failure: %q", resFail.Preview)
	}
}

// A strict write_file copies content into the box (never the host), reports whether the on-disk
// hash was confirmed, and refuses a write into the /skills mount.
func TestWriteFile_RoutedWritesInBoxAndSkillsFence(t *testing.T) {
	be := &fakeBoxBackend{}
	tool := &WriteFile{Router: newStrictBoxRouter(be)}
	ctx := ctxWith(t, "sess-fw", "call-fw")

	res, err := tool.Execute(ctx, json.RawMessage(`{"path":"/workspace/out.txt","content":"hello"}`))
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if !strings.Contains(res.Preview, "wrote 5 bytes to /workspace/out.txt") {
		t.Fatalf("routed write_file preview wrong: %q", res.Preview)
	}
	// The verified flag is what lets the model skip a confirming read; it must always be stated.
	if !strings.Contains(res.Preview, "verified:") {
		t.Errorf("write_file must report the verified flag: %q", res.Preview)
	}
	if len(be.writes) != 1 || be.writes[0].path != "/workspace/out.txt" || string(be.writes[0].content) != "hello" {
		t.Fatalf("routed write_file did not copy into the box: %#v", be.writes)
	}

	skills, err := tool.Execute(ctx, json.RawMessage(`{"path":"/skills/evil/evil.py","content":"x"}`))
	if err == nil {
		t.Fatalf("write into /skills must error (gated authoring), got result: %q", skills.Preview)
	}
	if !strings.Contains(err.Error(), "skills") {
		t.Fatalf("want a skills-fence error, got: %v", err)
	}
}
