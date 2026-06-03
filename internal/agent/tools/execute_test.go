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
	gotSession string
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

// RunPythonSession/RunShellSession satisfy the 2b session-bound Runner surface.
// The 2a execute tool does not call them yet (session_id is still inert until
// 08-08), so they record the session for any future test and replay the canned
// Result — keeping this double a complete sandbox.Runner.
func (f *fakeRunner) RunPythonSession(_ context.Context, sessionID, code string, timeoutSec int) (sandbox.Result, error) {
	f.gotLang, f.gotSession, f.gotCode, f.gotTimeout = "python", sessionID, code, timeoutSec
	return f.res, f.err
}

func (f *fakeRunner) RunShellSession(_ context.Context, sessionID, cmd string, timeoutSec int) (sandbox.Result, error) {
	f.gotLang, f.gotSession, f.gotCode, f.gotTimeout = "shell", sessionID, cmd, timeoutSec
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

// TestExecute_SessionIdDefaultsToConversation supersedes the 2a "reserved session"
// test: the inert-reject branch was REMOVED in 08-08 (D-02). An OMITTED session_id
// now defaults to the conversation id the agent stamps on the ctx (ctxWith uses
// "sess-x"), and the call routes through the session-bound Runner method.
func TestExecute_SessionIdDefaultsToConversation(t *testing.T) {
	fr := &fakeRunner{res: sandbox.Result{Stdout: "ok"}}
	e := &Execute{Runner: fr}
	if _, err := runExecute(t, e, `{"lang":"python","code":"x"}`); err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if fr.gotSession != "sess-x" {
		t.Fatalf("an omitted session_id must default to the conversation id (sess-x), got %q", fr.gotSession)
	}
}

// TestExecute_ExplicitSessionId honours a model-supplied session_id over the ctx
// default and routes it through the session-bound Runner method.
func TestExecute_ExplicitSessionId(t *testing.T) {
	fr := &fakeRunner{res: sandbox.Result{Stdout: "ok"}}
	e := &Execute{Runner: fr}
	if _, err := runExecute(t, e, `{"lang":"shell","code":"y","session_id":"conv-42"}`); err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if fr.gotSession != "conv-42" || fr.gotLang != "shell" {
		t.Fatalf("explicit session_id must reach the session Runner: session=%q lang=%q", fr.gotSession, fr.gotLang)
	}
}

// TestExecute_SessionIdTraversalRejected rejects a traversal-shaped session_id as an
// error ToolResult (validateID guard) before any Runner call.
func TestExecute_SessionIdTraversalRejected(t *testing.T) {
	fr := &fakeRunner{}
	e := &Execute{Runner: fr}
	out, err := runExecute(t, e, `{"lang":"python","code":"x","session_id":"../etc"}`)
	if err != nil {
		t.Fatalf("a malformed session_id → error ToolResult, not a Go error: %v", err)
	}
	if !strings.Contains(out.Preview, "session_id") || fr.gotSession != "" {
		t.Fatalf("traversal session_id must be rejected before the Runner, got preview=%q session=%q", out.Preview, fr.gotSession)
	}
}

// TestExecute_NetworkAdvisory appends the {risk_tier, gate_recommended} advisory
// line ONLY when a non-empty egress allowlist is configured (D-12). An arbitrary
// host is Risky → gate recommended; PyPI-only stays Safe → no gate.
func TestExecute_NetworkAdvisory(t *testing.T) {
	cases := []struct {
		name      string
		allow     []string
		wantTier  string
		wantGate  string
		wantPanel bool
	}{
		{"no allowlist → no advisory", nil, "", "", false},
		{"arbitrary host → risky+gate", []string{"example.com"}, "risky", "gate_recommended: true", true},
		{"pypi-only → safe+no gate", []string{"pypi.org", "files.pythonhosted.org"}, "safe", "gate_recommended: false", true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			e := &Execute{Runner: &fakeRunner{res: sandbox.Result{Stdout: "out\n"}}, NetworkAllow: tc.allow}
			out, err := runExecute(t, e, `{"lang":"python","code":"x"}`)
			if err != nil {
				t.Fatalf("Execute: %v", err)
			}
			hasAdvisory := strings.Contains(out.Preview, "[advisory]")
			if hasAdvisory != tc.wantPanel {
				t.Fatalf("advisory presence: want %v, got preview %q", tc.wantPanel, out.Preview)
			}
			if tc.wantPanel {
				if !strings.Contains(out.Preview, "risk_tier: "+tc.wantTier) || !strings.Contains(out.Preview, tc.wantGate) {
					t.Fatalf("advisory mismatch: want tier=%s %s, got %q", tc.wantTier, tc.wantGate, out.Preview)
				}
			}
		})
	}
}

// TestExecute_SessionAcquireReleased verifies that when a SessionAcquirer is wired,
// Execute Acquires before and Releases after the call (D-07 serialization seam).
func TestExecute_SessionAcquireReleased(t *testing.T) {
	fr := &fakeRunner{res: sandbox.Result{Stdout: "ok"}}
	fs := &fakeSessions{}
	e := &Execute{Runner: fr, Sessions: fs}
	if _, err := runExecute(t, e, `{"lang":"python","code":"x"}`); err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if fs.acquired != "sess-x" || fs.releases != 1 {
		t.Fatalf("Acquire/Release must bracket the exec: acquired=%q releases=%d", fs.acquired, fs.releases)
	}
}

// fakeSessions is a SessionAcquirer double recording the acquired session id +
// release count. It returns a zero *sandbox.Session so Release is a benign no-op.
type fakeSessions struct {
	acquired string
	releases int
	err      error
}

func (f *fakeSessions) Acquire(_ context.Context, sessionID string) (*sandbox.Session, error) {
	f.acquired = sessionID
	if f.err != nil {
		return nil, f.err
	}
	return &sandbox.Session{}, nil
}

func (f *fakeSessions) Release(*sandbox.Session) { f.releases++ }

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
