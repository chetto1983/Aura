package gateway

import (
	"context"
	"encoding/json"

	"github.com/chetto1983/aura/internal/agent/tools"
	"github.com/chetto1983/aura/internal/identityctx"
	"github.com/chetto1983/aura/internal/messagedrafts"
)

type reviewedMessageDraftKey struct{}

// WithReviewedMessageDraft carries the server-side one-send claim to the agent's
// normal gateway execution path. No model field can set this context value.
func WithReviewedMessageDraft(ctx context.Context, claim messagedrafts.Draft) context.Context {
	return context.WithValue(ctx, reviewedMessageDraftKey{}, claim)
}

func reviewedMessageClaim(ctx context.Context) (messagedrafts.Draft, bool) {
	claim, ok := ctx.Value(reviewedMessageDraftKey{}).(messagedrafts.Draft)
	return claim, ok
}

// decideReviewedMessage checks the claimed draft before policy shortcuts. A
// reviewed send skips generic standing approvals, but still enters the normal
// operation registry and reservation ledger under a strict profile.
func (g *Gateway) decideReviewedMessage(ctx context.Context, spec tools.Spec, args json.RawMessage, key ReservationKey) (Verdict, bool) {
	target, protected := messagedrafts.TrustedOutboundTarget(spec, args)
	claim, present := reviewedMessageClaim(ctx)
	if !protected && !present {
		return Verdict{}, false
	}
	if !protected || !present || !claim.MatchesDispatch(identityctx.IdentityID(ctx), spec.Name, target, args) ||
		key.ConversationID != claim.ConversationID || key.ToolCallID != claim.ToolCallID || key.RequestID != claim.ID {
		return g.refuseUnreviewedMessage(ctx, spec, args, key), true
	}
	if g == nil || !g.profile.Strict() {
		return Verdict{Decision: Allow, Reason: "reviewed message claim"}, true
	}
	spec.Mutating = true
	spec.Destructive = true
	tier := classify(spec, args)
	operationVerdict, proceed := g.beginOperation(ctx, spec, args, key, tier)
	if !proceed {
		return operationVerdict, true
	}
	verdict := g.reserve(ctx, spec, args, key, tier, claim.IdentityID, "", "")
	verdict.OperationDecision = operationVerdict.OperationDecision
	verdict.OperationClaimToken = operationVerdict.OperationClaimToken
	return verdict, true
}

// refuseUnreviewedMessage is the policy's answer to a send no review covers. On a strict
// profile it is recorded like the other two refusals: a headless job has no Runner writing its
// own row, so without the deny fact an injected send left no trace in the ledger (measured
// 2026-10-31, docs/superpowers/verification/2026-10-31-briefing-model-in-the-loop.md).
func (g *Gateway) refuseUnreviewedMessage(ctx context.Context, spec tools.Spec, args json.RawMessage, key ReservationKey) Verdict {
	tier := classify(spec, args)
	verdict := Verdict{Decision: Deny, Tier: tier, Reason: "message review required or claim mismatch", Refusal: true}
	if g != nil && g.profile.Strict() {
		g.recordDenyFact(ctx, spec, key, tier, verdict.Reason, map[string]any{"reason": "message_review"})
	}
	return verdict
}
