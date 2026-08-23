// approve.go holds the mutating-approve routing (D-03): responder-presence detection,
// the gateway_approval ResumeContext builder, the DENY-branch terminal decision-fact
// recorder, and the post-resume approved branch. approve is host/policy-side ONLY — no
// tool schema exposes it (D-03c), so the model can never self-approve a gated action.
package gateway

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/chetto1983/aura/internal/agent/tools"
	"github.com/chetto1983/aura/internal/config"
	"github.com/chetto1983/aura/internal/identityctx"
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

// routeApprove routes a mutating GateRecommended call by responder presence (D-03),
// mirroring shell_exec's requireShellApproval but hoisted ABOVE tool.Execute.
//
//   - A same-ctx resume carrying the operator's resolution → Verdict{Allow, OperatorID},
//     NO row (the executed marker rides 35-04's single reservation start Meta — the
//     in-process unit-test seam / same-Turn fast path). It stays at the very TOP, before
//     the deny gate, so a host-set same-ctx resolution short-circuits regardless of profile.
//   - server_production OR no positively-known responder → deny-with-guidance, plus a
//     durable degraded_deny(reason=no_approver) terminal `end` decision-fact (D-03a/b,
//     D-03 point 1 / GATE-01). Evaluated BEFORE any cross-turn Consume (WR-01
//     deny-before-Consume): a recorded/fabricated approval can NEVER be consumed by a
//     headless/production Decide. It NEVER pauses in place.
//   - A CROSS-TURN ledger hit (the production carrier: newGatewayResumeHook recorded the
//     operator's accept on a prior Turn) → Verdict{Allow, OperatorID} via a one-shot
//     Consume keyed on the re-computed args fingerprint. Reachable only under hardened +
//     a live responder now. NO Insert here (D-03 point 2).
//   - single_user_hardened + a live responder + no ledger hit → record a server-side
//     Challenge (the gateway-generated question, keyed on the AUTHENTICATED triple) and
//     return Verdict{Approve, ApprovalRequest} — the shell_exec-style approval-required
//     tool RESULT (no pause sentinel). The resume hook records the operator's accept ONLY
//     IF this challenge exists AND the operator-visible question matches it (CR-01).
func (g *Gateway) routeApprove(ctx context.Context, spec tools.Spec, tier scoring.RiskTier, rawArgs json.RawMessage, key ReservationKey) (Verdict, error) {
	if r, ok := resolvedApproval(ctx); ok && r.Approved {
		// Same-ctx fast path (unit seam): the operator's resolution was placed on THIS ctx.
		// Return Allow+OperatorID and write NOTHING — a bare executed(operator_id) Insert
		// would compete for the (conv,req,toolCall,event_kind) slot the real reservation
		// start/end needs, discarding the tool's outcome and blinding the 35-05 reconciler.
		return Verdict{Decision: Allow, Tier: tier, OperatorID: r.OperatorID}, nil
	}
	// WR-01 deny-before-Consume: the production/headless hard-deny is evaluated BEFORE the
	// cross-turn Consume, so a recorded (or CR-01-fabricated) approval can NEVER be consumed
	// on a headless/production run. D-03b: production identity is unverified pre-Phase-36, so
	// it is never interactive; D-03a: a headless run has no positively-known responder → DENY.
	if g.profile == config.ProfileServerProduction || !responderPresent(ctx) {
		g.recordDegradedDeny(ctx, spec, key, tier)
		return Verdict{Decision: Deny, Tier: tier, Reason: "no interactive approver — action declined"}, nil
	}
	// Standing grants (amendment #127) come FIRST, after the hard-deny and before the
	// one-shot ledger: an operator who said "for this conversation" or "always" already
	// answered this question, and reading a standing grant — unlike Consume — does not spend
	// it. Both are keyed on the SUBJECT (tool + multiplexed action), never on the argument
	// fingerprint, so a second delete of a DIFFERENT event is covered while a grant on
	// "calendar delete_event" still leaves "calendar send_email" to be asked.
	subject := subjectFor(spec, rawArgs)
	if r, ok := g.approvals.SessionGrant(key.ConversationID, subject); ok && r.Approved {
		return Verdict{Decision: Allow, Tier: tier, OperatorID: r.OperatorID, Scope: ScopeSession}, nil
	}
	if g.alwaysGranted(ctx, identityctx.IdentityID(ctx), subject) {
		return Verdict{Decision: Allow, Tier: tier, OperatorID: "local", Scope: ScopeAlways}, nil
	}
	// Cross-turn ledger re-entry (D-03 point 2, the production carrier): reachable only under
	// hardened + a live responder now. The operator resolved the relayed ask_user on a PRIOR
	// Turn and newGatewayResumeHook recorded the ResolvedApproval (challenge + question gated).
	// The resumed re-emit Consumes the one-shot approval keyed on the re-computed args
	// fingerprint (a tampered re-emit → different fingerprint → no hit → fail-closed) and
	// returns Allow+OperatorID so decide.go's funnel takes the SINGLE 35-04 reservation with
	// operator_id in Meta — never a competing Insert.
	fp := gatewayArgsFingerprint(rawArgs)
	if r, ok := g.approvals.Consume(key.ConversationID, spec.Name, fp); ok && r.Approved {
		return Verdict{Decision: Allow, Tier: tier, OperatorID: r.OperatorID}, nil
	}
	// PITFALLS §4 / 45.1 _meta cutover: gatewayArgsFingerprint hashes the MODEL's
	// own arguments, upstream of any host injection. Dropping a field from a
	// tool's schema (as the _meta identity cutover does) changes what the model
	// emits, so an approval withheld before a deploy and resumed after it can miss
	// this Consume under a shifted fingerprint — fail-closed but otherwise silent.
	// Logged on EVERY miss, not just this one cutover, because this failure class
	// is reachable whenever any tool's schema changes: n==0 means no challenge was
	// ever issued for this tool in this conversation; n>0 means one IS pending,
	// under a different fingerprint than this Consume attempt. Never logs rawArgs
	// — the fingerprint is the safe correlate and the arguments may carry memory
	// content.
	pendingCount := g.approvals.PendingCountForTool(key.ConversationID, spec.Name)
	missReason := "args_fingerprint_changed"
	if pendingCount == 0 {
		missReason = "no_pending_challenge"
	}
	slog.Info("gateway approval ledger miss",
		"conversation_id", key.ConversationID,
		"tool", spec.Name,
		"args_fingerprint", fp,
		"pending_count", pendingCount,
		"reason", missReason,
	)
	// single_user_hardened + a live responder + no ledger hit → record the server-side
	// challenge (the gateway-generated question keyed on the AUTHENTICATED triple) THEN
	// return the shell_exec-style approval-required tool RESULT (mirroring
	// shellApprovalRequiredResult). execTool returns it as a NORMAL result (no error,
	// tool.Execute withheld); the model relays it via ask_user (llm_agent_pause.go UNCHANGED)
	// and retries the exact call after the operator accepts. The single fp + question are
	// computed ONCE here and threaded into Challenge + the result (IN-02). The resume hook's
	// ApproveChallenge requires this challenge AND a matching operator-visible question before
	// recording — the informed-consent binding a benign relayed question cannot satisfy (CR-01).
	question := gatewayApprovalQuestion(spec, tier, rawArgs)
	g.approvals.Challenge(key.ConversationID, spec.Name, fp, question, subject)
	result := gatewayApprovalRequiredResult(spec, tier, key, fp, question, subject)
	return Verdict{Decision: Approve, Tier: tier, ApprovalRequest: &result}, nil
}

// gatewayApprovalRequiredResult builds the byte-for-byte analog of
// shellApprovalRequiredResult: a JSON payload (Preview) instructing the model to relay
// the approval via ask_user. It carries error="gateway_approval_required", the tool, the
// tier, args_sha256 (the fingerprint the resume must reproduce), a descriptive question,
// the three server-generated scope options (amendment #127 — the model relays them, it does
// not author them, and the resume hook re-resolves the answer against the challenge's own
// copy, so a reworded label grants nothing wider), the exact resume_context object
// (including args_sha256), and a message telling the model to call ask_user with
// kind="approval", the same question, the same options, the tier-ordered priority, and
// the same resume_context, then "retry the exact call only after the user accepts." The fp
// and question are computed ONCE by routeApprove and threaded in (IN-02: a single
// gatewayArgsFingerprint call site, and the SAME question recorded as the challenge).
func gatewayApprovalRequiredResult(
	spec tools.Spec, tier scoring.RiskTier, key ReservationKey, fp, question string, subject grantSubject,
) tools.ToolResult {
	resumeContext := gatewayApprovalContext(spec, tier, key, fp)
	payload := map[string]any{
		"error":          "gateway_approval_required",
		"tool":           spec.Name,
		"tier":           string(tier),
		"args_sha256":    fp,
		"question":       question,
		"options":        scopeOptions(subject),
		"resume_context": resumeContext,
		"message": "This mutating action requires operator approval and has been WITHHELD. " +
			"Call ask_user with kind=\"approval\", question exactly equal to the question field, " +
			"options exactly equal to the options field (copy all three verbatim — do not reword, " +
			"reorder or drop any), priority=" + strconv.Itoa(tools.ApprovalPriority(tier)) +
			", and resume_context exactly equal to the resume_context field. " +
			"Retry the exact call only after the user accepts.",
	}
	raw, err := json.Marshal(payload)
	if err != nil {
		raw = []byte(`{"error":"gateway_approval_required"}`)
	}
	return tools.ToolResult{Preview: string(raw), Bytes: len(raw)}
}

// gatewayApprovalQuestion renders a descriptive, secret-safe approval prompt (mirroring
// shellApprovalQuestion) the model copies verbatim into ask_user's question so
// approvals_api.go's handleListApprovals renders a meaningful (non-blank) pending. It
// surfaces the tool name + tier + the SORTED top-level argument KEYS (never their values),
// so a secret-bearing argument value is never leaked into the question or the pending list.
func gatewayApprovalQuestion(spec tools.Spec, tier scoring.RiskTier, rawArgs json.RawMessage) string {
	return fmt.Sprintf(
		"Approve %s (risk=%s)?\nThis mutating action is WITHHELD until you accept.\nargs: %s",
		spec.Name, tier, argKeySummary(rawArgs))
}

// argKeySummary renders the sorted top-level argument keys (NOT values — secret-safe) so
// the operator sees the shape of the withheld action without any secret value surfacing.
func argKeySummary(rawArgs json.RawMessage) string {
	var obj map[string]json.RawMessage
	if err := json.Unmarshal(rawArgs, &obj); err != nil || len(obj) == 0 {
		return "(none)"
	}
	keys := make([]string, 0, len(obj))
	for k := range obj {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return strings.Join(keys, ", ")
}

// gatewayApprovalContext builds the {"type":"gateway_approval",...} ResumeContext the
// host-side approval handler reads. It carries NO secret — only the tool + tier + the
// originating-conversation-keyed triple + args_sha256 (the fingerprint the resume hook
// records and routeApprove Consumes), so a resume can re-enter Decide for THIS exact call.
// fp is the already-computed fingerprint threaded from routeApprove (IN-02: no recompute).
func gatewayApprovalContext(spec tools.Spec, tier scoring.RiskTier, key ReservationKey, fp string) json.RawMessage {
	b, err := json.Marshal(map[string]any{
		"type":            gatewayApprovalType,
		"tool":            spec.Name,
		"tier":            string(tier),
		"conversation_id": key.ConversationID,
		"request_id":      key.RequestID,
		"tool_call_id":    key.ToolCallID,
		"args_sha256":     fp,
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
