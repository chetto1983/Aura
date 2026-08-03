package tools

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/chetto1983/aura/internal/sandbox/usersandbox"
)

// shell_exec's execution path (SBX-01, plan 37-07) — the ONLY one. Route returns a live
// per-identity box handle and the command runs INSIDE the box via Router.Exec (POSIX /bin/sh -c
// with a PLAIN pwd for the cwd marker — the Git-Bash `pwd -W` the deleted host arm needed does not
// exist there, Pitfall 6). A box exec infra failure returns the fail-CLOSED deny result
// (D-09/GATE-01); there is no host os/exec to fall back to. A timeout/cancel renders the normal
// shell footer, and a cwd the box cannot chdir into is reported as the argument error it is.

// executeInBox runs command inside the box handle and renders the same [aura_shell {...}] footer
// shape the host path produces (exit_code / cwd / duration_ms / timed_out), tracking the box $PWD
// across calls via the cwd marker.
func (s *ShellExec) executeInBox(ctx context.Context, h usersandbox.BoxHandle, command, cwdArg string, extraEnv map[string]string, timeoutMs int64) (ToolResult, error) {
	timeout := effectiveShellTimeout(s.DefaultTimeout, timeoutMs)
	runCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	started := time.Now()

	dir := s.boxWorkdir(ctx, cwdArg)
	res, execErr := s.Router.Exec(runCtx, h, usersandbox.ExecRequest{
		Command: wrapForCwdTracking(command),
		Dir:     dir,
		Env:     boxEnv(extraEnv),
	})

	timedOut := errors.Is(runCtx.Err(), context.DeadlineExceeded)
	cancelled := errors.Is(runCtx.Err(), context.Canceled)
	// A non-timeout/non-cancel exec error is a box INFRA failure — deny (fail-CLOSED), never host —
	// unless it is the caller's own cwd, which the daemon refuses to chdir into before the shell
	// ever starts (AG-018).
	if execErr != nil && !timedOut && !cancelled {
		if dirErr := s.rejectMissingBoxDir(ctx, h, dir, cwdArg); dirErr != nil {
			return ToolResult{}, dirErr
		}
		return sandboxUnavailableResult("shell_exec", execErr), nil
	}

	clean, capturedCwd := extractCwdMarker(capShellOutput(res.Stdout))
	finalCwd := dir
	if capturedCwd != "" {
		finalCwd = capturedCwd
		s.storeCwd(ctx, capturedCwd)
	}

	stderr := redactModelPreview(capShellOutput(res.Stderr))
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
	body = renderShellBody(body, status)

	// NewResultReservingTail, not NewResult: the [aura_shell {...}] footer is what the Spec tells
	// the model to read instead of spending a separate pwd or exit-code call, so it must never be
	// the part that pages into the sidecar on a large output.
	footer := renderShellFooter(ctx, body, stderr, status, shellExecFooter{
		ExitCode:   ecPtr,
		Cwd:        finalCwd,
		DurationMS: time.Since(started).Milliseconds(),
		TimedOut:   timedOut,
	})
	out, err := NewResultReservingTail(ctx, body, footer)
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

// boxDirProbeTimeout bounds the AG-018 cwd probe. It only ever runs on a path where the real exec
// has ALREADY failed fast, so this is the budget for one extra question, not for the command.
const boxDirProbeTimeout = 5 * time.Second

// rejectMissingBoxDir asks the box whether dir is a real directory, and returns a self-correctable
// argument error when it is not. nil means "not the cwd" — either the directory is fine, or the box
// could not answer at all, which is the outage the caller then denies on.
//
// AG-018 was a host os.Stat before the call while a host arm existed; a box path cannot be stat'ed
// from here, so the question is asked of the box itself, and only on the error path, so the common
// case pays nothing. Without it a mistyped cwd comes back as sandboxUnavailableResult, which tells
// the model the sandbox runtime is down and an operator must restore it — advice it can do nothing
// with except retry the same broken call forever.
func (s *ShellExec) rejectMissingBoxDir(ctx context.Context, h usersandbox.BoxHandle, dir, cwdArg string) error {
	if dir == "" {
		return nil
	}
	probeCtx, cancel := context.WithTimeout(ctx, boxDirProbeTimeout)
	defer cancel()
	res, err := s.Router.Exec(probeCtx, h, usersandbox.ExecRequest{Command: "[ -d " + shellQuoteArg(dir) + " ]"})
	if err != nil || res.ExitCode == 0 {
		return nil
	}
	if strings.TrimSpace(cwdArg) != "" {
		return fmt.Errorf("shell_exec: cwd %q is not a directory in your workspace container; "+
			"pass one that exists there, or create it first (mkdir -p)", dir)
	}
	// The dir came from this session's own tracked `cd` and is gone. Tracking is the ONLY cwd source
	// now that there is one execution path, so leaving it would resolve the same dead directory on
	// every later call and wedge the session permanently.
	s.Evict(shellSessionKey(ctx))
	return fmt.Errorf("shell_exec: %q, the directory this session had cd'd into, no longer exists in your "+
		"workspace container; the tracked directory has been reset to the workspace root — retry the command", dir)
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
