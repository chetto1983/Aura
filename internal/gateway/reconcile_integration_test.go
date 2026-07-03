//go:build db_integration

// Integration proof tier for the crash-orphan reconciler (D-01d / GATE-03 durability +
// GATE-04 recovery) against the live append-only aura.tool_invocations ledger (migration
// 0011). Requires a running Postgres with migrations applied through 0011:
//
//	make db-up && aura db migrate           # or the WSL equivalent
//	AURA_DB_URL + AURA_DB_MIGRATE_URL + POSTGRES_PASSWORD set in env
//
// Run via:
//
//	go test -tags db_integration -race ./internal/gateway -run 'Reconcile|SlowTool|Recheck' -count=1 -p 1
//
// No-skip-as-green: the shared migratedPool/envOrSkip harness (gateway_integration_test.go)
// t.Fatals under $CI when the DSN is unset. The package goleak gate (main_test.go) fails on
// any leaked pgx pool or reconciler goroutine.
package gateway

import (
	"context"
	"testing"
	"time"

	"github.com/chetto1983/aura/internal/toolinvocations"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

// insertAgedStart appends a `start`-without-`end` reservation whose started_at is `age`
// in the past (a crash-orphan candidate), and returns its triple.
func insertAgedStart(t *testing.T, store *toolinvocations.Store, convID string, age time.Duration) (requestID, toolCallID string) {
	t.Helper()
	requestID = uuid.Must(uuid.NewV7()).String()
	toolCallID = "call-recon-" + uuid.Must(uuid.NewV7()).String()[:8]
	if err := store.Insert(context.Background(), toolinvocations.Event{
		ConversationID: convID,
		RequestID:      requestID,
		ToolCallID:     toolCallID,
		ToolName:       "skill",
		Event:          toolinvocations.EventStart,
		Seq:            1,
		StartedAt:      time.Now().UTC().Add(-age),
		Meta:           map[string]any{"reservation": true},
	}); err != nil {
		t.Fatalf("insert aged start: %v", err)
	}
	return requestID, toolCallID
}

// rowCountForTriple counts every ledger row for the reservation triple (start + end).
func rowCountForTriple(t *testing.T, pool *pgxpool.Pool, convID, requestID, toolCallID string) int {
	t.Helper()
	var n int
	if err := pool.QueryRow(context.Background(),
		`SELECT count(*) FROM aura.tool_invocations
		 WHERE conversation_id = $1 AND request_id = $2 AND tool_call_id = $3`,
		convID, requestID, toolCallID).Scan(&n); err != nil {
		t.Fatalf("count triple rows: %v", err)
	}
	return n
}

// TestReconcileAppendsIndeterminateEndForOrphan proves D-01d against the live ledger: a
// start∧¬end older than the effective grace is closed by APPENDING an
// end{status='error', meta.indeterminate:true, reconciled:true} — never by UPDATEing the
// orphan (the original start row is left intact; exactly one start + one appended end). The
// synthetic end is an indeterminate error, NOT a fresh 'ok', proving the mutating orphan was
// never re-invoked (the reconciler has no tool-execution seam by construction).
func TestReconcileAppendsIndeterminateEndForOrphan(t *testing.T) {
	pool := migratedPool(t)
	store := toolinvocations.New(pool)
	convID := seedConversation(t, pool)
	ctx := context.Background()

	// Aged 1h: older than effectiveGrace(0)=30min → a provable crash-orphan.
	requestID, toolCallID := insertAgedStart(t, store, convID, time.Hour)

	r := NewReconciler(store, time.Hour, 0) // maxToolExecWindow=0 → 30-min effective grace
	r.reconcileOrphans(ctx)

	end, err := store.GetEnd(ctx, convID, requestID, toolCallID)
	if err != nil {
		t.Fatalf("GetEnd after reconcile: %v", err)
	}
	if end == nil {
		t.Fatal("reconciler appended no end for an aged crash-orphan; want an indeterminate end")
	}
	if end.Status != "error" {
		t.Errorf("appended end status = %q, want \"error\" (CHECK is IN ('ok','error'); indeterminate rides meta)", end.Status)
	}
	if end.Meta["indeterminate"] != true || end.Meta["reconciled"] != true {
		t.Errorf("appended end meta = %#v, want {indeterminate:true, reconciled:true}", end.Meta)
	}

	// APPEND, not UPDATE: the original start row is untouched and exactly one end was added.
	if got := startRowCount(t, pool, ReservationKey{ConversationID: convID, RequestID: requestID, ToolCallID: toolCallID}); got != 1 {
		t.Errorf("start rows = %d, want 1 (the reconciler must not touch the start row)", got)
	}
	if got := rowCountForTriple(t, pool, convID, requestID, toolCallID); got != 2 {
		t.Errorf("total rows for the triple = %d, want 2 (start + one appended end — APPEND not UPDATE)", got)
	}
}

// TestReconcileInGraceOrphanUntouchedThenSlowToolRealEndWins proves the WARNING-4 guard on
// the live ledger: a start aged just UNDER the effective grace is a legitimately slow tool,
// NOT a crash-orphan — the reconciler leaves it untouched (in-grace skip), and the tool's
// REAL end{status='ok'} then inserts (rows==1) and GetEnd returns the real 'ok', never a
// synthetic indeterminate error. The slow-but-within-lifetime outcome is never discarded.
func TestReconcileInGraceOrphanUntouchedThenSlowToolRealEndWins(t *testing.T) {
	pool := migratedPool(t)
	store := toolinvocations.New(pool)
	convID := seedConversation(t, pool)
	ctx := context.Background()

	// Aged 25min: younger than effectiveGrace(0)=30min → in-grace, must be skipped.
	requestID, toolCallID := insertAgedStart(t, store, convID, 25*time.Minute)

	r := NewReconciler(store, time.Hour, 0)
	r.reconcileOrphans(ctx)

	// In-grace: the reconciler appended nothing, so no end exists yet.
	if end, err := store.GetEnd(ctx, convID, requestID, toolCallID); err != nil {
		t.Fatalf("GetEnd (in-grace): %v", err)
	} else if end != nil {
		t.Fatalf("reconciler closed an in-grace orphan: %+v; want it left untouched", end)
	}

	// The slow tool now finishes: its REAL end lands as the first (and only) end.
	if err := store.Insert(ctx, toolinvocations.Event{
		ConversationID: convID, RequestID: requestID, ToolCallID: toolCallID,
		ToolName: "skill", Event: toolinvocations.EventEnd, Seq: 2,
		StartedAt: time.Now().UTC().Add(-25 * time.Minute), EndedAt: time.Now().UTC(),
		DurationMS: 1500, Status: "ok", ResultPreview: "real-slow-output", PreviewBytes: 16, ResultBytes: 16,
	}); err != nil {
		t.Fatalf("insert real slow-tool end: %v", err)
	}

	end, err := store.GetEnd(ctx, convID, requestID, toolCallID)
	if err != nil {
		t.Fatalf("GetEnd (real end): %v", err)
	}
	if end == nil || end.Status != "ok" || end.ResultPreview != "real-slow-output" {
		t.Fatalf("GetEnd = %+v, want the real ok end (the slow tool's outcome must win)", end)
	}
	if end.Meta["indeterminate"] == true {
		t.Error("the real end must not carry the reconciler's indeterminate marker")
	}
}

// TestReconcileRechecksBeforeAppendLiveRealEndWins proves the pre-append GetEnd re-check
// against the live ledger: when a real end already exists for an aged start (the end landed
// after the list snapshot), the reconciler SKIPS — it appends no synthetic end, so the real
// outcome is preserved (rows stay start+end, and GetEnd keeps returning the real 'ok').
func TestReconcileRechecksBeforeAppendLiveRealEndWins(t *testing.T) {
	pool := migratedPool(t)
	store := toolinvocations.New(pool)
	convID := seedConversation(t, pool)
	ctx := context.Background()

	requestID, toolCallID := insertAgedStart(t, store, convID, time.Hour)
	// The real end is already present at reconcile time (simulating an end that landed after
	// the list snapshot but before the append) — the pre-append re-check must skip.
	if err := store.Insert(ctx, toolinvocations.Event{
		ConversationID: convID, RequestID: requestID, ToolCallID: toolCallID,
		ToolName: "skill", Event: toolinvocations.EventEnd, Seq: 2,
		StartedAt: time.Now().UTC().Add(-time.Hour), EndedAt: time.Now().UTC(),
		DurationMS: 10, Status: "ok", ResultPreview: "raced-real-end", PreviewBytes: 14, ResultBytes: 14,
	}); err != nil {
		t.Fatalf("insert raced real end: %v", err)
	}

	r := NewReconciler(store, time.Hour, 0)
	r.reconcileOrphans(ctx)

	end, err := store.GetEnd(ctx, convID, requestID, toolCallID)
	if err != nil {
		t.Fatalf("GetEnd: %v", err)
	}
	if end == nil || end.Status != "ok" || end.ResultPreview != "raced-real-end" {
		t.Fatalf("GetEnd = %+v, want the real ok end preserved (re-check must skip)", end)
	}
	if got := rowCountForTriple(t, pool, convID, requestID, toolCallID); got != 2 {
		t.Errorf("total rows = %d, want 2 (start + the real end; no synthetic end appended)", got)
	}
}

// TestReconcileStartStopGoleakLive exercises the full Start/Stop lifecycle against the live
// ledger: Start runs the boot one-shot (a real anti-join over PG) and enters the tick loop,
// Stop joins it under the bounded wait. main_test.go's goleak.VerifyTestMain fails the
// package on any leaked goroutine — this proves the daemon-wired lifecycle is leak-clean.
func TestReconcileStartStopGoleakLive(t *testing.T) {
	pool := migratedPool(t)
	store := toolinvocations.New(pool)
	seedConversation(t, pool)

	r := NewReconciler(store, 50*time.Millisecond, 0)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	r.Start(ctx)
	time.Sleep(120 * time.Millisecond) // let the boot one-shot + at least one tick run
	r.Stop()
	r.Stop() // idempotent
}
