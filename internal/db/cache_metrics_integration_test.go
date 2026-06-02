//go:build db_integration

// Integration tests for the Phase-6 aura.cache_metrics surface (migration 0007 +
// cachemetrics.Store). Requires a running Postgres with the migrations applied through
// 0007:
//
//	make db-up && aura db migrate        # or the WSL equivalent
//	AURA_DB_URL + AURA_DB_MIGRATE_URL + POSTGRES_PASSWORD set in env
//
// Run via:
//
//	go test -tags db_integration -race ./internal/db -run TestCacheMetrics -count=1
//
// No-skip-as-green: envOrSkip (db_test.go) t.Fatals under $CI when the DSN is unset, so a
// skipped run can never pass green in the pipeline.
package db

import (
	"context"
	"testing"
	"time"

	"github.com/chetto1983/aura/internal/cachemetrics"
	"github.com/chetto1983/aura/internal/db/sqlc"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
)

// seededLocalIdentity is the fixed (local/system) identity 0004 seeds; it owns the test
// conversations so the cache_metrics FK resolves.
const seededLocalIdentity = "00000000-0000-0000-0000-000000000001"

// cacheMetricsPool migrates through 0007 and returns an aura_app pool, mirroring the
// internal/conversations integration harness.
func cacheMetricsPool(t *testing.T) *pgxpool.Pool {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	pwd := envOrSkip(t, "POSTGRES_PASSWORD")
	migrateURL := envOrSkip(t, "AURA_DB_MIGRATE_URL")
	appURL := envOrSkip(t, "AURA_DB_URL")

	if err := EnsureRoles(ctx, bootstrapURL(t), pwd); err != nil {
		t.Fatalf("EnsureRoles: %v", err)
	}
	if _, err := Migrate(ctx, migrateURL); err != nil {
		t.Fatalf("Migrate: %v", err)
	}
	pool, err := Open(ctx, &Config{URL: appURL})
	if err != nil {
		t.Fatalf("Open (aura_app): %v", err)
	}
	t.Cleanup(pool.Close)
	return pool
}

// newConversationForMetrics inserts a throwaway active conversation (owned by the seeded
// local identity) so the cache_metrics FK resolves, and registers its cleanup (the
// ON DELETE CASCADE removes its metric rows).
func newConversationForMetrics(t *testing.T, pool *pgxpool.Pool) string {
	t.Helper()
	ctx := context.Background()
	id := uuid.Must(uuid.NewV7()).String()
	if _, err := pool.Exec(ctx,
		"INSERT INTO aura.conversations (id, identity_id, model, status) VALUES ($1, $2, 'test-model', 'active')",
		id, seededLocalIdentity,
	); err != nil {
		t.Fatalf("seed conversation: %v", err)
	}
	t.Cleanup(func() {
		_, _ = pool.Exec(context.Background(), "DELETE FROM aura.conversations WHERE id = $1", id)
	})
	return id
}

// insertMetricAt inserts a cache_metrics row with an explicit ts so the window assertions
// can place rows on both sides of a --since cutoff (the generated InsertCacheMetric uses
// the now() default and is exercised separately in TestCacheMetrics_StoreInsert).
func insertMetricAt(t *testing.T, pool *pgxpool.Pool, convID string, seq int, ts time.Time, prompt, cached int, cost float64) {
	t.Helper()
	if _, err := pool.Exec(context.Background(),
		"INSERT INTO aura.cache_metrics (conversation_id, seq, ts, prompt_tokens, cached_tokens, cost_usd) VALUES ($1, $2, $3, $4, $5, $6)",
		convID, int32(seq), ts, int32(prompt), int32(cached), cost,
	); err != nil {
		t.Fatalf("insert metric seq %d: %v", seq, err)
	}
}

// TestCacheMetrics_WindowAndAggregate proves SC#4's query side: ListSince returns only
// in-window rows in ts ASC order, and AggregateSince sums prompt/cached/cost over the
// window. Rows are placed across a ts spread spanning the cutoff.
func TestCacheMetrics_WindowAndAggregate(t *testing.T) {
	pool := cacheMetricsPool(t)
	store := cachemetrics.New(pool)
	convID := newConversationForMetrics(t, pool)
	ctx := context.Background()

	base := time.Now().UTC().Add(24 * time.Hour).Truncate(time.Microsecond)
	cutoff := base.Add(-1 * time.Hour)

	// Two rows OUTSIDE the window (older than cutoff), three INSIDE — inserted out of
	// ts order so the ASC ordering assertion is meaningful.
	insertMetricAt(t, pool, convID, 1, cutoff.Add(-2*time.Hour), 1000, 0, 0.0100)   // out
	insertMetricAt(t, pool, convID, 2, cutoff.Add(-1*time.Hour), 2000, 500, 0.0200) // out
	insertMetricAt(t, pool, convID, 5, base.Add(-5*time.Minute), 5000, 4000, 0.0500)
	insertMetricAt(t, pool, convID, 3, cutoff.Add(15*time.Minute), 3000, 1500, 0.0300)
	insertMetricAt(t, pool, convID, 4, cutoff.Add(30*time.Minute), 4000, 2000, 0.0400)

	got, err := store.ListSince(ctx, cutoff)
	if err != nil {
		t.Fatalf("ListSince: %v", err)
	}
	// Filter to this conversation so a shared DB (other tests' rows) does not pollute.
	var window []cachemetrics.Metric
	for _, m := range got {
		if m.ConversationID == convID {
			window = append(window, m)
		}
	}
	if len(window) != 3 {
		t.Fatalf("ListSince: want 3 in-window rows for conv, got %d", len(window))
	}
	// ASC by ts: seq 3 (cutoff+15m), 4 (cutoff+30m), 5 (base-5m).
	wantSeq := []int{3, 4, 5}
	for i, m := range window {
		if m.Seq != wantSeq[i] {
			t.Errorf("ListSince row %d: want seq %d, got %d (ts %s)", i, wantSeq[i], m.Seq, m.TS)
		}
		if i > 0 && m.TS.Before(window[i-1].TS) {
			t.Errorf("ListSince not ts-ASC: row %d ts %s before row %d ts %s", i, m.TS, i-1, window[i-1].TS)
		}
	}
	// First in-window row carries its exact prompt/cached/cost (no scrambling).
	if window[0].PromptTokens != 3000 || window[0].CachedTokens != 1500 {
		t.Errorf("ListSince row 0 tokens: want 3000/1500, got %d/%d", window[0].PromptTokens, window[0].CachedTokens)
	}
	if window[0].CostUSD != 0.0300 {
		t.Errorf("ListSince row 0 cost: want 0.0300, got %v", window[0].CostUSD)
	}

	// Aggregate over the SAME future window. Ordinary now()-timestamped rows are out
	// of range, so the SQL time filter can be asserted exactly.
	agg, err := store.AggregateSince(ctx, cutoff)
	if err != nil {
		t.Fatalf("AggregateSince: %v", err)
	}
	const wantPrompt = 3000 + 4000 + 5000
	const wantCached = 1500 + 2000 + 4000
	if agg.TotalPromptTokens != wantPrompt {
		t.Errorf("AggregateSince prompt: want exactly %d, got %d", wantPrompt, agg.TotalPromptTokens)
	}
	if agg.TotalCachedTokens != wantCached {
		t.Errorf("AggregateSince cached: want exactly %d, got %d", wantCached, agg.TotalCachedTokens)
	}
	if agg.Turns != 3 {
		t.Errorf("AggregateSince turns: want exactly 3, got %d", agg.Turns)
	}
	wantCost := 0.0300 + 0.0400 + 0.0500
	if agg.TotalCostUSD < wantCost-1e-9 || agg.TotalCostUSD > wantCost+1e-9 {
		t.Errorf("AggregateSince cost: want exactly %.4f, got %v", wantCost, agg.TotalCostUSD)
	}
}

// TestCacheMetrics_StoreInsert proves the append-only INSERT path (the generated
// InsertCacheMetric via cachemetrics.Store.Insert), reading the row back to confirm the
// pgtype boundary round-trips (uuid + numeric(10,4) cost).
func TestCacheMetrics_StoreInsert(t *testing.T) {
	pool := cacheMetricsPool(t)
	store := cachemetrics.New(pool)
	convID := newConversationForMetrics(t, pool)
	ctx := context.Background()

	p, err := cachemetrics.NewInsertParams(cachemetrics.MetricParams{
		ConversationID: convID,
		Seq:            1,
		PromptTokens:   7777,
		CachedTokens:   6543,
		CostUSD:        0.1234,
	})
	if err != nil {
		t.Fatalf("NewInsertParams: %v", err)
	}
	if err := store.Insert(ctx, p); err != nil {
		t.Fatalf("Insert: %v", err)
	}

	var (
		gotConv         pgtype.UUID
		gotPrompt       int32
		gotCached       int32
		gotCost         pgtype.Numeric
		gotTSValid      bool
		gotTS           pgtype.Timestamptz
		costFloat       float64
		convUUIDMatches bool
	)
	if err := pool.QueryRow(ctx,
		"SELECT conversation_id, ts, prompt_tokens, cached_tokens, cost_usd FROM aura.cache_metrics WHERE conversation_id = $1 AND seq = 1",
		convID,
	).Scan(&gotConv, &gotTS, &gotPrompt, &gotCached, &gotCost); err != nil {
		t.Fatalf("read back: %v", err)
	}
	gotTSValid = gotTS.Valid
	convUUIDMatches = uuid.UUID(gotConv.Bytes).String() == convID
	if !convUUIDMatches {
		t.Errorf("conversation_id round-trip: want %s, got %s", convID, uuid.UUID(gotConv.Bytes).String())
	}
	if gotPrompt != 7777 || gotCached != 6543 {
		t.Errorf("tokens round-trip: want 7777/6543, got %d/%d", gotPrompt, gotCached)
	}
	if !gotTSValid {
		t.Error("ts: want a non-NULL default-now() value, got invalid")
	}
	if f, err := gotCost.Float64Value(); err == nil && f.Valid {
		costFloat = f.Float64
	}
	if costFloat != 0.1234 {
		t.Errorf("cost_usd round-trip: want 0.1234, got %v", costFloat)
	}

	// Verify InsertCacheMetricParams is the exact generated shape (compile-time guard
	// that the narrow CacheMetricStore surface matches the sqlc client).
	var _ = sqlc.InsertCacheMetricParams(p)
}
