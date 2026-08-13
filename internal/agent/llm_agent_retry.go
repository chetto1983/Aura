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
	"github.com/chetto1983/aura/internal/idempotency"
	"github.com/chetto1983/aura/internal/mcp"
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

// operationBookkeepingTimeout bounds the terminal write that closes an acquired operation
// claim. It runs on a context detached from the caller's, so it needs a deadline of its own.
const operationBookkeepingTimeout = 5 * time.Second

// bookkeepingCtx detaches the operation's terminal write from the caller's cancellation.
//
// Closing a claim RECORDS work that already happened, so it must not be cancellable by the
// thing that cancelled the work. With the caller's ctx, an abandoned turn (page reload,
// dropped SSE, per-call timeout) made tool.Execute fail with ctx.Err() AND made the
// MarkOperationIndeterminate that follows fail too — the claim then sat in_progress until
// its lease expired, and every identical retry in between was denied with "operation is in
// progress". Measured on the live appliance: 17 acquisitions, 6 recorded outcomes, 11 claims
// leaked. The same detach-then-rebound pattern is already used for the finalize/completion
// LLM calls (llm_agent_completion.go, llm_agent_finalize.go).
func bookkeepingCtx(ctx context.Context) (context.Context, context.CancelFunc) {
	return context.WithTimeout(context.WithoutCancel(ctx), operationBookkeepingTimeout)
}

// toolRetryBaseDelay is the linear-backoff unit (attempt n waits (n+1)·base). A var,
// not a const, so tests can shrink it without sleeping real backoff windows.
var toolRetryBaseDelay = 200 * time.Millisecond

// replayLayerAttributes derives the OTel evidence for a replayed tool call from
// state gateway.Verdict already carries — no new field on Verdict (D-10/T-45-10).
// operationDecision == idempotency.DecisionReplay means the shared operation
// registry served the replay (Layer B, decide.go:76-79's !proceed short-circuit);
// any other decision alongside replay==true means the reservation ledger did
// (Layer A, reserve.go's rows==0 branch). replayed is false only when there is
// no replay to attribute — a fresh execution.
func replayLayerAttributes(operationDecision idempotency.Decision, replay bool) (replayed bool, layer string) {
	if !replay {
		return false, ""
	}
	if operationDecision == idempotency.DecisionReplay {
		return true, "operation"
	}
	return true, "reservation"
}

// execTool runs one tool, retrying a non-mutating transient failure with a small
// linear backoff. It returns the last result/error once the tool succeeds, the
// error is non-transient, the attempt budget is spent, the tool is mutating, or the
// parent ctx is done.
//
// GATE-01: the gateway.Decide PEP is interposed at the TOP, BEFORE the retry loop, so
// every non-ask_user dispatch crosses exactly one policy decision before tool.Execute.
// A Deny returns *gateway.ErrDenied (the tool never executes); an Approve returns the
// gateway's shell_exec-style approval-required ToolResult as a NORMAL result (no error,
// tool.Execute NOT called — the mutating action is withheld). Because it is a normal
// result, runTool persists the REAL tool_call + args + the approval message, so the model
// relays the request via ask_user and retries the EXACT call after the operator accepts;
// a resume RE-ENTERS execTool → Decide → Consume → Allow. A nil gateway is an Allow no-op
// (dev-parity). ask_user is EXEMPT: it is the approval primitive, so gating it risks
// approve→ask_user→Decide→approve recursion — it structurally never reaches execTool
// (llm_agent_pause.go pre-executes ask_user), and this defensive short-circuit keeps it
// that way even if the dispatch path changes (D-03/CV-1).
func (a *LlmAgent) execTool(ctx context.Context, tool tools.Tool, mutating bool, args json.RawMessage) (tools.ToolResult, error) {
	spec := tool.Spec()
	if mutating {
		var err error
		ctx, err = deriveToolOperationContext(ctx, spec, args)
		if err != nil {
			return tools.ToolResult{}, err
		}
	}
	operationAcquired := false
	if spec.Name != askUserToolName {
		key := gateway.ReservationKey{
			ConversationID: a.ledgerConvID,
			RequestID:      tools.RequestIDFromContext(ctx),
			ToolCallID:     tools.ToolCallIDFromContext(ctx),
		}
		verdict, derr := a.gateway.Decide(ctx, spec, args, key)
		// Read the claim BEFORE the error check: Decide can acquire the operation and then
		// fail on the policy reservation that follows it, and returning early here left that
		// claim with nobody to close it — the second half of the same leak.
		operationAcquired = verdict.OperationDecision == idempotency.DecisionAcquired
		if derr != nil {
			if operationAcquired {
				bctx, cancel := bookkeepingCtx(ctx)
				defer cancel()
				return tools.ToolResult{}, errors.Join(derr, a.gateway.MarkOperationIndeterminate(bctx))
			}
			return tools.ToolResult{}, derr
		}
		if operationAcquired {
			claimedCtx, claimErr := idempotency.WithClaimToken(
				ctx,
				verdict.OperationClaimToken,
			)
			if claimErr != nil {
				return tools.ToolResult{}, claimErr
			}
			ctx = claimedCtx
		}
		if verdict.OperationDecision == idempotency.DecisionRejected {
			reason := verdict.Reason
			if verdict.OperationRejection != nil && verdict.OperationRejection.Preview != "" {
				reason = verdict.OperationRejection.Preview
			}
			return tools.ToolResult{}, &gateway.ErrOperationRejected{Reason: reason}
		}
		switch verdict.Decision {
		case gateway.Deny:
			denied := &gateway.ErrDenied{Reason: verdict.Reason, Tier: verdict.Tier}
			if operationAcquired {
				bctx, cancel := bookkeepingCtx(ctx)
				defer cancel()
				return tools.ToolResult{}, errors.Join(denied, a.gateway.MarkOperationIndeterminate(bctx))
			}
			return tools.ToolResult{}, denied
		case gateway.Approve:
			// The mutating action is WITHHELD: return the approval-required tool RESULT
			// (no error, tool.Execute not called). runTool persists the real call+args +
			// this message so the model relays it via ask_user and retries after approval
			// (the resume re-enters here → Consume → Allow). A nil ApprovalRequest (contract
			// violation) degrades to an empty result rather than a panic.
			if verdict.ApprovalRequest != nil {
				return *verdict.ApprovalRequest, nil
			}
			return tools.ToolResult{}, nil
		}
		// GATE-04: a non-nil Replay means the reservation slot was already held (rows==0) —
		// the tool ran on a prior (duplicate/retried) dispatch. Return the recorded outcome
		// WITHOUT calling tool.Execute, so the mutating side effect stays at-most-once.
		// D-10/T-45-10: stamp the SAME fact on the tool.execute span the ctx already
		// carries, so a replayed call is distinguishable from a fresh one in a trace too
		// (the marker on Preview is the model-facing half of this fact — reserve.go).
		if verdict.Replay != nil {
			replayed, layer := replayLayerAttributes(verdict.OperationDecision, true)
			stampReplayAttributes(ctx, replayed, layer)
			return *verdict.Replay, nil
		}
	}

	var res tools.ToolResult
	var err error
	for attempt := 0; ; attempt++ {
		res, err = tool.Execute(ctx, args)
		if err == nil || mutating || attempt >= maxToolRetries || ctx.Err() != nil || !isTransientToolErr(err) {
			if !operationAcquired {
				return res, err
			}
			// Detached from ctx on purpose: the most common way to get here is the caller's
			// context expiring, which is exactly when the claim MUST still be closed.
			bctx, cancel := bookkeepingCtx(ctx)
			defer cancel()
			if err != nil {
				var toolCallErr *mcp.ToolCallError
				if errors.As(err, &toolCallErr) && toolCallErr.DeterministicNoEffect() {
					return res, errors.Join(err, a.gateway.RejectOperation(bctx, toolCallErr))
				}
				return res, errors.Join(err, a.gateway.MarkOperationIndeterminate(bctx))
			}
			if completeErr := a.gateway.CompleteOperation(bctx, res); completeErr != nil {
				return res, errors.Join(completeErr, a.gateway.MarkOperationIndeterminate(bctx))
			}
			return res, nil
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
