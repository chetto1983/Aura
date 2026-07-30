//go:build db_integration

package documents

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"
)

// TestSetSearchDocumentStatusSpeaksTheSearchIDNamespace pins the seam the embedding worker
// actually calls. The worker holds one id — the content-derived doc_<hex> search id — and
// the catalog is keyed by an unrelated uuid. The first implementation of this method fed
// the search id straight to uuid.Parse, so every call errored, the worker swallowed the
// error into a WARN, and a document whose embeddings all failed kept advertising itself as
// "ready". Nothing caught it: the unit test's fake catalog stored whatever string it was
// handed. Only a real store over a real schema can tell the two namespaces apart.
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
		_, _ = pool.Exec(context.Background(), "DELETE FROM aura.documents WHERE id = $1", doc.ID)
	})

	if err := store.SetSearchDocumentStatus(ctx, searchDocumentID, DocumentStatusFailed, "embeddings never landed"); err != nil {
		t.Fatalf("SetSearchDocumentStatus: %v", err)
	}

	var status, reason string
	if err := pool.QueryRow(ctx,
		"SELECT status, coalesce(metadata->>'status_reason', '') FROM aura.documents WHERE id = $1",
		doc.ID).Scan(&status, &reason); err != nil {
		t.Fatalf("read back status: %v", err)
	}
	if status != string(DocumentStatusFailed) {
		t.Fatalf("status = %q, want %q — the document still claims a capability it does not have",
			status, DocumentStatusFailed)
	}
	if reason != "embeddings never landed" {
		t.Fatalf("status_reason = %q, want the cause the worker recorded", reason)
	}
}

// TestSetSearchDocumentStatusNamesTheUncataloguedCase covers legacy and deliberately
// ownerless ingests: chunks in the graph, a row in the ingest ledger, and no catalog row.
// The store must say so rather than silently report success on a row it never found.
func TestSetSearchDocumentStatusNamesTheUncataloguedCase(t *testing.T) {
	pool := migratedDocumentPool(t)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	store := NewPostgresCatalogStore(pool)
	orphan := DocumentID(fmt.Sprintf("never-catalogued-%d", time.Now().UnixNano()), "cli")

	err := store.SetSearchDocumentStatus(ctx, orphan, DocumentStatusFailed, "embeddings never landed")
	if !errors.Is(err, ErrDocumentNotCatalogued) {
		t.Fatalf("SetSearchDocumentStatus = %v, want ErrDocumentNotCatalogued", err)
	}

	if err := store.SetSearchDocumentStatus(ctx, "  ", DocumentStatusFailed, "x"); err == nil {
		t.Fatal("SetSearchDocumentStatus accepted an empty search document id")
	}
}

func TestOwnedCLIIngestCreatesProcessingCatalogRowThenEmbeddingMarksReady(t *testing.T) {
	pool := migratedDocumentPool(t)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	identityID := seedDocumentTestIdentity(t, ctx, pool)
	sourceID := fmt.Sprintf("owned-cli-catalog-%d", time.Now().UnixNano())
	path := writeNamedTempFile(t, "owned-cli.pdf", "owned CLI catalog fixture")
	service := &Service{
		Jobs:      NewPostgresJobStore(pool),
		Extractor: &fakeExtractor{resp: oneChunkResponse()},
		Indexer:   &fakeSparseIndexer{count: 1},
		Embedder:  &fakeEmbedQueue{},
		Catalog:   NewPostgresCatalogStore(pool),
	}
	t.Cleanup(func() {
		_, _ = pool.Exec(context.Background(), "DELETE FROM aura.document_ingest_jobs WHERE source_id = $1", sourceID)
	})

	job, err := service.IngestPath(ctx, IngestRequest{
		SourceID: sourceID, SourceKind: "local", IdentityID: identityID,
	}, path)
	if err != nil {
		t.Fatalf("owned CLI ingest: %v", err)
	}

	var catalogID, catalogIdentity, status, catalogJobID, catalogSourceID string
	err = pool.QueryRow(ctx, `
SELECT id::text, identity_id::text, status,
       metadata->>'document_job_id', metadata->>'source_id'
FROM aura.documents
WHERE metadata->>'search_document_id' = $1
  AND deleted_at IS NULL
ORDER BY created_at DESC
LIMIT 1`, job.DocumentID).Scan(
		&catalogID, &catalogIdentity, &status, &catalogJobID, &catalogSourceID,
	)
	if err != nil {
		t.Fatalf("read owned CLI catalog row: %v", err)
	}
	t.Cleanup(func() {
		_, _ = pool.Exec(context.Background(), "DELETE FROM aura.documents WHERE id = $1", catalogID)
	})
	if catalogIdentity != identityID || status != string(DocumentStatusProcessing) ||
		catalogJobID != job.ID || catalogSourceID != sourceID {
		t.Fatalf(
			"processing catalog row = identity:%q status:%q job:%q source:%q",
			catalogIdentity, status, catalogJobID, catalogSourceID,
		)
	}

	worker := &EmbeddingWorker{
		Jobs:       NewPostgresJobStore(pool),
		Generator:  &fakeEmbeddingGenerator{},
		Indexer:    &fakeEmbeddingIndexer{},
		Catalog:    NewPostgresCatalogStore(pool),
		MaxRetries: 1,
	}
	doc := service.Embedder.(*fakeEmbedQueue).docs[0]
	if err := worker.Process(ctx, doc); err != nil {
		t.Fatalf("embed owned CLI document: %v", err)
	}
	if err := pool.QueryRow(ctx, "SELECT status FROM aura.documents WHERE id = $1", catalogID).Scan(&status); err != nil {
		t.Fatalf("read ready catalog status: %v", err)
	}
	if status != string(DocumentStatusReady) {
		t.Fatalf("catalog status after embeddings = %q, want %q", status, DocumentStatusReady)
	}
}
