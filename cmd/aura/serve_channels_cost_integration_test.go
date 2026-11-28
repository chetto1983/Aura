//go:build db_integration

package main

import (
	"context"
	"testing"
	"time"

	"github.com/chetto1983/aura/internal/cachemetrics"
	"github.com/chetto1983/aura/internal/channels/telegram"
	"github.com/chetto1983/aura/internal/db"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// todayCost reads the day from the real ledger: the priced turns' sum beside the count of
// turns with no honest price (a NULL cost, migration 0161). The days sit far in the future,
// so rows the rest of the suite writes at now() never fall inside them.
func TestTodayCostCountsTheTurnsItCouldNotPrice(t *testing.T) {
	pool := bootstrapMigratedPool(t)
	convID := costConversation(t, pool)
	first := time.Date(2197, 3, 1, 0, 0, 0, 0, time.Local)
	later := first.AddDate(0, 0, 1)
	day := func(at time.Time) telegram.DayUsage {
		t.Helper()
		got, err := (&todayCost{metrics: cachemetrics.New(pool), now: func() time.Time { return at.Add(12 * time.Hour) }}).TodayUsage(context.Background())
		if err != nil {
			t.Fatalf("TodayUsage: %v", err)
		}
		return got
	}

	costRow(t, pool, convID, 1, first.Add(time.Hour), nil)
	costRow(t, pool, convID, 2, first.Add(2*time.Hour), nil)
	if got := day(first); got.Turns != 2 || got.UnpricedTurns != 2 || got.CostUSD == nil || *got.CostUSD != 0 || got.PromptTokens != 200 {
		t.Fatalf("an all-unpriced day = %+v, want 2 turns, both unpriced", got)
	}
	costRow(t, pool, convID, 3, first.Add(3*time.Hour), new(0.25))
	if got := day(first); got.Turns != 3 || got.UnpricedTurns != 2 || got.CostUSD == nil || *got.CostUSD != 0.25 {
		t.Fatalf("a mixed day = %+v, want 3 turns, 2 unpriced, 0.25 priced", got)
	}
	costRow(t, pool, convID, 4, later.Add(time.Hour), new(0.5))
	costRow(t, pool, convID, 5, later.Add(2*time.Hour), new(0.125))
	if got := day(later); got.Turns != 2 || got.UnpricedTurns != 0 || got.CostUSD == nil || *got.CostUSD != 0.625 {
		t.Fatalf("a priced day = %+v, want 2 turns, none unpriced, 0.625", got)
	}
}

func costConversation(t *testing.T, pool *pgxpool.Pool) string {
	t.Helper()
	const owner = "00000000-0000-0000-0000-000000000001"
	id := uuid.Must(uuid.NewV7()).String()
	write := func(sql string) {
		if err := db.WithIdentityTxRaw(context.Background(), pool, owner, func(tx pgx.Tx) error {
			_, err := tx.Exec(context.Background(), sql, id, owner)
			return err
		}); err != nil {
			t.Fatalf("%s: %v", sql, err)
		}
	}
	write("INSERT INTO aura.conversations (id, identity_id, model, status) VALUES ($1, $2, 'test-model', 'active')")
	t.Cleanup(func() { write("DELETE FROM aura.conversations WHERE id = $1 AND identity_id = $2") })
	return id
}

func costRow(t *testing.T, pool *pgxpool.Pool, convID string, seq int, ts time.Time, cost *float64) {
	t.Helper()
	if _, err := pool.Exec(context.Background(),
		"INSERT INTO aura.cache_metrics (conversation_id, seq, ts, prompt_tokens, cost_usd) VALUES ($1, $2, $3, 100, $4)",
		convID, int32(seq), ts, cost); err != nil {
		t.Fatalf("insert metric %d: %v", seq, err)
	}
}
