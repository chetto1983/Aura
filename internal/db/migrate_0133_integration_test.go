//go:build db_integration

package db

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

// 0133 makes an ingestion job's events leave with it. They used to stay with job_id NULL,
// naming a job that no longer exists and read by no query: 384 of the lab VM's 406 events on
// 2026-09-28. The up migration also removes the events already orphaned; the down migration
// puts SET NULL back and cannot bring those back.
func TestMigrate0133IngestionEventsLeaveWithTheirJob(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	admin, migrateURL, _ := fresh0093Database(t, ctx, "aura_migrate0133_events")
	migrateToVersion(t, ctx, migrateURL, admin, 132)

	orphaned := seed0133Job(t, ctx, admin)
	kept := seed0133Job(t, ctx, admin)
	deleteJob0133(t, ctx, admin, orphaned)
	if got := count0133Events(t, ctx, admin, orphaned); got != 1 {
		t.Fatalf("events of a job deleted before 0133 = %d, want the one SET NULL left", got)
	}

	if err := MigrateSteps(ctx, migrateURL, 1); err != nil {
		t.Fatalf("migrate 0133 up: %v", err)
	}
	if got := count0133Events(t, ctx, admin, orphaned); got != 0 {
		t.Fatalf("orphaned events after 0133 = %d, want them removed", got)
	}
	deleteJob0133(t, ctx, admin, kept)
	if got := count0133Events(t, ctx, admin, kept); got != 0 {
		t.Fatalf("events of a job deleted after 0133 = %d, want them gone with it", got)
	}

	if err := MigrateSteps(ctx, migrateURL, -1); err != nil {
		t.Fatalf("migrate 0133 down: %v", err)
	}
	restored := seed0133Job(t, ctx, admin)
	deleteJob0133(t, ctx, admin, restored)
	if got := count0133Events(t, ctx, admin, restored); got != 1 {
		t.Fatalf("events of a job deleted after 0133 down = %d, want SET NULL back", got)
	}
	if err := MigrateSteps(ctx, migrateURL, 1); err != nil {
		t.Fatalf("migrate 0133 up again: %v", err)
	}
}

// seed0133Job writes an ingestion job with the one event every job transition records: the
// job named as both the entity and the job.
func seed0133Job(t *testing.T, ctx context.Context, admin *pgxpool.Pool) string {
	t.Helper()
	id := uuid.NewString()
	if _, err := admin.Exec(ctx, `
INSERT INTO aura.ingestion_jobs (id, job_type, status, idempotency_key, identity_id)
VALUES ($1, 'asset_process', 'succeeded', $2, $3)`, id, "event-test:"+id, seededOperatorIdentity); err != nil {
		t.Fatalf("seed job: %v", err)
	}
	if _, err := admin.Exec(ctx, `
INSERT INTO aura.ingestion_events (entity_type, entity_id, job_id, event_type, identity_id)
VALUES ('ingestion_job', $1, $1, 'job_succeeded', $2)`, id, seededOperatorIdentity); err != nil {
		t.Fatalf("seed event: %v", err)
	}
	return id
}

func deleteJob0133(t *testing.T, ctx context.Context, admin *pgxpool.Pool, id string) {
	t.Helper()
	if _, err := admin.Exec(ctx, `DELETE FROM aura.ingestion_jobs WHERE id = $1`, id); err != nil {
		t.Fatalf("delete job: %v", err)
	}
}

func count0133Events(t *testing.T, ctx context.Context, admin *pgxpool.Pool, jobID string) int {
	t.Helper()
	var n int
	if err := admin.QueryRow(ctx, `SELECT count(*) FROM aura.ingestion_events WHERE entity_id = $1`, jobID).Scan(&n); err != nil {
		t.Fatalf("count events: %v", err)
	}
	return n
}
