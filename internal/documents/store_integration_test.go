//go:build db_integration

package documents

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/chetto1983/aura/internal/identityctx"
)

func TestPostgresJobStoreRoundTrip(t *testing.T) {
	pool := pipelineDisposablePool(t)
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	sourceID := fmt.Sprintf("doc-store-test-%d", time.Now().UnixNano())
	t.Cleanup(func() {
		_, _ = pool.Exec(context.Background(), "DELETE FROM aura.document_ingest_jobs WHERE source_id = $1", sourceID)
	})

	store := NewPostgresJobStore(pool)
	documentID := DocumentID("hash", sourceID)
	// The ledger is owner-scoped since 0093: identity_id is NOT NULL with a real FK, so an
	// ownerless job is no longer a thing the store will accept. Every READ below resolves
	// its owner from the CONTEXT rather than an argument (callerJobIdentity), so the
	// principal has to travel on ctx too — Create alone is not enough.
	identityID := seedDocumentTestIdentity(t, ctx, pool)
	ctx = identityctx.WithIdentityID(ctx, identityID)
	created, err := store.Create(ctx, CreateJobParams{
		IdentityID:   identityID,
		SourceID:     sourceID,
		SourceKind:   "local",
		DocumentID:   documentID,
		ContentHash:  "hash",
		OriginalPath: "/tmp/manual.pdf",
		FileName:     "manual.pdf",
		MIMEType:     "application/pdf",
		SizeBytes:    42,
		Status:       JobAccepted,
	})
	if err != nil {
		t.Fatal(err)
	}
	if created.ID == "" || created.Status != JobAccepted {
		t.Fatalf("created job = %#v", created)
	}

	got, err := store.Get(ctx, created.ID)
	if err != nil || got.DocumentID != documentID {
		t.Fatalf("Get = (%#v, %v)", got, err)
	}
	byDoc, err := store.GetByDocumentID(ctx, documentID)
	if err != nil || byDoc.ID != created.ID {
		t.Fatalf("GetByDocumentID = (%#v, %v)", byDoc, err)
	}
	failed, err := store.UpdateStatus(ctx, created.ID, JobFailed, "transient ingest failure")
	if err != nil || failed.Error == "" {
		t.Fatalf("UpdateStatus failed = (%#v, %v)", failed, err)
	}
	searchable, err := store.UpdateProgress(ctx, created.ID, JobSearchable, 3, 0)
	if err != nil || searchable.Status != JobSearchable || searchable.SparseChunks != 3 ||
		searchable.Error != "" || searchable.SearchableAt.IsZero() {
		t.Fatalf("UpdateProgress searchable = (%#v, %v)", searchable, err)
	}
	complete, err := store.UpdateProgress(ctx, created.ID, JobComplete, 3, 3)
	if err != nil || complete.Status != JobComplete || complete.EmbeddedChunks != 3 || complete.CompletedAt.IsZero() {
		t.Fatalf("UpdateProgress complete = (%#v, %v)", complete, err)
	}
	recent, err := store.ListRecent(ctx, 0)
	if err != nil {
		t.Fatal(err)
	}
	for _, job := range recent {
		if job.ID == created.ID {
			return
		}
	}
	t.Fatalf("created job %s missing from ListRecent", created.ID)
}

func TestPostgresJobStoreRejectsInvalidIDs(t *testing.T) {
	store := &PostgresJobStore{}
	if _, err := store.Get(t.Context(), "bad"); err == nil {
		t.Fatal("Get accepted invalid uuid")
	}
	if _, err := store.UpdateStatus(t.Context(), "bad", JobFailed, "boom"); err == nil {
		t.Fatal("UpdateStatus accepted invalid uuid")
	}
	if _, err := store.UpdateProgress(t.Context(), "bad", JobSearchable, 1, 0); err == nil {
		t.Fatal("UpdateProgress accepted invalid uuid")
	}
}
