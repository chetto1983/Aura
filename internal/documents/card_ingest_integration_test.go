//go:build db_integration

package documents

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

const cardIngestIdentity = "00000000-0000-0000-0000-0000000000d4"

// TestIngestWritesACardDescribingWhatIsInsideTheFile is the acceptance test for the
// card: a file is uploaded, NO agent opens it, and aura.documents.card already describes
// what is inside by a value that appears nowhere but in the bytes. Before the card, ingest
// wrote a title and nothing else — the library only knew about documents somebody had
// already found another way.
//
// It reads the column rather than searching it. Postgres ranking over the card was deleted
// on 2026-08-15 with the rest of the digest subsystem: routing is ArcadeDB's, and asserting
// through a query with no production caller proved the query, not the write.
func TestIngestWritesACardDescribingWhatIsInsideTheFile(t *testing.T) {
	pool := pipelineDisposablePool(t)
	ctx := context.Background()
	store := NewPostgresCatalogStore(pool)
	seedCardIngestIdentity(t, pool)

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

	// GHEDI is in no filename and no tag. Only the card has it.
	title, card := readCatalogCard(t, ctx, pool, job.DocumentID)
	if title != "export_2024_final_v3.csv" {
		t.Fatalf("catalogued title = %q", title)
	}
	if !strings.Contains(card, "GHEDI") || !strings.Contains(card, "Area, Localita, Agente") {
		t.Fatalf("ingest did not describe the file it read:\n%s", card)
	}
}

// readCatalogCard returns the title and card ingest wrote for one search document id.
func readCatalogCard(t *testing.T, ctx context.Context, pool *pgxpool.Pool, searchDocumentID string) (string, string) {
	t.Helper()
	var title, card string
	if err := asDocumentIdentity(ctx, pool, cardIngestIdentity, func(tx pgx.Tx) error {
		return tx.QueryRow(ctx, `
SELECT title, card FROM aura.documents
WHERE identity_id = $1 AND deleted_at IS NULL AND search_document_id = $2`,
			cardIngestIdentity, searchDocumentID).Scan(&title, &card)
	}); err != nil {
		t.Fatalf("read catalogued card: %v", err)
	}
	return title, card
}

// TestAssetVersionReusesTheCatalogRowIngestAlreadyWrote pins the duplicate an
// upload produced: documents.Service registers the file and writes its card, then
// the version recorder records the asset. When the second writer INSERTED, every
// upload became two library rows for one file, and the newest — the one
// document_open resolves — was the one without a card.
func TestAssetVersionReusesTheCatalogRowIngestAlreadyWrote(t *testing.T) {
	pool := pipelineDisposablePool(t)
	ctx := context.Background()
	store := NewPostgresCatalogStore(pool)
	seedCardIngestIdentity(t, pool)

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

	// The second writer: an asset arrives for the file ingest already catalogued.
	// recordAssetVersionFor runs the production RecordAssetVersion — it needs a real asset
	// row to bind, which is the only reason it is not inlined here — so the count below
	// answers the question that matters: did recording the asset produce a SECOND row?
	recordAssetVersionFor(t, ctx, pool, cardIngestIdentity, job.DocumentID, "listino.csv")

	var rows int
	if err := asDocumentIdentity(ctx, pool, cardIngestIdentity, func(tx pgx.Tx) error {
		return tx.QueryRow(ctx, `
SELECT count(*) FROM aura.documents
WHERE identity_id = $1 AND deleted_at IS NULL AND metadata->>'search_document_id' = $2`,
			cardIngestIdentity, job.DocumentID).Scan(&rows)
	}); err != nil {
		t.Fatal(err)
	}
	if rows != 1 {
		t.Fatalf("catalog rows for one uploaded file = %d, want 1", rows)
	}
	if _, card := readCatalogCard(t, ctx, pool, job.DocumentID); !strings.Contains(card, "ACME SPA") {
		t.Fatalf("recording the asset version lost the card:\n%s", card)
	}
}

// Takes the caller's pool rather than opening its own: every pool here is a database of
// its own, so a helper that made a second one would seed the identity somewhere the test
// never looks. Against the shared live database the two pools happened to agree, which is
// exactly why the coupling survived unnoticed.
func seedCardIngestIdentity(t *testing.T, pool *pgxpool.Pool) {
	t.Helper()
	ctx := context.Background()
	if _, err := pool.Exec(ctx, `
INSERT INTO aura.identities (id, name, kind)
VALUES ($1, 'card-ingest-test', 'service') ON CONFLICT (id) DO NOTHING`, cardIngestIdentity); err != nil {
		t.Fatalf("seed identity: %v", err)
	}
	// Both the clear and the cleanup go through a bound principal. Off the bare pool they
	// are no-ops under 0087's fail-closed floor — reporting success having deleted nothing
	// — and the second run of this file would then find two GHEDI documents where its
	// assertions expect exactly one.
	if err := clearCardIngestDocuments(ctx, pool); err != nil {
		t.Fatalf("clear: %v", err)
	}
	t.Cleanup(func() {
		_ = clearCardIngestDocuments(context.Background(), pool)
	})
}

func clearCardIngestDocuments(ctx context.Context, pool *pgxpool.Pool) error {
	return asDocumentIdentity(ctx, pool, cardIngestIdentity, func(tx pgx.Tx) error {
		_, err := tx.Exec(ctx, `DELETE FROM aura.documents WHERE identity_id = $1`, cardIngestIdentity)
		return err
	})
}
