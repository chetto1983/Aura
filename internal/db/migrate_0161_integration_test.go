//go:build db_integration

package db

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

// 0161 lets aura.cache_metrics.cost_usd hold NULL, the unknown cost of a turn with no honest
// price, and drops the 0 default that recorded such a turn as free. Down maps NULL back to 0.
func TestMigration0161MakesTheTurnCostNullable(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()
	admin, migrateURL, _ := fresh0093Database(t, ctx, "aura_migrate0161_cost_unknown")
	migrateToVersion(t, ctx, migrateURL, admin, 160)
	convID := uuid.New()
	if _, err := admin.Exec(ctx, "INSERT INTO aura.conversations (id, identity_id) VALUES ($1, $2)", convID, seededOperatorIdentity); err != nil {
		t.Fatalf("seed conversation: %v", err)
	}
	assertCostColumn(t, ctx, admin, "NO", "0")
	if _, err := admin.Exec(ctx, "INSERT INTO aura.cache_metrics (conversation_id, seq, cost_usd) VALUES ($1, 1, NULL)", convID); err == nil {
		t.Fatal("before 0161 a NULL cost was accepted")
	}

	if err := MigrateSteps(ctx, migrateURL, 1); err != nil {
		t.Fatalf("migrate 0161 up: %v", err)
	}
	assertCostColumn(t, ctx, admin, "YES", "")
	if _, err := admin.Exec(ctx, "INSERT INTO aura.cache_metrics (conversation_id, seq, cost_usd) VALUES ($1, 1, NULL), ($1, 2, 0.25)", convID); err != nil {
		t.Fatalf("insert an unknown and a priced cost: %v", err)
	}
	if _, err := admin.Exec(ctx, "INSERT INTO aura.cache_metrics (conversation_id, seq) VALUES ($1, 3)", convID); err != nil {
		t.Fatalf("insert without a cost: %v", err)
	}
	if unknown := nullCosts(t, ctx, admin, convID); unknown != 2 {
		t.Fatalf("after 0161 %d rows hold an unknown cost, want 2 (the NULL and the omitted one)", unknown)
	}

	if err := MigrateSteps(ctx, migrateURL, -1); err != nil {
		t.Fatalf("migrate 0161 down: %v", err)
	}
	assertCostColumn(t, ctx, admin, "NO", "0")
	if unknown := nullCosts(t, ctx, admin, convID); unknown != 0 {
		t.Fatalf("after 0161 down %d rows still hold NULL", unknown)
	}
	if err := MigrateSteps(ctx, migrateURL, 1); err != nil {
		t.Fatalf("migrate 0161 up again: %v", err)
	}
	assertCostColumn(t, ctx, admin, "YES", "")
}

func assertCostColumn(t *testing.T, ctx context.Context, admin *pgxpool.Pool, wantNullable, wantDefault string) {
	t.Helper()
	var nullable, def string
	if err := admin.QueryRow(ctx, `SELECT is_nullable, coalesce(column_default, '') FROM information_schema.columns
		WHERE table_schema = 'aura' AND table_name = 'cache_metrics' AND column_name = 'cost_usd'`).Scan(&nullable, &def); err != nil {
		t.Fatalf("read aura.cache_metrics.cost_usd: %v", err)
	}
	if nullable != wantNullable || def != wantDefault {
		t.Fatalf("cost_usd nullable=%s default=%q, want nullable=%s default=%q", nullable, def, wantNullable, wantDefault)
	}
}

func nullCosts(t *testing.T, ctx context.Context, admin *pgxpool.Pool, convID uuid.UUID) int {
	t.Helper()
	var n int
	if err := admin.QueryRow(ctx, "SELECT count(*) FROM aura.cache_metrics WHERE conversation_id = $1 AND cost_usd IS NULL", convID).Scan(&n); err != nil {
		t.Fatalf("count NULL costs: %v", err)
	}
	return n
}
