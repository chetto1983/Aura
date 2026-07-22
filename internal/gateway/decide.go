// decide.go is the PEP proper (GATE-01): Decide is interposed inside execTool above
// tool.Execute at all three composition roots. It branches on the runtime profile
// (dev/local_trusted host-direct no-op — SC-4; hardened/production fail-closed),
// classifies via the 35-01 substrate, records a read-only decision-fact (D-01e), and
// routes a mutating GateRecommended call to approve-by-responder (D-03). Enforcement
// (Strict()/GateRecommended) stays separate from classification (classify), per D-02d.
package gateway

import (
	"context"
	"encoding/json"
	"log/slog"
	"time"

	"github.com/chetto1983/aura/internal/agent/tools"
	"github.com/chetto1983/aura/internal/scoring"
	"github.com/chetto1983/aura/internal/toolinvocations"
)

// Decide is the policy-enforcement point. It returns a Verdict; on Approve the Verdict
// carries an ApprovalRequest tool result (the caller returns it as a normal tool result,
// tool.Execute withheld) — the gateway no longer mints a pause sentinel. A nil *Gateway
// and any non-Strict profile short-circuit to Allow with NO store write — the
// dev/local_trusted host-direct no-op preserving today's full-host experience (SC-4).
//
// The mutating-Allow outcomes (the not-GateRecommended auto-allow branch here AND
// routeApprove's post-resume Verdict{Allow, OperatorID}) converge on ONE synchronous
// store.Reserve call (reserve.go) before Allow is returned — exactly one durable
// reservation start per executed call (GATE-03/04, D-03 point 2).
func (g *Gateway) Decide(ctx context.Context, spec tools.Spec, rawArgs json.RawMessage, key ReservationKey) (Verdict, error) {
	if g == nil || !g.profile.Strict() {
		return Verdict{Decision: Allow, Reason: "no-op (dev/local_trusted)"}, nil
	}
	tier := classify(spec, rawArgs)
	if !spec.Mutating {
		// D-01e: a read-only tool call under a strict profile is a recorded decision-fact
		// ONLY — no reserve→execute→append machinery. The fact is a start row (see
		// recordDecisionFact), never an end row.
		g.recordDecisionFact(ctx, spec, key, tier)
		return Verdict{Decision: Allow, Tier: tier}, nil
	}
	// Mutating funnel. A GateRecommended call is routed to approve FIRST; a non-Allow
	// outcome (Deny / Approve with an ApprovalRequest) returns immediately. Both the
	// not-GateRecommended auto-allow AND routeApprove's post-resume Verdict{Allow,
	// OperatorID} then converge on the SAME single reserve call — exactly ONE reservation
	// start row per executed call, the operator_id (when present) folded into that one
	// start's Meta (GATE-03/04 / D-03 point 2). This is the unified reserve invariant.
	operatorID := ""
	if scoring.GateRecommended(tier) {
		v, err := g.routeApprove(ctx, spec, tier, rawArgs, key)
		if err != nil || v.Decision != Allow {
			return v, err
		}
		operatorID = v.OperatorID
	}
	operationVerdict, proceed := g.beginOperation(ctx, spec, rawArgs, key, tier)
	if !proceed {
		return operationVerdict, nil
	}
	verdict, err := g.reserve(ctx, spec, rawArgs, key, tier, operatorID)
	verdict.OperationDecision = operationVerdict.OperationDecision
	return verdict, err
}

// recordDecisionFact durably records an Allow decision-fact as a `start` row (verdict
// in Event.Meta). It is deliberately a START row, NEVER an end row: the call still
// executes, so an `end` decision-fact would pre-claim the (conv,req,toolCall,'end') slot
// and the async observer's real outcome write would be silently lost to
// ON CONFLICT DO NOTHING. A `start` decision-fact races harmlessly against the
// observer's own `start` (whichever lands first wins; the other is a rows==0 no-op) and
// never blocks the real `end` (D-01e). A store or key failure is a WARN, never a hard
// error — the Allow still stands; the fact is observability, and the call is safe to run.
func (g *Gateway) recordDecisionFact(ctx context.Context, spec tools.Spec, key ReservationKey, tier scoring.RiskTier) {
	if g.store == nil {
		return
	}
	ev := toolinvocations.Event{
		ConversationID: key.ConversationID,
		RequestID:      key.RequestID,
		ToolCallID:     key.ToolCallID,
		ToolName:       spec.Name,
		Event:          toolinvocations.EventStart,
		StartedAt:      time.Now().UTC(),
		Meta: map[string]any{
			"gateway_verdict": string(Allow),
			"gateway_tier":    string(tier),
			"decision_fact":   true,
		},
	}
	if err := g.store.Insert(ctx, ev); err != nil {
		slog.Warn("gateway: decision-fact insert failed",
			"tool", spec.Name, "conversation_id", key.ConversationID, "err", err)
	}
}
