//go:build db_integration

// gateway_adversarial_triad_integration_test.go proves D-25's adversarial triad
// (45-02-T2, Pitfall 5 item 4): four paired assertions naming WHICH duplicates must and
// must not collapse, not just that "duplicates are deduplicated". A suite green only on
// the dedup half is, per PITFALLS.md verbatim, precisely how F-1 shipped — so this file
// deliberately keeps all four side by side:
//
//  1. TestSameToolCallIDRetryReplaysViaLayerA     — MUST collapse (Layer A, same call id)
//  2. TestSchedulerReclaimExecutesExactlyOnceAcrossTurns — MUST collapse (Layer B, D-06)
//  3. TestLaterRoundReissueWithIdenticalArgumentsExecutesTwice — must NOT collapse (D-01)
//  4. TestUnaccountedPriorDispatchStaysDeniedNeverFabricated — the reserve.go:233-246
//     fabricated-success regression, re-run against the round-discriminated key shape
//
// Each test counts executions two ways and asserts BOTH agree: the spyTool's in-process
// invocation counter, and a SQL COUNT(*) over aura.tool_invocations filtered on
// event_kind='end'. A mismatch between the two counters is itself a failure worth
// asserting, because it is exactly how a fabricated success hides.
package gateway

import (
	"encoding/json"
	"errors"
	"testing"

	"github.com/chetto1983/aura/internal/agent/tools"
	"github.com/chetto1983/aura/internal/config"
	"github.com/chetto1983/aura/internal/idempotency"
	"github.com/chetto1983/aura/internal/scoring"
	"github.com/chetto1983/aura/internal/toolinvocations"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

// endRowCountForCall narrows endRowCount to one (conversation_id, request_id,
// tool_call_id) triple — the scoping these paired assertions need to isolate a single
// dispatch attempt's ledger footprint.
func endRowCountForCall(t *testing.T, pool *pgxpool.Pool, conversationID, requestID, toolCallID string) int {
	t.Helper()
	var n int
	if err := pool.QueryRow(ownerCtx(),
		`SELECT count(*) FROM aura.tool_invocations
		 WHERE conversation_id = $1 AND request_id = $2 AND tool_call_id = $3 AND event_kind = 'end'`,
		conversationID, requestID, toolCallID).Scan(&n); err != nil {
		t.Fatalf("count end rows for call: %v", err)
	}
	return n
}

// TestSameToolCallIDRetryReplaysViaLayerA proves the "MUST collapse" half: a dispatch
// retried with the SAME tool_call_id (a transport-level resend of the exact same call)
// replays the recorded result via Layer A's reservation ledger — the tool body runs
// once, and exactly one `end` row lands in aura.tool_invocations.
func TestSameToolCallIDRetryReplaysViaLayerA(t *testing.T) {
	pool := migratedPool(t)
	store := toolinvocations.New(pool)
	g := New(config.ProfileSingleUserHardened, store)
	convID := seedConversation(t, pool)
	key := newKey(convID, "call-same-id-retry")

	// skillRestoreArgs (not gatedArgs): a Destructive-tier call routes through
	// routeApprove's "no interactive approver" degraded-deny path instead of ever
	// reaching Layer A's reserve() — this test isolates Layer A, so it needs the
	// Normal-tier auto-allow shape every other reservation test in this package uses.
	spec := tools.Spec{Name: "skill_manage", Mutating: true}
	spy := &spyTool{spec: spec, result: tools.ToolResult{Preview: "first-attempt-output"}}
	res1, v1, err1 := gatedExec(ownerCtx(), g, spy, skillRestoreArgs, key)
	if err1 != nil || v1.Decision != Allow || v1.Replay != nil {
		t.Fatalf("first dispatch = (%+v, %v), want allow with no replay", v1, err1)
	}
	if spy.count != 1 {
		t.Fatalf("Execute count after first dispatch = %d, want 1", spy.count)
	}
	insertEnd(t, store, key, "first-attempt-output", "")

	res2, v2, err2 := gatedExec(ownerCtx(), g, spy, skillRestoreArgs, key)
	if err2 != nil {
		t.Fatalf("retry err: %v", err2)
	}
	if v2.Replay == nil {
		t.Fatal("retry must carry a replay (Layer A rows==0)")
	}
	if spy.count != 1 {
		t.Fatalf("Execute count after retry = %d, want 1 (replayed, not re-run)", spy.count)
	}
	// The retry is a Layer A replay, so it carries replayedMarker (HARN-03, D-10)
	// appended to the first attempt's recorded preview, not a byte-identical repeat.
	if res2.Preview != res1.Preview+replayedMarker {
		t.Fatalf("retry preview = %q, want the first attempt's recorded preview %q plus the replay marker", res2.Preview, res1.Preview)
	}

	endRows := endRowCountForCall(t, pool, key.ConversationID, key.RequestID, key.ToolCallID)
	if endRows != spy.count {
		t.Fatalf("SQL end-row count %d disagrees with the in-process Execute counter %d — exactly how a fabricated success hides", endRows, spy.count)
	}
	if endRows != 1 {
		t.Fatalf("aura.tool_invocations end rows = %d, want 1", endRows)
	}
}

// TestSchedulerReclaimExecutesExactlyOnceAcrossTurns proves the OTHER "MUST collapse"
// half, at Layer B: a simulated scheduler reclaim — a FRESH turn (new RequestID, new
// tool_call_id) under the SAME parent scheduler-run operation, with the round ordinal
// restarting at 1 (D-06) — derives the SAME child key as the original attempt and
// therefore executes exactly once IN TOTAL across both turns. This is the case that
// proves requestID was correctly EXCLUDED from the child key (D-01): if the reclaim
// executed again, the exclusion would be broken.
func TestSchedulerReclaimExecutesExactlyOnceAcrossTurns(t *testing.T) {
	pool := migratedPool(t)
	store := toolinvocations.New(pool)
	g := New(config.ProfileSingleUserHardened, store)
	g.SetOperationRegistry(idempotency.New(pool, idempotency.Config{}))
	convID := seedConversation(t, pool)

	spec := tools.Spec{
		Name: "task", Mutating: true,
		OperationScope: tools.OperationScopeAgent, OperationNormalizer: tools.OperationNormalizerCanonical,
		ReplayPolicy: tools.ReplayToolResult,
	}
	args := json.RawMessage(`{"action":"run_reminder","id":"reminder-42"}`)
	parentFingerprint, err := idempotency.FingerprintTyped(struct {
		SchedulerRunID string `json:"scheduler_run_id"`
	}{SchedulerRunID: "run-42"})
	if err != nil {
		t.Fatal(err)
	}
	// The scheduler-run parent's key/fingerprint are STABLE across the original attempt
	// and the reclaim — unlike an HTTP-mutation parent, a scheduler run is the SAME
	// public operation re-entering the loop, not a fresh request.
	parentKey := "scheduler-run-" + uuid.Must(uuid.NewV7()).String()
	// D-06: the reclaim's round ordinal restarts at 1 against the SAME stable parent, so
	// this derives the IDENTICAL child key both times.
	reclaimKey := deriveChildKeyForRound(t, idempotency.ScopeSchedulerRun, parentKey, parentFingerprint, spec, args, 1)

	// Turn 1: the original attempt. Fresh RequestID/tool_call_id, as every turn mints.
	turn1Request := uuid.Must(uuid.NewV7()).String()
	turn1Reservation := ReservationKey{ConversationID: convID, RequestID: turn1Request, ToolCallID: "call-turn-1"}
	spy := &spyTool{spec: spec, result: tools.ToolResult{Preview: "reminder-delivered"}}
	res1, v1, err1 := gatedExecWithOperationLifecycle(operationCtx(t, reclaimKey, spec, args), g, spy, args, turn1Reservation)
	if err1 != nil || v1.Decision != Allow || v1.Replay != nil {
		t.Fatalf("turn 1 dispatch = (%+v, %v), want allow with no replay", v1, err1)
	}
	if spy.count != 1 || res1.Preview != "reminder-delivered" {
		t.Fatalf("turn 1: Execute count = %d preview = %q, want 1/reminder-delivered", spy.count, res1.Preview)
	}
	insertEnd(t, store, turn1Reservation, "reminder-delivered", "")

	// Turn 2: the reclaim. A FRESH RequestID and tool_call_id — a genuinely new turn —
	// but the SAME derived child key.
	turn2Request := uuid.Must(uuid.NewV7()).String()
	turn2Reservation := ReservationKey{ConversationID: convID, RequestID: turn2Request, ToolCallID: "call-turn-2-reclaim"}
	_, v2, err2 := gatedExecWithOperationLifecycle(operationCtx(t, reclaimKey, spec, args), g, spy, args, turn2Reservation)
	if err2 != nil {
		t.Fatalf("reclaim dispatch err: %v", err2)
	}
	if v2.OperationDecision != idempotency.DecisionReplay {
		t.Fatalf("reclaim operation decision = %q, want replay (the SAME child key as turn 1)", v2.OperationDecision)
	}

	if spy.count != 1 {
		t.Fatalf("Execute count across both turns = %d, want 1 (the reclaim must not re-execute)", spy.count)
	}
	if got := endRowCountForCall(t, pool, convID, turn1Request, "call-turn-1"); got != 1 {
		t.Fatalf("turn 1 end rows = %d, want 1", got)
	}
	if got := endRowCountForCall(t, pool, convID, turn2Request, "call-turn-2-reclaim"); got != 0 {
		t.Fatalf("turn 2 (reclaim) end rows = %d, want 0 — the reclaim never reaches Layer A", got)
	}
}

// TestLaterRoundReissueWithIdenticalArgumentsExecutesTwice is the "must NOT collapse"
// paired half of TestSchedulerReclaimExecutesExactlyOnceAcrossTurns: a deliberate
// re-issue in a LATER round with byte-identical arguments derives a DIFFERENT child key
// (D-01's RoundOrdinal) and therefore executes again — two real dispatches, two `end`
// rows. This re-proves SC#1 (already covered by
// TestRoundOrdinalCrossRoundReissueExecutesTwice) beside the other three adversarial
// cases so the full contrast reads in one place.
func TestLaterRoundReissueWithIdenticalArgumentsExecutesTwice(t *testing.T) {
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
	parentKey := "public-agent-run-key-later-round-" + uuid.Must(uuid.NewV7()).String()
	requestID := uuid.Must(uuid.NewV7()).String()

	round1Key := deriveChildKeyForRound(t, idempotency.ScopeHTTPMutation, parentKey, parentFingerprint, spec, args, 1)
	round2Key := deriveChildKeyForRound(t, idempotency.ScopeHTTPMutation, parentKey, parentFingerprint, spec, args, 2)

	spy := &spyTool{spec: spec, result: tools.ToolResult{Preview: "later-round-output"}}
	firstReservation := ReservationKey{ConversationID: convID, RequestID: requestID, ToolCallID: "call-later-round-1"}
	_, v1, err1 := gatedExecWithOperationLifecycle(operationCtx(t, round1Key, spec, args), g, spy, args, firstReservation)
	if err1 != nil || v1.Decision != Allow || v1.Replay != nil {
		t.Fatalf("round 1 = (%+v, %v), want allow with no replay", v1, err1)
	}
	insertEnd(t, store, firstReservation, "later-round-output", "")

	secondReservation := ReservationKey{ConversationID: convID, RequestID: requestID, ToolCallID: "call-later-round-2"}
	_, v2, err2 := gatedExecWithOperationLifecycle(operationCtx(t, round2Key, spec, args), g, spy, args, secondReservation)
	if err2 != nil || v2.Decision != Allow || v2.Replay != nil {
		t.Fatalf("round 2 = (%+v, %v), want allow with no replay (a FRESH execution)", v2, err2)
	}
	insertEnd(t, store, secondReservation, "later-round-output", "")

	if spy.count != 2 {
		t.Fatalf("Execute count across both rounds = %d, want 2", spy.count)
	}
	end1 := endRowCountForCall(t, pool, convID, requestID, "call-later-round-1")
	end2 := endRowCountForCall(t, pool, convID, requestID, "call-later-round-2")
	if end1 != 1 || end2 != 1 {
		t.Fatalf("end rows = %d/%d, want 1/1 (two REAL executions, not one replayed twice)", end1, end2)
	}
	if end1+end2 != spy.count {
		t.Fatalf("SQL end-row total %d disagrees with the in-process Execute counter %d", end1+end2, spy.count)
	}
}

// TestUnaccountedPriorDispatchStaysDeniedNeverFabricated re-runs the reserve.go:233-246
// fabricated-success regression (already unit-pinned by
// TestReserveDeniesAnUnaccountedPriorDispatch in reserve_test.go, which this plan does
// not touch) against the round-discriminated key shape and a live Postgres ledger: a
// prior dispatch acquired the reservation and then crashed before any end fact was
// recorded. A second dispatch attempt for the SAME reservation triple must be DENIED,
// never served as a fabricated success.
func TestUnaccountedPriorDispatchStaysDeniedNeverFabricated(t *testing.T) {
	pool := migratedPool(t)
	store := toolinvocations.New(pool)
	g := New(config.ProfileSingleUserHardened, store)
	convID := seedConversation(t, pool)
	key := newKey(convID, "call-unaccounted-1")

	// skillRestoreArgs (not gatedArgs): a Destructive-tier second dispatch would route
	// through routeApprove's "no interactive approver" degraded-deny path and never reach
	// Layer A's reserve() at all — that writes its OWN `end` row (Meta.degraded_deny) for
	// a completely different reason, which would corrupt this test's SQL evidence. This
	// test isolates reserve.go's fabricated-success prevention specifically, so it needs
	// the Normal-tier auto-allow shape.
	spec := tools.Spec{Name: "skill_manage", Mutating: true}

	// Simulate a prior dispatch that acquired the reservation and then crashed before
	// any end fact was recorded — the exact case reserve.go:233-246 documents.
	firstVerdict, ferr := g.reserve(ownerCtx(), spec, skillRestoreArgs, key, scoring.Normal, "")
	if ferr != nil || firstVerdict.Decision != Allow || firstVerdict.Replay != nil {
		t.Fatalf("prior dispatch reservation = (%+v, %v), want allow with no replay", firstVerdict, ferr)
	}
	// Deliberately no insertEnd: the effect's outcome was never recorded.

	spy := &spyTool{spec: spec, result: tools.ToolResult{Preview: "must-not-run"}}
	res, v, derr := gatedExec(ownerCtx(), g, spy, skillRestoreArgs, key)
	if v.Decision != Deny {
		t.Fatalf("verdict = %q, want deny (an unaccounted prior dispatch must never be fabricated into a success)", v.Decision)
	}
	var denied *ErrDenied
	if !errors.As(derr, &denied) {
		t.Fatalf("err = %v, want *ErrDenied", derr)
	}
	if spy.count != 0 {
		t.Fatalf("Execute count = %d, want 0 (fail-closed deny, not a fabricated success)", spy.count)
	}
	if res.Preview != "" || v.Replay != nil {
		t.Fatalf("a denial must carry no replay/result — there is no outcome to fabricate, got preview=%q replay=%v", res.Preview, v.Replay)
	}

	endRows := endRowCountForCall(t, pool, key.ConversationID, key.RequestID, key.ToolCallID)
	if endRows != 0 {
		t.Fatalf("aura.tool_invocations end rows = %d, want 0 (no execution ever completed)", endRows)
	}
}
