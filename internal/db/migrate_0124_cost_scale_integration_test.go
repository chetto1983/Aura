//go:build db_integration

// Integration coverage for migration 0124 (widen the two money columns, T-02-36 /
// CRED-06 / phase 02 plan 07's checkpoint): aura.cache_metrics.cost_usd and
// aura.conversations.total_cost_usd move from numeric(10,4) to numeric(24,12) so a
// per-call cost as small as the measured 0.000004158 (02-CONTEXT.md M-09) survives
// instead of rounding to 0.0000.
//
// Asserts the BEFORE state as well as the AFTER: a migration test that only checks
// the after-state passes on a database that never had the bug (plan 02-07-PLAN.md
// Task 1's own acceptance criteria). Also asserts the .down.sql runs cleanly and
// narrows the column back to scale 4.
//
// Run via: go test -tags db_integration -race ./internal/db -run TestMigrate0124 -count=1

package db

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

// measuredPerCallCost is 02-CONTEXT.md M-09's measured live per-call cost -- seven
// fractional digits, which is exactly what numeric(10,4) cannot hold and
// numeric(24,12) can.
const measuredPerCallCost = 0.000004158

func TestMigrate0124_WidensCostScale(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	admin, migrateURL, _ := fresh0093Database(t, ctx, "aura_migrate0124_costscale")

	if _, err := Migrate(ctx, migrateURL); err != nil {
		t.Fatalf("migrate fresh database to head: %v", err)
	}
	headVersion := currentMigrationVersion(t, ctx, admin)
	if headVersion < 124 {
		t.Fatalf("full Migrate up reached version %d, want at least 124", headVersion)
	}

	// Step down to exactly the pre-0124 world (mirrors migrate_0121_integration_test.go's
	// MigrationStepsAbove future-proofing: this counts every migration above 123
	// regardless of how far HEAD has since advanced past 124).
	stepsAbove123, err := MigrationStepsAbove(123)
	if err != nil {
		t.Fatalf("MigrationStepsAbove(123): %v", err)
	}
	if err := MigrateSteps(ctx, migrateURL, -stepsAbove123); err != nil {
		t.Fatalf("MigrateSteps(%d) down to pre-0124: %v", -stepsAbove123, err)
	}
	if got := currentMigrationVersion(t, ctx, admin); got != 123 {
		t.Fatalf("version after stepping down = %d, want 123", got)
	}
	assertColumnScale(t, ctx, admin, "cache_metrics", "cost_usd", 10, 4)
	assertColumnScale(t, ctx, admin, "conversations", "total_cost_usd", 10, 4)

	// BEFORE: seed a conversation + one cache_metrics row at the measured cost, at the
	// pre-0124 scale, and prove the bug is REAL -- both money columns round it to zero.
	convID := uuid.New()
	if _, err := admin.Exec(ctx,
		"INSERT INTO aura.conversations (id, identity_id, total_cost_usd) VALUES ($1, $2, $3)",
		convID, seededOperatorIdentity, measuredPerCallCost,
	); err != nil {
		t.Fatalf("seed conversation: %v", err)
	}
	if _, err := admin.Exec(ctx,
		"INSERT INTO aura.cache_metrics (conversation_id, seq, cost_usd) VALUES ($1, 1, $2)",
		convID, measuredPerCallCost,
	); err != nil {
		t.Fatalf("seed cache_metrics row: %v", err)
	}
	assertNumericText(t, ctx, admin,
		"SELECT total_cost_usd::text FROM aura.conversations WHERE id = $1", convID, "0.0000",
		"conversations.total_cost_usd BEFORE 0124 must round the measured cost to zero (the bug being fixed)")
	assertNumericText(t, ctx, admin,
		"SELECT cost_usd::text FROM aura.cache_metrics WHERE conversation_id = $1 AND seq = 1", convID, "0.0000",
		"cache_metrics.cost_usd BEFORE 0124 must round the measured cost to zero (the bug being fixed)")

	// UP: migrate 0124.
	if err := MigrateSteps(ctx, migrateURL, 1); err != nil {
		t.Fatalf("migrate 0124 up: %v", err)
	}
	if got := currentMigrationVersion(t, ctx, admin); got != 124 {
		t.Fatalf("version after up = %d, want 124", got)
	}
	assertColumnScale(t, ctx, admin, "cache_metrics", "cost_usd", 24, 12)
	assertColumnScale(t, ctx, admin, "conversations", "total_cost_usd", 24, 12)

	// AFTER: a FRESH row (the widened column cannot recover precision already lost by
	// the BEFORE insert above -- that is exactly what the .down.sql comment documents
	// as one-way) at the same measured cost now stores and reads back EXACTLY.
	convID2 := uuid.New()
	if _, err := admin.Exec(ctx,
		"INSERT INTO aura.conversations (id, identity_id, total_cost_usd) VALUES ($1, $2, $3)",
		convID2, seededOperatorIdentity, measuredPerCallCost,
	); err != nil {
		t.Fatalf("seed post-migration conversation: %v", err)
	}
	if _, err := admin.Exec(ctx,
		"INSERT INTO aura.cache_metrics (conversation_id, seq, cost_usd) VALUES ($1, 1, $2)",
		convID2, measuredPerCallCost,
	); err != nil {
		t.Fatalf("seed post-migration cache_metrics row: %v", err)
	}
	assertNumericText(t, ctx, admin,
		"SELECT total_cost_usd::text FROM aura.conversations WHERE id = $1", convID2, "0.000004158000",
		"conversations.total_cost_usd AFTER 0124 must hold the measured cost exactly")
	assertNumericText(t, ctx, admin,
		"SELECT cost_usd::text FROM aura.cache_metrics WHERE conversation_id = $1 AND seq = 1", convID2, "0.000004158000",
		"cache_metrics.cost_usd AFTER 0124 must hold the measured cost exactly")

	// DOWN: 0124 reverses cleanly and the column is back to scale 4.
	if err := MigrateSteps(ctx, migrateURL, -1); err != nil {
		t.Fatalf("migrate 0124 down: %v", err)
	}
	if got := currentMigrationVersion(t, ctx, admin); got != 123 {
		t.Fatalf("version after down = %d, want 123", got)
	}
	assertColumnScale(t, ctx, admin, "cache_metrics", "cost_usd", 10, 4)
	assertColumnScale(t, ctx, admin, "conversations", "total_cost_usd", 10, 4)
}

// assertColumnScale reads information_schema.columns for table/column and fails the
// test unless numeric_precision/numeric_scale match exactly.
func assertColumnScale(t *testing.T, ctx context.Context, admin *pgxpool.Pool, table, column string, wantPrecision, wantScale int) {
	t.Helper()
	var precision, scale int
	if err := admin.QueryRow(ctx,
		`SELECT numeric_precision, numeric_scale FROM information_schema.columns
		 WHERE table_schema = 'aura' AND table_name = $1 AND column_name = $2`,
		table, column,
	).Scan(&precision, &scale); err != nil {
		t.Fatalf("read column scale for aura.%s.%s: %v", table, column, err)
	}
	if precision != wantPrecision || scale != wantScale {
		t.Fatalf("aura.%s.%s = numeric(%d,%d), want numeric(%d,%d)", table, column, precision, scale, wantPrecision, wantScale)
	}
}

// assertNumericText runs query (which must already cast its numeric column to ::text)
// with a single uuid arg and compares the exact decimal string to want -- a
// decimal-string comparison, never a float tolerance, per the plan's own instruction
// that this exactness assertion must be exact.
func assertNumericText(t *testing.T, ctx context.Context, admin *pgxpool.Pool, query string, arg uuid.UUID, want, msg string) {
	t.Helper()
	var got string
	if err := admin.QueryRow(ctx, query, arg).Scan(&got); err != nil {
		t.Fatalf("%s: query failed: %v", msg, err)
	}
	if got != want {
		t.Fatalf("%s: got %s, want %s", msg, got, want)
	}
}
