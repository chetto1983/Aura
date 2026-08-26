// reserve.go is the gateway-side orchestration of the ONE synchronous, fatal-on-failure
// pre-execution reservation (GATE-03 + GATE-04). EVERY mutating-Allow outcome — the
// not-GateRecommended auto-allow branch in decide.go AND routeApprove's post-resume
// Verdict{Allow, OperatorID} — converges on this single reserve call before Decide
// returns Allow, taking exactly ONE reservation `start` row before tool.Execute. The
// operator_id (when the Allow came from the approved-resume branch) rides that ONE
// start's Meta as the executed marker — never a separate competing Insert (D-03 point 2).
package gateway

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"time"

	"github.com/chetto1983/aura/internal/agent/tools"
	"github.com/chetto1983/aura/internal/idempotency"
	"github.com/chetto1983/aura/internal/scoring"
	"github.com/chetto1983/aura/internal/toolinvocations"
	"github.com/google/uuid"
)

// resultExpiredMarker is appended to a replayed preview when the recorded end's sidecar
// has been GC'd (F-040). Replay tolerates the missing sidecar — it returns the capped +
// redacted preview plus this marker, never an error, and never extends sidecar retention
// to chase the verbatim bytes (Pitfall 6).
const resultExpiredMarker = "\n\n[result expired: full output no longer retained]"

// replayedMarker is appended to a replayed preview on BOTH replay layers (HARN-03/D-10).
// Without it the model cannot tell a recorded result from one this call actually
// produced — Aura once diagnosed a stale replay as a live symptom and wrote that
// misdiagnosis into long-term memory as fact. It composes with resultExpiredMarker
// rather than replacing it: the two answer different questions (the body is gone /
// this result was not produced by this call).
const replayedMarker = "\n\n[replayed: this result is from a prior dispatch of this call, not a fresh execution]"

// markReplayed appends replayedMarker to result.Preview. Both replay layers —
// replayResult (Layer A, the reservation ledger) and decodeOperationReplay (Layer B,
// the operation registry, which had no marker at all before this) — call this ONE
// helper, so the marker string and the append rule live in exactly one place
// (CLAUDE.md REUSABLE CODE).
func markReplayed(result tools.ToolResult) tools.ToolResult {
	result.Preview += replayedMarker
	return result
}

const operationReplayRetention = 30 * 24 * time.Hour

// beginOperation consumes the shared registry before the internal policy
// reservation. A non-acquired decision returns a terminal/replay verdict and
// prevents both policy reservation and tool execution.
func (g *Gateway) beginOperation(ctx context.Context, spec tools.Spec, rawArgs json.RawMessage, key ReservationKey, tier scoring.RiskTier) (Verdict, bool) {
	if g.operations == nil {
		return Verdict{Decision: Allow, Tier: tier}, true
	}
	operation, ok := idempotency.OperationFromContext(ctx)
	if !ok {
		return Verdict{Decision: Deny, Tier: tier, Reason: "operation context missing"}, false
	}
	if operation.Key.Scope != spec.OperationScope || spec.OperationNormalizer == "" || spec.ReplayPolicy != tools.ReplayToolResult {
		return Verdict{Decision: Deny, Tier: tier, Reason: "operation metadata mismatch"}, false
	}
	expectedFingerprint, err := tools.OperationFingerprint(spec, rawArgs)
	if err != nil || expectedFingerprint != operation.Fingerprint {
		return Verdict{Decision: Deny, Tier: tier, Reason: "operation fingerprint mismatch"}, false
	}
	request := idempotency.BeginRequest{Operation: operation.Key, Fingerprint: operation.Fingerprint}
	if _, convErr := uuid.Parse(key.ConversationID); convErr == nil {
		if _, requestErr := uuid.Parse(key.RequestID); requestErr == nil && key.ToolCallID != "" {
			request.Audit = &idempotency.AuditLink{ConversationID: key.ConversationID, RequestID: key.RequestID, ToolCallID: key.ToolCallID}
		}
	}
	decision, beginErr := g.operations.Begin(ctx, request)
	if beginErr != nil && !errors.Is(beginErr, idempotency.ErrConflict) {
		return Verdict{Decision: Deny, Tier: tier, Reason: "operation registry unavailable"}, false
	}
	verdict := Verdict{
		Decision: Deny, Tier: tier,
		OperationDecision:   decision.Decision,
		OperationClaimToken: decision.ClaimToken,
	}
	switch decision.Decision {
	case idempotency.DecisionAcquired:
		verdict.Decision = Allow
		return verdict, true
	case idempotency.DecisionReplay:
		result, err := decodeOperationReplay(decision.Replay)
		if err != nil {
			verdict.Reason = "operation replay unavailable"
			return verdict, false
		}
		verdict.Decision = Allow
		verdict.Replay = &result
		return verdict, false
	case idempotency.DecisionConflict:
		verdict.Reason = "operation key conflicts with changed intent"
	case idempotency.DecisionInProgress:
		verdict.Reason = "operation is in progress"
	case idempotency.DecisionIndeterminate:
		verdict.Reason = "operation outcome is indeterminate"
	case idempotency.DecisionRejected:
		verdict.Reason = "operation was rejected"
		verdict.OperationRejection = decision.Replay
	case idempotency.DecisionResultExpired:
		verdict.Reason = "operation completed but replay result expired"
	default:
		verdict.Reason = "operation registry unavailable"
	}
	return verdict, false
}

func decodeOperationReplay(replay *idempotency.ReplayResult) (tools.ToolResult, error) {
	if replay == nil {
		return tools.ToolResult{}, errors.New("missing replay")
	}
	if len(replay.Body) == 0 && replay.Preview == "" && replay.SidecarRef == "" {
		return tools.ToolResult{}, errors.New("expired replay")
	}
	var result tools.ToolResult
	if len(replay.Body) != 0 {
		if err := json.Unmarshal(replay.Body, &result); err != nil {
			return tools.ToolResult{}, errors.New("invalid replay")
		}
	} else {
		result.Preview = replay.Preview
		result.FullPath = replay.SidecarRef
		result.Bytes = len(result.Preview)
	}
	return markReplayed(result), nil
}

// CompleteOperation records the durable typed result after a mutating effect has
// completed. It is a nil-safe no-op for gateways without the shared registry.
func (g *Gateway) CompleteOperation(ctx context.Context, result tools.ToolResult) error {
	if g == nil || g.operations == nil {
		return nil
	}
	operation, ok := idempotency.OperationFromContext(ctx)
	if !ok {
		return errors.New("complete operation: trusted operation context missing")
	}
	replayResult := boundedOperationReplay(result)
	if err := g.operations.Complete(ctx, idempotency.CompleteRequest{
		Operation: operation.Key, Fingerprint: operation.Fingerprint,
		ClaimToken: operation.ClaimToken, Result: replayResult,
	}); err != nil {
		return err
	}
	g.closeDelegatedReservation(ctx, "ok", "", result)
	return nil
}

// MarkOperationIndeterminate makes an acquired mutation terminal after an
// ambiguous execution or post-effect completion failure.
func (g *Gateway) MarkOperationIndeterminate(ctx context.Context) error {
	if g == nil || g.operations == nil {
		return nil
	}
	operation, ok := idempotency.OperationFromContext(ctx)
	if !ok {
		return errors.New("mark operation indeterminate: trusted operation context missing")
	}
	if err := g.operations.MarkIndeterminate(
		ctx, operation.Key, operation.Fingerprint, operation.ClaimToken,
	); err != nil {
		return err
	}
	// The ledger's CHECK is status IN ('ok','error'), so an indeterminate outcome rides
	// the error text rather than a third status value — the same shape syntheticEnd uses.
	g.closeDelegatedReservation(ctx, "error",
		"delegated dispatch ended with an indeterminate outcome", tools.ToolResult{})
	return nil
}

// RejectOperation records a deterministic no-effect tool failure. Registries
// predating the rejected state fail closed to indeterminate.
func (g *Gateway) RejectOperation(ctx context.Context, toolErr error) error {
	if g == nil || g.operations == nil {
		return nil
	}
	operation, ok := idempotency.OperationFromContext(ctx)
	if !ok {
		return errors.New("reject operation: trusted operation context missing")
	}
	registry, ok := g.operations.(rejectionRegistry)
	if !ok {
		return g.operations.MarkIndeterminate(
			ctx, operation.Key, operation.Fingerprint, operation.ClaimToken,
		)
	}
	replay := boundedOperationRejection(toolErr)
	return registry.MarkRejected(ctx, idempotency.RejectRequest{
		Operation: operation.Key, Fingerprint: operation.Fingerprint,
		ClaimToken: operation.ClaimToken, Result: replay,
	})
}

func boundedOperationReplay(result tools.ToolResult) idempotency.ReplayResult {
	copyResult := result
	copyResult.Preview = boundedString(copyResult.Preview, idempotency.MaxReplayPreviewBytes)
	copyResult.FullPath = boundedString(copyResult.FullPath, idempotency.MaxSidecarRefBytes)
	body, err := json.Marshal(copyResult)
	if err != nil || len(body) > idempotency.MaxReplayBodyBytes {
		body = nil
	}
	return idempotency.ReplayResult{
		Body: body, Preview: copyResult.Preview, SidecarRef: copyResult.FullPath,
		ExpiresAt: time.Now().UTC().Add(operationReplayRetention),
	}
}

func boundedOperationRejection(toolErr error) idempotency.ReplayResult {
	preview := "tool rejected operation"
	if toolErr != nil {
		preview = boundedString(toolErr.Error(), idempotency.MaxReplayPreviewBytes)
	}
	body, err := json.Marshal(toolErr)
	if err != nil || len(body) == 0 || len(body) > idempotency.MaxReplayBodyBytes ||
		string(body) == "{}" {
		body, _ = json.Marshal(map[string]string{"error": preview})
	}
	return idempotency.ReplayResult{
		Body: body, Preview: preview,
		ExpiresAt: time.Now().UTC().Add(operationReplayRetention),
	}
}

func boundedString(value string, limit int) string {
	if len(value) <= limit {
		return value
	}
	for limit > 0 && (value[limit]&0xc0) == 0x80 {
		limit--
	}
	return value[:limit]
}

// reserve takes the single durable pre-execution reservation for a mutating-Allow call.
//   - rows==1 (acquired) → Verdict{Allow}: the reservation is ours, Execute proceeds.
//   - rows==0 (already held) → Verdict{Allow, Replay:<recorded end>}: execTool returns the
//     recorded outcome, Execute is NOT called (GATE-04 idempotency, for a retried approved
//     call too).
//   - INSERT error → Verdict{Deny, "reservation failed"}: Execute is NOT called (GATE-03
//     fail-closed — even post-approval; a reservation that cannot be durably taken blocks).
//
// operatorID is "" for the auto-allow origin and set for the approved-resume origin; it is
// folded into the single start Meta so both origins produce the SAME single start∧¬end shape.
func (g *Gateway) reserve(
	ctx context.Context, spec tools.Spec, rawArgs json.RawMessage, key ReservationKey,
	tier scoring.RiskTier, operatorID string, scope ApprovalScope,
) (Verdict, error) {
	if g.store == nil {
		// A strict Gateway constructed without a ledger (standalone/tests): allow without
		// a reservation. The dev/local_trusted no-op already short-circuits before here.
		return Verdict{Decision: Allow, Tier: tier, OperatorID: operatorID, Scope: scope}, nil
	}
	acquired, replay, err := g.store.Reserve(ctx, g.reservationStart(spec, rawArgs, key, tier, operatorID, scope))
	if err != nil {
		return Verdict{Decision: Deny, Tier: tier, Reason: "reservation failed"}, nil
	}
	if acquired {
		return Verdict{Decision: Allow, Tier: tier, OperatorID: operatorID, Scope: scope}, nil
	}
	if replay == nil {
		// The slot is held by a prior dispatch that never recorded an end — crashed, or a
		// turn abandoned before its terminal write landed. The effect must NOT run again
		// (at-most-once), but it must not be reported as done either: this used to return
		// Allow with a Replay whose Preview was the literal string "[reservation held:
		// result not yet available]" and a nil error, so the model received a SUCCESSFUL
		// tool result for a tool that never executed. Aura's own diagnosis of it, verbatim:
		// "fs_write non ha salvato il file — la scrittura è finita in reservation held e non
		// è stata effettivamente scritta su disco". A denial is honest; a fabricated success
		// is the one outcome the model cannot recover from.
		return Verdict{
			Decision: Deny, Tier: tier,
			Reason: "a prior dispatch of this tool call is still unaccounted for; it was not re-run",
		}, nil
	}
	res := replayResult(replay)
	return Verdict{Decision: Allow, Tier: tier, OperatorID: operatorID, Scope: scope, Replay: &res}, nil
}

// reservationStart builds the append-only `start` Event for the reservation. The verdict
// rides Meta (D-01 zero-migration); RedactForLedger runs for free inside the store's
// toParams, so any secret on the tool command line is redacted before the durable column.
func (g *Gateway) reservationStart(
	spec tools.Spec, rawArgs json.RawMessage, key ReservationKey,
	tier scoring.RiskTier, operatorID string, scope ApprovalScope,
) toolinvocations.Event {
	meta := map[string]any{
		"gateway_verdict": string(Allow),
		"gateway_tier":    string(tier),
		"reservation":     true,
	}
	if operatorID != "" {
		// Approved-resume origin (D-03 point 2): the executed marker rides THIS single
		// reservation start's Meta — the approve branch writes NO competing row.
		meta["operator_id"] = operatorID
		meta["approved"] = true
	}
	if scope != "" {
		// A standing grant let this destructive call through without asking (amendment
		// #127). Recording WHICH grant is the whole point: an audit that shows an
		// unprompted destructive execution and cannot say why is worse than no record.
		meta["approval_scope"] = string(scope)
	}
	return toolinvocations.Event{
		ConversationID: key.ConversationID,
		RequestID:      key.RequestID,
		ToolCallID:     key.ToolCallID,
		ToolName:       spec.Name,
		Event:          toolinvocations.EventStart,
		StartedAt:      time.Now().UTC(),
		Arguments:      string(rawArgs),
		ArgsBytes:      len(rawArgs),
		Meta:           meta,
	}
}

// replayResult rebuilds the tool result from a recorded `end` fact for a rows==0 duplicate.
// It is only called with a NON-nil end: an unaccounted-for prior dispatch is denied by
// reserve rather than replayed, because there is no outcome to replay and inventing one
// reported a tool that never ran as successful. A recorded end whose sidecar has been GC'd
// degrades to the capped preview plus resultExpiredMarker (Pitfall 6), never an error.
func replayResult(end *toolinvocations.Event) tools.ToolResult {
	if end == nil {
		// Defensive only — reserve filters this case. Kept non-panicking, and worded so a
		// future caller that reintroduces the path is told the effect did NOT happen.
		return tools.ToolResult{Preview: "[reservation held: the tool did NOT run and no result was recorded]"}
	}
	preview := end.ResultPreview
	fullPath := end.ResultSidecarPath
	if fullPath != "" {
		if _, statErr := os.Stat(fullPath); statErr != nil {
			preview += resultExpiredMarker
			fullPath = ""
		}
	}
	return markReplayed(tools.ToolResult{
		Preview:   preview,
		FullPath:  fullPath,
		Bytes:     end.ResultBytes,
		Truncated: end.ResultTruncated,
	})
}
