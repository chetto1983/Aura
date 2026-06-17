//go:build db_integration

// Integration coverage for migration 0017 (D-09 conversation branch trees, CHAT-05):
// the additive parent_seq/branch_id pointers + the canonical default backfill + the
// down/up round-trip. Runs on a throwaway database so the shared `aura` DB (migrated by
// sibling tests) is untouched, mirroring TestMigrate_Phase4_AppliesAndSeeds.
//
// Run via: go test -tags db_integration ./internal/db -run TestMigrate0017 -count=1
//
// Asserts (25-06 Task 2 behavior):
//   - migrate up to HEAD then step down to pre-0017, MigrateSteps(+1) re-up is clean
//     (no CREATE INDEX CONCURRENTLY in a tx block), then latest HEAD can re-apply
//   - existing conversation_turns rows are defaulted into ONE canonical linear branch
//     (branch_id = all-zero sentinel, parent_seq = previous seq; root seq=1 has NULL)
//   - the down DROPs the columns (re-up restores them) — the pointers are a pure overlay

package db

import (
	"context"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
)

func TestMigrate0017_BranchPointersBackfillAndRoundTrip(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()

	pwd := envOrSkip(t, "POSTGRES_PASSWORD")
	host := os.Getenv("PGHOST")
	if host == "" {
		host = "127.0.0.1"
	}
	port := os.Getenv("PGPORT")
	if port == "" {
		port = "5432"
	}
	if err := EnsureRoles(ctx, bootstrapURL(t), pwd); err != nil {
		t.Fatalf("EnsureRoles: %v", err)
	}

	const freshDB = "aura_migrate0017_drill"
	dsn := func(role, db string) string {
		return fmt.Sprintf("postgres://%s:%s@%s:%s/%s?sslmode=disable", role, pwd, host, port, db)
	}

	admin, err := Open(ctx, &Config{URL: dsn("aura", "aura")})
	if err != nil {
		t.Fatalf("open admin pool: %v", err)
	}
	defer admin.Close()
	if _, err := admin.Exec(ctx, "DROP DATABASE IF EXISTS "+freshDB+" WITH (FORCE)"); err != nil {
		t.Fatalf("pre-drop fresh db: %v", err)
	}
	if _, err := admin.Exec(ctx, "CREATE DATABASE "+freshDB); err != nil {
		t.Fatalf("create fresh db: %v", err)
	}
	t.Cleanup(func() {
		_, _ = admin.Exec(context.Background(), "DROP DATABASE IF EXISTS "+freshDB+" WITH (FORCE)")
	})
	if _, err := admin.Exec(ctx, "GRANT CREATE ON DATABASE "+freshDB+" TO aura_migrate"); err != nil {
		t.Fatalf("grant create on fresh db: %v", err)
	}
	freshAdmin, err := Open(ctx, &Config{URL: dsn("aura", freshDB)})
	if err != nil {
		t.Fatalf("open fresh-db admin pool: %v", err)
	}
	defer freshAdmin.Close()
	if _, err := freshAdmin.Exec(ctx, "GRANT CREATE ON SCHEMA public TO aura_migrate"); err != nil {
		t.Fatalf("grant create on public schema: %v", err)
	}

	migrateURL := dsn("aura_migrate", freshDB)
	if _, err := Migrate(ctx, migrateURL); err != nil {
		t.Fatalf("full Migrate up on fresh db: %v", err)
	}
	headVersion := currentMigrationVersion(t, ctx, freshAdmin)
	if headVersion < 17 {
		t.Fatalf("full Migrate up reached version %d, want at least 17", headVersion)
	}
	stepsToBefore0017 := headVersion - 16

	// Seed a conversation + three linear turns on the app pool (post-0017 the inserts
	// rely on the column DEFAULTs leaving branch_id/parent_seq populated by 0017's
	// backfill only for PRE-EXISTING rows; rows inserted AFTER 0017 take the constant
	// branch_id default and a NULL parent_seq, so we set parent_seq explicitly here to
	// model a canonical linear chain — the backfill assertion below targets the
	// migration-applied default on a freshly-migrated chain).
	app, err := Open(ctx, &Config{URL: dsn("aura_app", freshDB)})
	if err != nil {
		t.Fatalf("open app pool: %v", err)
	}
	defer app.Close()

	const convID = "11111111-1111-1111-1111-111111111111"
	const seededIdentity = "00000000-0000-0000-0000-000000000001" // 0004 seed
	if _, err := app.Exec(ctx,
		"INSERT INTO aura.conversations (id, identity_id, model) VALUES ($1, $2, 'test-model')",
		convID, seededIdentity,
	); err != nil {
		t.Fatalf("seed conversation: %v", err)
	}

	// Roll 0017 DOWN, insert a row WITHOUT the branch columns (proves the down truly
	// dropped them), then re-apply 0017 and run the canonical backfill assertion against
	// the migration's UPDATE — by emulating a pre-0017 deployment: the rows exist before
	// 0017's columns/backfill run.
	if err := MigrateSteps(ctx, migrateURL, -stepsToBefore0017); err != nil {
		t.Fatalf("MigrateSteps(%d) down to pre-0017: %v", -stepsToBefore0017, err)
	}
	// The columns must be gone after the down.
	var hasBranchCol bool
	if err := app.QueryRow(ctx,
		`SELECT EXISTS (SELECT 1 FROM information_schema.columns
		   WHERE table_schema='aura' AND table_name='conversation_turns' AND column_name='branch_id')`,
	).Scan(&hasBranchCol); err != nil {
		t.Fatalf("check branch_id absent after down: %v", err)
	}
	if hasBranchCol {
		t.Fatal("after MigrateSteps(-1): branch_id column still present — down did not drop it")
	}

	// Insert three PRE-0017-shaped turns (no branch columns exist yet).
	for seq := 1; seq <= 3; seq++ {
		role := "user"
		if seq == 2 {
			role = "assistant"
		}
		if _, err := app.Exec(ctx,
			"INSERT INTO aura.conversation_turns (conversation_id, seq, role, content) VALUES ($1, $2, $3, $4)",
			convID, seq, role, fmt.Sprintf("turn-%d", seq),
		); err != nil {
			t.Fatalf("seed pre-0017 turn %d: %v", seq, err)
		}
	}

	// Re-apply 0017: the ADD COLUMN DEFAULTs + the canonical backfill UPDATE run over the
	// three existing rows.
	if err := MigrateSteps(ctx, migrateURL, 1); err != nil {
		t.Fatalf("MigrateSteps(+1) re-up 0017: %v", err)
	}

	// Re-apply any newer migrations, then a second Migrate must be a no-op. This keeps
	// the 0017 drill stable as HEAD advances beyond 0017.
	if _, err := Migrate(ctx, migrateURL); err != nil {
		t.Fatalf("post-round-trip Migrate to HEAD: %v", err)
	}
	n, err := Migrate(ctx, migrateURL)
	if err != nil {
		t.Fatalf("post-round-trip no-op Migrate: %v", err)
	}
	if n != 0 {
		t.Errorf("post-round-trip no-op Migrate: want 0 pending (0017 reversible), got %d", n)
	}

	// Canonical default backfill: every existing turn lands on the all-zero canonical
	// branch; parent_seq = seq-1 with the root (seq=1) NULL.
	const canonicalBranch = "00000000-0000-0000-0000-000000000000"
	type want struct {
		parentSeqValid bool
		parentSeq      int32
	}
	wants := map[int]want{
		1: {parentSeqValid: false},
		2: {parentSeqValid: true, parentSeq: 1},
		3: {parentSeqValid: true, parentSeq: 2},
	}
	rows, err := app.Query(ctx,
		"SELECT seq, branch_id, parent_seq FROM aura.conversation_turns WHERE conversation_id = $1 ORDER BY seq ASC",
		convID,
	)
	if err != nil {
		t.Fatalf("read backfilled turns: %v", err)
	}
	defer rows.Close()
	seen := 0
	for rows.Next() {
		var seq int32
		var branchID pgtype.UUID
		var parentSeq pgtype.Int4
		if err := rows.Scan(&seq, &branchID, &parentSeq); err != nil {
			t.Fatalf("scan backfilled turn: %v", err)
		}
		seen++
		gotBranch := fmt.Sprintf("%x-%x-%x-%x-%x",
			branchID.Bytes[0:4], branchID.Bytes[4:6], branchID.Bytes[6:8], branchID.Bytes[8:10], branchID.Bytes[10:16])
		if gotBranch != canonicalBranch {
			t.Errorf("seq %d: branch_id = %s, want canonical %s", seq, gotBranch, canonicalBranch)
		}
		w, ok := wants[int(seq)]
		if !ok {
			t.Errorf("unexpected seq %d", seq)
			continue
		}
		if parentSeq.Valid != w.parentSeqValid {
			t.Errorf("seq %d: parent_seq Valid = %v, want %v", seq, parentSeq.Valid, w.parentSeqValid)
		}
		if w.parentSeqValid && parentSeq.Int32 != w.parentSeq {
			t.Errorf("seq %d: parent_seq = %d, want %d", seq, parentSeq.Int32, w.parentSeq)
		}
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("iterate backfilled turns: %v", err)
	}
	if seen != 3 {
		t.Errorf("backfilled turns: want 3, got %d", seen)
	}
}

func currentMigrationVersion(t *testing.T, ctx context.Context, db *pgxpool.Pool) int {
	t.Helper()
	var version int
	if err := db.QueryRow(ctx, "SELECT version FROM schema_migrations").Scan(&version); err != nil {
		t.Fatalf("read schema_migrations version: %v", err)
	}
	return version
}
