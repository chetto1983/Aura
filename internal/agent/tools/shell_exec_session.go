package tools

import (
	"context"
	"os"
	"strings"
)

// Per-session working-directory tracking for ShellExec. The cwd map (keyed by the
// WithToolCallContext session id) gives Claude-Code Bash-tool parity — a `cd` in
// one call carries into the next — and Evict reclaims it when a conversation ends
// so a long-running `serve` daemon does not leak it (audit R-41 / AP-16).

// workdir resolves the call's starting directory: explicit cwd arg > the session's
// tracked cwd (Bash-tool parity) > WorkspaceRoot. A tracked dir that no longer
// exists (or a POSIX-only form a degraded shell stored) is skipped, never fatal.
func (s *ShellExec) workdir(ctx context.Context, cwd string) string {
	if strings.TrimSpace(cwd) != "" {
		return cwd
	}
	s.mu.Lock()
	tracked := s.cwd[shellSessionKey(ctx)]
	s.mu.Unlock()
	if tracked != "" {
		if _, err := os.Stat(tracked); err == nil {
			return tracked
		}
	}
	// Empty is valid — exec falls back to the Aura process's current directory.
	return s.WorkspaceRoot
}

// storeCwd records the shell's final $PWD as the session's tracked cwd. Empty
// (marker missing — e.g. the command exited the group early, or a timeout kill)
// keeps the previous tracking.
func (s *ShellExec) storeCwd(ctx context.Context, dir string) {
	if dir == "" {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.cwd == nil {
		s.cwd = map[string]string{}
	}
	s.cwd[shellSessionKey(ctx)] = dir
}

// Evict reclaims a finished session's tracked working directory (SessionEvictor,
// R-41). An unknown session id is a no-op; concurrency-safe under s.mu.
func (s *ShellExec) Evict(sessionID string) {
	s.mu.Lock()
	delete(s.cwd, sessionID)
	s.mu.Unlock()
}
