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
	verdict := Verdict{Decision: Deny, Tier: tier, OperationDecision: decision.Decision}
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
	return result, nil
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
	return g.operations.Complete(ctx, idempotency.CompleteRequest{
		Operation: operation.Key, Fingerprint: operation.Fingerprint, Result: replayResult,
	})
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
	return g.operations.MarkIndeterminate(ctx, operation.Key, operation.Fingerprint)
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
func (g *Gateway) reserve(ctx context.Context, spec tools.Spec, rawArgs json.RawMessage, key ReservationKey, tier scoring.RiskTier, operatorID string) (Verdict, error) {
	if g.store == nil {
		// A strict Gateway constructed without a ledger (standalone/tests): allow without
		// a reservation. The dev/local_trusted no-op already short-circuits before here.
		return Verdict{Decision: Allow, Tier: tier, OperatorID: operatorID}, nil
	}
	acquired, replay, err := g.store.Reserve(ctx, g.reservationStart(spec, rawArgs, key, tier, operatorID))
	if err != nil {
		return Verdict{Decision: Deny, Tier: tier, Reason: "reservation failed"}, nil
	}
	if acquired {
		return Verdict{Decision: Allow, Tier: tier, OperatorID: operatorID}, nil
	}
	res := replayResult(replay)
	return Verdict{Decision: Allow, Tier: tier, OperatorID: operatorID, Replay: &res}, nil
}

// reservationStart builds the append-only `start` Event for the reservation. The verdict
// rides Meta (D-01 zero-migration); RedactForLedger runs for free inside the store's
// toParams, so any secret on the tool command line is redacted before the durable column.
func (g *Gateway) reservationStart(spec tools.Spec, rawArgs json.RawMessage, key ReservationKey, tier scoring.RiskTier, operatorID string) toolinvocations.Event {
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
// A nil end (the prior reservation is still in-flight / crash-orphaned before its end) is
// NOT re-executed either — it returns an in-flight marker so the side effect stays
// at-most-once. A recorded end whose sidecar has been GC'd degrades to the capped preview
// plus resultExpiredMarker (Pitfall 6), never an error.
func replayResult(end *toolinvocations.Event) tools.ToolResult {
	if end == nil {
		return tools.ToolResult{Preview: "[reservation held: result not yet available]"}
	}
	preview := end.ResultPreview
	fullPath := end.ResultSidecarPath
	if fullPath != "" {
		if _, statErr := os.Stat(fullPath); statErr != nil {
			preview += resultExpiredMarker
			fullPath = ""
		}
	}
	return tools.ToolResult{
		Preview:   preview,
		FullPath:  fullPath,
		Bytes:     end.ResultBytes,
		Truncated: end.ResultTruncated,
	}
}
