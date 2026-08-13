//go:build db_integration

package documents

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
)

// TestSetSearchDocumentStatusSpeaksTheSearchIDNamespace pins the seam IngestPath calls
// when a document is catalogued but its job never reached searchable. The caller holds one
// id — the content-derived doc_<hex> search id — and the catalog is keyed by an unrelated
// uuid. The first implementation of this method fed the search id straight to uuid.Parse,
// so every call errored, the caller swallowed the error into a WARN, and a document whose
// ingestion failed kept advertising itself as "ready". Nothing caught it: the unit test's
// fake catalog stored whatever string it was handed. Only a real store over a real schema
// can tell the two namespaces apart.
func TestSetSearchDocumentStatusSpeaksTheSearchIDNamespace(t *testing.T) {
	pool := pipelineDisposablePool(t)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	identityID := seedDocumentTestIdentity(t, ctx, pool)
	searchDocumentID := DocumentID(fmt.Sprintf("catalog-store-%d", time.Now().UnixNano()), "catalog-store-test")
	store := NewPostgresCatalogStore(pool)

	// Catalogued, not published: 'ready' is admissible only at a positive pipeline
	// generation since 0093, and this test is about which id namespace the status write
	// speaks — it never needs the document to be rankable.
	doc, err := store.CreateDocument(ctx, CreateDocumentRequest{
		IdentityID:       identityID,
		Scope:            DocumentScopeLibrary,
		Title:            "catalog store integration",
		SourceKind:       "test",
		SourceKey:        searchDocumentID,
		SearchDocumentID: searchDocumentID,
		Metadata:         map[string]any{"search_document_id": searchDocumentID},
		Status:           DocumentStatusStored,
	})
	if err != nil {
		t.Fatalf("CreateDocument: %v", err)
	}
	t.Cleanup(func() {
		_ = asDocumentIdentity(context.Background(), pool, identityID, func(tx pgx.Tx) error {
			_, err := tx.Exec(context.Background(), "DELETE FROM aura.documents WHERE id = $1", doc.ID)
			return err
		})
	})

	if err := store.SetSearchDocumentStatus(
		ctx, identityID, searchDocumentID, DocumentStatusFailed, "ingest never finished",
	); err != nil {
		t.Fatalf("SetSearchDocumentStatus: %v", err)
	}

	// The cause lives in the error_message COLUMN, not a metadata key: 0093 gave documents
	// error_code/error_message as first-class lifecycle state (activation clears both), and
	// SetDocumentStatus writes there. Reading metadata->>'status_reason' compared "" to ""
	// and would have passed no matter what the store did.
	var status, reason string
	if err := asDocumentIdentity(ctx, pool, identityID, func(tx pgx.Tx) error {
		return tx.QueryRow(ctx,
			"SELECT status, error_message FROM aura.documents WHERE id = $1",
			doc.ID).Scan(&status, &reason)
	}); err != nil {
		t.Fatalf("read back status: %v", err)
	}
	if status != string(DocumentStatusFailed) {
		t.Fatalf("status = %q, want %q — the document still claims a capability it does not have",
			status, DocumentStatusFailed)
	}
	if reason != "ingest never finished" {
		t.Fatalf("status_reason = %q, want the recorded cause", reason)
	}
}

// TestSetSearchDocumentStatusNamesTheUncataloguedCase covers legacy and deliberately
// ownerless ingests: a row in the ingest ledger and no catalog row. The store must say so
// rather than silently report success on a row it never found.
func TestSetSearchDocumentStatusNamesTheUncataloguedCase(t *testing.T) {
	pool := pipelineDisposablePool(t)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	identityID := seedDocumentTestIdentity(t, ctx, pool)
	store := NewPostgresCatalogStore(pool)
	orphan := DocumentID(fmt.Sprintf("never-catalogued-%d", time.Now().UnixNano()), "cli")

	err := store.SetSearchDocumentStatus(ctx, identityID, orphan, DocumentStatusFailed, "ingest never finished")
	if !errors.Is(err, ErrDocumentNotCatalogued) {
		t.Fatalf("SetSearchDocumentStatus = %v, want ErrDocumentNotCatalogued", err)
	}

	if err := store.SetSearchDocumentStatus(ctx, identityID, "  ", DocumentStatusFailed, "x"); err == nil {
		t.Fatal("SetSearchDocumentStatus accepted an empty search document id")
	}
}

func TestOwnedIngestCreatesAStoredCatalogRow(t *testing.T) {
	pool := pipelineDisposablePool(t)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	identityID := seedDocumentTestIdentity(t, ctx, pool)
	sourceID := fmt.Sprintf("owned-cli-catalog-%d", time.Now().UnixNano())
	path := writeNamedTempFile(t, "owned-cli.pdf", "owned CLI catalog fixture")
	service := &Service{
		Jobs:    NewPostgresJobStore(pool),
		Catalog: NewPostgresCatalogStore(pool),
	}
	t.Cleanup(func() {
		_, _ = pool.Exec(context.Background(), "DELETE FROM aura.document_ingest_jobs WHERE source_id = $1", sourceID)
	})

	job, err := service.IngestPath(ctx, IngestRequest{
		SourceID: sourceID, SourceKind: "local", IdentityID: identityID,
	}, path)
	if err != nil {
		t.Fatalf("owned ingest: %v", err)
	}
	// accepted is the resting state, not a stall. Registration hands off to the pipeline,
	// and activation settles aura.documents rather than this ledger — nothing has written
	// JobSearchable since the convergence, so asserting it here only pinned a transition
	// that no longer exists.
	if job.Status != JobAccepted {
		t.Fatalf("job status = %q, want %q", job.Status, JobAccepted)
	}

	var catalogID, catalogIdentity, status, catalogJobID, catalogSourceKey string
	err = asDocumentIdentity(ctx, pool, identityID, func(tx pgx.Tx) error {
		return tx.QueryRow(ctx, `
SELECT id::text, identity_id::text, status,
       metadata->>'document_job_id', source_key
FROM aura.documents
WHERE metadata->>'search_document_id' = $1
  AND deleted_at IS NULL
ORDER BY created_at DESC
LIMIT 1`, job.DocumentID).Scan(
			&catalogID, &catalogIdentity, &status, &catalogJobID, &catalogSourceKey,
		)
	})
	if err != nil {
		t.Fatalf("read owned catalog row: %v", err)
	}
	t.Cleanup(func() {
		_ = asDocumentIdentity(context.Background(), pool, identityID, func(tx pgx.Tx) error {
			_, err := tx.Exec(context.Background(), "DELETE FROM aura.documents WHERE id = $1", catalogID)
			return err
		})
	})
	// Stored, not ready: 0093 gave the pipeline a later stage, and publishing is now its
	// job. A row that said 'ready' the instant it was registered is exactly the claim the
	// convergence removed — it advertised a document nothing had converted yet.
	//
	// The locator is asserted through the source_key COLUMN, not metadata: ingest stopped
	// copying source_id into the metadata blob when the column became the real one, so
	// reading it back from metadata would compare "" against "" and prove nothing.
	wantSourceKey, err := SourceKey(sourceID, "")
	if err != nil {
		t.Fatalf("SourceKey: %v", err)
	}
	if catalogIdentity != identityID || status != string(DocumentStatusStored) ||
		catalogJobID != job.ID || catalogSourceKey != wantSourceKey {
		t.Fatalf(
			"catalog row = identity:%q status:%q job:%q source_key:%q (want source_key %q)",
			catalogIdentity, status, catalogJobID, catalogSourceKey, wantSourceKey,
		)
	}
}
