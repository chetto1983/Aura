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
	pool := migratedDocumentPool(t)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	identityID := seedDocumentTestIdentity(t, ctx, pool)
	searchDocumentID := DocumentID(fmt.Sprintf("catalog-store-%d", time.Now().UnixNano()), "catalog-store-test")
	store := NewPostgresCatalogStore(pool)

	doc, err := store.CreateDocument(ctx, CreateDocumentRequest{
		IdentityID: identityID,
		Scope:      DocumentScopeLibrary,
		Title:      "catalog store integration",
		Metadata:   map[string]any{"search_document_id": searchDocumentID},
		Status:     DocumentStatusReady,
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

	var status, reason string
	if err := asDocumentIdentity(ctx, pool, identityID, func(tx pgx.Tx) error {
		return tx.QueryRow(ctx,
			"SELECT status, coalesce(metadata->>'status_reason', '') FROM aura.documents WHERE id = $1",
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
	pool := migratedDocumentPool(t)
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

func TestOwnedIngestCreatesAReadyCatalogRow(t *testing.T) {
	pool := migratedDocumentPool(t)
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
	if job.Status != JobSearchable {
		t.Fatalf("job status = %q, want %q", job.Status, JobSearchable)
	}

	var catalogID, catalogIdentity, status, catalogJobID, catalogSourceID string
	err = asDocumentIdentity(ctx, pool, identityID, func(tx pgx.Tx) error {
		return tx.QueryRow(ctx, `
SELECT id::text, identity_id::text, status,
       metadata->>'document_job_id', metadata->>'source_id'
FROM aura.documents
WHERE metadata->>'search_document_id' = $1
  AND deleted_at IS NULL
ORDER BY created_at DESC
LIMIT 1`, job.DocumentID).Scan(
			&catalogID, &catalogIdentity, &status, &catalogJobID, &catalogSourceID,
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
	// Ready, not processing: no later stage exists to promote it.
	if catalogIdentity != identityID || status != string(DocumentStatusReady) ||
		catalogJobID != job.ID || catalogSourceID != sourceID {
		t.Fatalf(
			"catalog row = identity:%q status:%q job:%q source:%q",
			catalogIdentity, status, catalogJobID, catalogSourceID,
		)
	}
}
