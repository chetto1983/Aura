// backend.go declares the per-identity sandbox provider SEAM: the Backend interface
// speaking the E2B verbs (corrected by spike 082, D-02) plus the BoxHandle / ExecRequest
// / ExecResult data types the verbs exchange. It is a contract only — Aura's Docker-backed
// implementation lands in a later plan, and the DGX agent-sandbox E2B gateway drops in
// behind this same interface unmodified. No moby client is imported here.

package usersandbox

import "context"

// Backend is the per-identity sandbox provider seam, speaking the E2B verbs. Consumers
// depend on this interface, never a concrete backend ("accept interfaces"), so the Docker
// backend (now) and the DGX agent-sandbox E2B gateway (later) are interchangeable.
type Backend interface {
	// Resolve is idempotent get-or-create for the identity's box — a DIRECT create
	// (never an agent-sandbox SandboxClaim, which is a warm-pool checkout, D-02). It also
	// transparently Resumes a Suspended box, so the caller always receives a live handle
	// (D-08). It keys on spec.IdentityID; the same identity always resolves to its one box.
	Resolve(ctx context.Context, spec SandboxSpec) (BoxHandle, error)
	// Exec runs a single command inside the box and returns its captured result.
	Exec(ctx context.Context, h BoxHandle, req ExecRequest) (ExecResult, error)
	// Suspend stops the box but RETAINS the container and its volume
	// (OperatingMode:Suspended, D-08), so a later Resolve/Resume is fast and lossless.
	Suspend(ctx context.Context, h BoxHandle) error
	// Resume restarts a Suspended box against its retained volume.
	Resume(ctx context.Context, h BoxHandle) error
	// Stop destroys the box AND its volume (ShutdownPolicy:Delete, D-08) — only on
	// explicit deprovision, never on idle.
	Stop(ctx context.Context, h BoxHandle) error
}

// BoxHandle identifies a resolved per-identity box. It carries the Docker ContainerID and
// the owning IdentityID — the key the idle reaper's lastUsedAt tracking and every
// per-identity scope use. It is backend-agnostic: an E2B backend stores its sandbox id in
// ContainerID.
type BoxHandle struct {
	ContainerID string
	IdentityID  string
}

// ExecRequest is a single command execution inside a box. Command is run through the box's
// POSIX /bin/sh (never the host's Windows shell — 37-RESEARCH Pitfall 6); Dir is the
// working directory; Env is the environment (the backend scrubs host secrets from it).
type ExecRequest struct {
	Command string
	Dir     string
	Env     []string
}

// ExecResult is the captured outcome of an ExecRequest: the demuxed stdout/stderr streams
// and the process exit code.
type ExecResult struct {
	Stdout   []byte
	Stderr   []byte
	ExitCode int
}
