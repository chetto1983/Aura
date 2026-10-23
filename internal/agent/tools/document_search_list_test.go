package tools

import (
	"context"
	"encoding/json"
	"slices"
	"testing"
	"time"

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
		{ID: "doc_a", SourceKey: "archivio/a.pdf", FileName: "a.pdf", SizeBytes: 1200, IndexedAt: "2026-10-23 10:40:00.123", RawSHA256: "aa"},
		{ID: "doc_b", SourceKey: "archivio/b.pdf", FileName: "b.pdf", SizeBytes: 900, IndexedAt: "2026-10-23 10:39:00.000", RawSHA256: "bb"},
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
		first.OriginalSHA256 != "aa" || !first.RequiresOpen || first.SizeBytes == nil || *first.SizeBytes != 1200 ||
		first.IndexedAt == nil || !first.IndexedAt.Equal(time.Date(2026, 10, 23, 10, 40, 0, 123e6, time.UTC)) {
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

// A call that names nothing to list, and an offset into a ranked search, are refused; known ids
// with a filter and no query are a listing.
func TestDocumentSearchRefusesNeitherQueryNorFilter(t *testing.T) {
	t.Parallel()
	tool := &DocumentSearch{Library: &fakeLibrary{}, Fields: listingFilters()}
	for _, raw := range []string{`{}`, `{"query":"  "}`, `{"query":"fatture","offset":1}`, `{"tags":["urgente"],"offset":-1}`} {
		if _, err := tool.Execute(toolTestContext(t), json.RawMessage(raw)); err == nil {
			t.Errorf("args %s: accepted", raw)
		}
	}
	if _, err := tool.Execute(toolTestContext(t), json.RawMessage(`{"tags":["urgente"],"document_ids":["doc_a"]}`)); err != nil {
		t.Errorf("known ids with a filter: %v", err)
	}
}

// A listing pages: next_offset says where the next page starts, and the last page has none.
func TestDocumentSearchListPages(t *testing.T) {
	t.Parallel()
	fields := listingFilters()
	tool := &DocumentSearch{Library: &fakeLibrary{}, Fields: fields}
	first := runFiltered(t, tool, `{"tags":["urgente"],"limit":2}`)
	if first.NextOffset != 2 || fields.listAsked.Offset != 0 {
		t.Fatalf("first page: next_offset %d, asked offset %d", first.NextOffset, fields.listAsked.Offset)
	}
	fields.listed.Rows = fields.listed.Rows[:1]
	last := runFiltered(t, tool, `{"tags":["urgente"],"limit":2,"offset":2}`)
	if last.NextOffset != 0 || fields.listAsked.Offset != 2 || len(last.Documents) != 1 {
		t.Fatalf("last page: next_offset %d, asked offset %d, %d documents", last.NextOffset, fields.listAsked.Offset, len(last.Documents))
	}
}

// Known ids list without a query: a selection comes back with its fields.
func TestDocumentSearchListsKnownIDs(t *testing.T) {
	t.Parallel()
	fields := listingFilters()
	out := runFiltered(t, &DocumentSearch{Library: &fakeLibrary{}, Fields: fields}, `{"document_ids":["doc_a"]}`)
	if q := fields.listAsked; q == nil || !slices.Equal(q.DocumentIDs, []string{"doc_a"}) || out.Profile != documentListProfile {
		t.Fatalf("list query = %+v, profile %q", q, out.Profile)
	}
}

// A page past the end is empty, but the filters still admit documents: it is no abstention.
func TestDocumentSearchListPastTheEndIsNotAnAbstention(t *testing.T) {
	t.Parallel()
	fields := listingFilters()
	fields.listed.Rows = []docfields.DocumentRow{}
	out := runFiltered(t, &DocumentSearch{Library: &fakeLibrary{}, Fields: fields}, `{"tags":["urgente"],"offset":5}`)
	if out.Abstained || out.NextOffset != 0 || out.Matching != 3 || len(out.Documents) != 0 {
		t.Fatalf("past the end = %+v, matching %d, next %d", out.RetrievalResponse, out.Matching, out.NextOffset)
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
