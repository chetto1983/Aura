//go:build db_integration

package documents

import (
	"context"
	"testing"

	"github.com/chetto1983/aura/internal/db"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

func batchTestRequests(identityID, keyPrefix string) []CreateIngestionJobRequest {
	return []CreateIngestionJobRequest{
		{IdentityID: identityID, JobType: delegationJobType, Status: "queued", IdempotencyKey: keyPrefix + "-1", MaxAttempts: 3, Payload: map[string]any{"goal": "one", "fanout_key": keyPrefix}},
		{IdentityID: identityID, JobType: delegationJobType, Status: "queued", IdempotencyKey: keyPrefix + "-2", MaxAttempts: 3, Payload: map[string]any{"goal": "two", "fanout_key": keyPrefix}},
		{IdentityID: identityID, JobType: delegationJobType, Status: "queued", IdempotencyKey: keyPrefix + "-3", MaxAttempts: 3, Payload: map[string]any{"goal": "three", "fanout_key": keyPrefix}},
	}
}

func countBatchTestRows(t *testing.T, ctx context.Context, store *PostgresIngestionJobStore, identityID, keyPrefix string) int {
	t.Helper()
	var count int
	err := db.WithIdentityTxRaw(ctx, store.pool, identityID, func(tx pgx.Tx) error {
		return tx.QueryRow(ctx,
			`SELECT count(*) FROM aura.ingestion_jobs WHERE identity_id = $1::uuid AND idempotency_key LIKE $2`,
			identityID, keyPrefix+"-%").Scan(&count)
	})
	if err != nil {
		t.Fatalf("count batch rows: %v", err)
	}
	return count
}

func TestCreateBatchRollsBackAllRowsOnMiddleInsertFailure(t *testing.T) {
	pool := pipelineDisposablePool(t)
	ctx := context.Background()
	identityID := seedDocumentTestIdentity(t, ctx, pool)
	store := NewPostgresIngestionJobStore(pool)
	keyPrefix := "batch-rollback-" + uuid.NewString()
	reqs := batchTestRequests(identityID, keyPrefix)
	reqs[1].Status = "invalid-status"

	if _, err := store.CreateBatch(ctx, reqs); err == nil {
		t.Fatal("CreateBatch with invalid middle row = nil error, want database constraint failure")
	}
	if got := countBatchTestRows(t, ctx, store, identityID, keyPrefix); got != 0 {
		t.Fatalf("rows after middle insert failure = %d, want 0", got)
	}
}

func TestCreateBatchIsSuccessfulAndIdempotent(t *testing.T) {
	pool := pipelineDisposablePool(t)
	ctx := context.Background()
	identityID := seedDocumentTestIdentity(t, ctx, pool)
	store := NewPostgresIngestionJobStore(pool)
	keyPrefix := "batch-idempotent-" + uuid.NewString()
	reqs := batchTestRequests(identityID, keyPrefix)

	first, err := store.CreateBatch(ctx, reqs)
	if err != nil {
		t.Fatalf("first CreateBatch: %v", err)
	}
	second, err := store.CreateBatch(ctx, reqs)
	if err != nil {
		t.Fatalf("second CreateBatch: %v", err)
	}
	if len(first) != len(reqs) || len(second) != len(reqs) {
		t.Fatalf("batch lengths = %d, %d, want %d", len(first), len(second), len(reqs))
	}
	for i := range first {
		if first[i].ID != second[i].ID {
			t.Fatalf("request %d IDs = %q, %q, want the same row on retry", i, first[i].ID, second[i].ID)
		}
	}
	if got := countBatchTestRows(t, ctx, store, identityID, keyPrefix); got != len(reqs) {
		t.Fatalf("rows after idempotent retry = %d, want %d", got, len(reqs))
	}
}
