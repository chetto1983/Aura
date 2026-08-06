//go:build db_integration

package documents

import (
	"context"
	"testing"

	"github.com/google/uuid"
)

// TestRouteDocumentCardsAnswersAnUnscopedQuery pins the defect that made the whole card leg
// silent in production.
//
// An absent document scope means "every ready document". It reaches RouteDocumentCards as a
// NIL []string — ResolveDocumentScope returns nil early when the caller named no ids — and
// pgx encodes a nil slice as SQL NULL, not as an empty array. The predicate was written
// `$3::text[] = '{}'::text[] OR document.id::text = ANY($3::text[])`, and under three-valued
// logic both halves are NULL when $3 is NULL, so the row was filtered out. Every unscoped
// call returned ZERO cards.
//
// Nothing caught it because the cascade hides it: HostRetriever also runs lexical and dense
// legs over ArcadeDB, so document_search still answered whenever a passage matched, and the
// missing leg only showed up as thinner recall. It is measured operator damage, not theory —
// on 2026-08-06 document_search("Clienti") returned nothing for a ready, activated
// Clienti.xlsx while document_search("Clienti.xlsx") found it.
//
// The sharper edge is the degraded path: when ArcadeDB is unreachable, Retrieve falls back to
// card-only and returns exactly these cards. With this defect that fallback returned an empty
// result set while reporting status "degraded_card_only" — a total retrieval failure dressed
// as a partial one.
//
// The nil case is therefore the assertion that matters; the others are here so a fix that
// simply ignores the scope cannot pass.
func TestRouteDocumentCardsAnswersAnUnscopedQuery(t *testing.T) {
	pool := digestSearchPool(t)
	ctx := context.Background()
	store := NewPostgresCatalogStore(pool)

	if _, err := pool.Exec(ctx, `
INSERT INTO aura.identities (id, name, kind)
VALUES ($1, 'retrieval-scope-test', 'service') ON CONFLICT (id) DO NOTHING`,
		digestSearchIdentity); err != nil {
		t.Fatalf("seed identity: %v", err)
	}
	if err := clearDigestSearchDocuments(ctx, pool); err != nil {
		t.Fatalf("clear: %v", err)
	}
	t.Cleanup(func() { _ = clearDigestSearchDocuments(context.Background(), pool) })

	searchID := "doc_retrieval_scope_" + uuid.NewString()
	doc, err := store.CreateDocument(ctx, CreateDocumentRequest{
		IdentityID: digestSearchIdentity, Scope: DocumentScopeLibrary,
		Title: "Clienti.xlsx", SourceKind: "test", SourceKey: searchID,
		SearchDocumentID: searchID, Status: DocumentStatusStored,
	})
	if err != nil {
		t.Fatalf("create document: %v", err)
	}
	publishForSearch(t, ctx, pool, digestSearchIdentity, searchID, "Clienti.xlsx")
	// The shape ingest writes: the machine card is what makes a file findable by its
	// contents before any agent has opened it.
	if err := store.SetCard(ctx, digestSearchIdentity, doc.ID,
		`Clienti.xlsx — spreadsheet, 1 sheet, 5889 rows x 7 columns. `+
			`Columns: Area Comm Fil Cli, Cliente, Ragione sociale, Localita, Venditore Esterno`,
	); err != nil {
		t.Fatalf("set card: %v", err)
	}

	retrieval := NewPostgresRetrievalStore(pool)
	cards := func(t *testing.T, query string, scope []string) []RetrievalCard {
		t.Helper()
		got, err := retrieval.RouteDocumentCards(ctx, digestSearchIdentity, query, scope, 8)
		if err != nil {
			t.Fatalf("RouteDocumentCards(%q): %v", query, err)
		}
		return got
	}

	t.Run("nil scope means every ready document", func(t *testing.T) {
		// The exact value Retrieve passes for a search that names no document.
		got := cards(t, "Clienti", nil)
		if len(got) != 1 {
			t.Fatalf("unscoped card leg returned %d card(s), want the ready document", len(got))
		}
		if got[0].Title != "Clienti.xlsx" || got[0].CatalogID != doc.ID {
			t.Fatalf("card = %q / %s, want Clienti.xlsx / %s", got[0].Title, got[0].CatalogID, doc.ID)
		}
	})

	t.Run("an empty non-nil scope behaves identically", func(t *testing.T) {
		if got := cards(t, "Clienti", []string{}); len(got) != 1 {
			t.Fatalf("empty-slice scope returned %d card(s), want 1 — the two spellings must agree", len(got))
		}
	})

	t.Run("a named scope still narrows", func(t *testing.T) {
		if got := cards(t, "Clienti", []string{doc.ID}); len(got) != 1 {
			t.Fatalf("in-scope document returned %d card(s), want 1", len(got))
		}
		// A real uuid this identity does not own: the scope must still exclude, or the fix
		// would have been "ignore $3 entirely".
		if got := cards(t, "Clienti", []string{uuid.NewString()}); len(got) != 0 {
			t.Fatalf("out-of-scope id returned %d card(s), want 0", len(got))
		}
	})

	t.Run("a query that matches nothing returns nothing", func(t *testing.T) {
		if got := cards(t, "fornitori", nil); len(got) != 0 {
			t.Fatalf("unrelated query returned %d card(s), want 0 — the leg must rank, not list", len(got))
		}
	})
}
