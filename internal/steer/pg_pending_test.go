//go:build db_integration

package steer

import (
	"github.com/chetto1983/aura/internal/db"
	"github.com/chetto1983/aura/internal/identityctx"
	"github.com/jackc/pgx/v5"
	"testing"
)

func TestPendingDelegationResultsWaitForFanoutAndSurvivePhoneNotification(t *testing.T) {
	pool := steerDisposablePool(t)
	owner, conv := seedIdentityAndConversation(t, pool)
	ctx := identityctx.WithIdentityID(t.Context(), owner)
	execJob := func(statement string) {
		t.Helper()
		if err := db.WithIdentityTxRaw(ctx, pool, owner, func(tx pgx.Tx) error {
			_, err := tx.Exec(ctx, statement, owner)
			return err
		}); err != nil {
			t.Fatal(err)
		}
	}
	store := NewPostgresStore(pool, Config{Max: 8, MaxBytes: 16384})
	if err := store.PushDelegationResult(conv, SourceWorker, "saved report", "pending-fanout"); err != nil {
		t.Fatal(err)
	}
	if _, err := store.MarkFanoutNudged(t.Context(), owner, "pending-fanout"); err != nil {
		t.Fatal(err)
	}
	if rows, err := store.ListPendingDelegationResults(ctx, 10); err != nil || len(rows) != 0 {
		t.Fatalf("historical unregistered result woke: %v %v", rows, err)
	}
	execJob(`INSERT INTO aura.ingestion_jobs (job_type,status,idempotency_key,payload,identity_id)
		VALUES ('swarm_delegation','succeeded','wake-test',jsonb_build_object('fanout_key','pending-fanout','wake_parent',true),$1)`)
	rows, err := store.ListPendingDelegationResults(ctx, 10)
	if err != nil || len(rows) != 1 || rows[0].IdentityID != owner {
		t.Fatalf("phone notification hid pending report: %v %v", rows, err)
	}
	execJob(`UPDATE aura.ingestion_jobs SET status='awaiting_input' WHERE identity_id=$1 AND idempotency_key='wake-test'`)
	if rows, err := store.ListPendingDelegationResults(ctx, 10); err != nil || len(rows) != 0 {
		t.Fatalf("woke unfinished fanout: %v %v", rows, err)
	}
	execJob(`UPDATE aura.ingestion_jobs SET status='succeeded' WHERE identity_id=$1 AND idempotency_key='wake-test'`)
	if rows, err := store.ListPendingDelegationResults(ctx, 10); err != nil || len(rows) != 1 {
		t.Fatalf("finished fanout not available: %v %v", rows, err)
	}
	if len(store.Drain(conv)) != 1 {
		t.Fatal("report was not consumed")
	}
	if rows, err := store.ListPendingDelegationResults(ctx, 10); err != nil || len(rows) != 0 {
		t.Fatalf("consumed report still wakes: %v %v", rows, err)
	}
}
