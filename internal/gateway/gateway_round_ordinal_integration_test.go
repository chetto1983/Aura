//go:build db_integration

// gateway_round_ordinal_integration_test.go is a concern split out of
// gateway_integration_test.go (CLAUDE.md's 600-LOC no-god-class cap): the D-01/D-04
// round-discrimination end-to-end proof for SC#1/SC#2 (45-02-T1), reusing that file's
// db_integration harness (envOrSkip, migratedPool, seedConversation, spyTool, insertEnd).
//
// This package cannot import internal/agent to call the real deriveToolOperationContext
// directly: internal/agent already imports internal/gateway, so the reverse import would
// cycle. The tests below therefore reproduce (mirror) the SAME FingerprintTyped struct
// shape internal/agent/idempotency_operation.go derives a child key from — Version
// "tool-child-v2" plus the RoundOrdinal field (D-01) — to prove Layer B's registry
// mechanics (aura.idempotency_operations, wired via g.SetOperationRegistry) are correct
// GIVEN round-discriminated keys. internal/agent/idempotency_operation_test.go is the
// tier that proves the real production function actually DERIVES that shape; keep the
// two definitions in lockstep if the key composition ever changes.
package gateway

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/chetto1983/aura/internal/agent/tools"
	"github.com/chetto1983/aura/internal/config"
	"github.com/chetto1983/aura/internal/idempotency"
	"github.com/chetto1983/aura/internal/toolinvocations"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

// deriveChildKeyForRound mirrors deriveToolOperationContext's child-key derivation
// (idempotency_operation.go) for a given round ordinal.
func deriveChildKeyForRound(t *testing.T, parentScope idempotency.Scope, parentKey string, parentFingerprint [32]byte, spec tools.Spec, args json.RawMessage, roundOrdinal uint32) idempotency.OperationKey {
	t.Helper()
	toolFingerprint, err := tools.OperationFingerprint(spec, args)
	if err != nil {
		t.Fatal(err)
	}
	childFingerprint, err := idempotency.FingerprintTyped(struct {
		Version           string            `json:"version"`
		ParentScope       idempotency.Scope `json:"parent_scope"`
		ParentKey         string            `json:"parent_key"`
		ParentFingerprint string            `json:"parent_fingerprint"`
		ToolScope         idempotency.Scope `json:"tool_scope"`
		ToolFingerprint   string            `json:"tool_fingerprint"`
		RoundOrdinal      uint32            `json:"round_ordinal"`
	}{
		Version: "tool-child-v2", ParentScope: parentScope, ParentKey: parentKey,
		ParentFingerprint: idempotency.FingerprintHex(parentFingerprint), ToolScope: spec.OperationScope,
		ToolFingerprint: idempotency.FingerprintHex(toolFingerprint), RoundOrdinal: roundOrdinal,
	})
	if err != nil {
		t.Fatal(err)
	}
	return idempotency.OperationKey{
		IdentityID: localIdentity,
		Scope:      spec.OperationScope,
		Key:        "child:" + idempotency.FingerprintHex(childFingerprint),
	}
}

// operationCtx attaches a trusted operation (the given child key, plus a fresh
// fingerprint over the mutating tool's canonical args) to ownerCtx().
func operationCtx(t *testing.T, key idempotency.OperationKey, spec tools.Spec, args json.RawMessage) context.Context {
	t.Helper()
	fingerprint, err := tools.OperationFingerprint(spec, args)
	if err != nil {
		t.Fatal(err)
	}
	ctx, err := idempotency.WithOperation(ownerCtx(), idempotency.Operation{Key: key, Fingerprint: fingerprint})
	if err != nil {
		t.Fatal(err)
	}
	return ctx
}

// endRowCount is SC#1/SC#2's exact evidence shape: a COUNT(*) over aura.tool_invocations
// scoped by conversation_id + request_id, not a log line.
func endRowCount(t *testing.T, pool *pgxpool.Pool, conversationID, requestID string) int {
	t.Helper()
	var n int
	if err := pool.QueryRow(ownerCtx(),
		`SELECT count(*) FROM aura.tool_invocations
		 WHERE conversation_id = $1 AND request_id = $2 AND event_kind = 'end'`,
		conversationID, requestID).Scan(&n); err != nil {
		t.Fatalf("count end rows: %v", err)
	}
	return n
}

// gatedExecWithOperationLifecycle extends gatedExec with the Layer B completion
// bookkeeping execTool performs after a genuine Execute (llm_agent_retry.go's
// operationAcquired branch): CompleteOperation persists the bounded replay so a
// SAME-key second dispatch reads DecisionReplay instead of DecisionInProgress.
func gatedExecWithOperationLifecycle(ctx context.Context, g *Gateway, tool tools.Tool, args json.RawMessage, key ReservationKey) (tools.ToolResult, Verdict, error) {
	v, derr := g.Decide(ctx, tool.Spec(), args, key)
	if derr != nil {
		return tools.ToolResult{}, v, derr
	}
	switch v.Decision {
	case Deny:
		return tools.ToolResult{}, v, &ErrDenied{Reason: v.Reason, Tier: v.Tier}
	case Approve:
		if v.ApprovalRequest != nil {
			return *v.ApprovalRequest, v, nil
		}
		return tools.ToolResult{}, v, nil
	}
	if v.Replay != nil {
		return *v.Replay, v, nil
	}
	res, err := tool.Execute(ctx, args)
	if err != nil {
		return res, v, err
	}
	if v.OperationDecision == idempotency.DecisionAcquired {
		claimedCtx, claimErr := idempotency.WithClaimToken(ctx, v.OperationClaimToken)
		if claimErr != nil {
			return res, v, claimErr
		}
		if completeErr := g.CompleteOperation(claimedCtx, res); completeErr != nil {
			return res, v, completeErr
		}
	}
	return res, v, nil
}

// TestRoundOrdinalCrossRoundReissueExecutesTwice proves SC#1: a mutating tool call
// re-issued in a LATER round of the same turn, with byte-identical arguments, derives a
// DIFFERENT child operation key (D-01's RoundOrdinal) and therefore executes again — two
// real dispatches, two committed `end` rows in aura.tool_invocations, not a replay of the
// first round's recorded result.
func TestRoundOrdinalCrossRoundReissueExecutesTwice(t *testing.T) {
	pool := migratedPool(t)
	store := toolinvocations.New(pool)
	g := New(config.ProfileSingleUserHardened, store)
	g.SetOperationRegistry(idempotency.New(pool, idempotency.Config{}))
	convID := seedConversation(t, pool)

	spec := tools.Spec{
		Name: "skill_manage", Mutating: true,
		OperationScope: tools.OperationScopeAgent, OperationNormalizer: tools.OperationNormalizerCanonical,
		ReplayPolicy: tools.ReplayToolResult,
	}
	args := skillRestoreArgs
	parentFingerprint, err := idempotency.FingerprintTyped(struct {
		Route string `json:"route"`
	}{Route: "agent_run"})
	if err != nil {
		t.Fatal(err)
	}
	// A per-run-unique parent key: the operation registry is scoped by
	// (identity, scope, key), not by conversation, so a fixed literal would collide
	// with a completed operation left behind by a prior run of this same test.
	parentKey := "public-agent-run-key-cross-round-" + uuid.Must(uuid.NewV7()).String()
	requestID := uuid.Must(uuid.NewV7()).String()

	round1Key := deriveChildKeyForRound(t, idempotency.ScopeHTTPMutation, parentKey, parentFingerprint, spec, args, 1)
	round2Key := deriveChildKeyForRound(t, idempotency.ScopeHTTPMutation, parentKey, parentFingerprint, spec, args, 2)
	if round1Key.Key == round2Key.Key {
		t.Fatal("round 1 and round 2 must derive different child keys")
	}

	spy1 := &spyTool{spec: spec, result: tools.ToolResult{Preview: "round-1-output"}}
	firstReservation := ReservationKey{ConversationID: convID, RequestID: requestID, ToolCallID: "call-round-1"}
	res1, v1, err1 := gatedExecWithOperationLifecycle(operationCtx(t, round1Key, spec, args), g, spy1, args, firstReservation)
	if err1 != nil || v1.Decision != Allow || v1.Replay != nil {
		t.Fatalf("round 1 dispatch = (%+v, %v), want allow with no replay", v1, err1)
	}
	if spy1.count != 1 || res1.Preview != "round-1-output" {
		t.Fatalf("round 1: Execute count = %d preview = %q, want 1/round-1-output", spy1.count, res1.Preview)
	}
	insertEnd(t, store, firstReservation, "round-1-output", "")

	spy2 := &spyTool{spec: spec, result: tools.ToolResult{Preview: "round-2-output"}}
	secondReservation := ReservationKey{ConversationID: convID, RequestID: requestID, ToolCallID: "call-round-2"}
	res2, v2, err2 := gatedExecWithOperationLifecycle(operationCtx(t, round2Key, spec, args), g, spy2, args, secondReservation)
	if err2 != nil || v2.Decision != Allow || v2.Replay != nil {
		t.Fatalf("round 2 dispatch = (%+v, %v), want allow with no replay", v2, err2)
	}
	if spy2.count != 1 || res2.Preview != "round-2-output" {
		t.Fatalf("round 2: Execute count = %d preview = %q, want 1/round-2-output (a FRESH execution, not a replay of round 1)", spy2.count, res2.Preview)
	}
	insertEnd(t, store, secondReservation, "round-2-output", "")

	if got := endRowCount(t, pool, convID, requestID); got != 2 {
		t.Fatalf("aura.tool_invocations end rows = %d, want 2 (two real executions across two rounds)", got)
	}
}

// TestRoundOrdinalSameRoundRetryExecutesOnce proves SC#2's paired half: a dispatch
// retried within the SAME round (same derived child key, D-01/D-02) collapses onto one
// execution via Layer B's replay — the second attempt derives the IDENTICAL key, so
// beginOperation returns DecisionReplay BEFORE Layer A's reservation is ever reached
// (decide.go: `if !proceed { return operationVerdict, nil }`), and aura.tool_invocations
// gains no second start/end row for it.
func TestRoundOrdinalSameRoundRetryExecutesOnce(t *testing.T) {
	pool := migratedPool(t)
	store := toolinvocations.New(pool)
	g := New(config.ProfileSingleUserHardened, store)
	g.SetOperationRegistry(idempotency.New(pool, idempotency.Config{}))
	convID := seedConversation(t, pool)

	spec := tools.Spec{
		Name: "skill_manage", Mutating: true,
		OperationScope: tools.OperationScopeAgent, OperationNormalizer: tools.OperationNormalizerCanonical,
		ReplayPolicy: tools.ReplayToolResult,
	}
	args := skillRestoreArgs
	parentFingerprint, err := idempotency.FingerprintTyped(struct {
		Route string `json:"route"`
	}{Route: "agent_run"})
	if err != nil {
		t.Fatal(err)
	}
	// A per-run-unique parent key: see the identical comment in
	// TestRoundOrdinalCrossRoundReissueExecutesTwice above.
	parentKey := "public-agent-run-key-same-round-" + uuid.Must(uuid.NewV7()).String()
	sameRoundKey := deriveChildKeyForRound(t, idempotency.ScopeHTTPMutation, parentKey, parentFingerprint, spec, args, 1)
	requestID := uuid.Must(uuid.NewV7()).String()

	spy1 := &spyTool{spec: spec, result: tools.ToolResult{Preview: "same-round-output"}}
	firstReservation := ReservationKey{ConversationID: convID, RequestID: requestID, ToolCallID: "call-same-round-1"}
	res1, v1, err1 := gatedExecWithOperationLifecycle(operationCtx(t, sameRoundKey, spec, args), g, spy1, args, firstReservation)
	if err1 != nil || v1.Decision != Allow || v1.Replay != nil {
		t.Fatalf("first attempt = (%+v, %v), want allow with no replay", v1, err1)
	}
	if spy1.count != 1 {
		t.Fatalf("first attempt Execute count = %d, want 1", spy1.count)
	}
	insertEnd(t, store, firstReservation, "same-round-output", "")

	// A second dispatch of the SAME round: a distinct tool_call_id (audit-only, D-01
	// deliberately excludes it from the key) but the IDENTICAL derived operation.
	_ = res1
	spy2 := &spyTool{spec: spec, result: tools.ToolResult{Preview: "must-not-run"}}
	secondReservation := ReservationKey{ConversationID: convID, RequestID: requestID, ToolCallID: "call-same-round-2"}
	res2, v2, err2 := gatedExecWithOperationLifecycle(operationCtx(t, sameRoundKey, spec, args), g, spy2, args, secondReservation)
	if err2 != nil {
		t.Fatalf("second attempt err: %v", err2)
	}
	if v2.OperationDecision != idempotency.DecisionReplay {
		t.Fatalf("second attempt operation decision = %q, want replay", v2.OperationDecision)
	}
	if spy2.count != 0 {
		t.Fatalf("second attempt Execute count = %d, want 0 (replayed, not re-run)", spy2.count)
	}
	if res2.Preview != "same-round-output" {
		t.Fatalf("second attempt preview = %q, want the replayed first result", res2.Preview)
	}

	if got := endRowCount(t, pool, convID, requestID); got != 1 {
		t.Fatalf("aura.tool_invocations end rows = %d, want 1 (the second attempt never reaches Layer A)", got)
	}
}
