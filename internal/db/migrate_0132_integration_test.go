//go:build db_integration

package db

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

// 0132 settles the assets the document processor left in 'processing' over a job that had
// succeeded: that status was never moved on from, because the ingest sidecar writes no
// Postgres row. A row whose job has not finished, or that has no job, is left alone: nothing
// says its processing is over.
func TestMigrate0132SettlesAssetsWhoseProcessingJobSucceeded(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	admin, migrateURL, _ := fresh0093Database(t, ctx, "aura_migrate0132_settle")
	migrateToVersion(t, ctx, migrateURL, admin, 131)

	named := seed0132Asset(t, ctx, admin, "processing", "succeeded")
	running := seed0132Asset(t, ctx, admin, "processing", "running")
	jobless := seed0132Asset(t, ctx, admin, "processing", "")
	failed := seed0132Asset(t, ctx, admin, "failed", "succeeded")

	if err := MigrateSteps(ctx, migrateURL, 1); err != nil {
		t.Fatalf("migrate 0132 up: %v", err)
	}
	want := map[string]string{named: "complete", running: "processing", jobless: "processing", failed: "failed"}
	assert0132Statuses(t, ctx, admin, want)
	var completedAt *time.Time
	if err := admin.QueryRow(ctx, `SELECT completed_at FROM aura.assets WHERE id = $1`, named).Scan(&completedAt); err != nil || completedAt == nil {
		t.Fatalf("settled asset completed_at = %v, %v; want it stamped", completedAt, err)
	}

	if err := MigrateSteps(ctx, migrateURL, -1); err != nil {
		t.Fatalf("migrate 0132 down: %v", err)
	}
	assert0132Statuses(t, ctx, admin, want)
	if err := MigrateSteps(ctx, migrateURL, 1); err != nil {
		t.Fatalf("migrate 0132 up again: %v", err)
	}
	assert0132Statuses(t, ctx, admin, want)
}

// seed0132Asset writes an asset in status with, unless jobStatus is empty, the asset_process
// job the durable worker ran for it.
func seed0132Asset(t *testing.T, ctx context.Context, admin *pgxpool.Pool, status, jobStatus string) string {
	t.Helper()
	id := uuid.NewString()
	if _, err := admin.Exec(ctx, `
INSERT INTO aura.assets (id, identity_id, source_kind, scope, modality, status, file_name, mime_type,
                         object_bucket, object_key, processed_at)
VALUES ($1, $2, 'web', 'thread', 'document', $3, 'notes.txt', 'text/plain', 'bucket', $4, now())`,
		id, seededOperatorIdentity, status, "chat/"+id+".txt"); err != nil {
		t.Fatalf("seed %s asset: %v", status, err)
	}
	if jobStatus == "" {
		return id
	}
	if _, err := admin.Exec(ctx, `
INSERT INTO aura.ingestion_jobs (job_type, status, idempotency_key, identity_id, asset_id, stage)
VALUES ('asset_process', $1, $2, $3, $4, 'accepted')`,
		jobStatus, "asset_process:"+id, seededOperatorIdentity, id); err != nil {
		t.Fatalf("seed %s job: %v", jobStatus, err)
	}
	return id
}

func assert0132Statuses(t *testing.T, ctx context.Context, admin *pgxpool.Pool, want map[string]string) {
	t.Helper()
	for id, status := range want {
		var got string
		if err := admin.QueryRow(ctx, `SELECT status FROM aura.assets WHERE id = $1`, id).Scan(&got); err != nil {
			t.Fatalf("read asset %s: %v", id, err)
		}
		if got != status {
			t.Errorf("asset %s status = %q, want %q", id, got, status)
		}
	}
}
