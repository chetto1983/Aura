//go:build db_integration

package documents

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

func TestPostgresDeleteJobStoreFencesAndVerifiedFinalize(t *testing.T) {
	pool := pipelineDisposablePool(t)
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	pipeline := NewPostgresPipelineStore(pool)
	fixture := newPipelineStoreFixture(t, ctx, pool, pipeline)
	var chunkCount int
	if err := asDocumentIdentity(ctx, pool, fixture.identityID, func(tx pgx.Tx) error {
		return tx.QueryRow(ctx, `SELECT count(*) FROM aura.document_chunks
WHERE identity_id = $1 AND document_id = $2`, fixture.identityID, fixture.documentID).
			Scan(&chunkCount)
	}); err != nil || chunkCount != 0 {
		t.Fatalf("pre-delete chunks = %d (err=%v), want zero", chunkCount, err)
	}
	catalog := NewPostgresCatalogStore(pool)
	deleting, err := catalog.SoftDeleteDocument(ctx, fixture.identityID, fixture.documentID)
	if err != nil || deleting.Status != DocumentStatusDeleting {
		t.Fatalf("SoftDeleteDocument = (%#v, %v)", deleting, err)
	}

	store := NewPostgresDurableDeleteStore(pool)
	jobs, err := store.Claim(ctx, ClaimDeleteJobsRequest{
		IdentityID: fixture.identityID, WorkerID: "delete-integration-worker",
		LeaseDuration: time.Minute, BatchSize: 1,
	})
	if err != nil || len(jobs) != 1 {
		t.Fatalf("Claim = (%#v, %v)", jobs, err)
	}
	job := jobs[0]
	snapshot, err := normalizedDeleteSnapshot(job)
	if err != nil || len(snapshot.Objects) != 2 ||
		len(snapshot.ProjectionGenerations) != 2 ||
		snapshot.ProjectionGenerations[0] != fixture.v1.PipelineGeneration ||
		snapshot.ProjectionGenerations[1] != fixture.v2.PipelineGeneration {
		t.Fatalf("snapshot = (%#v, %v)", snapshot, err)
	}
	staleFence := fenceForDeleteJob(job)
	staleFence.LeaseGeneration++
	if err := store.MarkObjectDeleted(ctx, MarkDeletedObjectRequest{
		DeleteJobFence: staleFence, StorageObjectID: snapshot.Objects[0].ID,
		DeletionGeneration: snapshot.Objects[0].DeletionGeneration,
	}); !errors.Is(err, ErrDeleteJobLeaseLost) {
		t.Fatalf("stale object verification error = %v", err)
	}
	if _, err := store.Finalize(ctx, FinalizeDeleteJobRequest{
		DeleteJobFence: fenceForDeleteJob(job), ProjectionVerified: false,
	}); err == nil {
		t.Fatal("finalize accepted projection_verified=false")
	}
	if err := store.MarkObjectDeleted(ctx, MarkDeletedObjectRequest{
		DeleteJobFence: fenceForDeleteJob(job), StorageObjectID: snapshot.Objects[0].ID,
		DeletionGeneration: snapshot.Objects[0].DeletionGeneration + 1,
	}); !errors.Is(err, ErrDeleteJobLeaseLost) {
		t.Fatalf("wrong deletion generation error = %v", err)
	}
	assertDocumentLifecycleStatus(t, ctx, pool, fixture.identityID, fixture.documentID, "deleting")

	for _, object := range snapshot.Objects {
		if err := store.MarkObjectDeleted(ctx, MarkDeletedObjectRequest{
			DeleteJobFence: fenceForDeleteJob(job), StorageObjectID: object.ID,
			DeletionGeneration: object.DeletionGeneration,
		}); err != nil {
			t.Fatalf("MarkObjectDeleted(%s): %v", object.ID, err)
		}
	}
	deleted, err := store.Finalize(ctx, FinalizeDeleteJobRequest{
		DeleteJobFence: fenceForDeleteJob(job), ProjectionVerified: true,
	})
	if err != nil || deleted.Status != DocumentStatusDeleted || deleted.DeletedAt.IsZero() {
		t.Fatalf("Finalize = (%#v, %v)", deleted, err)
	}
	assertDocumentLifecycleStatus(t, ctx, pool, fixture.identityID, fixture.documentID, "deleted")
	if err := store.Heartbeat(ctx, HeartbeatDeleteJobRequest{
		DeleteJobFence: fenceForDeleteJob(job), LeaseDuration: time.Minute,
	}); !errors.Is(err, ErrDeleteJobLeaseLost) {
		t.Fatalf("post-finalize heartbeat error = %v", err)
	}
}

func TestPostgresDeleteJobStoreRetryAdvancesFenceAndDeadLettersAtBound(t *testing.T) {
	pool := pipelineDisposablePool(t)
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	pipeline := NewPostgresPipelineStore(pool)
	fixture := newPipelineStoreFixture(t, ctx, pool, pipeline)
	catalog := NewPostgresCatalogStore(pool)
	if _, err := catalog.SoftDeleteDocument(ctx, fixture.identityID, fixture.documentID); err != nil {
		t.Fatalf("SoftDeleteDocument: %v", err)
	}
	if err := asDocumentIdentity(ctx, pool, fixture.identityID, func(tx pgx.Tx) error {
		_, execErr := tx.Exec(ctx, `UPDATE aura.delete_jobs SET max_attempts = 1
WHERE identity_id = $1 AND document_id = $2`, fixture.identityID, fixture.documentID)
		return execErr
	}); err != nil {
		t.Fatalf("bound delete attempts: %v", err)
	}

	store := NewPostgresDurableDeleteStore(pool)
	jobs, err := store.Claim(ctx, ClaimDeleteJobsRequest{
		IdentityID: fixture.identityID, WorkerID: "bounded-delete-worker",
		LeaseDuration: time.Minute, BatchSize: 1,
	})
	if err != nil || len(jobs) != 1 || jobs[0].AttemptCount != 1 {
		t.Fatalf("Claim = (%#v, %v)", jobs, err)
	}
	job := jobs[0]
	terminal, err := store.Retry(ctx, RetryDeleteJobRequest{
		DeleteJobFence: fenceForDeleteJob(job), ErrorCode: "object_verify_failed",
		NextAttemptAt: time.Now().UTC().Add(time.Minute),
	})
	if err != nil || terminal.Status != "dead_letter" {
		t.Fatalf("Retry at bound = (%#v, %v)", terminal, err)
	}
	assertDocumentLifecycleStatus(t, ctx, pool, fixture.identityID, fixture.documentID, "deleting")
	if err := store.Heartbeat(ctx, HeartbeatDeleteJobRequest{
		DeleteJobFence: fenceForDeleteJob(job), LeaseDuration: time.Minute,
	}); !errors.Is(err, ErrDeleteJobLeaseLost) {
		t.Fatalf("terminal fence remained usable: %v", err)
	}

	permanent := newPipelineStoreFixture(t, ctx, pool, pipeline)
	if _, err := catalog.SoftDeleteDocument(ctx, permanent.identityID, permanent.documentID); err != nil {
		t.Fatalf("SoftDeleteDocument permanent: %v", err)
	}
	permanentJobs, err := store.Claim(ctx, ClaimDeleteJobsRequest{
		IdentityID: permanent.identityID, WorkerID: "permanent-delete-worker",
		LeaseDuration: time.Minute, BatchSize: 1,
	})
	if err != nil || len(permanentJobs) != 1 {
		t.Fatalf("Claim permanent = (%#v, %v)", permanentJobs, err)
	}
	dead, err := store.DeadLetter(ctx, DeadLetterDeleteJobRequest{
		DeleteJobFence: fenceForDeleteJob(permanentJobs[0]), ErrorCode: "snapshot_invalid",
	})
	if err != nil || dead.Status != "dead_letter" {
		t.Fatalf("DeadLetter = (%#v, %v)", dead, err)
	}
	assertDocumentLifecycleStatus(t, ctx, pool, permanent.identityID, permanent.documentID, "deleting")
}

func assertDocumentLifecycleStatus(
	t *testing.T,
	ctx context.Context,
	pool *pgxpool.Pool,
	identityID, documentID, want string,
) {
	t.Helper()
	var status string
	err := asDocumentIdentity(ctx, pool, identityID, func(tx pgx.Tx) error {
		return tx.QueryRow(ctx, `SELECT status FROM aura.documents
WHERE identity_id = $1 AND id = $2`, identityID, documentID).Scan(&status)
	})
	if err != nil || status != want {
		t.Fatalf("document status = %q, want %q (err=%v)", status, want, err)
	}
}
