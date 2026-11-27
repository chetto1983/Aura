// decide.go is the PEP proper (GATE-01): Decide is interposed inside execTool above
// tool.Execute at all three composition roots. It branches on the runtime profile
// (dev/local_trusted no-op — SC-4; hardened/production fail-closed),
// classifies via the 35-01 substrate, records a read-only decision-fact (D-01e), and
// routes a mutating GateRecommended call to approve-by-responder (D-03). Enforcement
// (Strict()/GateRecommended) stays separate from classification (classify), per D-02d.
package gateway

import (
	"context"
	"encoding/json"
	"log/slog"

	"github.com/chetto1983/aura/internal/agent/tools"
	"github.com/chetto1983/aura/internal/scoring"
	"github.com/chetto1983/aura/internal/toolinvocations"
)

// gated reports whether a tier stops the turn for operator approval. ONLY Destructive
// does — the tier that means "this erases something that does not come back".
//
// It used to be scoring.GateRecommended, i.e. Risky OR Destructive, and Risky is where
// everything ordinary landed: authoring a skill, running a scheduled task the operator
// just asked for, spawning a swarm, any multiplexed action the classifier could not parse.
// The operator on the live appliance was answering approval prompts for writes that were
// the entire point of the request. A prompt that fires on the expected case is not a
// safety property — it trains the answer "yes" and is then still there, unread, when the
// one call that mattered arrives.
//
// Everything below Destructive is still classified, reserved, recorded and auditable;
// scoring.GateRecommended still reports the advisory tier for the skills audit trail and
// the governance UI. What changed is only whether the turn STOPS. shell_exec keeps its own
// destructive-pattern approval in internal/agent/tools, which this predicate never sees.
func gated(t scoring.RiskTier) bool { return t == scoring.Destructive }

// personActions are the verbs that never stop a conversation and never run without one. A tier
// cannot say this: Destructive would prompt the person who just asked, and everything below it
// runs headless. The check reads only the responder, not the profile, because no approval is
// being asked for: routeApprove's production clause is about trusting an approver.
var personActions = map[grantSubject]bool{
	{Tool: "mail_watch", Action: "add_rule"}: true,
	{Tool: "mail_watch", Action: "correct"}:  true,
}

func needsPerson(subject grantSubject) bool { return personActions[subject] }

// personPresent reports whether the person is in the conversation this call belongs to. A swarm
// worker spawned from a live turn inherits the turn's responder marker and has nobody on it.
func personPresent(ctx context.Context) bool {
	return responderPresent(ctx) && !tools.IsDelegatedDispatch(ctx)
}

// needsPersonReason is what a headless run's model reads, in the shape of policyDenyReason.
func needsPersonReason(subject grantSubject) string {
	return "tool \"" + subject.String() + "\" needs a person in the conversation and this run has none: do not retry it, tell the operator what you would change"
}

// Decide is the policy-enforcement point. It returns a Verdict; on Approve the Verdict
// carries an ApprovalRequest tool result (the caller returns it as a normal tool result,
// tool.Execute withheld) — the gateway no longer mints a pause sentinel. A nil *Gateway
// and any non-Strict profile short-circuit to Allow with NO store write — the
// dev/local_trusted no-op (SC-4).
//
// What the call needs is read by needFor, the half Review shares; Decide adds the writes.
// The identity's policy outranks everything below it (prd.md §5, 2026-10-09): a deny refuses
// even a read-only call, and an ask turns any call into a reserved, approved one.
//
// Every mutating Allow — an ungated write, an operator's accept, a standing grant, or
// routeApprove's ledger re-entry — converges on ONE synchronous store.Reserve call
// (reserve.go): exactly one durable reservation start per executed call, the operator and
// scope (when present) folded into that one start's Meta (GATE-03/04, D-03 point 2).
func (g *Gateway) Decide(ctx context.Context, spec tools.Spec, rawArgs json.RawMessage, key ReservationKey) (Verdict, error) {
	if verdict, handled := g.decideReviewedMessage(ctx, spec, rawArgs, key); handled {
		return verdict, nil
	}
	if g == nil || !g.profile.Strict() {
		return Verdict{Decision: Allow, Reason: "no-op (dev/local_trusted)"}, nil
	}
	n := g.needFor(ctx, spec, rawArgs, key)
	spec, tier := n.spec, n.tier
	switch n.kind {
	case needDeniedByPolicy:
		reason := policyDenyReason(n.subject)
		g.recordDenyFact(ctx, spec, key, tier, reason, map[string]any{"gateway_policy": string(PolicyDeny), "reason": "policy"})
		return Verdict{Decision: Deny, Tier: tier, Reason: reason, Refusal: true}, nil
	case needPerson:
		reason := needsPersonReason(n.subject)
		g.recordDenyFact(ctx, spec, key, tier, reason, map[string]any{"reason": "needs_person"})
		return Verdict{Decision: Deny, Tier: tier, Reason: reason, Refusal: true}, nil
	case needNoApprover:
		// D-03a/b: production identity is never interactive, and a run with nobody on it
		// has no one to ask. It never pauses in place.
		g.recordDenyFact(ctx, spec, key, tier, "gateway degraded_deny: no interactive approver",
			map[string]any{"degraded_deny": true, "reason": "no_approver"})
		return Verdict{Decision: Deny, Tier: tier, Reason: "no interactive approver — action declined", Refusal: true}, nil
	case needApproval:
		v, err := g.routeApprove(ctx, spec, tier, rawArgs, key, n.subject, n.policy)
		if err != nil || v.Decision != Allow {
			return v, err
		}
		n.operatorID, n.scope = v.OperatorID, v.Scope
	case needNone:
		if !spec.Mutating {
			// D-01e: a read-only tool call under a strict profile is a recorded decision-fact
			// ONLY — no reserve→execute→append machinery. The fact is a start row (see
			// recordDecisionFact), never an end row.
			g.recordDecisionFact(ctx, spec, rawArgs, key, tier)
			return Verdict{Decision: Allow, Tier: tier}, nil
		}
	}
	operationVerdict, proceed := g.beginOperation(ctx, spec, rawArgs, key, tier)
	if !proceed {
		return operationVerdict, nil
	}
	verdict := g.reserve(ctx, spec, rawArgs, key, tier, n.operatorID, n.scope, n.policy)
	verdict.OperationDecision = operationVerdict.OperationDecision
	verdict.OperationClaimToken = operationVerdict.OperationClaimToken
	return verdict, nil
}

// recordDecisionFact durably records an Allow decision-fact as a `start` row (verdict
// in Event.Meta). It is deliberately a START row, NEVER an end row: the call still
// executes, so an `end` decision-fact would pre-claim the (conv,req,toolCall,'end') slot
// and the async observer's real outcome write would be silently lost to
// ON CONFLICT DO NOTHING. A `start` decision-fact races harmlessly against the
// observer's own `start` (whichever lands first wins; the other is a rows==0 no-op) and
// never blocks the real `end` (D-01e). A store or key failure is a WARN, never a hard
// error — the Allow still stands; the fact is observability, and the call is safe to run.
func (g *Gateway) recordDecisionFact(ctx context.Context, spec tools.Spec, rawArgs json.RawMessage, key ReservationKey, tier scoring.RiskTier) {
	if g.store == nil {
		return
	}
	ev := toolinvocations.Event{
		ConversationID: key.ConversationID,
		RequestID:      key.RequestID,
		ToolCallID:     key.ToolCallID,
		ToolName:       spec.Name,
		Event:          toolinvocations.EventStart,
		StartedAt:      dispatchStartedAt(ctx),
		Arguments:      string(rawArgs),
		ArgsBytes:      len(rawArgs),
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
