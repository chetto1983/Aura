//go:build db_integration

package db

import (
	"context"
	"errors"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
)

// TestMigration0035AssetsSourceKindAgent proves migration 0035 widens the aura.assets.source_kind
// CHECK to admit 'agent' (so assets.Service.IngestAgentFile can persist a send_file deliverable,
// WEBART-01/D-06) and that its down + re-up is clean:
//
//	(a) after HEAD an INSERT with source_kind='agent' SUCCEEDS (the widened CHECK admits it);
//	(b) positioned at v34 (before 0035) the same INSERT FAILS with a check_violation (23514);
//	(c) the 0035 down (pre-deletes agent rows, restores the 0020 CHECK) + re-up straddle is clean.
func TestMigration0035AssetsSourceKindAgent(t *testing.T) {
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

	const freshDB = "aura_migrate0035_drill"
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
	head := currentMigrationVersion(t, ctx, freshAdmin)
	if head < 35 {
		t.Fatalf("full Migrate up reached version %d, want at least 35", head)
	}

	app, err := Open(ctx, &Config{URL: dsn("aura_app", freshDB)})
	if err != nil {
		t.Fatalf("open app pool: %v", err)
	}
	defer app.Close()

	// (a) After HEAD: the widened CHECK admits an agent asset INSERT.
	if err := insertAgentAsset0035(ctx, app); err != nil {
		t.Fatalf("insert source_kind='agent' asset after HEAD: want success, got %v", err)
	}

	// Position DOWN to exactly v34 before the straddle. golang-migrate Steps(n) is RELATIVE to the
	// current head, so anchoring off `head` isolates 0035's OWN down regardless of how many
	// migrations sit above it. The 0035 down pre-deletes the agent row just inserted, so the
	// re-added narrower (0020) CHECK cannot fail on it — this exercises the down's row-cleanup too.
	stepDownToV34 := 34 - head
	if err := MigrateSteps(ctx, migrateURL, stepDownToV34); err != nil {
		t.Fatalf("MigrateSteps(%d) position down to v34: %v", stepDownToV34, err)
	}

	// (b) Before 0035 (at v34): the narrower CHECK rejects source_kind='agent' with 23514.
	err = insertAgentAsset0035(ctx, app)
	if err == nil {
		t.Fatalf("insert source_kind='agent' asset at v34: want check_violation, got success")
	}
	var pgErr *pgconn.PgError
	if !errors.As(err, &pgErr) || pgErr.Code != "23514" {
		t.Fatalf("insert source_kind='agent' asset at v34: want check_violation (23514), got %v", err)
	}

	// (c) Re-up 0035 (v34 -> v35): the widen returns; the INSERT succeeds again.
	if err := MigrateSteps(ctx, migrateURL, 1); err != nil {
		t.Fatalf("MigrateSteps(+1) re-up 0035: %v", err)
	}
	if err := insertAgentAsset0035(ctx, app); err != nil {
		t.Fatalf("insert source_kind='agent' asset after re-up 0035: want success, got %v", err)
	}

	// (c) Straddle down+up at v35: down (pre-deletes agent rows + narrows) then re-up.
	if err := MigrateSteps(ctx, migrateURL, -1); err != nil {
		t.Fatalf("MigrateSteps(-1) down 0035: %v", err)
	}
	if err := MigrateSteps(ctx, migrateURL, 1); err != nil {
		t.Fatalf("MigrateSteps(+1) re-up 0035 (straddle): %v", err)
	}
	if err := insertAgentAsset0035(ctx, app); err != nil {
		t.Fatalf("insert source_kind='agent' asset after straddle: want success, got %v", err)
	}

	// Restore to HEAD: re-applying any migrations above 0035 (none today) is a clean no-op.
	if _, err := Migrate(ctx, migrateURL); err != nil {
		t.Fatalf("post-round-trip Migrate back to HEAD: %v", err)
	}
	n, err := Migrate(ctx, migrateURL)
	if err != nil {
		t.Fatalf("post-round-trip no-op Migrate: %v", err)
	}
	if n != 0 {
		t.Errorf("post-round-trip no-op Migrate: want 0 pending (0035 reversible), got %d", n)
	}
}

// insertAgentAsset0035 inserts a minimal source_kind='agent' asset on the aura_app pool (the
// runtime role). identity_id is the `local` identity seeded by migration 0004 (fixed UUID
// 00000000-0000-0000-0000-000000000001), satisfying the assets.identity_id FK. A fresh random
// UUID id + a derived object_key keep each call independent (the assets_identity_object_key_idx
// unique index forbids a duplicate (identity_id, object_key)), so the same helper serves both the
// success (widened CHECK) and failure (narrow CHECK → 23514) assertions without a stale-row clash.
func insertAgentAsset0035(ctx context.Context, pool *pgxpool.Pool) error {
	newID := uuid.New()
	id := pgtype.UUID{Bytes: newID, Valid: true}
	objectKey := "identity/00000000-0000-0000-0000-000000000001/asset/" + newID.String() + "/original"
	_, err := pool.Exec(ctx,
		`INSERT INTO aura.assets
		    (id, identity_id, source_kind, scope, modality, status, file_name, object_bucket, object_key)
		 VALUES
		    ($1, '00000000-0000-0000-0000-000000000001', 'agent', 'thread', 'document', 'accepted', 'report.pdf', 'asset-test', $2)`,
		id, objectKey)
	return err
}
