//go:build db_integration

package db

import (
	"context"
	"errors"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/chetto1983/aura/internal/db/sqlc"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
)

func TestMigrate0093FreshUpDownUp(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	admin, migrateURL, _ := fresh0093Database(t, ctx, "aura_migrate0093_roundtrip")

	if _, err := Migrate(ctx, migrateURL); err != nil {
		t.Fatalf("migrate fresh database to 0093: %v", err)
	}
	assert0093Schema(t, ctx, admin)

	if err := MigrateSteps(ctx, migrateURL, -1); err != nil {
		t.Fatalf("migrate 0093 down to 0092: %v", err)
	}
	if got := currentMigrationVersion(t, ctx, admin); got != 92 {
		t.Fatalf("version after down = %d, want 92", got)
	}
	var stageTable *string
	if err := admin.QueryRow(ctx, `SELECT to_regclass('aura.document_pipeline_stages')::text`).Scan(&stageTable); err != nil {
		t.Fatalf("check stage table after down: %v", err)
	}
	if stageTable != nil {
		t.Fatalf("stage table survived down: %q", *stageTable)
	}
	assertColumnExists(t, ctx, admin, "storage_objects", "status", false)

	if err := MigrateSteps(ctx, migrateURL, 1); err != nil {
		t.Fatalf("migrate 0093 up again: %v", err)
	}
	assert0093Schema(t, ctx, admin)
}

func TestMigrate0093DownBlocksAfterVerifiedObjectDeletion(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	admin, migrateURL, _ := fresh0093Database(t, ctx, "aura_migrate0093_deleted")
	if _, err := Migrate(ctx, migrateURL); err != nil {
		t.Fatalf("migrate fresh database to 0093: %v", err)
	}
	if _, err := admin.Exec(ctx, `
INSERT INTO aura.storage_objects (
    identity_id, bucket, object_key, kind, size_bytes, status, deletion_verified_at
) VALUES ($1::uuid, 'documents', 'verified-deleted', 'raw', 0, 'object_deleted', now())`, seededOperatorIdentity); err != nil {
		t.Fatalf("seed verified deletion: %v", err)
	}
	if err := MigrateSteps(ctx, migrateURL, -1); err == nil {
		t.Fatal("0093 down after verified object deletion succeeded; want forward-fix refusal")
	}
	var version int
	var dirty bool
	if err := admin.QueryRow(ctx, "SELECT version, dirty FROM schema_migrations").Scan(&version, &dirty); err != nil {
		t.Fatalf("read refused migration state: %v", err)
	}
	if version != 92 || !dirty {
		t.Fatalf("refused down state = version:%d dirty:%v, want 92/dirty", version, dirty)
	}
	assertColumnExists(t, ctx, admin, "storage_objects", "deletion_verified_at", true)
}

func TestMigrate0093OwnerScopedJobReclaimFenceAndManualRetry(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	admin, migrateURL, appURL := fresh0093Database(t, ctx, "aura_migrate0093_jobs")
	if _, err := Migrate(ctx, migrateURL); err != nil {
		t.Fatalf("migrate fresh database to 0093: %v", err)
	}
	app, err := Open(ctx, &Config{URL: appURL})
	if err != nil {
		t.Fatalf("open app pool: %v", err)
	}
	defer app.Close()
	identityID := pgtype.UUID{Bytes: uuid.MustParse(seededOperatorIdentity), Valid: true}
	interval := pgtype.Interval{Microseconds: time.Minute.Microseconds(), Valid: true}
	now := pgtype.Timestamptz{Time: time.Now().UTC(), Valid: true}

	var claimed sqlc.ClaimIngestionJobsRow
	err = WithIdentityTx(ctx, app, seededOperatorIdentity, func(q *sqlc.Queries) error {
		created, queryErr := q.CreateIngestionJob(ctx, sqlc.CreateIngestionJobParams{
			IdentityID: identityID, JobType: "convert", Status: "queued",
			IdempotencyKey: "reclaim", Stage: "convert", MaxAttempts: 5,
			NextAttemptAt: now, Payload: []byte(`{}`), PipelineGeneration: 7,
		})
		if queryErr != nil {
			return queryErr
		}
		jobs, queryErr := q.ClaimIngestionJobs(ctx, sqlc.ClaimIngestionJobsParams{
			IdentityID: identityID, BatchSize: 1,
			LockedBy: pgtype.Text{String: "worker-a", Valid: true}, LeaseDuration: interval,
		})
		if queryErr != nil {
			return queryErr
		}
		if len(jobs) != 1 || jobs[0].ID != created.ID {
			t.Fatalf("initial claim = %#v", jobs)
		}
		claimed = jobs[0]
		return nil
	})
	if err != nil {
		t.Fatalf("create and claim: %v", err)
	}
	if _, err := admin.Exec(ctx, `UPDATE aura.ingestion_jobs SET locked_until = now() - interval '1 second' WHERE id = $1`, claimed.ID); err != nil {
		t.Fatalf("expire lease: %v", err)
	}

	var reclaimed sqlc.ClaimIngestionJobsRow
	err = WithIdentityTx(ctx, app, seededOperatorIdentity, func(q *sqlc.Queries) error {
		jobs, queryErr := q.ClaimIngestionJobs(ctx, sqlc.ClaimIngestionJobsParams{
			IdentityID: identityID, BatchSize: 1,
			LockedBy: pgtype.Text{String: "worker-b", Valid: true}, LeaseDuration: interval,
		})
		if queryErr != nil {
			return queryErr
		}
		if len(jobs) != 1 {
			t.Fatalf("reclaim = %#v", jobs)
		}
		reclaimed = jobs[0]
		return nil
	})
	if err != nil {
		t.Fatalf("reclaim expired lease: %v", err)
	}
	if !reclaimed.LockedBy.Valid || reclaimed.LockedBy.String != "worker-b" {
		t.Fatalf("reclaimed worker = %#v", reclaimed)
	}
	err = WithIdentityTx(ctx, app, seededOperatorIdentity, func(q *sqlc.Queries) error {
		_, staleErr := q.UpdateIngestionJobStatus(ctx, sqlc.UpdateIngestionJobStatusParams{
			ID: claimed.ID, IdentityID: identityID,
			LockedBy:        pgtype.Text{String: "worker-a", Valid: true},
			LeaseGeneration: claimed.LeaseGeneration, Status: "succeeded", Stage: "convert",
			EventType: "job_succeeded", EventDetail: []byte(`{}`),
		})
		if !errors.Is(staleErr, pgx.ErrNoRows) {
			t.Fatalf("stale transition error = %v, want pgx.ErrNoRows", staleErr)
		}
		return nil
	})
	if err != nil {
		t.Fatalf("reject stale fence: %v", err)
	}
	err = WithIdentityTx(ctx, app, seededOperatorIdentity, func(q *sqlc.Queries) error {
		_, queryErr := q.UpdateIngestionJobStatus(ctx, sqlc.UpdateIngestionJobStatusParams{
			ID: reclaimed.ID, IdentityID: identityID,
			LockedBy:        pgtype.Text{String: "worker-b", Valid: true},
			LeaseGeneration: reclaimed.LeaseGeneration, Status: "succeeded", Stage: "convert",
			EventType: "job_succeeded", EventDetail: []byte(`{}`),
		})
		return queryErr
	})
	if err != nil {
		t.Fatalf("current fenced transition: %v", err)
	}
	if reclaimed.LeaseGeneration != claimed.LeaseGeneration+1 {
		t.Fatalf("reclaimed lease generation = %d, want %d", reclaimed.LeaseGeneration, claimed.LeaseGeneration+1)
	}

	err = WithIdentityTx(ctx, app, seededOperatorIdentity, func(q *sqlc.Queries) error {
		terminal, queryErr := q.CreateIngestionJob(ctx, sqlc.CreateIngestionJobParams{
			IdentityID: identityID, JobType: "embed", Status: "dead_letter",
			IdempotencyKey: "manual", Stage: "embed", MaxAttempts: 5,
			NextAttemptAt: now, Payload: []byte(`{}`), PipelineGeneration: 8,
		})
		if queryErr != nil {
			return queryErr
		}
		retried, queryErr := q.ManualRetryIngestionJobByKey(ctx, sqlc.ManualRetryIngestionJobByKeyParams{
			IdentityID: identityID, JobType: "embed", IdempotencyKey: "manual",
			Stage: "embed", NextAttemptAt: now,
		})
		if queryErr != nil {
			return queryErr
		}
		if retried.ID != terminal.ID || retried.Status != "queued" || retried.AttemptCount != 0 ||
			retried.AttemptGeneration != terminal.AttemptGeneration+1 ||
			retried.LeaseGeneration != terminal.LeaseGeneration+1 {
			t.Fatalf("manual retry = %#v, terminal = %#v", retried, terminal)
		}
		events, queryErr := q.ListIngestionEventsByJob(ctx, sqlc.ListIngestionEventsByJobParams{
			IdentityID: identityID, JobID: retried.ID,
		})
		if queryErr != nil {
			return queryErr
		}
		if len(events) != 1 || events[0].EventType != "job_manual_retry" {
			t.Fatalf("manual retry events = %#v", events)
		}
		return nil
	})
	if err != nil {
		t.Fatalf("manual retry: %v", err)
	}

	var invisible int
	if err := app.QueryRow(ctx, `SELECT count(*) FROM aura.ingestion_jobs`).Scan(&invisible); err != nil {
		t.Fatalf("unscoped RLS read: %v", err)
	}
	if invisible != 0 {
		t.Fatalf("unscoped RLS read exposed %d jobs", invisible)
	}
}

func assert0093Schema(t *testing.T, ctx context.Context, admin *pgxpool.Pool) {
	t.Helper()
	if got := currentMigrationVersion(t, ctx, admin); got != 93 {
		t.Fatalf("migration version = %d, want 93", got)
	}
	assertColumnExists(t, ctx, admin, "documents", "search_document_id", true)
	assertColumnExists(t, ctx, admin, "ingestion_jobs", "lease_generation", true)
	assertColumnExists(t, ctx, admin, "storage_objects", "deletion_verified_at", true)

	for _, table := range []string{
		"storage_objects", "ingestion_jobs", "ingestion_events", "delete_jobs",
		"document_ingest_jobs", "document_pipeline_stages",
	} {
		var rls bool
		if err := admin.QueryRow(ctx, `
SELECT relrowsecurity FROM pg_class
WHERE oid = ('aura.' || $1)::regclass`, table).Scan(&rls); err != nil {
			t.Fatalf("read %s RLS: %v", table, err)
		}
		if !rls {
			t.Fatalf("%s RLS is disabled", table)
		}
		var restrictive int
		if err := admin.QueryRow(ctx, `
SELECT count(*) FROM pg_policy
WHERE polrelid = ('aura.' || $1)::regclass AND NOT polpermissive`, table).Scan(&restrictive); err != nil {
			t.Fatalf("read %s policies: %v", table, err)
		}
		if restrictive == 0 {
			t.Fatalf("%s has no restrictive fail-closed policy", table)
		}
	}

	for _, constraint := range []string{
		"document_versions_document_identity_fkey",
		"storage_objects_document_identity_fkey",
		"ingestion_jobs_document_identity_fkey",
		"ingestion_events_job_identity_fkey",
		"delete_jobs_document_identity_fkey",
	} {
		var exists bool
		if err := admin.QueryRow(ctx, `SELECT EXISTS (
SELECT 1 FROM pg_constraint WHERE conname = $1 AND contype = 'f')`, constraint).Scan(&exists); err != nil {
			t.Fatalf("read constraint %s: %v", constraint, err)
		}
		if !exists {
			t.Fatalf("missing owner-coherent FK %s", constraint)
		}
	}
}

func assertColumnExists(t *testing.T, ctx context.Context, admin *pgxpool.Pool, table, column string, want bool) {
	t.Helper()
	var got bool
	if err := admin.QueryRow(ctx, `SELECT EXISTS (
SELECT 1 FROM information_schema.columns
WHERE table_schema = 'aura' AND table_name = $1 AND column_name = $2)`, table, column).Scan(&got); err != nil {
		t.Fatalf("check %s.%s: %v", table, column, err)
	}
	if got != want {
		t.Fatalf("column aura.%s.%s exists = %v, want %v", table, column, got, want)
	}
}

func fresh0093Database(t *testing.T, ctx context.Context, name string) (*pgxpool.Pool, string, string) {
	t.Helper()
	pwd := envOrSkip(t, "POSTGRES_PASSWORD")
	host, port := os.Getenv("PGHOST"), os.Getenv("PGPORT")
	if host == "" {
		host = "127.0.0.1"
	}
	if port == "" {
		port = "5432"
	}
	if err := EnsureRoles(ctx, bootstrapURL(t), pwd); err != nil {
		t.Fatalf("EnsureRoles: %v", err)
	}
	dsn := func(role, database string) string {
		return fmt.Sprintf("postgres://%s:%s@%s:%s/%s?sslmode=disable", role, pwd, host, port, database)
	}
	root, err := Open(ctx, &Config{URL: dsn("aura", "aura")})
	if err != nil {
		t.Fatalf("open root database: %v", err)
	}
	if _, err := root.Exec(ctx, "DROP DATABASE IF EXISTS "+name+" WITH (FORCE)"); err != nil {
		t.Fatalf("drop stale database: %v", err)
	}
	if _, err := root.Exec(ctx, "CREATE DATABASE "+name); err != nil {
		t.Fatalf("create database: %v", err)
	}
	t.Cleanup(func() {
		_, _ = root.Exec(context.Background(), "DROP DATABASE IF EXISTS "+name+" WITH (FORCE)")
		root.Close()
	})
	if _, err := root.Exec(ctx, "GRANT CREATE ON DATABASE "+name+" TO aura_migrate"); err != nil {
		t.Fatalf("grant database create: %v", err)
	}
	admin, err := Open(ctx, &Config{URL: dsn("aura", name)})
	if err != nil {
		t.Fatalf("open fresh database: %v", err)
	}
	t.Cleanup(admin.Close)
	if _, err := admin.Exec(ctx, "GRANT CREATE ON SCHEMA public TO aura_migrate"); err != nil {
		t.Fatalf("grant public schema create: %v", err)
	}
	return admin, dsn("aura_migrate", name), dsn("aura_app", name)
}
