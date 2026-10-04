//go:build db_integration

package db

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

// 0136 retires the asset statuses searchable and embedding, which nothing writes, and their
// searchable_at column. A row still holding either is not guessed at: the migration stops,
// and the recovery its message names -- settle the row, mark the tracker clean at 135 --
// is what this test follows.
func TestMigrate0136RetiresSearchableAndStopsOnStrandedRows(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	admin, migrateURL, _ := fresh0093Database(t, ctx, "aura_migrate0136_retire")
	migrateToVersion(t, ctx, migrateURL, admin, 135)

	stranded := seed0132Asset(t, ctx, admin, "searchable", "")
	err := MigrateSteps(ctx, migrateURL, 1)
	if err == nil || !strings.Contains(err.Error(), "migration 0136: 1 aura.assets rows") {
		t.Fatalf("migrate 0136 over a searchable row = %v, want the stranded-row refusal", err)
	}
	if !columnExists(t, ctx, admin, "searchable_at") {
		t.Fatal("the refused migration still dropped searchable_at; it must apply nothing")
	}

	if _, err := admin.Exec(ctx, `UPDATE aura.assets SET status = 'complete' WHERE id = $1`, stranded); err != nil {
		t.Fatalf("settle stranded row: %v", err)
	}
	if _, err := admin.Exec(ctx, `UPDATE schema_migrations SET version = 135, dirty = false`); err != nil {
		t.Fatalf("mark tracker clean: %v", err)
	}
	if err := MigrateSteps(ctx, migrateURL, 1); err != nil {
		t.Fatalf("migrate 0136 up after settling: %v", err)
	}
	if columnExists(t, ctx, admin, "searchable_at") {
		t.Fatal("searchable_at survived 0136")
	}
	for _, status := range []string{"searchable", "embedding"} {
		if _, err := admin.Exec(ctx, `UPDATE aura.assets SET status = $2 WHERE id = $1`, stranded, status); err == nil {
			t.Errorf("the CHECK still admits %q after 0136", status)
		}
	}

	if err := MigrateSteps(ctx, migrateURL, -1); err != nil {
		t.Fatalf("migrate 0136 down: %v", err)
	}
	if !columnExists(t, ctx, admin, "searchable_at") {
		t.Fatal("0136 down did not restore searchable_at")
	}
	if _, err := admin.Exec(ctx, `UPDATE aura.assets SET status = 'searchable' WHERE id = $1`, stranded); err != nil {
		t.Fatalf("0136 down did not restore the searchable status: %v", err)
	}
	if _, err := admin.Exec(ctx, `UPDATE aura.assets SET status = 'complete' WHERE id = $1`, stranded); err != nil {
		t.Fatalf("reset row before re-applying: %v", err)
	}
	if err := MigrateSteps(ctx, migrateURL, 1); err != nil {
		t.Fatalf("migrate 0136 up again: %v", err)
	}
	if _, err := admin.Exec(ctx, `INSERT INTO aura.assets (id, identity_id, source_kind, scope, modality, status,
		file_name, mime_type, object_bucket, object_key) VALUES ($1, $2, 'web', 'thread', 'document', 'complete',
		'notes.txt', 'text/plain', 'bucket', $3)`, uuid.NewString(), seededOperatorIdentity, "chat/"+uuid.NewString()); err != nil {
		t.Fatalf("a complete asset is refused after 0136: %v", err)
	}
}

func columnExists(t *testing.T, ctx context.Context, admin *pgxpool.Pool, column string) bool {
	t.Helper()
	var exists bool
	if err := admin.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM information_schema.columns
		WHERE table_schema = 'aura' AND table_name = 'assets' AND column_name = $1)`, column).Scan(&exists); err != nil {
		t.Fatalf("read column %s: %v", column, err)
	}
	return exists
}
