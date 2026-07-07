package tools

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/chetto1983/aura/internal/sandbox/usersandbox"
)

// shell_exec's ROUTED branch (SBX-01, plan 37-07): under a strict profile Route returns a live
// per-identity box handle and the command runs INSIDE the box via Router.Exec (POSIX /bin/sh -c,
// the same cwd-marker wrap the host path uses but with PLAIN pwd — Pitfall 6), NEVER host os/exec.
// A box exec infra failure returns the fail-CLOSED deny result (D-09/GATE-01) rather than falling
// back to the host; a timeout/cancel renders the normal shell footer.

// executeInBox runs command inside the box handle and renders the same [aura_shell {...}] footer
// shape the host path produces (exit_code / cwd / duration_ms / timed_out), tracking the box $PWD
// across calls via the cwd marker.
func (s *ShellExec) executeInBox(ctx context.Context, h usersandbox.BoxHandle, command, cwdArg string, extraEnv map[string]string, timeoutMs int64) (ToolResult, error) {
	timeout := effectiveShellTimeout(s.DefaultTimeout, timeoutMs)
	runCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	started := time.Now()

	res, execErr := s.Router.Exec(runCtx, h, usersandbox.ExecRequest{
		Command: wrapForCwdTrackingBox(command),
		Dir:     s.boxWorkdir(ctx, cwdArg),
		Env:     boxEnv(extraEnv),
	})

	timedOut := errors.Is(runCtx.Err(), context.DeadlineExceeded)
	cancelled := errors.Is(runCtx.Err(), context.Canceled)
	// A non-timeout/non-cancel exec error is a box INFRA failure — deny (fail-CLOSED), never host.
	if execErr != nil && !timedOut && !cancelled {
		return sandboxUnavailableResult("shell_exec", execErr), nil
	}

	stdout := string(res.Stdout)
	clean, capturedCwd := extractCwdMarker(stdout)
	finalCwd := s.boxWorkdir(ctx, cwdArg)
	if capturedCwd != "" {
		finalCwd = capturedCwd
		s.storeCwd(ctx, capturedCwd)
	}

	stderr := redactModelPreview(string(res.Stderr))
	body := redactModelPreview(clean)
	if strings.TrimSpace(stderr) != "" {
		if strings.TrimSpace(body) != "" {
			body += "\n"
		}
		body += stderr
	}

	var ecPtr *int
	status := ""
	switch {
	case timedOut:
		status = "[command timed out]"
	case cancelled:
		status = "[command cancelled]"
	default:
		ec := res.ExitCode
		ecPtr = &ec
		if ec != 0 {
			status = fmt.Sprintf("[exit code %d]", ec)
		}
	}
	if strings.TrimSpace(body) == "" && status == "" {
		body = "[no output]"
	}

	var b strings.Builder
	b.WriteString(body)
	if status != "" {
		ensureTrailingNewline(&b)
		b.WriteString(status)
	}
	rendered := appendShellFooter(b.String(), shellExecFooter{
		ExitCode:   ecPtr,
		Cwd:        finalCwd,
		DurationMS: time.Since(started).Milliseconds(),
		TimedOut:   timedOut,
	})

	out, err := NewResult(ctx, rendered)
	if err != nil {
		return ToolResult{}, err
	}
	meta := ToolResultMeta{"cwd": finalCwd, "timed_out": timedOut}
	if ecPtr != nil {
		meta["exit_code"] = *ecPtr
	}
	out.Meta = &meta
	return out, nil
}

// boxWorkdir resolves the box working directory: an explicit box cwd arg, else the session's
// tracked in-box $PWD (an absolute POSIX path captured from a prior box pwd), else "" so the box
// uses its image WORKDIR (e.g. /workspace). It deliberately never falls back to the host
// WorkspaceRoot (a host path is meaningless inside the box).
func (s *ShellExec) boxWorkdir(ctx context.Context, cwd string) string {
	if strings.TrimSpace(cwd) != "" {
		return cwd
	}
	s.mu.Lock()
	tracked := s.cwd[shellSessionKey(ctx)]
	s.mu.Unlock()
	if strings.HasPrefix(tracked, "/") {
		return tracked
	}
	return ""
}
