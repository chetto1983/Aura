//go:build db_integration

// Real Go claim-path coverage for the background delegation queue (SWARM-03/09).
// This closes spike 100's own stated gap: its D-01 measurement drove the claim
// predicate as a hand-built SQL SELECT over synthetic rows, never the real
// ClaimIngestionJobs Go path under an actual EnqueueDelegation/Claim round trip.
//
// Runs as aura_app (never the superuser aura role, which is BYPASSRLS and would
// produce a false green on the identity-scoping assertion below) via
// delegationDisposablePool -- a fully throwaway per-test database, copying
// internal/documents/integration_pool_helper_test.go's aura/aura_migrate/aura_app
// three-role DSN composition VERBATIM rather than inventing a second bootstrap.
package swarm

import (
	"context"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/chetto1983/aura/internal/db"
	"github.com/chetto1983/aura/internal/documents"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

func delegationDisposablePool(t *testing.T) *pgxpool.Pool {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	password := delegationEnvOrSkip(t, "POSTGRES_PASSWORD")
	host, port := os.Getenv("PGHOST"), os.Getenv("PGPORT")
	if host == "" {
		host = "127.0.0.1"
	}
	if port == "" {
		port = "5432"
	}
	dsn := func(role, database string) string {
		return fmt.Sprintf("postgres://%s:%s@%s:%s/%s?sslmode=disable", role, password, host, port, database)
	}
	if err := db.EnsureRoles(ctx, dsn("aura", "aura"), password); err != nil {
		t.Fatalf("EnsureRoles: %v", err)
	}
	root, err := db.Open(ctx, &db.Config{URL: dsn("aura", "aura")})
	if err != nil {
		t.Fatalf("open root database: %v", err)
	}
	database := "aura_swarm_" + strings.ReplaceAll(uuid.NewString(), "-", "")
	if _, err = root.Exec(ctx, "CREATE DATABASE "+database); err != nil {
		root.Close()
		t.Fatalf("create disposable swarm database: %v", err)
	}
	var admin, pool *pgxpool.Pool
	t.Cleanup(func() {
		if pool != nil {
			pool.Close()
		}
		if admin != nil {
			admin.Close()
		}
		_, _ = root.Exec(context.Background(), "DROP DATABASE "+database+" WITH (FORCE)")
		root.Close()
	})
	if _, err = root.Exec(ctx, "GRANT CREATE ON DATABASE "+database+" TO aura_migrate"); err != nil {
		t.Fatalf("grant disposable swarm database: %v", err)
	}
	admin, err = db.Open(ctx, &db.Config{URL: dsn("aura", database)})
	if err != nil {
		t.Fatalf("open disposable swarm database: %v", err)
	}
	if _, err = admin.Exec(ctx, "GRANT CREATE ON SCHEMA public TO aura_migrate"); err != nil {
		t.Fatalf("grant disposable public schema: %v", err)
	}
	if _, err = db.Migrate(ctx, dsn("aura_migrate", database)); err != nil {
		t.Fatalf("migrate disposable swarm database: %v", err)
	}
	pool, err = db.Open(ctx, &db.Config{URL: dsn("aura_app", database)})
	if err != nil {
		t.Fatalf("open disposable app database: %v", err)
	}
	return pool
}

func delegationEnvOrSkip(t *testing.T, key string) string {
	t.Helper()
	value := os.Getenv(key)
	if value == "" {
		if os.Getenv("CI") != "" {
			t.Fatalf("swarm delegation integration requires %s under CI", key)
		}
		t.Skipf("swarm delegation integration requires %s", key)
	}
	return value
}

// seedSwarmTestIdentity creates a throwaway identity so a delegation row's
// identity_id foreign key is satisfied without borrowing a real one, mirroring
// internal/documents/live_identity_test.go's seedDocumentTestIdentity.
func seedSwarmTestIdentity(t *testing.T, ctx context.Context, pool *pgxpool.Pool) string {
	t.Helper()
	id := uuid.NewString()
	name := "swarm-delegation-test+" + id[:8] + "@aura.local"
	if _, err := pool.Exec(ctx,
		`INSERT INTO aura.identities (id, name, kind) VALUES ($1::uuid, $2, 'service')`, id, name); err != nil {
		t.Fatalf("seed identity: %v", err)
	}
	t.Cleanup(func() {
		_, _ = pool.Exec(context.Background(), "DELETE FROM aura.identities WHERE id = $1", id)
	})
	return id
}

// TestBackgroundDelegationEnqueues proves EnqueueDelegation writes exactly one
// durable row per goal (SWARM-03/09), scoped to job_type=swarm_delegation and
// the caller's identity, and that re-running the same enqueue (same
// conversation/parent-run/goal set) adds no second row -- the ON CONFLICT
// (identity_id, job_type, idempotency_key) unique key deduplicates it.
func TestBackgroundDelegationEnqueues(t *testing.T) {
	pool := delegationDisposablePool(t)
	ctx := context.Background()
	identityID := seedSwarmTestIdentity(t, ctx, pool)

	enq := &DelegationEnqueuer{Store: documents.NewPostgresIngestionJobStore(pool)}
	brief := DelegationPayload{ConversationID: "conv-enqueue", ParentRunID: "run-1", Depth: 1}
	goals := []string{"first goal", "second goal"}

	msg, err := EnqueueDelegation(ctx, enq, identityID, goals, brief)
	if err != nil {
		t.Fatalf("EnqueueDelegation: %v", err)
	}
	if !strings.Contains(msg, "queued") {
		t.Fatalf("EnqueueDelegation summary = %q, want it to mention queued work", msg)
	}

	count := countDelegationRows(t, ctx, pool, identityID)
	if count != len(goals) {
		t.Fatalf("row count = %d, want %d (one per goal)", count, len(goals))
	}

	// Re-running the identical enqueue must not add a second row per goal.
	if _, err := EnqueueDelegation(ctx, enq, identityID, goals, brief); err != nil {
		t.Fatalf("second EnqueueDelegation: %v", err)
	}
	count = countDelegationRows(t, ctx, pool, identityID)
	if count != len(goals) {
		t.Fatalf("row count after re-enqueue = %d, want %d (idempotency key must dedupe)", count, len(goals))
	}
}

// countDelegationRows reads through an identity-scoped tx (db.WithIdentityTxRaw)
// because aura.ingestion_jobs is RLS-scoped on app.current_identity: a raw
// pool.QueryRow with no bound principal silently returns zero rows rather than
// erroring, which would otherwise misreport "not enqueued" as a false failure.
func countDelegationRows(t *testing.T, ctx context.Context, pool *pgxpool.Pool, identityID string) int {
	t.Helper()
	var count int
	err := db.WithIdentityTxRaw(ctx, pool, identityID, func(tx pgx.Tx) error {
		return tx.QueryRow(ctx,
			`SELECT count(*) FROM aura.ingestion_jobs WHERE identity_id = $1::uuid AND job_type = $2`,
			identityID, JobTypeSwarmDelegation).Scan(&count)
	})
	if err != nil {
		t.Fatalf("count delegation rows: %v", err)
	}
	return count
}

// TestDelegationClaimReclaim exercises the REAL ClaimIngestionJobs Go path
// (documents.PostgresIngestionJobStore.Claim, not a hand-built SQL SELECT) for
// three properties the tracer's `<behavior>` names: (1) a row belonging to a
// FOREIGN identity is invisible to the claim -- the false-green risk of running
// this as the superuser `aura` role (BYPASSRLS) instead of `aura_app` is exactly
// what this assertion would silently hide; (2) a claim only ever picks up
// job_type=swarm_delegation rows, never an ingestion row from the SAME shared
// table; (3) a claimed row whose lease has since expired is reclaimed by the
// next ProcessOnce-shaped claim, while a row with a live lease is left alone.
func TestDelegationClaimReclaim(t *testing.T) {
	pool := delegationDisposablePool(t)
	ctx := context.Background()
	ownIdentity := seedSwarmTestIdentity(t, ctx, pool)
	foreignIdentity := seedSwarmTestIdentity(t, ctx, pool)

	store := documents.NewPostgresIngestionJobStore(pool)

	// A foreign identity's own delegation row must never be visible to this
	// identity's claim -- the identity-scoping assertion the aura_app role
	// makes meaningful (a BYPASSRLS superuser run would pass here regardless).
	if _, err := store.Create(ctx, documents.CreateIngestionJobRequest{
		IdentityID: foreignIdentity, JobType: JobTypeSwarmDelegation, Status: "queued",
		IdempotencyKey: "foreign-row", MaxAttempts: 3,
		Payload: map[string]any{"goal": "foreign goal", "conversation_id": "conv-foreign", "fanout_key": "f-foreign"},
	}); err != nil {
		t.Fatalf("create foreign row: %v", err)
	}

	// An ingestion row (a different job_type) sharing the SAME identity must
	// never be claimed by the delegation-scoped claim.
	if _, err := store.Create(ctx, documents.CreateIngestionJobRequest{
		IdentityID: ownIdentity, JobType: "asset_process", Status: "queued",
		IdempotencyKey: "own-ingestion-row", MaxAttempts: 3,
		Payload: map[string]any{"asset_id": "asset-1"},
	}); err != nil {
		t.Fatalf("create ingestion row: %v", err)
	}

	delegationJob, err := store.Create(ctx, documents.CreateIngestionJobRequest{
		IdentityID: ownIdentity, JobType: JobTypeSwarmDelegation, Status: "queued",
		IdempotencyKey: "own-delegation-row", MaxAttempts: 3,
		Payload: map[string]any{"goal": "own goal", "conversation_id": "conv-own", "fanout_key": "f-own"},
	})
	if err != nil {
		t.Fatalf("create delegation row: %v", err)
	}

	claimed, err := store.Claim(ctx, documents.ClaimIngestionJobsRequest{
		IdentityID: ownIdentity, JobType: JobTypeSwarmDelegation, WorkerID: "test-worker",
		LeaseDuration: 100 * time.Millisecond, BatchSize: 10,
	})
	if err != nil {
		t.Fatalf("claim: %v", err)
	}
	if len(claimed) != 1 {
		t.Fatalf("claimed %d rows, want exactly 1 (the own delegation row, never the foreign or ingestion row)", len(claimed))
	}
	if claimed[0].ID != delegationJob.ID {
		t.Fatalf("claimed row %s, want the delegation row %s", claimed[0].ID, delegationJob.ID)
	}
	if claimed[0].JobType != JobTypeSwarmDelegation {
		t.Fatalf("claimed row job_type = %q, want %q", claimed[0].JobType, JobTypeSwarmDelegation)
	}

	// A second claim attempt WHILE the lease is still live must not re-claim it.
	stillLive, err := store.Claim(ctx, documents.ClaimIngestionJobsRequest{
		IdentityID: ownIdentity, JobType: JobTypeSwarmDelegation, WorkerID: "test-worker-2",
		LeaseDuration: 100 * time.Millisecond, BatchSize: 10,
	})
	if err != nil {
		t.Fatalf("claim while live: %v", err)
	}
	if len(stillLive) != 0 {
		t.Fatalf("claimed %d rows while the lease was still live, want 0", len(stillLive))
	}

	// Let the 100ms lease expire, then reclaim through the SAME real Claim path.
	time.Sleep(250 * time.Millisecond)
	reclaimed, err := store.Claim(ctx, documents.ClaimIngestionJobsRequest{
		IdentityID: ownIdentity, JobType: JobTypeSwarmDelegation, WorkerID: "test-worker-3",
		LeaseDuration: 5 * time.Second, BatchSize: 10,
	})
	if err != nil {
		t.Fatalf("reclaim: %v", err)
	}
	if len(reclaimed) != 1 || reclaimed[0].ID != delegationJob.ID {
		t.Fatalf("reclaim did not pick up the expired-lease row: got %+v", reclaimed)
	}
	if reclaimed[0].AttemptCount < 2 {
		t.Fatalf("reclaimed row attempt_count = %d, want >= 2 (claimed twice)", reclaimed[0].AttemptCount)
	}
}

// TestDelegationDeadLettersAtMaxAttempts proves a delegation row that has
// failed max_attempts times stops being claimed instead of looping (D-01):
// DelegationClaimLoop's own recordFailure branch (reused verbatim from
// jobs_worker.go's recordHandlerFailure shape) transitions it to dead_letter,
// and a dead-lettered row is never returned by a subsequent claim.
func TestDelegationDeadLettersAtMaxAttempts(t *testing.T) {
	pool := delegationDisposablePool(t)
	ctx := context.Background()
	identityID := seedSwarmTestIdentity(t, ctx, pool)

	store := documents.NewPostgresIngestionJobStore(pool)
	loop := &DelegationClaimLoop{
		Store: store, IdentityID: identityID, WorkerID: "test-worker",
		Delivery: &DelegationDelivery{Recorder: &fakeConversationRecorder{}, Archiver: successfulReportArchiver()},
	}

	job, err := store.Create(ctx, documents.CreateIngestionJobRequest{
		IdentityID: identityID, JobType: JobTypeSwarmDelegation, Status: "queued",
		IdempotencyKey: "dead-letter-row", MaxAttempts: 1,
		Payload: map[string]any{"goal": "will fail", "conversation_id": "conv-fail", "child_id": "w1", "fanout_key": "f-fail"},
	})
	if err != nil {
		t.Fatalf("create row: %v", err)
	}

	claimed, err := store.Claim(ctx, documents.ClaimIngestionJobsRequest{
		IdentityID: identityID, JobType: JobTypeSwarmDelegation, WorkerID: "test-worker",
		LeaseDuration: 5 * time.Second, BatchSize: 10,
	})
	if err != nil {
		t.Fatalf("claim: %v", err)
	}
	if len(claimed) != 1 {
		t.Fatalf("claimed %d rows, want 1", len(claimed))
	}
	// attempt_count is now 1 == max_attempts (1): the NEXT failure must dead-letter.
	if err := loop.recordFailure(ctx, claimed[0], fmt.Errorf("simulated worker failure")); err != nil {
		t.Fatalf("recordFailure: %v", err)
	}

	var status string
	err = db.WithIdentityTxRaw(ctx, pool, identityID, func(tx pgx.Tx) error {
		return tx.QueryRow(ctx, `SELECT status FROM aura.ingestion_jobs WHERE id = $1::uuid`, job.ID).Scan(&status)
	})
	if err != nil {
		t.Fatalf("read status: %v", err)
	}
	if status != "dead_letter" {
		t.Fatalf("status = %q, want dead_letter", status)
	}

	again, err := store.Claim(ctx, documents.ClaimIngestionJobsRequest{
		IdentityID: identityID, JobType: JobTypeSwarmDelegation, WorkerID: "test-worker",
		LeaseDuration: 5 * time.Second, BatchSize: 10,
	})
	if err != nil {
		t.Fatalf("claim after dead-letter: %v", err)
	}
	if len(again) != 0 {
		t.Fatalf("claimed %d rows after dead-letter, want 0 (stops being claimed, does not loop)", len(again))
	}
}

// TestExpiredFinalAttemptRecoversAsDeliveryOnly proves a process death during
// the final worker execution cannot turn the configured attempt cap into an
// unbounded replay loop. The first recovery stages a dead-letter report and a
// failed projection schedules delivery-only work; the second recovery finishes
// that delivery without invoking the model on either pass.
func TestExpiredFinalAttemptRecoversAsDeliveryOnly(t *testing.T) {
	pool := delegationDisposablePool(t)
	ctx := context.Background()
	identityID := seedSwarmTestIdentity(t, ctx, pool)

	const goal = "must not execute again"
	router := newRouter().route(goal, outcome{kind: "fail"})
	recorder := &fakeConversationRecorder{}
	pub := &fakeSteerPublisher{err: fmt.Errorf("steer unavailable")}
	store := documents.NewPostgresIngestionJobStore(pool)
	loop := &DelegationClaimLoop{
		Store: store, IdentityID: identityID, WorkerID: "recovery-worker",
		Worker: testRunConfig(t, router, 25), RetryBackoff: time.Nanosecond,
		Delivery: &DelegationDelivery{Recorder: recorder, Steer: pub, Archiver: successfulReportArchiver()},
	}

	job, err := store.Create(ctx, documents.CreateIngestionJobRequest{
		IdentityID: identityID, JobType: JobTypeSwarmDelegation, Status: "queued",
		IdempotencyKey: "expired-final-attempt", MaxAttempts: 2,
		Payload: map[string]any{
			"goal": goal, "conversation_id": "conv-expired", "child_id": "w-expired", "fanout_key": "f-expired",
		},
	})
	if err != nil {
		t.Fatalf("create row: %v", err)
	}
	err = db.WithIdentityTxRaw(ctx, pool, identityID, func(tx pgx.Tx) error {
		_, updateErr := tx.Exec(ctx, `
			UPDATE aura.ingestion_jobs
			SET status = 'running', attempt_count = max_attempts,
			    locked_by = 'crashed-worker', locked_until = now() - interval '1 second'
			WHERE id = $1::uuid`, job.ID)
		return updateErr
	})
	if err != nil {
		t.Fatalf("seed expired final attempt: %v", err)
	}

	claimed, err := store.Claim(ctx, documents.ClaimIngestionJobsRequest{
		IdentityID: identityID, JobType: JobTypeSwarmDelegation, WorkerID: "recovery-worker",
		LeaseDuration: 5 * time.Second, BatchSize: 1,
	})
	if err != nil || len(claimed) != 1 {
		t.Fatalf("claim expired final attempt = %+v, %v", claimed, err)
	}
	if claimed[0].AttemptCount <= claimed[0].MaxAttempts {
		t.Fatalf("attempt count = %d, want post-claim count above cap %d", claimed[0].AttemptCount, claimed[0].MaxAttempts)
	}
	if err := loop.processJob(ctx, claimed[0]); err != nil {
		t.Fatalf("first recovery: %v", err)
	}

	var status, errorCode string
	err = db.WithIdentityTxRaw(ctx, pool, identityID, func(tx pgx.Tx) error {
		return tx.QueryRow(ctx, `
			UPDATE aura.ingestion_jobs SET next_attempt_at = now()
			WHERE id = $1::uuid
			RETURNING status, error_code`, job.ID).Scan(&status, &errorCode)
	})
	if err != nil {
		t.Fatalf("read retry state: %v", err)
	}
	if status != "queued" || errorCode != "delivery_failed" {
		t.Fatalf("first recovery state = %s/%s, want queued/delivery_failed", status, errorCode)
	}

	pub.err = nil
	claimed, err = store.Claim(ctx, documents.ClaimIngestionJobsRequest{
		IdentityID: identityID, JobType: JobTypeSwarmDelegation, WorkerID: "recovery-worker",
		LeaseDuration: 5 * time.Second, BatchSize: 1,
	})
	if err != nil || len(claimed) != 1 {
		t.Fatalf("claim delivery-only retry = %+v, %v", claimed, err)
	}
	if err := loop.processJob(ctx, claimed[0]); err != nil {
		t.Fatalf("delivery-only recovery: %v", err)
	}

	router.mu.Lock()
	modelCalls := router.calls[goal]
	router.mu.Unlock()
	if modelCalls != 0 {
		t.Fatalf("model calls = %d, want zero beyond the final attempt", modelCalls)
	}
	if len(recorder.appended) != 1 || len(pub.pushes) != 1 {
		t.Fatalf("terminal projections = turns:%d pushes:%d, want exactly one each", len(recorder.appended), len(pub.pushes))
	}
	err = db.WithIdentityTxRaw(ctx, pool, identityID, func(tx pgx.Tx) error {
		return tx.QueryRow(ctx, `SELECT status FROM aura.ingestion_jobs WHERE id = $1::uuid`, job.ID).Scan(&status)
	})
	if err != nil || status != "dead_letter" {
		t.Fatalf("terminal state = %q, %v, want dead_letter", status, err)
	}
}
