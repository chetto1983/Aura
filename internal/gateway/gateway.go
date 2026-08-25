// gateway.go holds the Gateway PEP's core types: the struct + constructor, the
// narrow reservationStore ledger seam, and the Decision/Verdict/ReservationKey/
// ErrDenied vocabulary. The enforcement logic lives in decide.go (the PEP) and
// approve.go (responder-presence routing). This file is DB-free beyond the store
// seam; classification is delegated to classify.go (35-01), enforcement to the
// profile branch in decide.go — kept separate per D-02d.
package gateway

import (
	"context"
	"fmt"

	"github.com/chetto1983/aura/internal/agent/tools"
	"github.com/chetto1983/aura/internal/config"
	"github.com/chetto1983/aura/internal/idempotency"
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

// operationRegistry is the shared public-operation state machine. Only an
// acquired decision authorizes policy reservation or a mutating effect.
type operationRegistry interface {
	Begin(context.Context, idempotency.BeginRequest) (idempotency.BeginDecision, error)
	Complete(context.Context, idempotency.CompleteRequest) error
	MarkIndeterminate(
		context.Context,
		idempotency.OperationKey,
		[32]byte,
		idempotency.ClaimToken,
	) error
}

type rejectionRegistry interface {
	MarkRejected(context.Context, idempotency.RejectRequest) error
}

// Decision is the gateway's verdict on a single tool dispatch.
type Decision string

const (
	// Allow lets the dispatch proceed to tool.Execute (a host-direct no-op under the
	// dev profiles, or a recorded decision-fact under the strict profiles).
	Allow Decision = "allow"
	// Deny blocks the dispatch: execTool returns *ErrDenied instead of executing.
	Deny Decision = "deny"
	// Approve withholds the mutating dispatch pending an interactive operator decision;
	// the gateway pairs it with Verdict.ApprovalRequest — a normal tool RESULT (mirroring
	// shell_exec) the caller returns WITHOUT calling tool.Execute. It is NOT a pause
	// sentinel: the gateway no longer mints a pause (D-03 point 2); the model relays the
	// request via ask_user and retries the exact call after the operator accepts.
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
	// Scope is set on an Allow that a STANDING grant produced (amendment #127) — a
	// ScopeSession or ScopeAlways the operator granted on an earlier accept. It is empty on
	// every other Allow, including the one-shot approved re-drive, whose authorization was
	// spent on this call and left nothing standing. It rides the reservation start's Meta so
	// the audit trail says which grant let a destructive call through without asking.
	Scope ApprovalScope
	// Replay is non-nil ONLY when Reserve found the (conv,req,toolCall) slot already
	// held (rows==0): it carries the recorded outcome so execTool returns it WITHOUT
	// re-invoking tool.Execute (GATE-04 idempotency — a duplicate/retried mutating call,
	// approved or auto-allowed, applies its side effect exactly once).
	Replay *tools.ToolResult
	// ApprovalRequest is non-nil ONLY when Decision==Approve: it is the shell_exec-style
	// approval-required tool RESULT (mirroring shellApprovalRequiredResult) that execTool
	// returns as a NORMAL result (no error, tool.Execute withheld). Its Preview instructs
	// the model to relay the request via ask_user (kind=approval, resume_context carrying
	// args_sha256) and to retry the exact call after the operator accepts — keeping the
	// REAL tool name + args in persisted history so the resume re-emits an args-matching
	// call (D-03 point 2; the round-trip the pre-dispatch intercept could not achieve).
	ApprovalRequest *tools.ToolResult
	// OperationDecision records the shared registry outcome. Acquired tells the
	// executor it owns completion/indeterminate transition responsibility.
	OperationDecision idempotency.Decision
	// OperationClaimToken fences terminal writes to the acquired generation.
	OperationClaimToken idempotency.ClaimToken
	// OperationRejection is non-nil when a prior deterministic no-effect
	// failure is replayed. It is returned as an error, never as ToolResult.
	OperationRejection *idempotency.ReplayResult
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

// ErrOperationRejected is the replay of a terminal domain rejection.
type ErrOperationRejected struct {
	Reason string
}

func (e *ErrOperationRejected) Error() string {
	if e == nil || e.Reason == "" {
		return "operation was rejected"
	}
	return "operation was rejected: " + e.Reason
}

// Error renders the denial as a control-plane string the model sees as a tool error.
func (e *ErrDenied) Error() string {
	if e.Reason == "" {
		return "tool dispatch denied by policy"
	}
	return "tool dispatch denied by policy: " + e.Reason
}

// Gateway is the single in-process policy-enforcement point (GATE-01). It holds the
// resolved runtime profile, the append-only ledger seam, and the cross-turn approval
// ledger; the agent stays DB-free by delegating to it (mirroring how LlmAgent holds
// *HookManager). A nil *Gateway is an Allow no-op — dev-parity for tests/standalone
// construction.
type Gateway struct {
	profile    config.RuntimeProfile
	store      reservationStore
	operations operationRegistry
	approvals  *GatewayApprovals // cross-turn carrier for an operator's ResolvedApproval (D-03 point 2)
	grants     grantStore        // durable ScopeAlways grants (amendment #127); nil = the two in-memory scopes only
}

// New builds a Gateway over the resolved runtime profile and the append-only tool
// invocation ledger. The composition root constructs exactly one and injects it at
// the three NewLlmAgent roots (runner, swarm, cron). It always owns a fresh
// GatewayApprovals so the resume hook has a session ledger to write through.
func New(profile config.RuntimeProfile, store reservationStore) *Gateway {
	return &Gateway{profile: profile, store: store, approvals: NewGatewayApprovals()}
}

// OwnsToolStartRows reports whether this gateway writes the ledger's `start` row for a tool
// call, which it does exactly when it reserves — i.e. under a strict profile.
//
// The ledger observer MUST NOT write that row when this is true. Reserve INSERTs the start
// under the UNIQUE (conversation_id, request_id, tool_call_id, event_kind) index with ON
// CONFLICT DO NOTHING and reads the rows-affected count AS the idempotency key (GATE-03/04),
// so a second writer destroys the key: the gateway reads its own conflict as "someone already
// holds this slot" and refuses to run the tool. Exposed here rather than passed as a separate
// flag so the fact cannot drift from the behaviour it describes.
func (g *Gateway) OwnsToolStartRows() bool {
	return g != nil && g.store != nil && g.profile.Strict()
}

// SetOperationRegistry wires the one process-wide registry constructed by the
// composition root. Nil preserves legacy standalone/dev behavior.
func (g *Gateway) SetOperationRegistry(registry operationRegistry) {
	if g != nil {
		g.operations = registry
	}
}

// RecordResolvedApproval is a LOW-LEVEL test-seed seam that records a resolved approval
// straight into the cross-turn ledger, keyed on (convID, toolName, argsFingerprint) —
// the parity analog of ShellApprovals.Approve. It bypasses the challenge/question binding,
// so it is NOT the faithful production writer any more: the host-side newGatewayResumeHook
// records through the challenge-gated ApproveChallenge (below). A nil Gateway is a no-op
// (dev-parity); it REFUSES under ProfileServerProduction (WR-01 defense-in-depth: a
// production run records no approval by any path); GatewayApprovals is nil-receiver-safe.
func (g *Gateway) RecordResolvedApproval(convID, toolName, argsFingerprint string, r ResolvedApproval) {
	if g == nil || g.profile == config.ProfileServerProduction {
		return
	}
	g.approvals.Approve(convID, toolName, argsFingerprint, r)
}

// ApproveChallenge is the host-side, challenge-gated recorder newGatewayResumeHook calls to
// record an operator's accept: it REFUSES under ProfileServerProduction (WR-01
// defense-in-depth — a production run records no approval by any path) then delegates to
// GatewayApprovals.ApproveChallenge, which moves pending→approved ONLY when routeApprove
// previously issued a challenge for (convID, toolName, argsFingerprint) AND the
// operator-visible question matches the gateway-generated one (CR-01 informed-consent). It
// is the faithful production analog of ShellApprovals.ApproveChallenge; the model relaying
// via ask_user does NOT grant approval (D-03c).
//
// It then acts on the scope the operator chose (amendment #127) and returns the scope that
// actually took effect, which is NOT always the one they picked: a ScopeAlways with no
// durable store or no authenticated identity degrades to ScopeSession. The returned scope is
// what the caller may report; the requested one is not. A nil Gateway is a no-op returning
// ScopeOnce and nil.
func (g *Gateway) ApproveChallenge(ctx context.Context, a ApprovalAccept) (ApprovalScope, error) {
	if g == nil {
		return ScopeOnce, nil
	}
	if g.profile == config.ProfileServerProduction {
		return ScopeOnce, fmt.Errorf("gateway approval refused under server_production")
	}
	resolved := ResolvedApproval{Approved: true, OperatorID: a.OperatorID}
	scope, subject, err := g.approvals.ApproveChallenge(
		a.ConversationID, a.Tool, a.ArgsFingerprint, a.Question, a.Answer, resolved)
	if err != nil {
		return ScopeOnce, err
	}
	switch scope {
	case ScopeSession:
		g.approvals.GrantSession(a.ConversationID, subject, resolved)
	case ScopeAlways:
		return g.recordAlwaysGrant(ctx, a, subject), nil
	}
	return scope, nil
}

// DiscardApprovalChallenge removes one unresolved challenge after its durable pause
// expired. It never creates a grant and is nil-safe like the rest of the ledger surface.
func (g *Gateway) DiscardApprovalChallenge(convID, toolName, argsFingerprint string) {
	if g == nil {
		return
	}
	g.approvals.DiscardChallenge(convID, toolName, argsFingerprint)
}

// EvictSession drops a conversation's resolved-but-unconsumed approvals (R-41 parity
// with ShellApprovals.Evict) so a long-running serve daemon does not retain them across
// conversations. The gateway ledger lives OUTSIDE the tool registry, so the runner's
// registry-ranging SessionEvictor loop cannot reach it — this explicit call is required.
// A nil Gateway is a no-op.
func (g *Gateway) EvictSession(convID string) {
	if g == nil {
		return
	}
	g.approvals.Evict(convID)
}
