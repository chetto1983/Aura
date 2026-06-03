package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/chetto1983/aura/internal/sandbox"
	"github.com/chetto1983/aura/internal/scoring"
)

// maxToolTimeoutSec is the defense-in-depth tool-side timeout ceiling. The Runner
// re-clamps to the same value and substitutes the config default for an omitted
// (0) timeout, so the tool clamps only to keep the model from sending an absurd
// value on the wire (D-16/D-19).
const maxToolTimeoutSec = 600

// Execute is the repo's FIRST Deferred:true tool (CLAUDE.md deferred-tool
// partition): the long description + enum schema + safety examples stay out of the
// default LLM manifest until tool_search loads them. It holds an injected
// sandbox.Runner (mirrors ToolSearch{Registry}) and delegates untrusted python/
// shell snippets to the per-conversation session-bound sidecar (2b), then formats
// the D-17 lean preview and routes it through NewResult (D-25 cap + spillover) —
// zero new spillover code.
//
// NetworkAllow is the operator-configured egress allowlist
// (AURA_SANDBOX_NETWORK_ALLOW_HOSTS, parsed to hosts at boot). It feeds the
// advisory scoring.ComputeSandboxTier annotation appended to the lean preview when
// non-empty — advisory ONLY (no pending-state persistence, D-12); the proxy
// (08-06) is what actually enforces egress.
//
// Sessions is the optional per-conversation session control plane (08-05). When
// set, Execute Acquires the session (creating/locking the per-conversation
// container) for the call's duration and Releases it after — so concurrent execs
// on the same conversation serialize (D-07). When nil (the unit tier), the call
// routes straight through the session-bound Runner methods against the supplied
// session id.
type Execute struct {
	Runner       sandbox.Runner
	Sessions     SessionAcquirer
	NetworkAllow []string
}

// SessionAcquirer is the narrow session-lifecycle surface Execute needs: Acquire
// locks (and lazily creates) the per-conversation container, Release unlocks it.
// *sandbox.SessionManager satisfies it; the unit tier leaves it nil so the tool
// exercises the Runner path without a live control plane. Declared HERE (consumer-
// side) so tools never imports more of sandbox than it calls.
type SessionAcquirer interface {
	Acquire(ctx context.Context, sessionID string) (*sandbox.Session, error)
	Release(*sandbox.Session)
}

type executeArgs struct {
	Lang       string `json:"lang"`
	Code       string `json:"code"`
	TimeoutSec int    `json:"timeout_sec"`
	SessionID  string `json:"session_id"`
}

func (e *Execute) Spec() Spec {
	params := json.RawMessage(`{
  "type": "object",
  "properties": {
    "lang": {"type": "string", "enum": ["python", "shell"], "description": "python = run the code with python3; shell = run it with bash."},
    "code": {"type": "string", "description": "The snippet to run. For python it is fed to python3 -c; for shell it is fed to bash -c. Multi-line is fine."},
    "timeout_sec": {"type": "integer", "minimum": 1, "maximum": 600, "description": "Optional wall-clock timeout in seconds (capped at 600). Omit to use the server default."},
    "session_id": {"type": "string", "description": "Optional session id (the conversation id). Omit it to use the current conversation's session — that is the normal case. Two calls sharing a session_id run in the SAME long-lived sandbox: Python variables, imports, and /workspace files persist across calls; shell cd/export do NOT (each shell call is a fresh bash, so persist state by writing files under /workspace)."}
  },
  "required": ["lang", "code"]
}`)
	return Spec{
		Name:    "execute",
		Summary: "Run a Python or shell snippet in an isolated, session-bound sandbox.",
		Description: "Run untrusted Python 3.12 or shell (bash) code in a hardened, read-only sandbox and get back its stdout/stderr/exit code. " +
			"Calls in the same conversation share a long-lived session container with an ASYMMETRIC persistence contract (D-02): Python state — variables, imports, and files written under /workspace — PERSISTS across calls in the session; shell state does NOT — cd and export are scoped to a single bash invocation, so persist shell results by writing files under /workspace, never by relying on a previous call's cwd or env. " +
			"A curated batteries-included set is preinstalled (numpy, pandas, scipy, sympy, matplotlib, pillow, beautifulsoup4, lxml, pyyaml, python-dateutil, openpyxl); runtime pip is NOT available unless the operator allowlists PyPI egress. " +
			"A non-zero exit, stderr, a denied syscall (EPERM), a timeout, or an out-of-memory kill are NORMAL results you should read and adapt to — they are not failures of the tool. " +
			"Example (python): {\"lang\":\"python\",\"code\":\"print(sum(range(10)))\"}. " +
			"Example (shell): {\"lang\":\"shell\",\"code\":\"echo hello && uname -a\"}.",
		Parameters: params,
		Deferred:   true,
	}
}

// Execute resolves the session_id (empty defaults to the conversation id the agent
// injected via WithToolCallContext — D-25, no InvocationContext change), validates
// it with the traversal guard, clamps the optional timeout, routes the call through
// the session-bound Runner (Acquiring/Releasing the per-conversation container when
// a SessionManager is wired — D-07), and routes the D-17 lean preview through
// NewResult. When a non-empty network_allow is configured it appends an advisory
// {risk_tier, gate_recommended} line (ComputeSandboxTier, D-12) — advisory ONLY, no
// pending-state persistence; the proxy (08-06) is what enforces egress. A typed
// sandbox error (ErrSandboxUnreachable/Protocol/SessionCapReached) propagates to the
// loop; a non-zero exit is a normal ToolResult (D-18).
func (e *Execute) Execute(ctx context.Context, raw json.RawMessage) (ToolResult, error) {
	var a executeArgs
	if err := json.Unmarshal(raw, &a); err != nil {
		return ToolResult{}, fmt.Errorf("execute args: %w", err)
	}
	if a.Lang != "python" && a.Lang != "shell" {
		// Enum validation as an error ToolResult so the model retries (ask_user.go
		// switch-default pattern). Not a Go error: a bad lang is the model's fault.
		return NewResult(ctx, fmt.Sprintf("error: lang %q must be one of python|shell", a.Lang))
	}

	sessionID := a.SessionID
	if sessionID == "" {
		// Default to the conversation id (D-26: session_id == conversation_id) the
		// agent stamped on the ctx; the CLI/agent always sets it. A missing context
		// id is a wiring fault surfaced as an error ToolResult, not a panic.
		tc, ok := toolCallCtx(ctx)
		if !ok || tc.sessionID == "" {
			return NewResult(ctx, "error: no session_id and no conversation context — cannot route a session-bound exec")
		}
		sessionID = tc.sessionID
	}
	if err := validateID("session_id", sessionID); err != nil {
		return NewResult(ctx, fmt.Sprintf("error: %v", err))
	}

	timeoutSec := a.TimeoutSec
	if timeoutSec > maxToolTimeoutSec {
		timeoutSec = maxToolTimeoutSec
	}

	if e.Sessions != nil {
		sess, err := e.Sessions.Acquire(ctx, sessionID)
		if err != nil {
			// Session-create faults (cap reached, privacy-mode, docker absent) are
			// environment faults → propagate to the loop (D-18).
			return ToolResult{}, err
		}
		defer e.Sessions.Release(sess)
	}

	res, err := e.runSession(ctx, a.Lang, sessionID, a.Code, timeoutSec)
	if err != nil {
		// Typed sandbox error → propagate to the loop (D-18 environment fault).
		return ToolResult{}, err
	}
	return NewResult(ctx, e.formatWithAdvisory(res))
}

// runSession dispatches the lang to the session-bound Runner method. lang is
// pre-validated to python|shell by the caller.
func (e *Execute) runSession(ctx context.Context, lang, sessionID, code string, timeoutSec int) (sandbox.Result, error) {
	if lang == "python" {
		return e.Runner.RunPythonSession(ctx, sessionID, code, timeoutSec)
	}
	return e.Runner.RunShellSession(ctx, sessionID, code, timeoutSec)
}

// formatWithAdvisory builds the lean preview and, when a non-empty egress allowlist
// is configured, appends a single advisory line carrying the scoring tier + whether
// a confirmation gate is recommended (D-12). It is advisory ONLY — no pending-state
// is persisted; the proxy enforces egress regardless of what this annotation says.
func (e *Execute) formatWithAdvisory(res sandbox.Result) string {
	lean := FormatLean(res)
	if len(e.NetworkAllow) == 0 {
		return lean
	}
	tier := scoring.ComputeSandboxTier(scoring.SandboxArgs{NetworkAllow: e.NetworkAllow})
	advisory := fmt.Sprintf("[advisory] risk_tier: %s, gate_recommended: %t", tier, scoring.GateRecommended(tier))
	if lean == "" {
		return advisory
	}
	if strings.HasSuffix(lean, "\n") {
		return lean + advisory
	}
	return lean + "\n" + advisory
}

// FormatLean builds the D-17 lean preview a human/model reads: stdout verbatim as
// primary content (no fence/label); "stderr:"+stderr appended only when stderr is
// non-empty (the split-stream HTTP contract loses TTY interleave order, so the
// label is needed); an "exit_code: N" line ONLY when the exit is non-zero (success
// is silent); a "[limit: …] (elapsed_ms ms)" line ONLY when a limit fired;
// "(no output, exit 0)" when both streams are empty and the exit is 0. Exported so
// `aura exec` reuses the exact same formatter (no drift). The whole string is
// meant to flow through NewResult (D-25 cap + spillover).
func FormatLean(res sandbox.Result) string {
	var b strings.Builder
	b.WriteString(res.Stdout)
	if res.Stderr != "" {
		if b.Len() > 0 && !strings.HasSuffix(b.String(), "\n") {
			b.WriteByte('\n')
		}
		b.WriteString("stderr:")
		b.WriteString(res.Stderr)
	}
	if res.ExitCode != 0 {
		ensureNL(&b)
		fmt.Fprintf(&b, "exit_code: %d", res.ExitCode)
	}
	if res.LimitHit != "" {
		ensureNL(&b)
		fmt.Fprintf(&b, "[limit: %s] (%d ms)", res.LimitHit, res.ElapsedMs)
	}
	if b.Len() == 0 {
		return "(no output, exit 0)"
	}
	return b.String()
}

// ensureNL appends a newline separator before a trailing annotation line unless the
// buffer is empty or already ends in one.
func ensureNL(b *strings.Builder) {
	if b.Len() > 0 && !strings.HasSuffix(b.String(), "\n") {
		b.WriteByte('\n')
	}
}
