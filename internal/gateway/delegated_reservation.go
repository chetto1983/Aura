package gateway

import (
	"context"
	"log/slog"
	"time"

	"github.com/chetto1983/aura/internal/agent/tools"
	"github.com/chetto1983/aura/internal/toolinvocations"
)

// A dispatch's ledger row has two halves and, before this, two different writers:
// the gateway INSERTs `start` wherever it reserves (OwnsToolStartRows), while the
// `end` is written by the Runner from the turn's agent.Event frames. That split
// works only while every dispatch flows through a Runner, and a swarm worker's does
// not — runChild consumes worker.Run(ic) itself and dumps to a per-child transcript,
// so a worker's frames never reach one.
//
// Measured live in spike 099: worker calls closed 0 of 5 reservations while the
// parent closed 3 of 3, and thirty minutes later the reconciler appended
// `status='error', meta.indeterminate` over calls that had SUCCEEDED — permanently,
// in a ledger nothing updates or deletes.
//
// The rule is LibreChat's (D-00): whoever opens a record closes it, so no component
// ever has to find and close a row another one opened. These two carriers give the
// gateway what it needs to honour that at its own terminal hooks — the key it
// reserved under, and whether anyone else is watching this stream.

type reservationKeyContextKey struct{}

// reservedDispatch is the {conversation, request, tool_call} triple the gateway
// reserved under plus the tool it reserved for — everything the `end` row needs
// that the terminal hooks do not otherwise receive.
type reservedDispatch struct {
	key  ReservationKey
	tool string
}

// WithReservationKey carries what the gateway reserved under, so its terminal hooks
// can close the same row they opened without the key being threaded through every
// signature between them. Set by the agent's dispatch, which is the one place that
// holds both the key and the spec.
func WithReservationKey(ctx context.Context, key ReservationKey, toolName string) context.Context {
	return context.WithValue(ctx, reservationKeyContextKey{}, reservedDispatch{key: key, tool: toolName})
}

func reservedDispatchFromContext(ctx context.Context) (reservedDispatch, bool) {
	d, ok := ctx.Value(reservationKeyContextKey{}).(reservedDispatch)
	return d, ok
}

// WithDelegatedDispatch marks a dispatch whose events no Runner observes, which is
// what makes the gateway responsible for the `end` row too. Set by the swarm for a
// worker's context; absent everywhere else, so a parent's dispatch keeps the
// Runner's richer end (exit code, duration, sidecar path) and the gateway stays out
// of a first-writer-wins race it would win with a poorer row.
//
// The marker itself lives in internal/agent/tools (WithDelegatedDispatch /
// IsDelegatedDispatch), not here: D-10 (Phase 51) needs the same "is this a
// worker's dispatch" signal inside internal/agent/mcptools, which this package
// cannot be imported from without a cycle (classify.go already imports
// internal/agent/mcptools). This wrapper keeps the exported name and signature
// identical so every existing caller (internal/swarm's runChild) is untouched.
func WithDelegatedDispatch(ctx context.Context) context.Context {
	return tools.WithDelegatedDispatch(ctx)
}

// isDelegatedDispatch reports whether this dispatch has no Runner observing it.
func isDelegatedDispatch(ctx context.Context) bool {
	return tools.IsDelegatedDispatch(ctx)
}

// closeDelegatedReservation appends the terminal `end` row for a dispatch the
// gateway reserved and no Runner will close. It is a no-op for every other
// dispatch, so it can be called unconditionally from both terminal hooks.
//
// Best-effort by design, matching the ledger's own contract (migration 0011: the
// ledger is operational observability, not a permission system). The mutating
// effect has already happened by the time we get here; failing the call because the
// audit row would not write would turn a successful mutation into a reported error.
// A failure is a WARN and the reconciler remains the backstop it always was.
func (g *Gateway) closeDelegatedReservation(ctx context.Context, status, errText string, result tools.ToolResult) {
	if g == nil || g.store == nil || !isDelegatedDispatch(ctx) {
		return
	}
	dispatch, ok := reservedDispatchFromContext(ctx)
	if !ok {
		// Reaching a terminal hook on a delegated dispatch with no recorded key means
		// the row cannot be addressed, which is worth saying out loud: the reservation
		// is about to be orphaned exactly as it was before this existed.
		slog.Warn("gateway: delegated dispatch has no reservation key; end row not written",
			"status", status)
		return
	}
	end := toolinvocations.Event{
		ConversationID:    dispatch.key.ConversationID,
		RequestID:         dispatch.key.RequestID,
		ToolCallID:        dispatch.key.ToolCallID,
		ToolName:          dispatch.tool,
		Event:             toolinvocations.EventEnd,
		EndedAt:           time.Now().UTC(),
		Status:            status,
		Error:             errText,
		ResultPreview:     result.Preview,
		PreviewBytes:      len(result.Preview),
		ResultBytes:       result.Bytes,
		ResultTruncated:   result.Truncated,
		ResultSidecarPath: result.FullPath,
		Meta:              map[string]any{"delegated": true},
	}
	// Detached for the same reason the Runner detaches its own end: this row closes a
	// reservation, and the most common way to reach it is a context that has just
	// expired. Recording that an effect happened must not be cancellable by whatever
	// cancelled the work.
	insertCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), delegatedEndInsertTimeout)
	defer cancel()
	if err := g.store.Insert(insertCtx, end); err != nil {
		slog.Warn("gateway: delegated end row insert failed; reservation left for the reconciler",
			"tool", dispatch.tool, "conversation_id", dispatch.key.ConversationID, "err", err)
	}
}

const delegatedEndInsertTimeout = 5 * time.Second
