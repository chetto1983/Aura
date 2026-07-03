// gateway.go holds the Gateway PEP's core types: the struct + constructor, the
// narrow reservationStore ledger seam, and the Decision/Verdict/ReservationKey/
// ErrDenied vocabulary. The enforcement logic lives in decide.go (the PEP) and
// approve.go (responder-presence routing). This file is DB-free beyond the store
// seam; classification is delegated to classify.go (35-01), enforcement to the
// profile branch in decide.go — kept separate per D-02d.
package gateway

import (
	"context"

	"github.com/chetto1983/aura/internal/agent/tools"
	"github.com/chetto1983/aura/internal/config"
	"github.com/chetto1983/aura/internal/scoring"
	"github.com/chetto1983/aura/internal/toolinvocations"
)

// reservationStore is the narrow append-only ledger seam the Gateway records
// decision-facts and reservations through. *toolinvocations.Store satisfies it.
//   - Insert: the read-only decision-fact and the degraded_deny terminal fact.
//   - Reserve: the synchronous fatal-on-failure pre-execution reservation whose
//     rows-affected count is the GATE-04 idempotency key (rows==1 acquire / rows==0
//     replay / err deny).
//   - GetEnd: the replay fetch for a rows==0 duplicate (Reserve already calls it, but
//     the seam exposes it so the reconciler/tests can query an end fact directly).
type reservationStore interface {
	Insert(ctx context.Context, e toolinvocations.Event) error
	Reserve(ctx context.Context, start toolinvocations.Event) (acquired bool, replay *toolinvocations.Event, err error)
	GetEnd(ctx context.Context, conversationID, requestID, toolCallID string) (*toolinvocations.Event, error)
}

// Decision is the gateway's verdict on a single tool dispatch.
type Decision string

const (
	// Allow lets the dispatch proceed to tool.Execute (a host-direct no-op under the
	// dev profiles, or a recorded decision-fact under the strict profiles).
	Allow Decision = "allow"
	// Deny blocks the dispatch: execTool returns *ErrDenied instead of executing.
	Deny Decision = "deny"
	// Approve suspends the dispatch pending an interactive operator decision; the
	// gateway pairs it with a *tools.ErrAwaitingUserInput pause sentinel.
	Approve Decision = "approve"
)

// Verdict carries the Decide outcome. OperatorID is set ONLY on the post-resume
// approved Allow (routeApprove's resolved branch): plan 35-04 folds it into the
// SINGLE reservation start Meta, so an operator-approved call is recorded exactly
// once by its reservation — never by a second competing ledger write (D-03 point 2).
type Verdict struct {
	Decision   Decision
	Tier       scoring.RiskTier
	Reason     string
	OperatorID string
	// Replay is non-nil ONLY when Reserve found the (conv,req,toolCall) slot already
	// held (rows==0): it carries the recorded outcome so execTool returns it WITHOUT
	// re-invoking tool.Execute (GATE-04 idempotency — a duplicate/retried mutating call,
	// approved or auto-allowed, applies its side effect exactly once).
	Replay *tools.ToolResult
}

// ReservationKey is the ledger triple the decision-fact is keyed on. It is ALWAYS
// the ORIGINATING conversation UUID (runner convID / swarm RunConfig.ConvID / cron
// OriginConversationID) + request_id + tool_call_id — a headless flat session
// (`<conv>-swarm-w<i>`, `agent_job:<runID>`) never keys the ledger (Open Q1 full
// enforcement).
type ReservationKey struct {
	ConversationID string
	RequestID      string
	ToolCallID     string
}

// ErrDenied is the typed error execTool returns when the gateway denies a dispatch.
// The mutating tool is NOT executed — Decide returns before tool.Execute.
type ErrDenied struct {
	Reason string
	Tier   scoring.RiskTier
}

// Error renders the denial as a control-plane string the model sees as a tool error.
func (e *ErrDenied) Error() string {
	if e.Reason == "" {
		return "tool dispatch denied by policy"
	}
	return "tool dispatch denied by policy: " + e.Reason
}

// Gateway is the single in-process policy-enforcement point (GATE-01). It holds the
// resolved runtime profile + the append-only ledger seam; the agent stays DB-free by
// delegating to it (mirroring how LlmAgent holds *HookManager). A nil *Gateway is an
// Allow no-op — dev-parity for tests/standalone construction.
type Gateway struct {
	profile config.RuntimeProfile
	store   reservationStore
}

// New builds a Gateway over the resolved runtime profile and the append-only tool
// invocation ledger. The composition root constructs exactly one and injects it at
// the three NewLlmAgent roots (runner, swarm, cron).
func New(profile config.RuntimeProfile, store reservationStore) *Gateway {
	return &Gateway{profile: profile, store: store}
}
