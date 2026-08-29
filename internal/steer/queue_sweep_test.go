//go:build db_integration

package steer

import (
	"strings"
	"testing"
	"time"

	"github.com/chetto1983/aura/internal/conversations"
	"github.com/chetto1983/aura/internal/db"
	"github.com/jackc/pgx/v5"
)

// TestExpireDue proves D-07/D-08 end-to-end: a due row is marked expired AND leaves a
// readable conversation trace, in the same transaction, naming its kind and source.
//
// Pushed through the explicit delegation-result entry point, so it reads its TTL from
// Config.DelegationResultTTL, never SteerTTL.
func TestExpireDue(t *testing.T) {
	pool := steerDisposablePool(t)
	conv := conversations.New(pool, conversations.Config{})
	sweeper := NewSweeper(pool, conv)
	identityID, convID := seedIdentityAndConversation(t, pool)
	store := NewPostgresStore(pool, Config{Max: 8, MaxBytes: 16384, DelegationResultTTL: time.Millisecond})

	if err := store.PushDelegationResult(convID, SourceWorker, "worker report, never delivered", "f-expire"); err != nil {
		t.Fatalf("PushDelegationResult: %v", err)
	}
	time.Sleep(5 * time.Millisecond)

	expired, err := sweeper.ExpireDue(t.Context(), time.Now(), 100)
	if err != nil {
		t.Fatalf("ExpireDue: %v", err)
	}
	if expired != 1 {
		t.Fatalf("expired = %d, want 1", expired)
	}

	var expiredAtValid bool
	var expiryReason string
	if err := pool.QueryRow(t.Context(),
		"SELECT expired_at IS NOT NULL, expiry_reason FROM aura.steer_queue WHERE conversation_id = $1", convID,
	).Scan(&expiredAtValid, &expiryReason); err != nil {
		t.Fatalf("query row: %v", err)
	}
	if !expiredAtValid {
		t.Error("expired_at is NULL, want set")
	}
	if expiryReason != "delegation_result_ttl_expired" {
		t.Errorf("expiry_reason = %q, want delegation_result_ttl_expired", expiryReason)
	}

	// aura.conversation_turns is fail-closed RLS (migration 0089): a read on the plain
	// pool connection (no app.current_identity set) would see zero rows regardless of
	// whether the sweep's trace write happened, so this read runs identity-scoped like
	// seedIdentityAndConversation's own conversation write does.
	var traceContent string
	if err := db.WithIdentityTxRaw(t.Context(), pool, identityID, func(tx pgx.Tx) error {
		return tx.QueryRow(t.Context(),
			"SELECT content FROM aura.conversation_turns WHERE conversation_id = $1 ORDER BY seq DESC LIMIT 1", convID,
		).Scan(&traceContent)
	}); err != nil {
		t.Fatalf("query trace turn: %v", err)
	}
	if !strings.Contains(traceContent, "worker") || !strings.Contains(traceContent, SourceWorker) {
		t.Errorf("trace content = %q, want it to name the kind (worker) and the source (%s)", traceContent, SourceWorker)
	}
}

// TestExpireDueNeverExpiresNullExpiresAt proves a row with expires_at IS NULL is
// never selected as due, regardless of how much wall-clock time passes.
func TestExpireDueNeverExpiresNullExpiresAt(t *testing.T) {
	pool := steerDisposablePool(t)
	conv := conversations.New(pool, conversations.Config{})
	sweeper := NewSweeper(pool, conv)
	_, convID := seedIdentityAndConversation(t, pool)
	store := NewPostgresStore(pool, Config{Max: 8, MaxBytes: 16384, SteerTTL: 0})

	if err := store.Push(convID, "cockpit", "never expires"); err != nil {
		t.Fatalf("Push: %v", err)
	}

	expired, err := sweeper.ExpireDue(t.Context(), time.Now().Add(365*24*time.Hour), 100)
	if err != nil {
		t.Fatalf("ExpireDue: %v", err)
	}
	if expired != 0 {
		t.Fatalf("expired = %d, want 0 (expires_at IS NULL must never be selected)", expired)
	}
}

// TestExpireDueIdempotent proves a second sweep pass over the same already-expired
// row expires zero additional rows.
func TestExpireDueIdempotent(t *testing.T) {
	pool := steerDisposablePool(t)
	conv := conversations.New(pool, conversations.Config{})
	sweeper := NewSweeper(pool, conv)
	identityID, convID := seedIdentityAndConversation(t, pool)
	store := NewPostgresStore(pool, Config{Max: 8, MaxBytes: 16384, SteerTTL: time.Millisecond})

	if err := store.Push(convID, "cockpit", "expires once"); err != nil {
		t.Fatalf("Push: %v", err)
	}
	time.Sleep(5 * time.Millisecond)

	now := time.Now()
	first, err := sweeper.ExpireDue(t.Context(), now, 100)
	if err != nil {
		t.Fatalf("first ExpireDue: %v", err)
	}
	if first != 1 {
		t.Fatalf("first pass expired = %d, want 1", first)
	}
	second, err := sweeper.ExpireDue(t.Context(), now, 100)
	if err != nil {
		t.Fatalf("second ExpireDue: %v", err)
	}
	if second != 0 {
		t.Fatalf("second pass expired = %d, want 0 (idempotent)", second)
	}

	// aura.conversation_turns is fail-closed RLS (migration 0089): read identity-scoped,
	// matching TestExpireDue's own reasoning above.
	var turnCount int
	if err := db.WithIdentityTxRaw(t.Context(), pool, identityID, func(tx pgx.Tx) error {
		return tx.QueryRow(t.Context(),
			"SELECT count(*) FROM aura.conversation_turns WHERE conversation_id = $1", convID,
		).Scan(&turnCount)
	}); err != nil {
		t.Fatalf("count turns: %v", err)
	}
	if turnCount != 1 {
		t.Fatalf("conversation_turns for %s = %d, want exactly 1 trace (no duplicate on the idempotent second pass)", convID, turnCount)
	}
}

// TestExpireDueDrainedRowNeverExpired proves a row already delivered (drained_at set)
// is never selected as due, even past its own expires_at — a delivered row has
// already reached the operator; expiring it afterward would be meaningless.
//
// Unlike the other tests in this file, the TTL here must survive the Push-then-Drain
// round trip itself (both need to observe the row as still live) before later being
// swept as due — a 1ms TTL would race the network round trip to a real Postgres
// container, so this uses a wider TTL/sleep margin than the "just needs to already be
// due" tests above.
func TestExpireDueDrainedRowNeverExpired(t *testing.T) {
	pool := steerDisposablePool(t)
	conv := conversations.New(pool, conversations.Config{})
	sweeper := NewSweeper(pool, conv)
	_, convID := seedIdentityAndConversation(t, pool)
	store := NewPostgresStore(pool, Config{Max: 8, MaxBytes: 16384, SteerTTL: 200 * time.Millisecond})

	if err := store.Push(convID, "cockpit", "delivered before expiry"); err != nil {
		t.Fatalf("Push: %v", err)
	}
	if got := store.Drain(convID); len(got) != 1 {
		t.Fatalf("Drain = %+v, want 1 message", got)
	}
	time.Sleep(300 * time.Millisecond)

	expired, err := sweeper.ExpireDue(t.Context(), time.Now(), 100)
	if err != nil {
		t.Fatalf("ExpireDue: %v", err)
	}
	if expired != 0 {
		t.Fatalf("expired = %d, want 0 (a drained row must never be swept)", expired)
	}
}
