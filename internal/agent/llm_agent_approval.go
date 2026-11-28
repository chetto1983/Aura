package agent

import (
	"context"
	"encoding/json"
	"log/slog"

	"github.com/chetto1983/aura/internal/agent/tools"
	"github.com/chetto1983/aura/internal/gateway"
	"github.com/chetto1983/aura/internal/identityctx"
	"github.com/chetto1983/aura/internal/llm"
)

// approvalSourceGateway is toolapprovals.SourceGateway, the value the Runner stores as is.
const approvalSourceGateway = "gateway"

// withheldCalls is pass 2 of pause detection: the calls the host parks before they run, in
// the model's order. One call has one card (spec decision 2), so the sources are asked in a
// fixed order and the first that parks a call owns it.
//
// Drafts and host review read the whole batch first. Their park drops the terminal
// text_response (D19), and only then does the gateway source read what is left, so whether a
// gated call parks never depends on where the draft sits in the batch (ruling P9). A batch
// that still carries the terminal is not the gateway's to park: it keeps today's
// terminal-exclusivity rejection (D14).
func (a *LlmAgent) withheldCalls(ctx context.Context, calls []llm.ToolCall) []pauseCall {
	withheld := make([]*pauseCall, len(calls))
	terminalDropped := false
	for i, call := range calls {
		if p, ok := a.withholdOutboundMessage(ctx, call); ok {
			withheld[i] = &p
		} else if p, ok := a.withholdHostReviewedCall(ctx, call); ok {
			withheld[i] = &p
		}
		terminalDropped = terminalDropped || withheld[i] != nil
	}
	if terminalDropped || !hasTerminal(calls) {
		// index counts the calls the park round keeps, which is CallIndex's frame.
		index := 0
		for i, call := range calls {
			if call.Function.Name == terminalTool {
				continue
			}
			if withheld[i] == nil {
				if p, ok := a.withholdForApproval(ctx, call, index); ok {
					withheld[i] = &p
				}
			}
			index++
		}
	}
	var out []pauseCall
	for _, p := range withheld {
		if p != nil {
			out = append(out, *p)
		}
	}
	return out
}

func hasTerminal(calls []llm.ToolCall) bool {
	for _, call := range calls {
		if call.Function.Name == terminalTool {
			return true
		}
	}
	return false
}

// withholdForApproval parks a call the gateway would stop to ask the operator about, so the
// host shows its own card and the model never relays one. index is the call's position in
// the park round's assistant message.
//
// It needs the switch, a person on this turn, an identity, and a tool whose schema the model
// has read: a deferred tool called unloaded is refused at dispatch, never parked. Everything
// else is the gateway's reading (D7): a denied call, one a grant answers, and one the
// production profile refuses are not needed. A review error is not an approval either: the
// call goes on to its normal dispatch, where the gateway decides again.
//
// The review gets the call's own context (AM-4), the one runTool hands the tool, so a session
// grant and the describer see this conversation and this call id.
func (a *LlmAgent) withholdForApproval(ctx context.Context, call llm.ToolCall, index int) (pauseCall, bool) {
	if !a.parkApprovals || !gateway.HasResponder(ctx) || tools.IsDelegatedDispatch(ctx) || identityctx.IdentityID(ctx) == "" {
		return pauseCall{}, false
	}
	name := call.Function.Name
	// ask_user is the approval primitive and never reaches the gateway (execTool).
	if name == askUserToolName || a.isDeferredUnloaded(name) {
		return pauseCall{}, false
	}
	tool, ok := a.registry.Get(name)
	if !ok {
		return pauseCall{}, false
	}
	spec := tool.Spec()
	raw := json.RawMessage(call.Function.Arguments)
	reviewCtx := tools.WithOriginConversationID(
		tools.WithToolCallContext(ctx, a.sessionID, call.ID, a.runDir, a.previewCap), a.originConversationID)
	key := gateway.ReservationKey{ConversationID: a.ledgerConvID, RequestID: tools.RequestIDFromContext(ctx), ToolCallID: call.ID}
	describer, _ := tool.(tools.ApprovalDescriber)
	review, err := a.gateway.Review(reviewCtx, spec, raw, key, describer)
	if err != nil {
		slog.Warn("agent: approval review failed; the call goes to its dispatch", "tool", name, "tool_call_id", call.ID, "err", err)
		return pauseCall{}, false
	}
	if !review.Needed {
		return pauseCall{}, false
	}
	options := make([]tools.Option, len(review.Options))
	for i, o := range review.Options {
		options[i] = tools.Option{Label: o.Label, Value: o.Value}
	}
	return pauseCall{
		call: call,
		// No ResumeContext: it is streamed, and the Runner writes the tool_approval context
		// from Approval, which never is (D10).
		pause: &tools.ErrAwaitingUserInput{
			Question: review.Question, Options: options, Kind: tools.KindApproval,
			Priority: tools.ApprovalPriority(review.Tier), ToolCallID: call.ID,
		},
		originalToolName: spec.Name, originalArguments: call.Function.Arguments,
		approval: &ParkedApproval{
			Source: approvalSourceGateway, ToolName: spec.Name,
			ExecuteArgs: append(json.RawMessage(nil), raw...), Fingerprint: tools.CallFingerprint(raw),
			Presentation: review.Presentation, SubjectTool: review.SubjectTool, SubjectAction: review.SubjectAction,
			CallIndex: index,
		},
	}, true
}
