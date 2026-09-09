package tools

import (
	"context"
	"fmt"
	"time"

	"github.com/chetto1983/aura/internal/sandbox/usersandbox"
)

// boxOutcome carries a detached exec's result back to the call that started it, so the
// call can either render it (finished in time) or hand it to the registry (promoted).
type boxOutcome struct {
	res usersandbox.ExecResult
	err error
}

// Promotion at the cap (2026-09-09). A synchronous command that outlives its timeout used
// to be KILLED and reported as "[command timed out]", discarding whatever it had done —
// so the model's only move was to run the same command again and lose the work twice.
//
// The parameter that avoids this existed all along, and was never once used: measured
// across every recorded turn, 28 tool-call turns with zero `"background": true`. That is
// not the model choosing badly. The choice is demanded BEFORE anyone can know how long a
// command will take, which is a decision nobody can make well.
//
// So the runtime makes it instead, after the fact — the shape this session's own harness
// uses, measured live: a 10s command under a 3s cap reported "moved to the background"
// and still reached exit code 0.
//
// Why adoption rather than routing everything through startBox: ExecStream takes ONE
// writer, so the streamed path cannot keep stdout and stderr apart, and the separation is
// what lets a big failing build still show its stderr tail (shouldReserveStderrTail). The
// command therefore keeps running on its own detached context, and only its OUTCOME is
// adopted. The trade is that a promoted job's output arrives whole at the end rather than
// incrementally — shell_poll shows nothing until it finishes.

// promotedShell is the registry handle for an adopted job: the id the model polls, plus
// the shell the completion writes into.
type promotedShell struct {
	id string
	sh *bgShell
}

// adopt registers a job that is ALREADY RUNNING under cancel, returning the id the model
// polls. cancel is wired as the shell's terminator so shell_kill (and the TTL reaper)
// stops a promoted job exactly like a natively-backgrounded one — promotion must never
// mint work nobody can stop.
func (b *BackgroundShells) adopt(callerCtx context.Context, cancel context.CancelFunc) (*promotedShell, error) {
	id, err := newBackgroundShellID()
	if err != nil {
		return nil, err
	}
	sh := b.newShell(callerCtx, id)
	sh.cancel = func() { cancel() }
	if err := b.register(id, sh); err != nil {
		return nil, err
	}
	return &promotedShell{id: id, sh: sh}, nil
}

// settle delivers a promoted job's outcome: the output it produced, then the finish that
// flips its status, releases the TTL and fires the completion hook — the same notification
// a natively-backgrounded job sends when it exits.
//
// The exit code is wrapped in *bgBoxExit because that is the ONLY shape finish() reads one
// from: a streamed box exec reports its exit that way, and anything else is treated as an
// infra failure with no code — which would report a perfectly successful promoted job as
// "killed". Measured while writing this: a job that returned exit 0 came back "killed"
// until the outcome was wrapped.
func (p *promotedShell) settle(output string, exitCode int, waitErr error) {
	if output != "" {
		_, _ = p.sh.Write([]byte(output))
	}
	if waitErr == nil {
		waitErr = &bgBoxExit{code: exitCode}
	}
	p.sh.finish(waitErr)
}

// promotionNotice is what the model reads INSTEAD of the old dead end. It states what
// happened, that nothing was lost, the id, and the one call that collects it.
func promotionNotice(id string, cap fmt.Stringer) string {
	// "Aura notifies this conversation" is not a promise invented here: it is the existing
	// shellCompletionDispatcher (cmd/aura/shell_completion.go), the same wake path the swarm
	// uses to deliver a finished delegation. Promotion only puts work into a channel that was
	// already built and, until now, never carried anything.
	//
	// The id is never followed by punctuation: a trailing "." reads as part of the token
	// to anything parsing it — the model included.
	return fmt.Sprintf("[still running after the %s cap — NOT killed, moved to the background as shell_id: %s — "+
		"Aura notifies this conversation when it exits, and shell_poll with that id reads its output. "+
		"Continue with other work meanwhile]", cap, id)
}

// promoteAtCap hands a still-running command to the background registry and arranges for
// its eventual outcome to land there. It returns the id the model polls.
//
// A registry that is absent or full is NOT fatal and NOT silent: the caller falls back to
// the old kill-at-the-cap behaviour, which is strictly what happened before promotion
// existed.
func (s *ShellExec) promoteAtCap(ctx context.Context, cancel context.CancelFunc, outcome <-chan boxOutcome) (string, error) {
	if s.Background == nil {
		return "", fmt.Errorf("no background registry to promote into")
	}
	p, err := s.Background.adopt(ctx, cancel)
	if err != nil {
		return "", err
	}
	go func() {
		defer cancel()
		done := <-outcome
		body := capShellOutput(done.res.Stdout)
		if stderr := capShellOutput(done.res.Stderr); stderr != "" {
			body = joinFooterSections(body, stderr)
		}
		p.settle(body, done.res.ExitCode, done.err)
	}()
	return p.id, nil
}

// promotedResult renders what the model gets in place of the old dead end: the notice, the
// id, and the same [aura_shell {...}] footer shape every other shell result carries — with
// timed_out FALSE, because nothing timed out any more; the job is still running.
func (s *ShellExec) promotedResult(ctx context.Context, id, dir string, cap, took time.Duration) (ToolResult, error) {
	body := renderShellBody("", promotionNotice(id, cap))
	footer := renderShellFooter(ctx, body, "", promotionNotice(id, cap), shellExecFooter{
		Cwd:        dir,
		DurationMS: took.Milliseconds(),
	})
	out, err := NewResultReservingTail(ctx, body, footer)
	if err != nil {
		return ToolResult{}, err
	}
	meta := ToolResultMeta{"cwd": dir, "timed_out": false, "background_shell_id": id}
	out.Meta = &meta
	return out, nil
}
