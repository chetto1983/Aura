// Package sandbox owns isolated execution of Python and shell snippets. The real
// runner (DockerRunner) is a thin HTTP client against a container-isolated sidecar
// (seccomp + ulimit + net-deny, gVisor-primary on x86) reached over AURA_SANDBOX_URL.
package sandbox

import (
	"context"
)

// Runner is what the `execute` tool delegates against. The per-call timeoutSec
// rides every call (D-16/D-19): the wire body carries {"code","timeout_sec"} and
// the CLI/tool expose an optional timeout — so the value must reach the runner.
// A timeoutSec <= 0 means "use the config default"; the runner then clamps to
// <=600 (the cap is runner-side, not in config).
//
// The Run{Python,Shell}Session methods are the 2b session-bound additions: they
// carry a sessionID so the sidecar routes to its long-lived per-session namespace
// (python) / per-session cwd (shell) — see sandbox/sidecar.py /session/{id}/exec.
// They are ADDITIVE: the stateless 2a methods + callers (execute.go, exec.go) are
// unchanged. The session lifecycle (container create/reap) is the SessionManager's
// (08-05); these methods only drive HTTP exec against an already-live session.
type Runner interface {
	RunPython(ctx context.Context, code string, timeoutSec int) (Result, error)
	RunShell(ctx context.Context, cmd string, timeoutSec int) (Result, error)
	RunPythonSession(ctx context.Context, sessionID, code string, timeoutSec int) (Result, error)
	RunShellSession(ctx context.Context, sessionID, cmd string, timeoutSec int) (Result, error)
}

// Result is the D-16 wire response decoded 1:1. The four original fields
// (Stdout/Stderr/ExitCode/ElapsedMs) keep the agent-loop binding intact; Truncated
// + LimitHit are the sidecar's server-side signals the lean preview (D-17) reads.
type Result struct {
	Stdout    string
	Stderr    string
	ExitCode  int
	ElapsedMs int64
	Truncated bool   // a stream was truncated to 1 MiB server-side
	LimitHit  string // "timeout" | "oom" | "pids" | "" when no limit fired
}
