package tools

import (
	"context"
	"encoding/json"
	"slices"
	"testing"

	"github.com/chetto1983/aura/internal/docfields"
	"github.com/chetto1983/aura/internal/documents"
)

func (f *fakeFieldFilters) List(_ context.Context, _ string, q docfields.ListQuery) (docfields.ListResult, error) {
	f.listAsked = &q
	return f.listed, nil
}

func listingFilters() *fakeFieldFilters {
	fields := officeFilters()
	fields.listed = docfields.ListResult{Total: 3, Rows: []docfields.DocumentRow{
		{ID: "doc_a", SourceKey: "archivio/a.pdf", FileName: "a.pdf", SizeBytes: 1200, IndexedAt: "2026-10-23T10:40:00Z", RawSHA256: "aa"},
		{ID: "doc_b", SourceKey: "archivio/b.pdf", FileName: "b.pdf", SizeBytes: 900, IndexedAt: "2026-10-23T10:39:00Z", RawSHA256: "bb"},
	}}
	return fields
}

// "Show me the documents tagged urgente": with no query the filters' own list answers, the one the
// cockpit shows, with no ranking and no floor in the way.
func TestDocumentSearchListsWhatTheFiltersAdmit(t *testing.T) {
	t.Parallel()
	library, fields := &fakeLibrary{}, listingFilters()
	out := runFiltered(t, &DocumentSearch{Library: library, Fields: fields}, `{"tags":["urgente"],"limit":2}`)
	if library.request.IdentityID != "" || library.request.Query != "" {
		t.Fatal("a listing went through the ranked search")
	}
	q := fields.listAsked
	if q == nil || q.FileType != "all" || q.Sort != "-indexed_at" || q.Limit != 2 ||
		!slices.Equal(q.Fields.Tags, []string{"l-urgente"}) || !q.Fields.IncludeUnreviewed {
		t.Fatalf("list query = %+v", q)
	}
	if out.Matching != 3 || out.Profile != documentListProfile || out.Abstained || len(out.Documents) != 2 {
		t.Fatalf("listing = %+v, matching %d", out.RetrievalResponse, out.Matching)
	}
	if first := out.Documents[0]; first.DocumentID != "doc_a" || first.Title != "a.pdf" || first.SourceKey != "archivio/a.pdf" ||
		first.OriginalSHA256 != "aa" || !first.RequiresOpen || first.SizeBytes == nil || *first.SizeBytes != 1200 || first.IndexedAt == nil {
		t.Fatalf("first listed = %+v", first)
	}
	if out.Lists == nil {
		t.Fatal("a listing carried no library_lists")
	}
	if !slices.Equal(fields.namedAsked, []string{"doc_a", "doc_b"}) {
		t.Fatalf("asked names for %v", fields.namedAsked)
	}
}

// The run's @-scope narrows a listing as it narrows a search.
func TestDocumentSearchListHonoursTheRunScope(t *testing.T) {
	t.Parallel()
	fields := listingFilters()
	ctx := documents.WithSourceScopes(toolTestContext(t), []documents.SourceScope{
		{Kind: documents.SourceScopeFolder, Path: "archivio"}, {Kind: documents.SourceScopeFile, Path: "altro/c.pdf"},
	})
	if _, err := (&DocumentSearch{Library: &fakeLibrary{}, Fields: fields}).Execute(ctx, json.RawMessage(`{"type":["Fattura"]}`)); err != nil {
		t.Fatal(err)
	}
	if q := fields.listAsked; q == nil || !slices.Equal(q.SourcePrefixes, []string{"archivio/"}) || !slices.Equal(q.SourceKeys, []string{"altro/c.pdf"}) {
		t.Fatalf("list query = %+v", q)
	}
}

func TestDocumentSearchRefusesNeitherQueryNorFilter(t *testing.T) {
	t.Parallel()
	tool := &DocumentSearch{Library: &fakeLibrary{}, Fields: listingFilters()}
	for _, raw := range []string{`{}`, `{"query":"  "}`, `{"tags":["urgente"],"document_ids":["doc_a"]}`} {
		if _, err := tool.Execute(toolTestContext(t), json.RawMessage(raw)); err == nil {
			t.Errorf("args %s: accepted", raw)
		}
	}
}

// Filters that each admit documents can still admit none together: the listing says so as an
// abstention on the filters, not as an empty library.
func TestDocumentSearchListOfNothingAbstains(t *testing.T) {
	t.Parallel()
	fields := officeFilters()
	fields.listed = docfields.ListResult{Rows: []docfields.DocumentRow{}}
	out := runFiltered(t, &DocumentSearch{Library: &fakeLibrary{}, Fields: fields}, `{"type":["Fattura"],"tags":["urgente"]}`)
	if !out.Abstained || out.AbstentionReason != documents.AbstainedFilterAdmitsNothing || out.Documents == nil {
		t.Fatalf("listing = %+v", out.RetrievalResponse)
	}
}
