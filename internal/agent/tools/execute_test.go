package tools

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/chetto1983/aura/internal/sandbox"
)

// fakeRunner is a sandbox.Runner double: it records the last call's code+timeout
// and replays a canned Result or error.
type fakeRunner struct {
	res        sandbox.Result
	err        error
	gotLang    string
	gotCode    string
	gotTimeout int
}

func (f *fakeRunner) RunPython(_ context.Context, code string, timeoutSec int) (sandbox.Result, error) {
	f.gotLang, f.gotCode, f.gotTimeout = "python", code, timeoutSec
	return f.res, f.err
}

func (f *fakeRunner) RunShell(_ context.Context, cmd string, timeoutSec int) (sandbox.Result, error) {
	f.gotLang, f.gotCode, f.gotTimeout = "shell", cmd, timeoutSec
	return f.res, f.err
}

func runExecute(t *testing.T, e *Execute, raw string) (ToolResult, error) {
	t.Helper()
	ctx := ctxWith(t, "sess-x", "call-x")
	return e.Execute(ctx, json.RawMessage(raw))
}

func TestExecute_DeferredSpec(t *testing.T) {
	e := &Execute{Runner: &fakeRunner{}}
	spec := e.Spec()
	if !spec.Deferred {
		t.Fatal("execute MUST be Deferred:true (the repo's first deferred tool)")
	}
	if spec.Name != "execute" {
		t.Fatalf("name: want execute, got %q", spec.Name)
	}
	if !strings.Contains(string(spec.Parameters), `"python"`) || !strings.Contains(string(spec.Parameters), `"shell"`) {
		t.Fatalf("params must carry the lang enum python|shell, got %s", spec.Parameters)
	}
}

func TestExecute_LeanPreview(t *testing.T) {
	cases := []struct {
		name string
		res  sandbox.Result
		want string
	}{
		{"silent success", sandbox.Result{Stdout: "4\n"}, "4\n"},
		{"stderr appended", sandbox.Result{Stdout: "out\n", Stderr: "warn"}, "out\nstderr:warn"},
		{"non-zero exit line", sandbox.Result{Stdout: "x\n", ExitCode: 2}, "x\nexit_code: 2"},
		{"limit hit", sandbox.Result{Stdout: "loop\n", LimitHit: "timeout", ElapsedMs: 2000}, "loop\n[limit: timeout] (2000 ms)"},
		{"empty exit 0", sandbox.Result{}, "(no output, exit 0)"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			e := &Execute{Runner: &fakeRunner{res: tc.res}}
			out, err := runExecute(t, e, `{"lang":"python","code":"x"}`)
			if err != nil {
				t.Fatalf("Execute: %v", err)
			}
			if out.Preview != tc.want {
				t.Fatalf("preview mismatch:\n got %q\nwant %q", out.Preview, tc.want)
			}
		})
	}
}

func TestExecute_LangEnumRejected(t *testing.T) {
	e := &Execute{Runner: &fakeRunner{}}
	out, err := runExecute(t, e, `{"lang":"ruby","code":"puts 1"}`)
	if err != nil {
		t.Fatalf("a bad lang is the model's fault → error ToolResult, not a Go error: %v", err)
	}
	if !strings.Contains(out.Preview, "must be one of python|shell") {
		t.Fatalf("want an enum-rejection preview, got %q", out.Preview)
	}
}

func TestExecute_SessionIdReserved(t *testing.T) {
	e := &Execute{Runner: &fakeRunner{}}
	out, err := runExecute(t, e, `{"lang":"python","code":"x","session_id":"abc"}`)
	if err != nil {
		t.Fatalf("reserved session_id → error ToolResult, not a Go error: %v", err)
	}
	if !strings.Contains(out.Preview, "reserved for Phase 8") {
		t.Fatalf("want a reserved-session preview, got %q", out.Preview)
	}
}

func TestExecute_TimeoutPassThrough(t *testing.T) {
	// An explicit in-range timeout reaches the Runner verbatim.
	fr := &fakeRunner{res: sandbox.Result{Stdout: "ok"}}
	e := &Execute{Runner: fr}
	if _, err := runExecute(t, e, `{"lang":"python","code":"x","timeout_sec":12}`); err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if fr.gotTimeout != 12 || fr.gotLang != "python" || fr.gotCode != "x" {
		t.Fatalf("timeout/lang/code must reach the Runner: %+v", fr)
	}

	// >600 is clamped to 600 tool-side (defense-in-depth).
	if _, err := runExecute(t, e, `{"lang":"shell","code":"y","timeout_sec":9000}`); err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if fr.gotTimeout != 600 {
		t.Fatalf("timeout_sec>600 must be clamped to 600 at the tool, got %d", fr.gotTimeout)
	}

	// Omitted → 0 passed through (the Runner substitutes the config default).
	if _, err := runExecute(t, e, `{"lang":"python","code":"z"}`); err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if fr.gotTimeout != 0 {
		t.Fatalf("an omitted timeout must pass 0 through (Runner defaults it), got %d", fr.gotTimeout)
	}
}

func TestExecute_TypedErrorPropagates(t *testing.T) {
	e := &Execute{Runner: &fakeRunner{err: sandbox.ErrSandboxUnreachable}}
	_, err := runExecute(t, e, `{"lang":"python","code":"x"}`)
	if !errors.Is(err, sandbox.ErrSandboxUnreachable) {
		t.Fatalf("a typed sandbox error must propagate as the Execute error, got %v", err)
	}
}

func TestExecute_NonZeroExitIsResult(t *testing.T) {
	e := &Execute{Runner: &fakeRunner{res: sandbox.Result{Stderr: "boom", ExitCode: 1}}}
	out, err := runExecute(t, e, `{"lang":"shell","code":"false"}`)
	if err != nil {
		t.Fatalf("a non-zero exit is a ToolResult, never a Go error (D-18): %v", err)
	}
	if !strings.Contains(out.Preview, "exit_code: 1") || !strings.Contains(out.Preview, "stderr:boom") {
		t.Fatalf("want exit_code + stderr in the preview, got %q", out.Preview)
	}
}
