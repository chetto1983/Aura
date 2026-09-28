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
func (g *Gateway) decideReviewedMessage(ctx context.Context, spec tools.Spec, args json.RawMessage, key ReservationKey) (Verdict, bool, error) {
	target, protected := messagedrafts.TrustedOutboundTarget(spec, args)
	claim, present := reviewedMessageClaim(ctx)
	if !protected && !present {
		return Verdict{}, false, nil
	}
	if !protected || !present || !claim.MatchesDispatch(identityctx.IdentityID(ctx), spec.Name, target, args) ||
		key.ConversationID != claim.ConversationID || key.ToolCallID != claim.ToolCallID || key.RequestID != claim.ID {
		return Verdict{Decision: Deny, Reason: "message review required or claim mismatch"}, true, nil
	}
	if g == nil || !g.profile.Strict() {
		return Verdict{Decision: Allow, Reason: "reviewed message claim"}, true, nil
	}
	spec.Mutating = true
	spec.Destructive = true
	tier := classify(spec, args)
	operationVerdict, proceed := g.beginOperation(ctx, spec, args, key, tier)
	if !proceed {
		return operationVerdict, true, nil
	}
	verdict, err := g.reserve(ctx, spec, args, key, tier, claim.IdentityID, "")
	verdict.OperationDecision = operationVerdict.OperationDecision
	verdict.OperationClaimToken = operationVerdict.OperationClaimToken
	return verdict, true, err
}
