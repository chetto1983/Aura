package agent

import (
	"context"
	"encoding/json"
	"errors"
	"net"
	"time"

	"github.com/chetto1983/aura/internal/agent/tools"
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
func (a *LlmAgent) execTool(ctx context.Context, tool tools.Tool, mutating bool, args json.RawMessage) (tools.ToolResult, error) {
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

// isTransientToolErr reports whether err is a retryable transient failure, decided
// strictly by type: a net.Error that timed out, or a deadline-exceeded. Everything
// else (validation errors, unknown tool, business failures) is permanent and fed
// straight back to the model as an observation.
func isTransientToolErr(err error) bool {
	if err == nil {
		return false
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return true
	}
	var ne net.Error
	if errors.As(err, &ne) && ne.Timeout() {
		return true
	}
	return false
}
