package tools

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/chetto1983/aura/internal/sandboxagent"
)

type fakeSandboxRunner struct {
	got sandboxagent.RunRequest
	res sandboxagent.RunResponse
	err error
}

func (f *fakeSandboxRunner) Run(_ context.Context, req sandboxagent.RunRequest) (sandboxagent.RunResponse, error) {
	f.got = req
	return f.res, f.err
}

func TestSandboxExecSpecIsNotDeferredAndLocal(t *testing.T) {
	s := (&SandboxExec{}).Spec()
	if s.Name != "sandbox_exec" {
		t.Fatalf("name = %q, want sandbox_exec", s.Name)
	}
	// Must NOT be deferred: the model needs the command/args schema in the manifest
	// to call sandbox_exec correctly. A live E2E with it deferred had the agent cram
	// the whole command line into `command` ("python3 -c ...") → sandbox-agent 502
	// "failed to spawn"; non-deferred, one clean call returned 42.
	if s.Deferred {
		t.Fatal("sandbox_exec must NOT be deferred — the model needs the arg schema")
	}
	if !strings.Contains(s.Description, "local Aura sandbox container") {
		t.Fatalf("description should make local-container scope explicit: %q", s.Description)
	}
}

func TestSandboxExecRunsThroughSandboxAgent(t *testing.T) {
	exit := 0
	r := &fakeSandboxRunner{res: sandboxagent.RunResponse{
		Stdout:     "42\n",
		Stderr:     "",
		ExitCode:   &exit,
		DurationMs: 9,
	}}
	tool := &SandboxExec{Runner: r}
	ctx := ctxWith(t, "sess-sbx", "call-sbx")

	res, err := tool.Execute(ctx, json.RawMessage(`{"command":"python","args":["-c","print(40+2)"],"cwd":"/workspace","timeout_ms":30000}`))
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if r.got.Command != "python" || len(r.got.Args) != 2 || r.got.Cwd != "/workspace" {
		t.Fatalf("request not passed to runner: %+v", r.got)
	}
	if r.got.TimeoutMs == nil || *r.got.TimeoutMs != 30000 {
		t.Fatalf("timeout not passed: %+v", r.got.TimeoutMs)
	}
	if !strings.Contains(res.Preview, `"stdout":"42\n"`) || !strings.Contains(res.Preview, `"exit_code":0`) {
		t.Fatalf("result preview missing command output: %q", res.Preview)
	}
}

func TestSandboxExecUnavailableIsInlineResult(t *testing.T) {
	tool := &SandboxExec{}
	ctx := ctxWith(t, "sess-sbx", "call-sbx")

	res, err := tool.Execute(ctx, json.RawMessage(`{"command":"python","args":["-c","print(40+2)"]}`))
	if err != nil {
		t.Fatalf("unavailable sandbox should be model-visible inline result, not Go error: %v", err)
	}
	if !strings.Contains(res.Preview, `"error":"sandbox_unavailable"`) {
		t.Fatalf("preview should carry sandbox_unavailable JSON, got %q", res.Preview)
	}
}
