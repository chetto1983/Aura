package assets

import (
	"context"
	"errors"
	"strings"
	"testing"
)

// The knowledge catalog is how the agent learns that a document it can search exists at all.
// It used to be gated on Status == StatusSearchable, and on the live deployment NO asset has
// ever held that status: measured 2026-08-13, presigned 6 / processing 2 / accepted 2 /
// searchable 0. So the catalog was always empty and the agent was never told about a single
// uploaded file.
//
// The status is not repairable, it is obsolete. Only replayedAssetResult writes searchable,
// and only for a re-upload onto an ACTIVE version -- while ActivatePipelineCandidate, the one
// statement that activates anything, has no callers since the in-process pipeline was deleted.
// The flag belongs to a machine that no longer runs.
//
// document_processor.go says what replaces it: "searchable is now a property of ArcadeDB (does
// this source_key have passages?) rather than of a Postgres column." That question already has
// an implementation -- ResolveDocumentScope -- so the catalog asks it.
type fakeScope struct {
	indexed []string
	err     error
	asked   []string
}

func (f *fakeScope) ResolveDocumentScope(_ context.Context, _ string, ids []string) ([]string, error) {
	f.asked = append(f.asked, ids...)
	if f.err != nil {
		return nil, f.err
	}
	return f.indexed, nil
}

func catalogAssets() []Asset {
	return []Asset{
		{ID: "a1", Status: StatusProcessing, DocumentID: "doc_indexed", FileName: "Perizia.pdf"},
		{ID: "a2", Status: StatusProcessing, DocumentID: "doc_missing", FileName: "Bozza.pdf"},
	}
}

func TestKnowledgeCatalogAdvertisesWhatTheIndexActuallyHas(t *testing.T) {
	scope := &fakeScope{indexed: []string{"doc_indexed"}}

	got := BuildKnowledgeCatalog(catalogAssets(), map[string]bool{}, indexedSet(t, scope))

	if !strings.Contains(got, "doc_indexed") || !strings.Contains(got, "Perizia.pdf") {
		t.Fatalf("an indexed document was not advertised:\n%s", got)
	}
	if strings.Contains(got, "doc_missing") || strings.Contains(got, "Bozza.pdf") {
		t.Fatalf("a document the index does not have was advertised:\n%s", got)
	}
}

// The status column stops deciding anything. A 'processing' asset whose passages are in
// ArcadeDB is searchable in every sense the agent cares about, and that is the whole point:
// the state machine that would have said so is gone.
func TestKnowledgeCatalogIgnoresTheObsoleteStatusColumn(t *testing.T) {
	scope := &fakeScope{indexed: []string{"doc_indexed", "doc_missing"}}
	assets := catalogAssets()
	assets[0].Status = StatusAccepted
	assets[1].Status = StatusPresigned

	got := BuildKnowledgeCatalog(assets, map[string]bool{}, indexedSet(t, scope))

	for _, want := range []string{"doc_indexed", "doc_missing"} {
		if !strings.Contains(got, want) {
			t.Fatalf("status gated %q out even though the index has it:\n%s", want, got)
		}
	}
}

// An unreachable index advertises nothing rather than everything. The catalog tells the agent
// these documents ARE retrievable; saying so about documents nobody can retrieve sends it
// looking for what is not there, which is worse than staying quiet.
func TestKnowledgeCatalogSaysNothingWhenTheIndexCannotAnswer(t *testing.T) {
	scope := &fakeScope{err: errors.New("arcadedb down")}

	if got := BuildKnowledgeCatalog(catalogAssets(), map[string]bool{}, indexedSet(t, scope)); got != "" {
		t.Fatalf("an unreachable index still advertised documents:\n%s", got)
	}
}

func TestKnowledgeCatalogAsksOnlyAboutAssetsThatCarryADocumentID(t *testing.T) {
	scope := &fakeScope{indexed: []string{"doc_indexed"}}
	assets := append(catalogAssets(), Asset{ID: "a3", Status: StatusProcessing, FileName: "nessun-doc.pdf"})

	BuildKnowledgeCatalog(assets, map[string]bool{}, indexedSet(t, scope))

	for _, asked := range scope.asked {
		if asked == "" {
			t.Fatalf("an empty document id was sent to the index: %v", scope.asked)
		}
	}
	if len(scope.asked) != 2 {
		t.Fatalf("asked the index about %v, want only the two ids that exist", scope.asked)
	}
}

func indexedSet(t *testing.T, scope DocumentScopeResolver) func([]string) map[string]bool {
	t.Helper()
	return func(ids []string) map[string]bool {
		return resolveIndexed(context.Background(), scope, "identity-1", ids)
	}
}
