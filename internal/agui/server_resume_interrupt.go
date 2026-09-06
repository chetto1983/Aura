package agui

import (
	"context"
	"fmt"

	"github.com/ag-ui-protocol/ag-ui/sdks/community/go/pkg/core/types"
	"github.com/google/uuid"
)

// server_resume_interrupt.go closes the gap between the id the gateway PUBLISHES on an
// interrupt and the id its resume path ACCEPTS.
//
// RUN_FINISHED carries outcome.interrupts[].id, and translator.go sets it to the pause's
// tool_call id ("c0"). The resume path keys on the askuser pause TOKEN, a uuid. So a
// conforming AG-UI client that echoes back the id the server just handed it was answered
// `invalid token "c0": invalid UUID length: 2` — measured live 2026-09-06 driving the
// protocol directly. The cockpit never hit it because it resolves through
// GET /api/approvals, which returns the token; only a third-party client could.
//
// The token is deliberately NOT published on the event instead. runner_persist.go states
// the invariant (A1): a pause Event carries no token, because the token is minted before
// the row is committed and a consumer must not learn it until the flush makes the pause
// real. Putting it in the SSE frame would hand a client a token to resume against a row
// that does not exist yet. So the translation happens on the way IN, against the store,
// after the flush — which is the only side where both facts are true at once.
//
// resolveInterruptIDs is therefore a no-op for a caller that already sends a token: only a
// non-uuid id is looked up, so nothing about the existing path changes.

// resolveInterruptIDs rewrites any non-uuid interruptId into the pause token it names,
// matching on (this thread, that tool_call_id) among the caller's OWN pending pauses.
//
// Unresolvable ids are left untouched rather than rejected here: the existing
// GetByToken path already produces the right error for them, and duplicating that
// judgement would give the same input two different messages depending on the day.
func (s *Server) resolveInterruptIDs(
	ctx context.Context, threadID string, entries []types.ResumeEntry,
) ([]types.ResumeEntry, error) {
	if s.approvals == nil || len(entries) == 0 {
		return entries, nil
	}
	needed := false
	for _, e := range entries {
		if _, err := uuid.Parse(e.InterruptID); err != nil {
			needed = true
			break
		}
	}
	if !needed {
		return entries, nil
	}
	pendings, err := s.approvals.ListPendingAllForIdentity(ctx, scopedIdentityID(ctx), defaultApprovalsLimit)
	if err != nil {
		return nil, fmt.Errorf("resolve interrupt ids: %w", err)
	}
	// A tool_call id is unique within one round, not across a conversation's history, so
	// two still-pending pauses on this thread can legitimately share one. Refusing beats
	// resolving to whichever the store listed first and answering the wrong question.
	tokens := make(map[string]string, len(pendings))
	ambiguous := make(map[string]bool, len(pendings))
	for _, p := range pendings {
		if p.ConversationID != threadID || p.ToolCallID == "" {
			continue
		}
		if _, seen := tokens[p.ToolCallID]; seen {
			ambiguous[p.ToolCallID] = true
			continue
		}
		tokens[p.ToolCallID] = p.Token
	}
	out := make([]types.ResumeEntry, len(entries))
	copy(out, entries)
	for i, e := range out {
		if _, err := uuid.Parse(e.InterruptID); err == nil {
			continue
		}
		if ambiguous[e.InterruptID] {
			return nil, fmt.Errorf(
				"resume %q: two pending pauses on this thread carry that tool call id — resume by pause token",
				e.InterruptID)
		}
		if token, ok := tokens[e.InterruptID]; ok {
			out[i].InterruptID = token
		}
	}
	return out, nil
}
