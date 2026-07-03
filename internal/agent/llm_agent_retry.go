package agent

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net"
	"syscall"
	"time"

	"github.com/chetto1983/aura/internal/agent/tools"
	"github.com/chetto1983/aura/internal/gateway"
)

// Tool-execution retry seam (parity item P2 — the "error hook"). A non-mutating
// tool whose Execute fails with a TYPED transient network/timeout error is retried
// a bounded number of times while the parent context is still alive. This mirrors
// streamWithOpenRetry (already applied to the LLM stream) and closes the
// "no middleware around tool execution" gap vs Claude Code's harness.
//
// Deliberately conservative:
//   - A MUTATING tool is NEVER retried — side effects must stay at-most-once (a
//     half-applied fs_write / shell_exec must not run twice).
//   - "Transient" is decided by TYPE (net.Error.Timeout / context.DeadlineExceeded),
//     never by string-matching an opaque error message ([[feedback_no_regex_for_nlp]]).
//   - A tripped parent ctx (cancel or the per-call total-timeout) stops retrying
//     immediately: the whole call is over, not a per-tool blip.
const maxToolRetries = 2 // up to 3 total attempts for a non-mutating transient failure

// toolRetryBaseDelay is the linear-backoff unit (attempt n waits (n+1)·base). A var,
// not a const, so tests can shrink it without sleeping real backoff windows.
var toolRetryBaseDelay = 200 * time.Millisecond

// execTool runs one tool, retrying a non-mutating transient failure with a small
// linear backoff. It returns the last result/error once the tool succeeds, the
// error is non-transient, the attempt budget is spent, the tool is mutating, or the
// parent ctx is done.
//
// GATE-01: the gateway.Decide PEP is interposed at the TOP, BEFORE the retry loop, so
// every non-ask_user dispatch crosses exactly one policy decision before tool.Execute.
// A Deny returns *gateway.ErrDenied (the tool never executes); an Approve returns the
// gateway's *tools.ErrAwaitingUserInput (the mutating action is withheld pending an
// operator decision; a resume RE-ENTERS execTool → Decide). A nil gateway is an Allow
// no-op (dev-parity). ask_user is EXEMPT: it is the approval primitive, so gating it
// risks approve→ask_user→Decide→approve recursion — it structurally never reaches
// execTool (llm_agent_pause.go pre-executes ask_user), and this defensive short-circuit
// keeps it that way even if the dispatch path changes (D-03/CV-1).
func (a *LlmAgent) execTool(ctx context.Context, tool tools.Tool, mutating bool, args json.RawMessage) (tools.ToolResult, error) {
	spec := tool.Spec()
	if spec.Name != askUserToolName {
		key := gateway.ReservationKey{
			ConversationID: a.ledgerConvID,
			RequestID:      tools.RequestIDFromContext(ctx),
			ToolCallID:     tools.ToolCallIDFromContext(ctx),
		}
		verdict, pause, derr := a.gateway.Decide(ctx, spec, args, key)
		if derr != nil {
			return tools.ToolResult{}, derr
		}
		switch verdict.Decision {
		case gateway.Deny:
			return tools.ToolResult{}, &gateway.ErrDenied{Reason: verdict.Reason, Tier: verdict.Tier}
		case gateway.Approve:
			return tools.ToolResult{}, pause
		}
	}

	var res tools.ToolResult
	var err error
	for attempt := 0; ; attempt++ {
		res, err = tool.Execute(ctx, args)
		if err == nil || mutating || attempt >= maxToolRetries || ctx.Err() != nil || !isTransientToolErr(err) {
			return res, err
		}
		select {
		case <-ctx.Done():
			return res, err
		case <-time.After(time.Duration(attempt+1) * toolRetryBaseDelay):
		}
	}
}

// isTransientNetworkErr reports whether err is a TYPED transient network failure —
// the subset shared by the tool-retry and stream-open-retry classifiers (QUAL-03).
// It matches a net.Error that timed out or any wrapped connection sentinel
// (io.EOF/io.ErrUnexpectedEOF, ECONNRESET/ECONNREFUSED/ETIMEDOUT) via errors.Is, so
// a sentinel survives wrapping even when its rendered message carries no substring
// marker. It deliberately EXCLUDES context.*, HTTP status, url.Error,
// ErrStreamIdleTimeout, and the retryableNetworkText fallback — those stay
// domain-specific to each caller. The stream path's context.*->false guard is the
// reason a symmetric merge is unsafe (see retryableStreamOpenError, Pitfall 2):
// context.DeadlineExceeded is itself a net.Error{Timeout}, so this predicate would
// classify it as transient — only the stream path's leading guard prevents that.
func isTransientNetworkErr(err error) bool {
	if err == nil {
		return false
	}
	var ne net.Error
	if errors.As(err, &ne) && ne.Timeout() {
		return true
	}
	return errors.Is(err, io.ErrUnexpectedEOF) || errors.Is(err, io.EOF) ||
		errors.Is(err, syscall.ECONNRESET) || errors.Is(err, syscall.ECONNREFUSED) ||
		errors.Is(err, syscall.ETIMEDOUT)
}

// isTransientToolErr reports whether a non-mutating tool failure is worth retrying.
// It is an INTENTIONAL WIDENING (QUAL-03) of the original timeout-only rule: it now
// retries the full shared typed-network subset (ECONNRESET/ECONNREFUSED/ETIMEDOUT,
// io.EOF/io.ErrUnexpectedEOF) IN ADDITION to context.DeadlineExceeded, which the
// tool path has always retried. Everything else (validation errors, unknown tool,
// business failures) stays permanent and is fed straight back to the model as an
// observation. The stream path does NOT share this widening — it keeps a strict
// context.*->false guard (Pitfall 2).
func isTransientToolErr(err error) bool {
	return errors.Is(err, context.DeadlineExceeded) || isTransientNetworkErr(err)
}
