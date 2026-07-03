package toolinvocations

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/chetto1983/aura/internal/db/sqlc"
	"github.com/jackc/pgx/v5"
)

// Reserve is the synchronous, fatal-on-failure pre-execution reservation (GATE-03 +
// GATE-04). The `start` Event is INSERTed under the existing UNIQUE index
// (conversation_id, request_id, tool_call_id, event_kind) with ON CONFLICT DO NOTHING,
// so the rows-affected count IS the idempotency key:
//
//   - n==1 ⇒ the reservation was acquired by THIS call → the mutating tool may Execute.
//   - n==0 ⇒ the slot is already held (a duplicate/retried tool_call) → the recorded
//     `end` fact is fetched and returned for replay so the side effect is NOT re-applied
//     (may be nil if the prior call is still in-flight / crash-orphaned before its end).
//   - INSERT error ⇒ wrapped error; the gateway maps it to a fail-closed deny (Execute
//     is never called — Pitfall 3: a reservation that cannot be durably taken blocks).
//
// Redaction runs for free inside toParams (RedactForLedger), so any secret on the tool
// command line is [REDACTED] before it reaches the append-only column. The verdict and
// operator_id ride start.Meta (D-01 zero-migration) — the caller folds them in.
func (s *Store) Reserve(ctx context.Context, start Event) (acquired bool, replay *Event, err error) {
	p, err := start.toParams()
	if err != nil {
		return false, nil, fmt.Errorf("reserve tool invocation: %w", err)
	}
	n, err := s.q.InsertToolInvocation(ctx, p)
	if err != nil {
		return false, nil, fmt.Errorf("reserve tool invocation %s/%s/%s: %w",
			start.ConversationID, start.RequestID, start.ToolCallID, err)
	}
	if n == 1 {
		return true, nil, nil
	}
	// n==0: the reservation slot is already held — return the recorded end for replay.
	replay, err = s.GetEnd(ctx, start.ConversationID, start.RequestID, start.ToolCallID)
	if err != nil {
		return false, nil, err
	}
	return false, replay, nil
}

// GetEnd fetches the single `end` fact for the GATE-04 triple, or (nil, nil) when no end
// row exists yet (a crash-orphaned in-flight start whose end never landed). pgx.ErrNoRows
// is mapped to a nil replay, NOT an error: "already reserved but not yet finished" is a
// valid replay state (the reconciler in 35-05 later synthesizes a terminal end for it).
func (s *Store) GetEnd(ctx context.Context, conversationID, requestID, toolCallID string) (*Event, error) {
	conv, err := uuidParam("conversation_id", conversationID)
	if err != nil {
		return nil, fmt.Errorf("get tool invocation end: %w", err)
	}
	req, err := uuidParam("request_id", requestID)
	if err != nil {
		return nil, fmt.Errorf("get tool invocation end: %w", err)
	}
	row, err := s.q.GetToolInvocationEnd(ctx, sqlc.GetToolInvocationEndParams{
		ConversationID: conv,
		RequestID:      req,
		ToolCallID:     toolCallID,
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, nil
		}
		return nil, fmt.Errorf("get tool invocation end %s/%s/%s: %w", conversationID, requestID, toolCallID, err)
	}
	ev := eventFromRow(row)
	return &ev, nil
}

// ListInFlightBefore returns every `start` fact with NO matching `end` whose started_at
// is older than cutoff — a start∧¬end anti-join. It is consumed by the 35-05 reconciler:
// a start∧¬end older than the reconciler's grace window is a crash-orphaned in-flight
// mutating call. This method only READS; the reconciler decides what to append.
func (s *Store) ListInFlightBefore(ctx context.Context, cutoff time.Time) ([]Event, error) {
	rows, err := s.q.ListInFlightToolInvocationsBefore(ctx, timestamptz(cutoff))
	if err != nil {
		return nil, fmt.Errorf("list in-flight tool invocations before %s: %w", cutoff.UTC(), err)
	}
	out := make([]Event, 0, len(rows))
	for _, r := range rows {
		out = append(out, eventFromRow(r))
	}
	return out, nil
}
