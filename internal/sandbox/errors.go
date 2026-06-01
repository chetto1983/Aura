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
var (
	ErrSandboxUnreachable = errors.New("sandbox sidecar unreachable (auto-start failed)")
	ErrSandboxProtocol    = errors.New("sandbox sidecar returned a malformed response")
)
