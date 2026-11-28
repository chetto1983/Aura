// approve.go holds the mutating-approve routing (D-03): responder-presence detection,
// the gateway_approval ResumeContext builder, the DENY-branch terminal decision-fact
// recorder, and the cross-turn approved branch. approve is host/policy-side ONLY — no
// tool schema exposes it (D-03c), so the model can never self-approve a gated action.
package gateway

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"maps"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/chetto1983/aura/internal/agent/tools"
	"github.com/chetto1983/aura/internal/redact"
	"github.com/chetto1983/aura/internal/scoring"
	"github.com/chetto1983/aura/internal/toolinvocations"
)

// gatewayApprovalType is the ResumeContext discriminator for a gateway-minted approval,
// distinct from ask_user's own skill_approval type so a host-side handler can route it.
const gatewayApprovalType = "gateway_approval"

// responderCtxKey marks a ctx as an interactive session with a positively-known human
// responder (a live CLI REPL / cockpit SSE subscriber / live Telegram chat). A cron run and a
// background delegation never carry it, so responderPresent defaults to false → DENY (D-03a).
// A synchronous swarm worker inherits it from the turn that spawned it: see personPresent.
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

// HasResponder reports whether a live owner can review a withheld outbound call.
// Headless agent roots must fail closed instead of creating an unanswerable draft.
func HasResponder(ctx context.Context) bool { return responderPresent(ctx) }

type approvalDescriberCtxKey struct{}

// WithApprovalDescriber carries the dispatched tool's describer to routeApprove, which asks
// it only right before a prompt. The agent's dispatch sets it on every call, nil for a tool
// that describes nothing, so a call dispatched inside another tool's call never borrows the
// outer tool's describer.
func WithApprovalDescriber(ctx context.Context, d tools.ApprovalDescriber) context.Context {
	return context.WithValue(ctx, approvalDescriberCtxKey{}, d)
}

func describerFrom(ctx context.Context) tools.ApprovalDescriber {
	d, _ := ctx.Value(approvalDescriberCtxKey{}).(tools.ApprovalDescriber)
	return d
}

// describeTarget asks the tool what the withheld call acts on and returns the answer through
// approvalLine, ready to keep on the challenge or the card.
func describeTarget(ctx context.Context, d tools.ApprovalDescriber, rawArgs json.RawMessage) (string, error) {
	if d == nil {
		return "", nil
	}
	target, err := d.DescribeForApproval(ctx, rawArgs)
	if err != nil {
		return "", err
	}
	return approvalLine(target), nil
}

// ResolvedApproval is an operator's answer to a gateway_approval pause. The resume hook
// records it in the one-shot cross-turn ledger consumed by the next Decide.
type ResolvedApproval struct {
	Approved   bool
	OperatorID string
}

// routeApprove is the challenge route of a gated call that needFor found nobody had answered
// yet: hardened, a person present, no accept and no standing grant. The hard deny, the
// accept and the grants are needFor's, which runs before it, so a recorded or fabricated
// one-shot approval is never Consumed by a headless or production Decide (WR-01).
//
//   - A CROSS-TURN ledger hit (newGatewayResumeHook recorded the operator's accept on a prior
//     Turn) → Verdict{Allow, OperatorID} via a one-shot Consume keyed on the re-computed args
//     fingerprint. NO Insert here (D-03 point 2).
//   - No ledger hit → ask the tool's describer, if any, then record a server-side Challenge
//     (the gateway-generated question and the described target, keyed on the AUTHENTICATED
//     triple) and return Verdict{Approve, ApprovalRequest} — the shell_exec-style
//     approval-required tool RESULT (no pause sentinel). The resume hook records the
//     operator's accept ONLY IF this challenge exists AND the operator-visible question
//     matches it (CR-01).
func (g *Gateway) routeApprove(
	ctx context.Context, spec tools.Spec, tier scoring.RiskTier, rawArgs json.RawMessage, key ReservationKey,
	subject grantSubject, policy Policy,
) (Verdict, error) {
	// The resumed re-emit Consumes the one-shot approval keyed on the re-computed args
	// fingerprint (a tampered re-emit → different fingerprint → no hit → fail-closed) and
	// returns Allow+OperatorID so decide.go's funnel takes the SINGLE 35-04 reservation with
	// operator_id in Meta — never a competing Insert.
	fp := tools.CallFingerprint(rawArgs)
	if r, ok := g.approvals.Consume(key.ConversationID, spec.Name, fp); ok && r.Approved {
		return Verdict{Decision: Allow, Tier: tier, OperatorID: r.OperatorID}, nil
	}
	// PITFALLS §4 / 45.1 _meta cutover: the fingerprint hashes the MODEL's
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
		"conversation_id", redact.Line(key.ConversationID),
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
	//
	// Only now is the tool asked what the call acts on (spec 2026-10-12): everything above has
	// decided, so a call that never reaches a prompt is never described. A describer's error
	// refuses the call as a failure, not as the gateway's refusal: no deny fact, nothing
	// reserved yet, and execTool reports it as not executed. The target stays on the challenge
	// with the withheld call's id; the question the model relays does not change.
	target, err := describeTarget(ctx, describerFrom(ctx), rawArgs)
	if err != nil {
		return Verdict{Tier: tier}, err
	}
	question := gatewayApprovalQuestion(spec, tier, rawArgs)
	g.approvals.Challenge(key.ConversationID, spec.Name, fp, gatewayChallenge{
		question: question, subject: subject, policy: policy, target: target, toolCallID: key.ToolCallID,
	})
	result := gatewayApprovalRequiredResult(spec, tier, key, fp, question, subject, policy)
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
// tools.CallFingerprint call site, and the SAME question recorded as the challenge).
//
// It is Aura's own text, marked trusted so the model copies it without reading through an
// HTML-escaped envelope (spec 2026-10-12): the registry's tool name, the tier, the
// fingerprint, server-generated options and a question that carries no third-party text. The
// model-chosen parts (argument keys, the action in the option labels) went through
// approvalLine, and json.Marshal's HTML-safe encoding keeps every <, > and & out of the bytes.
func gatewayApprovalRequiredResult(
	spec tools.Spec, tier scoring.RiskTier, key ReservationKey, fp, question string, subject grantSubject, policy Policy,
) tools.ToolResult {
	resumeContext := gatewayApprovalContext(spec, tier, key, fp)
	payload := map[string]any{
		"error":          "gateway_approval_required",
		"tool":           spec.Name,
		"tier":           string(tier),
		"args_sha256":    fp,
		"question":       question,
		"options":        scopeOptions(subject, policy),
		"resume_context": resumeContext,
		"message": "This mutating action requires operator approval and has been WITHHELD. " +
			"Call ask_user with kind=\"approval\", question exactly equal to the question field, " +
			"options exactly equal to the options field (copy every one verbatim — do not reword, " +
			"reorder or drop any), priority=" + strconv.Itoa(tools.ApprovalPriority(tier)) +
			", and resume_context exactly equal to the resume_context field. " +
			"Retry the exact call only after the user accepts.",
	}
	raw, err := json.Marshal(payload)
	if err != nil {
		raw = []byte(`{"error":"gateway_approval_required"}`)
	}
	return tools.ToolResult{
		Preview: string(raw), Bytes: len(raw),
		Provenance: &tools.ToolResultProvenance{Source: "aura:gateway_approval", Trust: tools.TrustTrusted},
	}
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

// maxArgKeySummaryBytes is the approval card's limit on its args parameter
// (approvaltext's maxArgsBytes): a longer summary makes the whole card invalid.
const maxArgKeySummaryBytes = 1024

// argKeySummary renders the sorted top-level argument keys (NOT values — secret-safe) so
// the operator sees the shape of the withheld action without any secret value surfacing.
// A key is the model's own text, so each one is cut to one line by approvalLine. The list is
// cut at a key boundary to fit the card, so a cut never splits a rune.
func argKeySummary(rawArgs json.RawMessage) string {
	var obj map[string]json.RawMessage
	if err := json.Unmarshal(rawArgs, &obj); err != nil || len(obj) == 0 {
		return "(none)"
	}
	keys := make([]string, 0, len(obj))
	for k := range obj {
		keys = append(keys, approvalLine(k))
	}
	sort.Strings(keys)
	if summary := strings.Join(keys, ", "); len(summary) <= maxArgKeySummaryBytes {
		return summary
	}
	const elided = ", …"
	var b strings.Builder
	for _, k := range keys {
		next := k
		if b.Len() > 0 {
			next = ", " + k
		}
		if b.Len()+len(next)+len(elided) > maxArgKeySummaryBytes {
			break
		}
		b.WriteString(next)
	}
	b.WriteString(elided)
	return b.String()
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

// recordDenyFact durably records a refusal as a TERMINAL `end` row (event_kind='end',
// status='error') keyed on the ORIGINATING conversation UUID (D-03 point 1 / GATE-01). Four
// refusals write it: the headless/production degraded deny (reason=no_approver), an
// identity's deny policy (reason=policy), an outbound send no review covers
// (reason=message_review) and a person's verb in a run with no person (reason=needs_person);
// marker carries which. This is the only legal
// terminal shape (migration 0011 event_kind ∈ {'start','end'}); because the call never
// executes, a lone `end` row is correct — the 35-05 reconciler's start∧¬end anti-join never
// flags it, and a denied triple never later executes (a model retry yields a fresh
// tool_call_id), so it never collides with a future start/end. A store or key failure is a
// WARN: the denial itself still stands (fail-closed) — only the audit fact is best-effort.
func (g *Gateway) recordDenyFact(ctx context.Context, spec tools.Spec, key ReservationKey, tier scoring.RiskTier, message string, marker map[string]any) {
	if g.store == nil {
		return
	}
	meta := map[string]any{
		"gateway_verdict": string(Deny),
		"gateway_tier":    string(tier),
	}
	maps.Copy(meta, marker)
	ev := toolinvocations.Event{
		ConversationID: key.ConversationID,
		RequestID:      key.RequestID,
		ToolCallID:     key.ToolCallID,
		ToolName:       spec.Name,
		Event:          toolinvocations.EventEnd,
		EndedAt:        time.Now().UTC(),
		Status:         "error",
		Error:          message,
		Meta:           meta,
	}
	if err := g.store.Insert(ctx, ev); err != nil {
		slog.Warn("gateway: deny fact insert failed",
			"tool", spec.Name, "conversation_id", key.ConversationID, "reason", marker["reason"], "err", err)
	}
}
