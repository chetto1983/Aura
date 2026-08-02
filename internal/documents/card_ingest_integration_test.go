//go:build db_integration

package documents

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const cardIngestIdentity = "00000000-0000-0000-0000-0000000000d4"

// TestIngestCardMakesAFileFindableByWhatIsInsideIt is the acceptance test for the
// whole card: a file is uploaded, NO agent opens it, and document_search finds it
// by a value that appears nowhere but inside the file. Before the card, ingest
// wrote a title and nothing else, and this query returned nothing — the library
// only knew about documents somebody had already found another way.
func TestIngestCardMakesAFileFindableByWhatIsInsideIt(t *testing.T) {
	pool := digestSearchPool(t)
	ctx := context.Background()
	store := NewPostgresCatalogStore(pool)
	seedCardIngestIdentity(t)

	path := filepath.Join(t.TempDir(), "export_2024_final_v3.csv")
	body := "Area;Localita;Agente\nNORD;GHEDI;ROSSI\nNORD;GHEDI;VERDI\nSUD;GENOVA;BIANCHI\n"
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}

	service := &Service{Jobs: NewPostgresJobStore(pool), Catalog: store}
	job, err := service.IngestPath(ctx, IngestRequest{
		SourceID: "card-ingest-test", SourceKind: "local", IdentityID: cardIngestIdentity,
	}, path)
	if err != nil {
		t.Fatalf("ingest: %v", err)
	}

	// GHEDI is in no filename, no tag and no agent note. Only the card has it.
	hits, err := store.SearchDigests(ctx, cardIngestIdentity, "ghedi", 5)
	if err != nil {
		t.Fatalf("search: %v", err)
	}
	if len(hits) != 1 {
		t.Fatalf("hits = %d, want exactly the ingested file", len(hits))
	}
	if hits[0].Title != "export_2024_final_v3.csv" {
		t.Fatalf("top hit = %q", hits[0].Title)
	}
	if !strings.Contains(hits[0].Card, "GHEDI") || !strings.Contains(hits[0].Card, "Area, Localita, Agente") {
		t.Fatalf("card did not travel with the hit:\n%s", hits[0].Card)
	}
	if hits[0].Digest != "" {
		t.Fatalf("ingest wrote the agent's column: %q", hits[0].Digest)
	}

	// The card is the machine's; the note is the agent's. Writing one must not
	// touch the other, or the first thing document_describe says deletes the
	// structure ingest measured.
	if err := store.SetDigest(ctx, cardIngestIdentity, job.DocumentID,
		"Estrazione clienti per la revisione agenti 2026."); err != nil {
		t.Fatalf("set digest: %v", err)
	}
	after, err := store.SearchDigests(ctx, cardIngestIdentity, "ghedi", 5)
	if err != nil || len(after) != 1 {
		t.Fatalf("search after describe: %d hits, err %v", len(after), err)
	}
	if !strings.Contains(after[0].Card, "GHEDI") {
		t.Fatal("the agent's note overwrote the machine card")
	}
	if !strings.Contains(after[0].Digest, "revisione agenti") {
		t.Fatalf("the note did not land: %q", after[0].Digest)
	}
	// Both legs are in the ranking vector, so the note is searchable too.
	byNote, err := store.SearchDigests(ctx, cardIngestIdentity, "revisione agenti", 5)
	if err != nil || len(byNote) != 1 {
		t.Fatalf("note is not indexed: %d hits, err %v", len(byNote), err)
	}
}

// TestAssetVersionReusesTheCatalogRowIngestAlreadyWrote pins the duplicate an
// upload produced: documents.Service registers the file and writes its card, then
// the version recorder records the asset. When the second writer INSERTED, every
// upload became two library rows for one file, and the newest — the one
// document_open resolves — was the one without a card.
func TestAssetVersionReusesTheCatalogRowIngestAlreadyWrote(t *testing.T) {
	pool := digestSearchPool(t)
	ctx := context.Background()
	store := NewPostgresCatalogStore(pool)
	seedCardIngestIdentity(t)

	path := filepath.Join(t.TempDir(), "listino.csv")
	if err := os.WriteFile(path, []byte("Codice;Fornitore\nA1;ACME SPA\nA2;ACME SPA\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	service := &Service{Jobs: NewPostgresJobStore(pool), Catalog: store}
	job, err := service.IngestPath(ctx, IngestRequest{
		SourceID: "card-asset-test", SourceKind: "upload", IdentityID: cardIngestIdentity,
	}, path)
	if err != nil {
		t.Fatalf("ingest: %v", err)
	}

	if _, err := store.RecordAssetVersion(ctx, RecordAssetVersionRequest{
		IdentityID:       cardIngestIdentity,
		Scope:            DocumentScopeLibrary,
		Title:            "listino.csv",
		FileName:         "listino.csv",
		MIMEType:         "text/csv",
		SizeBytes:        42,
		ObjectBucket:     "aura-assets",
		ObjectKey:        "card-asset-test/listino.csv",
		SearchDocumentID: job.DocumentID,
		JobID:            job.ID,
		SHA256:           "e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855",
		VersionNumber:    1,
		DocumentStatus:   DocumentStatusReady,
		VersionStatus:    "ready",
		StorageKind:      defaultStorageKind,
		RetentionClass:   defaultRetentionClass,
		Metadata: map[string]any{
			"search_document_id": job.DocumentID,
			"document_job_id":    job.ID,
		},
	}); err != nil {
		t.Fatalf("record asset version: %v", err)
	}

	var rows int
	if err := pool.QueryRow(ctx, `
SELECT count(*) FROM aura.documents
WHERE identity_id = $1 AND deleted_at IS NULL AND metadata->>'search_document_id' = $2`,
		cardIngestIdentity, job.DocumentID).Scan(&rows); err != nil {
		t.Fatal(err)
	}
	if rows != 1 {
		t.Fatalf("catalog rows for one uploaded file = %d, want 1", rows)
	}
	hits, err := store.SearchDigests(ctx, cardIngestIdentity, "acme", 5)
	if err != nil {
		t.Fatalf("search: %v", err)
	}
	if len(hits) != 1 || !strings.Contains(hits[0].Card, "ACME SPA") {
		t.Fatalf("recording the asset version lost the card: %d hits", len(hits))
	}
}

func seedCardIngestIdentity(t *testing.T) {
	t.Helper()
	pool := digestSearchPool(t)
	ctx := context.Background()
	if _, err := pool.Exec(ctx, `
INSERT INTO aura.identities (id, name, kind)
VALUES ($1, 'card-ingest-test', 'service') ON CONFLICT (id) DO NOTHING`, cardIngestIdentity); err != nil {
		t.Fatalf("seed identity: %v", err)
	}
	if _, err := pool.Exec(ctx,
		`DELETE FROM aura.documents WHERE identity_id = $1`, cardIngestIdentity); err != nil {
		t.Fatalf("clear: %v", err)
	}
	t.Cleanup(func() {
		_, _ = pool.Exec(context.Background(),
			`DELETE FROM aura.documents WHERE identity_id = $1`, cardIngestIdentity)
	})
}
