package sandbox

import "errors"

// Typed sentinels for the D-18 error taxonomy: environment faults are Go errors,
// code faults (non-zero exit, EPERM, timeout, OOM, pids-cap) are normal Results.
// Both are errors.Is-friendly — DockerRunner wraps them with fmt.Errorf("…: %w",…)
// so callers (the agent loop, `aura exec`) classify without string matching.
//
// ErrSandboxUnreachable surfaces after a connect failure AND a failed one-shot
// docker-CLI-gated auto-start (D-09); ErrSandboxProtocol surfaces on a non-2xx or
// undecodable sidecar response. The CLI maps ErrSandboxUnreachable to exit 70.
//
// ErrSessionCapReached surfaces when AURA_SANDBOX_MAX_CONCURRENT_SESSIONS live
// sessions already exist and a new conversation asks for one — the SessionManager
// returns this sentinel rather than silently LRU-evicting an in-use session (D-12).
// ErrWorkspaceQuotaExceeded surfaces when a workspace write would push the
// per-conversation tree past AURA_SANDBOX_WORKSPACE_MAX_BYTES. Both are
// errors.Is-friendly so callers (sessions.go, workspace.go, the CLI) classify
// without string matching; the enforcement logic lands in 08-05/08-06.
var (
	ErrSandboxUnreachable     = errors.New("sandbox sidecar unreachable (auto-start failed)")
	ErrSandboxProtocol        = errors.New("sandbox sidecar returned a malformed response")
	ErrSessionCapReached      = errors.New("sandbox session cap reached")
	ErrWorkspaceQuotaExceeded = errors.New("sandbox workspace quota exceeded")
)
