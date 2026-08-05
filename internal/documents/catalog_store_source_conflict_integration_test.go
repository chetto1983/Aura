//go:build db_integration

// What a document's source key means while it is dying, and after it is gone.
//
// aura.documents_source_unique was a FULL unique constraint over
// (identity_id, source_kind, source_key), so a deleted row kept owning its source forever
// and re-ingesting the same file returned 23505 — the user-visible shape being "delete a
// document, upload it again, nothing happens". documentForAssetVersion could not see it
// coming: it looks the document up with deleted_at IS NULL, finds nothing, and inserts
// into a constraint still counting the row it could not see.
//
// Deletion is asynchronous, which is what makes three tests necessary rather than one.
// deleted_at is written by FinalizeDocumentDelete, not by SoftDeleteDocument, so between
// them the row is live to the index and a naked upsert would attach a fresh upload to a
// document whose finalize then erases it. That case must fail loudly, not quietly.

package documents

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

func TestCatalogStoreReleasesSourceKeyOnFinalizedDelete(t *testing.T) {
	pool := pipelineDisposablePool(t)
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	fixture := newPipelineStoreFixture(t, ctx, pool, NewPostgresPipelineStore(pool))
	catalog := NewPostgresCatalogStore(pool)
	kind, key, searchID := documentSource(t, ctx, pool, fixture.identityID, fixture.documentID)

	finalizeDocumentDelete(t, ctx, pool, fixture.identityID, fixture.documentID)

	reingested, err := catalog.CreateDocument(ctx,
		sourceConflictRequest(fixture.identityID, kind, key, searchID, "Second upload"))
	if err != nil {
		t.Fatalf("re-ingest after finalized delete: %v", err)
	}
	if reingested.ID == fixture.documentID {
		t.Fatalf("re-ingest resurrected the deleted document %s", fixture.documentID)
	}
	if got := liveDocumentsForSource(t, ctx, pool, fixture.identityID, key); got != 1 {
		t.Fatalf("live documents for the source = %d, want exactly 1", got)
	}
}

func TestCatalogStoreRefusesReingestWhileDeleteInFlight(t *testing.T) {
	pool := pipelineDisposablePool(t)
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	fixture := newPipelineStoreFixture(t, ctx, pool, NewPostgresPipelineStore(pool))
	catalog := NewPostgresCatalogStore(pool)
	kind, key, searchID := documentSource(t, ctx, pool, fixture.identityID, fixture.documentID)

	deleting, err := catalog.SoftDeleteDocument(ctx, fixture.identityID, fixture.documentID)
	if err != nil || deleting.Status != DocumentStatusDeleting {
		t.Fatalf("SoftDeleteDocument = (%#v, %v)", deleting, err)
	}

	_, err = catalog.CreateDocument(ctx,
		sourceConflictRequest(fixture.identityID, kind, key, searchID, "Upload during delete"))
	if !errors.Is(err, ErrDocumentDeleteInFlight) {
		t.Fatalf("re-ingest during delete = %v, want ErrDocumentDeleteInFlight; "+
			"a silent upsert would attach these bytes to a document the finalize erases", err)
	}
}

func TestCatalogStoreRepeatLiveSourceIsIdempotent(t *testing.T) {
	pool := pipelineDisposablePool(t)
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	fixture := newPipelineStoreFixture(t, ctx, pool, NewPostgresPipelineStore(pool))
	catalog := NewPostgresCatalogStore(pool)
	kind, key, searchID := documentSource(t, ctx, pool, fixture.identityID, fixture.documentID)

	repeat, err := catalog.CreateDocument(ctx,
		sourceConflictRequest(fixture.identityID, kind, key, searchID, "Re-upload title"))
	if err != nil {
		t.Fatalf("repeat create: %v", err)
	}
	if repeat.ID != fixture.documentID {
		t.Fatalf("repeat create minted a second document: %s then %s", fixture.documentID, repeat.ID)
	}
	if repeat.Title == "Re-upload title" {
		t.Fatal("repeat create overwrote the title; an operator's edit must survive re-ingest")
	}
	if got := liveDocumentsForSource(t, ctx, pool, fixture.identityID, key); got != 1 {
		t.Fatalf("live documents for the source = %d, want exactly 1", got)
	}
}

// sourceConflictRequest builds a re-ingest of the SAME logical file the fixture already
// catalogued: same identity, same source, same search id — a real re-upload reproduces all
// three, and the search id has its own live-only unique index that would fire instead of
// the one under test if it were left to default or derived differently.
func sourceConflictRequest(identityID, kind, key, searchID, title string) CreateDocumentRequest {
	return CreateDocumentRequest{
		IdentityID:       identityID,
		SourceKind:       kind,
		SourceKey:        key,
		SearchDocumentID: searchID,
		Scope:            DocumentScopeLibrary,
		Title:            title,
		Status:           DocumentStatusStored,
		Metadata:         map[string]any{"source": "source-conflict-test"},
	}
}

// documentSource reads back the fixture document's source and search id. Re-ingesting one
// file means reproducing all three: the source pair is what this change scopes to live
// rows, and the search id has its own live-only unique index that would otherwise be the
// constraint that fires.
func documentSource(
	t *testing.T, ctx context.Context, pool *pgxpool.Pool, identityID, documentID string,
) (kind, key, searchID string) {
	t.Helper()
	if err := asDocumentIdentity(ctx, pool, identityID, func(tx pgx.Tx) error {
		return tx.QueryRow(ctx,
			`SELECT source_kind, source_key, search_document_id FROM aura.documents
			 WHERE identity_id = $1::uuid AND id = $2::uuid`,
			identityID, documentID).Scan(&kind, &key, &searchID)
	}); err != nil {
		t.Fatalf("read document source: %v", err)
	}
	return kind, key, searchID
}

// liveDocumentsForSource counts non-deleted documents for one identity's source key, inside
// an identity-bound transaction — aura.documents fails RLS closed (migration 0087), so an
// unscoped read on the raw pool would silently return zero rows instead of erroring.
func liveDocumentsForSource(
	t *testing.T, ctx context.Context, pool *pgxpool.Pool, identityID, key string,
) int {
	t.Helper()
	var count int
	if err := asDocumentIdentity(ctx, pool, identityID, func(tx pgx.Tx) error {
		return tx.QueryRow(ctx,
			`SELECT count(*) FROM aura.documents
			 WHERE identity_id = $1::uuid AND source_key = $2 AND deleted_at IS NULL`,
			identityID, key).Scan(&count)
	}); err != nil {
		t.Fatalf("count live documents: %v", err)
	}
	return count
}

// finalizeDocumentDelete drives the real durable delete to completion, the way the worker
// does: soft-delete to enqueue the job, claim it, erase every object in its snapshot, then
// finalize with the projection verified. Only that path writes documents.deleted_at — see
// delete_durable_integration_test.go:15-90, which this mirrors.
func finalizeDocumentDelete(
	t *testing.T, ctx context.Context, pool *pgxpool.Pool, identityID, documentID string,
) {
	t.Helper()
	catalog := NewPostgresCatalogStore(pool)
	deleting, err := catalog.SoftDeleteDocument(ctx, identityID, documentID)
	if err != nil || deleting.Status != DocumentStatusDeleting {
		t.Fatalf("SoftDeleteDocument = (%#v, %v)", deleting, err)
	}

	store := NewPostgresDurableDeleteStore(pool)
	jobs, err := store.Claim(ctx, ClaimDeleteJobsRequest{
		IdentityID: identityID, WorkerID: "source-conflict-finalize-worker",
		LeaseDuration: time.Minute, BatchSize: 1,
	})
	if err != nil || len(jobs) != 1 {
		t.Fatalf("Claim = (%#v, %v)", jobs, err)
	}
	job := jobs[0]
	snapshot, err := normalizedDeleteSnapshot(job)
	if err != nil {
		t.Fatalf("normalizedDeleteSnapshot: %v", err)
	}
	for _, object := range snapshot.Objects {
		if err := store.MarkObjectDeleted(ctx, MarkDeletedObjectRequest{
			DeleteJobFence: fenceForDeleteJob(job), StorageObjectID: object.ID,
			DeletionGeneration: object.DeletionGeneration,
		}); err != nil {
			t.Fatalf("MarkObjectDeleted(%s): %v", object.ID, err)
		}
	}
	if _, err := store.Finalize(ctx, FinalizeDeleteJobRequest{
		DeleteJobFence: fenceForDeleteJob(job), ProjectionVerified: true,
	}); err != nil {
		t.Fatalf("Finalize: %v", err)
	}
}
