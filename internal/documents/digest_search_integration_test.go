//go:build db_integration

package documents

import (
	"context"
	"os"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

const digestSearchIdentity = "00000000-0000-0000-0000-0000000000d2"

func digestSearchPool(t *testing.T) *pgxpool.Pool {
	t.Helper()
	dsn := strings.TrimSpace(os.Getenv("AURA_DB_URL"))
	if dsn == "" {
		if os.Getenv("CI") != "" {
			t.Fatal("AURA_DB_URL must be set in CI: a skipped integration tier is a falsely-green job")
		}
		t.Skip("AURA_DB_URL not set")
	}
	pool, err := pgxpool.New(context.Background(), dsn)
	if err != nil {
		t.Skipf("postgres: %v", err)
	}
	t.Cleanup(pool.Close)
	return pool
}

func clearDigestSearchDocuments(ctx context.Context, pool *pgxpool.Pool) error {
	return asDocumentIdentity(ctx, pool, digestSearchIdentity, func(tx pgx.Tx) error {
		_, err := tx.Exec(ctx, `DELETE FROM aura.documents WHERE identity_id = $1`, digestSearchIdentity)
		return err
	})
}

// TestSearchDigestsFindsADocumentByAWordInsideItsFilename pins the defect 0081
// fixed. Postgres' text parser classifies `Clienti.xlsx` as ONE `file` token, so
// under 0080 the library returned zero hits for the single most likely query
// there is — the document's own name — while happily listing it. A ranking that
// cannot find a file by its name is not a library.
func TestSearchDigestsFindsADocumentByAWordInsideItsFilename(t *testing.T) {
	pool := digestSearchPool(t)
	ctx := context.Background()
	store := NewPostgresCatalogStore(pool)

	// The clear, the fixture insert and the cleanup all bind the principal: aura.documents
	// fails closed under 0087, so off the bare pool the INSERT is refused 42501 outright
	// and the two DELETEs report success having removed nothing.
	if err := clearDigestSearchDocuments(ctx, pool); err != nil {
		t.Fatalf("clear: %v", err)
	}
	// The FK is real, so the seed must SUCCEED, not be logged and skipped: the
	// insert below fails with 23503 otherwise and the test reports a schema
	// mistake as a retrieval failure. Columns are (id, name, kind) and kind is
	// constrained to system/user/channel/service.
	if _, err := pool.Exec(ctx, `
INSERT INTO aura.identities (id, name, kind)
VALUES ($1, 'digest-search-test', 'service') ON CONFLICT (id) DO NOTHING`,
		digestSearchIdentity); err != nil {
		t.Fatalf("seed identity: %v", err)
	}
	// Catalogue, publish, then describe — the order production uses. The row cannot be
	// inserted 'ready' directly: 0093 admits that status only at a positive pipeline
	// generation, and SearchDigests ranks nothing that lacks a live active version.
	// publishForSearch drives the real activation so this fixture cannot drift from what
	// the pipeline actually produces.
	searchDocumentID := "doc_digest_search_" + uuid.NewString()
	doc, err := store.CreateDocument(ctx, CreateDocumentRequest{
		IdentityID: digestSearchIdentity, Scope: DocumentScopeLibrary,
		Title: "Clienti.xlsx", Tags: []string{"fatturato-2026"},
		SourceKind: "test", SourceKey: searchDocumentID,
		SearchDocumentID: searchDocumentID, Status: DocumentStatusStored,
	})
	if err != nil {
		t.Fatalf("create document: %v", err)
	}
	t.Cleanup(func() {
		_ = clearDigestSearchDocuments(context.Background(), pool)
	})
	publishForSearch(t, ctx, pool, digestSearchIdentity, searchDocumentID, "Clienti.xlsx")
	if err := store.SetDigest(ctx, digestSearchIdentity, doc.ID,
		"Clienti.xlsx — Tabular data. Clienti (5889 rows): "+
			"Ragione sociale, Località, Venditore Esterno"); err != nil {
		t.Fatalf("set digest: %v", err)
	}

	for _, query := range []string{
		"clienti",         // a word INSIDE the filename — the 0080 miss
		"Clienti.xlsx",    // the filename as written
		"ragione sociale", // a column header, from the digest
		"fatturato-2026",  // an operator-written tag
		"venditore",       // one word of a header
	} {
		t.Run(query, func(t *testing.T) {
			hits, err := store.SearchDigests(ctx, digestSearchIdentity, query, 5)
			if err != nil {
				t.Fatalf("search: %v", err)
			}
			if len(hits) == 0 {
				t.Fatalf("%q found nothing", query)
			}
			if hits[0].Title != "Clienti.xlsx" {
				t.Fatalf("%q top1 = %q", query, hits[0].Title)
			}
			if len(hits[0].Tags) != 1 || hits[0].Tags[0] != "fatturato-2026" {
				t.Errorf("tags did not travel with the hit: %v", hits[0].Tags)
			}
			t.Logf("%-18s rank %.4f", query, hits[0].Rank)
		})
	}

	t.Run("blank lists the library", func(t *testing.T) {
		hits, err := store.SearchDigests(ctx, digestSearchIdentity, "", 5)
		if err != nil || len(hits) == 0 {
			t.Fatalf("blank query: %d hits, err %v", len(hits), err)
		}
	})

	t.Run("another identity sees nothing", func(t *testing.T) {
		// A uuid that owns no documents: the scoping is in SQL, so this asserts the
		// WHERE clause, not the absence of a row.
		hits, err := store.SearchDigests(ctx, "00000000-0000-0000-0000-0000000000d3", "clienti", 5)
		if err != nil {
			t.Fatalf("search: %v", err)
		}
		if len(hits) != 0 {
			t.Fatalf("library is not identity-scoped: %d hits", len(hits))
		}
	})
}
