// router_tools.go adds the tool-facing passthroughs the routed host tools (37-07) call AFTER
// Route returns a live BoxHandle: Exec runs a command inside the box (shell_exec / fs_read),
// CopyArtifactOut streams a /workspace artifact out (send_file), and WriteFile copies a file in
// (fs_write). Copy-out and copy-in are resolved through a structural type-assertion on the
// backend (backendAs) so the core Backend seam (backend.go) stays minimal — an E2B gateway
// backend opts into artifact/file transfer by implementing the SAME method set, and a backend
// that does not support them yields a clean error the tool surfaces as a fail-CLOSED deny (never
// a host fallback). A nil router / nil backend is a safe error everywhere.

package usersandbox

import (
	"context"
	"fmt"
	"io"
)

// Exec runs req inside the identity's box via the backend. It is the thin passthrough the routed
// shell_exec / fs_read tools call once Route has returned a live handle; the backend scrubs host
// secrets from req.Env (docker_backend_exec.go).
func (r *SandboxRouter) Exec(ctx context.Context, h BoxHandle, req ExecRequest) (ExecResult, error) {
	if r == nil || r.backend == nil {
		return ExecResult{}, fmt.Errorf("sandbox router: no backend configured")
	}
	return r.backend.Exec(ctx, h, req)
}

// artifactCopier is the optional box→host copy-out capability send_file's routed branch needs
// (D-10). DockerBackend satisfies it (materialize.go); it is deliberately NOT on the core Backend
// interface so a backend without artifact transfer stays a valid Backend.
type artifactCopier interface {
	CopyArtifactsOut(ctx context.Context, h BoxHandle, boxPath string) (io.ReadCloser, error)
}

// CopyArtifactOut streams a box /workspace artifact out (send_file). It resolves the copy-out
// capability structurally; a backend that does not implement it returns an error the tool turns
// into a fail-CLOSED deny (never a host read).
func (r *SandboxRouter) CopyArtifactOut(ctx context.Context, h BoxHandle, boxPath string) (io.ReadCloser, error) {
	co, ok := backendAs[artifactCopier](r)
	if !ok {
		return nil, fmt.Errorf("sandbox backend does not support artifact copy-out")
	}
	return co.CopyArtifactsOut(ctx, h, boxPath)
}

// fileWriter is the optional host→box copy-in capability fs_write's routed branch needs. The tar
// copy-in (never an in-box heredoc) sidesteps ARG_MAX + quoting/binary limits an exec-based write
// would hit for large content.
type fileWriter interface {
	CopyFileIn(ctx context.Context, h BoxHandle, boxPath string, content []byte, mode int64) error
}

// WriteFile copies content to boxPath inside the box (fs_write). A backend without copy-in returns
// an error the tool surfaces as a fail-CLOSED deny (never a host write).
func (r *SandboxRouter) WriteFile(ctx context.Context, h BoxHandle, boxPath string, content []byte) error {
	w, ok := backendAs[fileWriter](r)
	if !ok {
		return fmt.Errorf("sandbox backend does not support file write")
	}
	return w.CopyFileIn(ctx, h, boxPath, content, 0o644)
}

// backgroundExecStreamer is the optional streamed/detached background-exec capability shell_bg's
// routed branch needs (37-09). DockerBackend satisfies it (docker_backend_exec.go); it is
// deliberately NOT on the core Backend interface — D-02 keeps that seam at exactly 5 verbs so the
// DGX agent-sandbox E2B impl stays a valid Backend, and the streaming verb is surfaced structurally
// here instead of widening the interface.
type backgroundExecStreamer interface {
	ExecStream(ctx context.Context, h BoxHandle, req ExecRequest, out io.Writer) (*ExecStreamHandle, error)
}

// ExecStream starts a streamed/detached background exec inside the box (shell_bg). It resolves the
// streaming capability structurally; a backend without it returns an error the tool turns into a
// fail-CLOSED deny (never a host background process).
func (r *SandboxRouter) ExecStream(ctx context.Context, h BoxHandle, req ExecRequest, out io.Writer) (*ExecStreamHandle, error) {
	s, ok := backendAs[backgroundExecStreamer](r)
	if !ok {
		return nil, fmt.Errorf("sandbox backend does not support background exec streaming")
	}
	return s.ExecStream(ctx, h, req, out)
}

// backendAs resolves an optional capability interface off the router's backend, nil-safe.
func backendAs[T any](r *SandboxRouter) (T, bool) {
	var zero T
	if r == nil || r.backend == nil {
		return zero, false
	}
	t, ok := r.backend.(T)
	return t, ok
}
