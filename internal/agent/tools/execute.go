package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/chetto1983/aura/internal/sandbox"
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
// shell snippets to the isolated sidecar, then formats the D-17 lean preview and
// routes it through NewResult (D-25 cap + spillover) — zero new spillover code.
type Execute struct {
	Runner sandbox.Runner
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
    "session_id": {"type": "string", "description": "RESERVED for Phase 8 / Slice 2b (session-bound sandbox); leave unset in 2a."}
  },
  "required": ["lang", "code"]
}`)
	return Spec{
		Name:    "execute",
		Summary: "Run a Python or shell snippet in an isolated network-less sandbox.",
		Description: "Run untrusted Python 3.12 or shell (bash) code in a hardened, network-less, read-only, stateless sandbox and get back its stdout/stderr/exit code. " +
			"The sandbox has no network access and no persistent filesystem — every call is a fresh process. A curated batteries-included set is preinstalled (numpy, pandas, scipy, sympy, matplotlib, pillow, beautifulsoup4, lxml, pyyaml, python-dateutil, openpyxl); runtime pip is NOT available. " +
			"A non-zero exit, stderr, a denied syscall (EPERM), a timeout, or an out-of-memory kill are NORMAL results you should read and adapt to — they are not failures of the tool. " +
			"Example (python): {\"lang\":\"python\",\"code\":\"print(sum(range(10)))\"}. " +
			"Example (shell): {\"lang\":\"shell\",\"code\":\"echo hello && uname -a\"}. " +
			"Do NOT set session_id — it is reserved for a future session-bound sandbox.",
		Parameters: params,
		Deferred:   true,
	}
}

// Execute validates lang/session_id, clamps the optional timeout, delegates to the
// Runner's new 3-arg call, and routes the D-17 lean preview through NewResult. A
// typed sandbox error (ErrSandboxUnreachable/Protocol) propagates to the loop so
// the model sees "sandbox unavailable"; a non-zero exit is a normal ToolResult
// (D-18).
func (e *Execute) Execute(ctx context.Context, raw json.RawMessage) (ToolResult, error) {
	var a executeArgs
	if err := json.Unmarshal(raw, &a); err != nil {
		return ToolResult{}, fmt.Errorf("execute args: %w", err)
	}
	if a.SessionID != "" {
		// Reserved-but-inert in 2a: an error ToolResult so the model self-corrects.
		return NewResult(ctx, "error: session_id is reserved for Phase 8 / Slice 2b (session-bound sandbox); omit it in 2a")
	}
	timeoutSec := a.TimeoutSec
	if timeoutSec > maxToolTimeoutSec {
		timeoutSec = maxToolTimeoutSec
	}

	var (
		res  sandbox.Result
		err  error
		code = a.Code
	)
	switch a.Lang {
	case "python":
		res, err = e.Runner.RunPython(ctx, code, timeoutSec)
	case "shell":
		res, err = e.Runner.RunShell(ctx, code, timeoutSec)
	default:
		// Enum validation as an error ToolResult so the model retries (ask_user.go
		// switch-default pattern). Not a Go error: a bad lang is the model's fault.
		return NewResult(ctx, fmt.Sprintf("error: lang %q must be one of python|shell", a.Lang))
	}
	if err != nil {
		// Typed sandbox error → propagate to the loop (D-18 environment fault).
		return ToolResult{}, err
	}
	return NewResult(ctx, FormatLean(res))
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
