package agent

import (
	"context"
	"encoding/json"

	"github.com/chetto1983/aura/internal/agent/tools"
	"github.com/chetto1983/aura/internal/gateway"
	"github.com/chetto1983/aura/internal/identityctx"
	"github.com/chetto1983/aura/internal/llm"
)

// withholdHostReviewedCall parks a call whose tool asks the host to review it before it runs
// (tools.HostReviewer), as withholdOutboundMessage parks a trusted send: the pause is keyed to
// the model's own call id, and the tool never runs in this turn.
//
// It needs a person in this conversation (decision 21 of
// docs/superpowers/plans/2026-11-05-scheduled-delivery-defaults-and-messages.md), an owner, and
// an identity policy that does not deny the call (decision 31): a denied call goes on to
// execTool, where the gateway refuses it and records why.
func (a *LlmAgent) withholdHostReviewedCall(ctx context.Context, call llm.ToolCall) (pauseCall, bool) {
	if !gateway.HasResponder(ctx) || tools.IsDelegatedDispatch(ctx) || identityctx.IdentityID(ctx) == "" {
		return pauseCall{}, false
	}
	tool, ok := a.registry.Get(call.Function.Name)
	if !ok {
		return pauseCall{}, false
	}
	reviewer, ok := tool.(tools.HostReviewer)
	if !ok {
		return pauseCall{}, false
	}
	raw := json.RawMessage(call.Function.Arguments)
	spec := tool.Spec()
	if a.gateway.PolicyDenies(ctx, spec, raw) {
		return pauseCall{}, false
	}
	pause, parked := reviewer.HostReview(ctx, a.ledgerConvID, call.ID, raw)
	if !parked {
		return pauseCall{}, false
	}
	return pauseCall{
		call: call, pause: pause,
		originalToolName: spec.Name, originalArguments: call.Function.Arguments,
	}, true
}
