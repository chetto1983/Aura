// approve.go holds the mutating-approve routing (D-03): responder-presence detection,
// the gateway_approval ResumeContext builder, the DENY-branch terminal decision-fact
// recorder, and the post-resume approved branch. approve is host/policy-side ONLY — no
// tool schema exposes it (D-03c), so the model can never self-approve a gated action.
package gateway

import (
	"context"
	"encoding/json"
	"log/slog"
	"time"

	"github.com/chetto1983/aura/internal/agent/tools"
	"github.com/chetto1983/aura/internal/config"
	"github.com/chetto1983/aura/internal/scoring"
	"github.com/chetto1983/aura/internal/toolinvocations"
)

// gatewayApprovalType is the ResumeContext discriminator for a gateway-minted approval,
// distinct from ask_user's own skill_approval type so a host-side handler can route it.
const gatewayApprovalType = "gateway_approval"

// responderCtxKey marks a ctx as an interactive session with a positively-known human
// responder (a live CLI REPL / cockpit SSE subscriber / live Telegram chat). Headless
// cron/swarm ctxs never carry it, so responderPresent defaults to false → DENY (D-03a).
type responderCtxKey struct{}

// WithResponder marks ctx as an interactive session with a live human responder. The
// interactive composition root (the runner) sets it; the headless roots (cron
// agent_job, swarm child) never do — so a gateway approve degrades to deny there.
func WithResponder(ctx context.Context) context.Context {
	return context.WithValue(ctx, responderCtxKey{}, true)
}

// responderPresent reports whether an interactive human responder is positively known
// on ctx. The default is DENY (D-03a): the absence of the marker means "no responder",
// never "assume one" — a mistaken pause hangs a headless run.
func responderPresent(ctx context.Context) bool {
	v, _ := ctx.Value(responderCtxKey{}).(bool)
	return v
}

// ResolvedApproval is an operator's answer to a gateway_approval pause, carried back
// into a resumed Decide (the resume RE-ENTERS execTool → Decide). Approved==true routes
// routeApprove to Verdict{Allow, OperatorID} with NO ledger write of its own.
type ResolvedApproval struct {
	Approved   bool
	OperatorID string
}

// resolvedApprovalCtxKey carries a ResolvedApproval into a resumed Decide (D-03 point 2).
type resolvedApprovalCtxKey struct{}

// WithResolvedApproval carries an operator's gateway_approval resolution into a resumed
// Decide. The resume orchestrator (a later plan) sets it before re-entering execTool so
// the approved call returns Allow+OperatorID without re-pausing.
func WithResolvedApproval(ctx context.Context, r ResolvedApproval) context.Context {
	return context.WithValue(ctx, resolvedApprovalCtxKey{}, r)
}

func resolvedApproval(ctx context.Context) (ResolvedApproval, bool) {
	r, ok := ctx.Value(resolvedApprovalCtxKey{}).(ResolvedApproval)
	return r, ok
}

// routeApprove routes a mutating GateRecommended call by responder presence (D-03).
//
//   - A resume carrying the operator's resolution → Verdict{Allow, OperatorID}, NO row
//     (the executed marker rides 35-04's single reservation start Meta — D-03 point 2).
//   - server_production OR no positively-known responder → deny-with-guidance, plus a
//     durable degraded_deny(reason=no_approver) terminal `end` decision-fact (D-03a/b,
//     D-03 point 1 / GATE-01). It NEVER pauses in place.
//   - single_user_hardened + a live responder → Verdict{Approve} + the shipped
//     *ErrAwaitingUserInput pause carrying a {"type":"gateway_approval",...} ResumeContext.
func (g *Gateway) routeApprove(ctx context.Context, spec tools.Spec, tier scoring.RiskTier, key ReservationKey) (Verdict, *tools.ErrAwaitingUserInput, error) {
	if r, ok := resolvedApproval(ctx); ok && r.Approved {
		// D-03 point 2: the operator approved on a prior turn; the resume re-entered
		// Decide. Return Allow+OperatorID and write NOTHING — a bare executed(operator_id)
		// Insert here would compete for the (conv,req,toolCall,event_kind) slot the real
		// reservation start/end needs, silently discarding the tool's outcome and blinding
		// the 35-05 reconciler. The marker rides the single reservation start Meta (35-04).
		return Verdict{Decision: Allow, Tier: tier, OperatorID: r.OperatorID}, nil, nil
	}
	// D-03b: production identity is unverified pre-Phase-36, so it is never interactive;
	// D-03a: a headless run has no positively-known responder → default DENY.
	if g.profile == config.ProfileServerProduction || !responderPresent(ctx) {
		g.recordDegradedDeny(ctx, spec, key, tier)
		return Verdict{Decision: Deny, Tier: tier, Reason: "no interactive approver — action declined"}, nil, nil
	}
	// single_user_hardened + a live responder → emit the shipped pause sentinel. Reuse
	// tools.ApprovalPriority (the skill-approval FIFO priority) — do NOT re-derive it.
	pause := &tools.ErrAwaitingUserInput{
		Kind:          tools.KindApproval,
		Priority:      tools.ApprovalPriority(tier),
		ResumeContext: gatewayApprovalContext(spec, tier, key),
	}
	return Verdict{Decision: Approve, Tier: tier}, pause, nil
}

// gatewayApprovalContext builds the {"type":"gateway_approval",...} ResumeContext the
// host-side approval handler reads. It carries NO secret — only the tool + tier + the
// originating-conversation-keyed triple, so a resume can re-enter Decide for THIS call.
func gatewayApprovalContext(spec tools.Spec, tier scoring.RiskTier, key ReservationKey) json.RawMessage {
	b, err := json.Marshal(map[string]any{
		"type":            gatewayApprovalType,
		"tool":            spec.Name,
		"tier":            string(tier),
		"conversation_id": key.ConversationID,
		"request_id":      key.RequestID,
		"tool_call_id":    key.ToolCallID,
	})
	if err != nil {
		return nil
	}
	return b
}

// recordDegradedDeny durably records the headless/production denial as a TERMINAL `end`
// row (event_kind='end', status='error', reason=no_approver in Event.Meta) keyed on the
// ORIGINATING conversation UUID (D-03 point 1 / GATE-01). This is the only legal terminal
// shape (migration 0011 event_kind ∈ {'start','end'}); because the call never executes, a
// lone `end` row is correct — the 35-05 reconciler's start∧¬end anti-join never flags it,
// and a denied triple never later executes (a model retry yields a fresh tool_call_id), so
// it never collides with a future start/end. A store or key failure is a WARN: the denial
// itself still stands (fail-closed) — only the audit fact is best-effort.
func (g *Gateway) recordDegradedDeny(ctx context.Context, spec tools.Spec, key ReservationKey, tier scoring.RiskTier) {
	if g.store == nil {
		return
	}
	ev := toolinvocations.Event{
		ConversationID: key.ConversationID,
		RequestID:      key.RequestID,
		ToolCallID:     key.ToolCallID,
		ToolName:       spec.Name,
		Event:          toolinvocations.EventEnd,
		EndedAt:        time.Now().UTC(),
		Status:         "error",
		Error:          "gateway degraded_deny: no interactive approver",
		Meta: map[string]any{
			"gateway_verdict": string(Deny),
			"gateway_tier":    string(tier),
			"degraded_deny":   true,
			"reason":          "no_approver",
		},
	}
	if err := g.store.Insert(ctx, ev); err != nil {
		slog.Warn("gateway: degraded_deny fact insert failed",
			"tool", spec.Name, "conversation_id", key.ConversationID, "err", err)
	}
}
