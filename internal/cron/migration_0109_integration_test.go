//go:build db_integration

package cron

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/chetto1983/aura/internal/db"
	"github.com/jackc/pgx/v5/pgxpool"
)

// TestMigration0109BackfillsAndConstrainsExplicitDelivery proves the nullable
// scheduler/outbox and ungrouped steer-result shapes are transformed before
// their constraints land. It uses a throwaway database and exercises down/up.
func TestMigration0109BackfillsAndConstrainsExplicitDelivery(t *testing.T) {
	migrateURL := disposableMigrateURL(t)
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()
	positionDisposableMigration(t, ctx, migrateURL, 108)

	pool, err := db.Open(ctx, &db.Config{URL: migrateURL})
	if err != nil {
		t.Fatalf("open disposable database: %v", err)
	}
	defer pool.Close()

	const (
		taskID  = "10000000-0000-0000-0000-000000000109"
		runID   = "20000000-0000-0000-0000-000000000109"
		notifID = "30000000-0000-0000-0000-000000000109"
		steerID = "40000000-0000-0000-0000-000000000109"
		localID = "00000000-0000-0000-0000-000000000001"
	)
	seed := []struct {
		sql  string
		args []any
	}{
		{`INSERT INTO aura.scheduler_tasks
			(id, kind, schedule_kind, every_minutes, tz, payload, status, next_run_at, notify_route, identity_id)
			VALUES ($1, 'reminder', 'every', 5, 'UTC', '{}'::jsonb, 'active', now(), NULL, 'local')`, []any{taskID}},
		{`INSERT INTO aura.agent_job_runs (id, task_id, status) VALUES ($1, $2, 'running')`, []any{runID, taskID}},
		{`INSERT INTO aura.pending_notifications
			(id, run_id, notify_route, body, notify_after, status, identity_id)
			VALUES ($1, $2, NULL, 'retry', now(), 'pending', NULL)`, []any{notifID, runID}},
		{`INSERT INTO aura.steer_queue
			(id, identity_id, conversation_id, kind, source, body, fanout_key)
			VALUES ($1, $2, 'conv-0109', 'delegation_result', 'swarm', '[]', NULL)`, []any{steerID, localID}},
	}
	for _, stmt := range seed {
		if _, err := pool.Exec(ctx, stmt.sql, stmt.args...); err != nil {
			t.Fatalf("seed 0108 shape: %v", err)
		}
	}

	if err := db.MigrateSteps(ctx, migrateURL, 1); err != nil {
		t.Fatalf("apply 0109: %v", err)
	}
	assert0109Backfill(t, ctx, pool, taskID, notifID, steerID)

	if _, err := pool.Exec(ctx, `UPDATE aura.scheduler_tasks SET notify_route = NULL WHERE id = $1`, taskID); err == nil {
		t.Fatal("0109 accepted NULL scheduler notify_route")
	}
	if _, err := pool.Exec(ctx, `UPDATE aura.pending_notifications SET steer_queue_id = $2 WHERE id = $1`, notifID, steerID); err == nil {
		t.Fatal("0109 accepted a pending notification with both durable owners")
	}
	if _, err := pool.Exec(ctx, `UPDATE aura.steer_queue SET fanout_key = NULL WHERE id = $1`, steerID); err == nil {
		t.Fatal("0109 accepted a delegation_result without fanout_key")
	}

	if err := db.MigrateSteps(ctx, migrateURL, -1); err != nil {
		t.Fatalf("revert 0109: %v", err)
	}
	reset := []struct {
		sql string
		id  string
	}{
		{`UPDATE aura.scheduler_tasks SET notify_route = NULL WHERE id = $1`, taskID},
		{`UPDATE aura.pending_notifications SET notify_route = NULL, identity_id = NULL WHERE id = $1`, notifID},
		{`UPDATE aura.steer_queue SET fanout_key = NULL WHERE id = $1`, steerID},
	}
	for _, stmt := range reset {
		if _, err := pool.Exec(ctx, stmt.sql, stmt.id); err != nil {
			t.Fatalf("0109 down did not restore the 0108 nullable shape: %v", err)
		}
	}
	if err := db.MigrateSteps(ctx, migrateURL, 1); err != nil {
		t.Fatalf("re-apply 0109: %v", err)
	}
	assert0109Backfill(t, ctx, pool, taskID, notifID, steerID)
}

// TestMigration0110BackfillsAndConstrainsDelegationJobs pins the follow-up
// migration required after 0109 had already reached the live database. Fresh and
// deployed databases converge on the same non-empty job fan-out key contract.
func TestMigration0110BackfillsAndConstrainsDelegationJobs(t *testing.T) {
	migrateURL := disposableMigrateURL(t)
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()
	positionDisposableMigration(t, ctx, migrateURL, 109)

	pool, err := db.Open(ctx, &db.Config{URL: migrateURL})
	if err != nil {
		t.Fatalf("open disposable database: %v", err)
	}
	defer pool.Close()

	const (
		jobID   = "50000000-0000-0000-0000-000000000110"
		localID = "00000000-0000-0000-0000-000000000001"
	)
	if _, err := pool.Exec(ctx, `INSERT INTO aura.ingestion_jobs
		(id, identity_id, job_type, status, idempotency_key, stage, max_attempts, next_attempt_at, payload)
		VALUES ($1, $2, 'swarm_delegation', 'queued', 'migration-0110', 'dispatch', 3, now(),
		'{"goal":"g","conversation_id":"conv-0110"}'::jsonb)`, jobID, localID); err != nil {
		t.Fatalf("seed 0109 job shape: %v", err)
	}

	if err := db.MigrateSteps(ctx, migrateURL, 1); err != nil {
		t.Fatalf("apply 0110: %v", err)
	}
	assert0110JobBackfill(t, ctx, pool, jobID)
	if _, err := pool.Exec(ctx, `UPDATE aura.ingestion_jobs SET payload = payload - 'fanout_key' WHERE id = $1`, jobID); err == nil {
		t.Fatal("0110 accepted a swarm_delegation payload without fanout_key")
	}

	if err := db.MigrateSteps(ctx, migrateURL, -1); err != nil {
		t.Fatalf("revert 0110: %v", err)
	}
	if _, err := pool.Exec(ctx, `UPDATE aura.ingestion_jobs SET payload = payload - 'fanout_key' WHERE id = $1`, jobID); err != nil {
		t.Fatalf("0110 down did not restore the 0109 job shape: %v", err)
	}
	if err := db.MigrateSteps(ctx, migrateURL, 1); err != nil {
		t.Fatalf("re-apply 0110: %v", err)
	}
	assert0110JobBackfill(t, ctx, pool, jobID)
}

func positionDisposableMigration(t *testing.T, ctx context.Context, migrateURL string, version int64) {
	t.Helper()
	if _, err := db.Migrate(ctx, migrateURL); err != nil {
		t.Fatalf("migrate disposable database to head: %v", err)
	}
	steps, err := db.MigrationStepsAbove(version)
	if err != nil {
		t.Fatalf("count migrations above %04d: %v", version, err)
	}
	if steps > 0 {
		if err := db.MigrateSteps(ctx, migrateURL, -steps); err != nil {
			t.Fatalf("position disposable database at %04d: %v", version, err)
		}
	}
}

func assert0109Backfill(t *testing.T, ctx context.Context, pool *pgxpool.Pool, taskID, notifID, steerID string) {
	t.Helper()
	var taskRoute, pendingRoute, pendingIdentity, steerKey string
	if err := pool.QueryRow(ctx, `
		SELECT t.notify_route, n.notify_route, n.identity_id, s.fanout_key
		FROM aura.scheduler_tasks t
		JOIN aura.agent_job_runs r ON r.task_id = t.id
		JOIN aura.pending_notifications n ON n.run_id = r.id
		JOIN aura.steer_queue s ON s.id = $3
		WHERE t.id = $1 AND n.id = $2`, taskID, notifID, steerID).
		Scan(&taskRoute, &pendingRoute, &pendingIdentity, &steerKey); err != nil {
		t.Fatalf("read 0109 backfill: %v", err)
	}
	if taskRoute != "none" || pendingRoute != "none" || pendingIdentity != "local" {
		t.Fatalf("route backfill = task:%q pending:%q identity:%q, want none/none/local", taskRoute, pendingRoute, pendingIdentity)
	}
	if !strings.HasPrefix(steerKey, "f-") {
		t.Fatalf("steer fan-out backfill = %q, want deterministic f-* key", steerKey)
	}
}

func assert0110JobBackfill(t *testing.T, ctx context.Context, pool *pgxpool.Pool, jobID string) {
	t.Helper()
	var jobKey string
	if err := pool.QueryRow(ctx, `SELECT payload->>'fanout_key' FROM aura.ingestion_jobs WHERE id = $1`, jobID).Scan(&jobKey); err != nil {
		t.Fatalf("read 0110 job backfill: %v", err)
	}
	if !strings.HasPrefix(jobKey, "f-") {
		t.Fatalf("job fan-out backfill = %q, want deterministic f-* key", jobKey)
	}
}
