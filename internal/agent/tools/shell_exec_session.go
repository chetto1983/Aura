package tools

import (
	"context"
)

// Per-session working-directory tracking for ShellExec. The cwd map (keyed by the
// WithToolCallContext session id) gives Claude-Code Bash-tool parity — a `cd` in
// one call carries into the next — and Evict reclaims it when a conversation ends
// so a long-running `serve` daemon does not leak it (audit R-41 / AP-16).
//
// Resolution itself lives in boxWorkdir (shell_exec_sandbox.go): the tracked value is a BOX
// path, so there is exactly one resolver and it never host-stats. The deleted host resolver
// stat'ed these box paths, silently fell back to the host workspace when the stat failed, and
// bound the destructive-command approval digest to a directory the command never entered.

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
