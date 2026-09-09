//go:build db_integration

// credit_ledger_integration_test.go proves the CRED-06 period-spend read against a LIVE
// Postgres, over migration 0124's widened aura.cache_metrics.cost_usd /
// aura.conversations.total_cost_usd (numeric(24,12)). Reuses this package's existing
// db_integration fixtures (migratedPool, ownerCtx, seedAsOwner, seedConversationForIdentity
// from audit_store_integration_test.go) rather than duplicating them.
//
// No-skip-as-green: envOrSkip/migratedPool t.Fatal under $CI when the DSN is unset.

package agui

import (
	"context"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

// seedIdentity (fresh throwaway identity, fresh per test run so the append-only
// cache_metrics rows never need cleanup) is defined once in
// capability_denial_integration_test.go and reused here.

// seedCacheMetric inserts one aura.cache_metrics row at an explicit ts, so the boundary
// test can place rows precisely either side of a window start.
func seedCacheMetric(t *testing.T, pool *pgxpool.Pool, conversationID string, seq int, cost float64, ts time.Time) {
	t.Helper()
	if _, err := pool.Exec(context.Background(),
		"INSERT INTO aura.cache_metrics (conversation_id, seq, cost_usd, ts) VALUES ($1, $2, $3, $4)",
		conversationID, seq, cost, ts,
	); err != nil {
		t.Fatalf("seed cache_metrics row seq=%d: %v", seq, err)
	}
}

// TestSumIdentitySpendSince proves the M-09 exact-agreement property survives the round
// trip through the widened column and this read: four calls at the measured
// 0.000004158 sum to exactly 0.000016632, compared as an exact float equality (both
// sides derive from the SAME canonical decimal string via the same correctly-rounded
// decimal-to-float64 conversion, so this is not a tolerance-masked flaky assertion).
func TestSumIdentitySpendSince(t *testing.T) {
	pool := migratedPool(t)
	ctx := ownerCtx()

	idA := seedIdentity(t, pool, "sum")
	conv := seedConversationForIdentity(t, pool, idA)

	now := time.Now().UTC()
	for i := 1; i <= 4; i++ {
		seedCacheMetric(t, pool, conv, i, 0.000004158, now)
	}

	reader := NewPgSpendReader(pool)
	got, err := reader.PeriodSpend(ctx, idA, "monthly")
	if err != nil {
		t.Fatalf("PeriodSpend: %v", err)
	}
	const want = 0.000016632
	if got != want {
		t.Fatalf("PeriodSpend = %v, want exactly %v (M-09)", got, want)
	}
}

// TestSumIdentitySpendIsolatesIdentities proves B's rows are never in A's sum (CRED-06
// "Adjacency" probe): two identities spending simultaneously produce two independent
// figures.
func TestSumIdentitySpendIsolatesIdentities(t *testing.T) {
	pool := migratedPool(t)
	ctx := ownerCtx()

	idA := seedIdentity(t, pool, "isoA")
	idB := seedIdentity(t, pool, "isoB")
	convA := seedConversationForIdentity(t, pool, idA)
	convB := seedConversationForIdentity(t, pool, idB)

	now := time.Now().UTC()
	seedCacheMetric(t, pool, convA, 1, 1.50, now)
	seedCacheMetric(t, pool, convB, 1, 9.99, now)

	reader := NewPgSpendReader(pool)
	gotA, err := reader.PeriodSpend(ctx, idA, "monthly")
	if err != nil {
		t.Fatalf("PeriodSpend(A): %v", err)
	}
	if gotA < 1.50-1e-9 || gotA > 1.50+1e-9 {
		t.Errorf("PeriodSpend(A) = %v, want ~1.50 (B's spend must not leak in)", gotA)
	}
	gotB, err := reader.PeriodSpend(ctx, idB, "monthly")
	if err != nil {
		t.Fatalf("PeriodSpend(B): %v", err)
	}
	if gotB < 9.99-1e-9 || gotB > 9.99+1e-9 {
		t.Errorf("PeriodSpend(B) = %v, want ~9.99 (A's spend must not leak in)", gotB)
	}
}

// TestSumIdentitySpendEmptyIsZeroNotError proves an identity with no cache_metrics rows
// reads as a real zero and a nil error (CRED-06 "Empty" probe) -- never an error, and
// never confused with the CRED-09 local-backend exemption, which this function does not
// decide (that is credit_api.go's job, one layer up).
func TestSumIdentitySpendEmptyIsZeroNotError(t *testing.T) {
	pool := migratedPool(t)
	ctx := ownerCtx()

	idA := seedIdentity(t, pool, "empty")

	reader := NewPgSpendReader(pool)
	got, err := reader.PeriodSpend(ctx, idA, "monthly")
	if err != nil {
		t.Fatalf("PeriodSpend on an identity with no rows: unexpected error %v", err)
	}
	if got != 0 {
		t.Fatalf("PeriodSpend on an identity with no rows = %v, want exactly 0", got)
	}
}

// TestSpendPeriodBoundaryFollowsResetInterval proves the window start is derived from
// the stored reset interval rather than assumed monthly (CRED-06 "Ordering" probe): a
// row just before the weekly boundary is excluded, one just after is included. Calls
// windowStart directly (same package) to compute the REAL current boundary rather than
// injecting a fake clock, since PeriodSpend itself always evaluates against time.Now().
func TestSpendPeriodBoundaryFollowsResetInterval(t *testing.T) {
	pool := migratedPool(t)
	ctx := ownerCtx()

	idA := seedIdentity(t, pool, "boundary")
	conv := seedConversationForIdentity(t, pool, idA)

	weekStart := windowStart(time.Now().UTC(), "weekly")
	before := weekStart.Add(-1 * time.Second)
	after := weekStart.Add(1 * time.Second)

	seedCacheMetric(t, pool, conv, 1, 100.00, before) // excluded: before the window
	seedCacheMetric(t, pool, conv, 2, 1.00, after)    // included: inside the window

	reader := NewPgSpendReader(pool)
	got, err := reader.PeriodSpend(ctx, idA, "weekly")
	if err != nil {
		t.Fatalf("PeriodSpend: %v", err)
	}
	if got < 1.00-1e-9 || got > 1.00+1e-9 {
		t.Fatalf("PeriodSpend(weekly) = %v, want ~1.00 (the pre-boundary 100.00 row must be excluded)", got)
	}

	// The SAME two rows under a monthly window prove the interval genuinely changes
	// which rows are in-window, not just that "weekly" happens to work -- PROVIDED the
	// pre-boundary "before" row did not itself cross into the PREVIOUS calendar month
	// (rare: only when the current week's Monday falls in the previous month from
	// "now"). Guarded rather than asserted unconditionally, so this test is never
	// calendar-date flaky.
	monthStart := windowStart(time.Now().UTC(), "monthly")
	if !before.Before(monthStart) {
		gotMonthly, err := reader.PeriodSpend(ctx, idA, "monthly")
		if err != nil {
			t.Fatalf("PeriodSpend(monthly): %v", err)
		}
		if gotMonthly < 101.00-1e-9 || gotMonthly > 101.00+1e-9 {
			t.Fatalf("PeriodSpend(monthly) = %v, want ~101.00 (both rows in-window under the wider interval)", gotMonthly)
		}
	}
}
