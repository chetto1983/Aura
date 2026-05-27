// Package sandbox owns isolated execution of Python and shell snippets.
// First version is a stub: the real implementation lands in a follow-up
// slice and wraps a container-isolated sidecar (seccomp + ulimit + net-deny).
package sandbox

import (
	"context"
	"fmt"
)

// Runner is what the `execute` tool delegates against. Real implementation
// lives in the sandbox slice; this stub keeps the agent loop compilable.
type Runner interface {
	RunPython(ctx context.Context, code string) (Result, error)
	RunShell(ctx context.Context, cmd string) (Result, error)
}

type Result struct {
	Stdout    string
	Stderr    string
	ExitCode  int
	ElapsedMs int64
}

// Stub returns "not yet implemented" for any call. Wire it into the registry
// only for the agent-loop smoke test; replace with the real runner in the
// sandbox slice.
type Stub struct{}

func (Stub) RunPython(_ context.Context, _ string) (Result, error) {
	return Result{}, fmt.Errorf("sandbox.Runner not yet implemented — see sandbox slice")
}

func (Stub) RunShell(_ context.Context, _ string) (Result, error) {
	return Result{}, fmt.Errorf("sandbox.Runner not yet implemented — see sandbox slice")
}
