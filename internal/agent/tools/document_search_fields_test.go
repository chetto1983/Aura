package tools

import (
	"context"
	"encoding/json"
	"errors"
	"slices"
	"strings"
	"testing"

	"github.com/chetto1983/aura/internal/arcadedb"
	"github.com/chetto1983/aura/internal/docfields"
	"github.com/chetto1983/aura/internal/documents"
)

// fakeFieldFilters resolves names against fixed lists and answers admissions by dimension.
type fakeFieldFilters struct {
	lists      map[docfields.Kind]map[string]string
	admissions map[arcadedb.FieldDimension]arcadedb.FieldAdmission
	asked      arcadedb.FieldFilter
	library    docfields.LibraryLists
	libraryErr error
}

func (f *fakeFieldFilters) LibraryLists(context.Context, string) (docfields.LibraryLists, error) {
	return f.library, f.libraryErr
}

func (f *fakeFieldFilters) ResolveNames(_ context.Context, _ string, kind docfields.Kind, names []string) ([]string, map[string][]string, error) {
	var ids []string
	unknown := map[string][]string{}
	for _, name := range names {
		if id, ok := f.lists[kind][docfields.NameKey(name)]; ok {
			ids = append(ids, id)
		} else {
			unknown[strings.TrimSpace(name)] = []string{"ACME SpA"}
		}
	}
	return ids, unknown, nil
}

func (f *fakeFieldFilters) Admissions(_ context.Context, _ string, filter arcadedb.FieldFilter) ([]arcadedb.FieldAdmission, error) {
	f.asked = filter
	applied := map[arcadedb.FieldDimension]bool{
		arcadedb.FieldType: len(filter.Types) > 0, arcadedb.FieldCounterparty: len(filter.Counterparties) > 0,
		arcadedb.FieldTags: len(filter.Tags) > 0, arcadedb.FieldDate: filter.DateFrom != "" || filter.DateTo != "",
	}
	var out []arcadedb.FieldAdmission
	for _, dimension := range []arcadedb.FieldDimension{arcadedb.FieldType, arcadedb.FieldCounterparty, arcadedb.FieldTags, arcadedb.FieldDate} {
		if applied[dimension] {
			out = append(out, f.admissions[dimension])
		}
	}
	return out, nil
}

func officeFilters() *fakeFieldFilters {
	return &fakeFieldFilters{
		lists: map[docfields.Kind]map[string]string{
			docfields.KindType:         {"fattura": "l-fattura"},
			docfields.KindCounterparty: {"acme spa": "l-acme"},
			docfields.KindTag:          {"urgente": "l-urgente", "estero": "l-estero"},
		},
		admissions: map[arcadedb.FieldDimension]arcadedb.FieldAdmission{
			arcadedb.FieldType:         {Dimension: arcadedb.FieldType, Admitted: 30, Reviewed: 30},
			arcadedb.FieldCounterparty: {Dimension: arcadedb.FieldCounterparty, Admitted: 12, Reviewed: 9},
			arcadedb.FieldTags:         {Dimension: arcadedb.FieldTags, Admitted: 3, Reviewed: 3},
			arcadedb.FieldDate:         {Dimension: arcadedb.FieldDate, Admitted: 40, Reviewed: 40},
		},
		library: docfields.LibraryLists{
			Types: []docfields.ListedName{{Name: "Fattura", Documents: 30}},
			Tags:  []docfields.ListedName{{Name: "interno", Documents: 3}},
		},
	}
}

type filteredSearch struct {
	documents.RetrievalResponse
	Filters []struct {
		Name               string              `json:"name"`
		Values             []string            `json:"values"`
		AdmittedDocuments  int                 `json:"admitted_documents"`
		RestsOnSuggestions bool                `json:"rests_on_suggestions"`
		UnknownNames       map[string][]string `json:"unknown_names"`
	} `json:"filters"`
	Lists *docfields.LibraryLists `json:"library_lists"`
}

func runFiltered(t *testing.T, tool *DocumentSearch, raw string) filteredSearch {
	t.Helper()
	result, err := tool.Execute(toolTestContext(t), json.RawMessage(raw))
	if err != nil {
		t.Fatal(err)
	}
	var out filteredSearch
	if err := json.Unmarshal([]byte(result.Preview), &out); err != nil {
		t.Fatalf("result is not JSON (%q): %v", result.Preview, err)
	}
	return out
}

// The seven field arguments decode strictly beside the four that were there: an unknown field
// is refused as before, and so is a tags_mode outside any and all.
func TestDocumentSearchSevenArgumentsStrict(t *testing.T) {
	t.Parallel()
	fields := officeFilters()
	library := &fakeLibrary{}
	tool := &DocumentSearch{Library: library, Fields: fields}
	out := runFiltered(t, tool, `{"query":"fatture","type":["Fattura"],"counterparty":[" acme SPA "],"tags":["urgente","estero"],`+
		`"tags_mode":"all","date_from":"2026-01-01","date_to":"2026-03-31","include_unreviewed":false}`)
	want := arcadedb.FieldFilter{Types: []string{"l-fattura"}, Counterparties: []string{"l-acme"},
		Tags: []string{"l-urgente", "l-estero"}, TagsAll: true, DateFrom: "2026-01-01", DateTo: "2026-03-31"}
	got := library.request.Fields
	if got == nil || !slices.Equal(got.Types, want.Types) || !slices.Equal(got.Counterparties, want.Counterparties) ||
		!slices.Equal(got.Tags, want.Tags) || got.TagsAll != want.TagsAll || got.DateFrom != want.DateFrom ||
		got.DateTo != want.DateTo || got.IncludeUnreviewed {
		t.Fatalf("retrieval fields = %+v, want %+v", got, want)
	}
	if fields.asked.IncludeUnreviewed {
		t.Fatal("admissions counted suggestions the search excludes")
	}
	if len(out.Filters) != 4 {
		t.Fatalf("filters = %+v", out.Filters)
	}

	for _, raw := range []string{
		`{"query":"x","owner":"me"}`,
		`{"query":"x","tags":["urgente"],"tags_mode":"some"}`,
		`{"query":"x","type":"Fattura"}`,
		`{"query":"x","include_unreviewed":"no"}`,
	} {
		if _, err := tool.Execute(toolTestContext(t), json.RawMessage(raw)); err == nil {
			t.Errorf("args %s: accepted", raw)
		}
	}
	if _, err := (&DocumentSearch{Library: library}).Execute(toolTestContext(t),
		json.RawMessage(`{"query":"x","type":["Fattura"]}`)); err == nil {
		t.Error("a filter was accepted with no document fields store to read it")
	}
}

// Suggestions count unless the caller says otherwise, and the report says when a match rests on
// them, so the agent can tell the operator.
func TestDocumentSearchReportsWhatEachFilterAdmits(t *testing.T) {
	t.Parallel()
	library := &fakeLibrary{response: documents.RetrievalResponse{Query: "fatture", Documents: []documents.RetrievalDocument{}}}
	out := runFiltered(t, &DocumentSearch{Library: library, Fields: officeFilters()},
		`{"query":"fatture","counterparty":["ACME SpA"],"date_from":"2026-01-01"}`)
	if !library.request.Fields.IncludeUnreviewed {
		t.Fatal("include_unreviewed did not default to true")
	}
	if len(out.Filters) != 2 {
		t.Fatalf("filters = %+v", out.Filters)
	}
	counterparty, date := out.Filters[0], out.Filters[1]
	if counterparty.Name != "counterparty" || !slices.Equal(counterparty.Values, []string{"ACME SpA"}) ||
		counterparty.AdmittedDocuments != 12 || !counterparty.RestsOnSuggestions {
		t.Fatalf("counterparty report = %+v", counterparty)
	}
	if date.Name != "date" || !slices.Equal(date.Values, []string{">= 2026-01-01"}) || date.RestsOnSuggestions {
		t.Fatalf("date report = %+v", date)
	}
	if out.Abstained {
		t.Fatal("a search with admitting filters abstained")
	}
}

// A name on no list admits nothing: the library is not searched, the answer abstains on the
// filter, and the closest names come back so the agent can correct itself.
func TestDocumentSearchUnknownNameAbstainsWithTheClosestNames(t *testing.T) {
	t.Parallel()
	library := &fakeLibrary{}
	out := runFiltered(t, &DocumentSearch{Library: library, Fields: officeFilters()},
		`{"query":"fatture","counterparty":["Acme"],"tags":["urgente"]}`)
	if library.request.Query != "" {
		t.Fatal("the library was searched under a filter that admits nothing")
	}
	if !out.Abstained || out.AbstentionReason != documents.AbstainedFilterAdmitsNothing || out.Documents == nil {
		t.Fatalf("response = %+v", out.RetrievalResponse)
	}
	counterparty := out.Filters[0]
	if counterparty.AdmittedDocuments != 0 || !slices.Equal(counterparty.UnknownNames["Acme"], []string{"ACME SpA"}) {
		t.Fatalf("counterparty report = %+v", counterparty)
	}
}

// A plain query names the library's types and tags, so the agent can filter by a tag the
// operator's words do not spell ("i documenti interni" → interno).
func TestDocumentSearchNamesTheLibraryLists(t *testing.T) {
	t.Parallel()
	library := &fakeLibrary{response: documents.RetrievalResponse{Query: "documenti interni", Documents: []documents.RetrievalDocument{}}}
	out := runFiltered(t, &DocumentSearch{Library: library, Fields: officeFilters()}, `{"query":"documenti interni"}`)
	if out.Lists == nil || !slices.Equal(out.Lists.Tags, []docfields.ListedName{{Name: "interno", Documents: 3}}) ||
		!slices.Equal(out.Lists.Types, []docfields.ListedName{{Name: "Fattura", Documents: 30}}) {
		t.Fatalf("library_lists = %+v", out.Lists)
	}
}

// The abstention on a filter carries the lists too: it is where the agent corrects a name.
func TestDocumentSearchListsTheLibraryOnAnAbstention(t *testing.T) {
	t.Parallel()
	out := runFiltered(t, &DocumentSearch{Library: &fakeLibrary{}, Fields: officeFilters()},
		`{"query":"documenti interni","tags":["interni"]}`)
	if !out.Abstained || out.Lists == nil || len(out.Lists.Tags) != 1 {
		t.Fatalf("abstained %v, library_lists %+v", out.Abstained, out.Lists)
	}
}

// Lists that cannot be read never fail the search: the answer comes back without them.
func TestDocumentSearchAnswersWithoutListsItCannotRead(t *testing.T) {
	t.Parallel()
	fields := officeFilters()
	fields.libraryErr = errors.New("arcadedb: connection refused")
	library := &fakeLibrary{response: documents.RetrievalResponse{Query: "fatture", Documents: []documents.RetrievalDocument{}}}
	result, err := (&DocumentSearch{Library: library, Fields: fields}).Execute(toolTestContext(t), json.RawMessage(`{"query":"fatture"}`))
	if err != nil {
		t.Fatal(err)
	}
	if keys := resultKeys(t, result); slices.Contains(keys, "library_lists") || !slices.Contains(keys, "documents") {
		t.Fatalf("result keys = %v", keys)
	}
}

// With no document fields store there is nothing to list, and the result has no key for it
// (Review Focus 5).
func TestDocumentSearchWithoutFieldsCarriesNoLists(t *testing.T) {
	t.Parallel()
	result, err := (&DocumentSearch{Library: &fakeLibrary{}}).Execute(toolTestContext(t), json.RawMessage(`{"query":"fatture"}`))
	if err != nil {
		t.Fatal(err)
	}
	if keys := resultKeys(t, result); slices.Contains(keys, "library_lists") {
		t.Fatalf("result keys = %v", keys)
	}
}

func resultKeys(t *testing.T, result ToolResult) []string {
	t.Helper()
	var raw map[string]json.RawMessage
	if err := json.Unmarshal([]byte(result.Preview), &raw); err != nil {
		t.Fatalf("result is not JSON (%q): %v", result.Preview, err)
	}
	keys := make([]string, 0, len(raw))
	for key := range raw {
		keys = append(keys, key)
	}
	return keys
}

// A result past the preview cap is cut, and what the agent decides its next search from must sit
// before the cut: the lists and the filter reports come ahead of the passages.
func TestDocumentSearchListsSurviveACutPreview(t *testing.T) {
	t.Parallel()
	library := &fakeLibrary{response: documents.RetrievalResponse{Query: "documenti interni", Documents: []documents.RetrievalDocument{{
		DocumentID: "doc_1", Title: "Verbale.pdf",
		Passages: []documents.RetrievalPassage{{Text: strings.Repeat("verbale della riunione ", 400), CitationToken: "document:doc_1@1"}},
	}}}}
	result, err := (&DocumentSearch{Library: library, Fields: officeFilters()}).Execute(toolTestContext(t),
		json.RawMessage(`{"query":"documenti interni","date_from":"2026-01-01"}`))
	if err != nil {
		t.Fatal(err)
	}
	if !result.Truncated || !strings.Contains(result.Preview, `"library_lists"`) || !strings.Contains(result.Preview, `"filters"`) {
		t.Fatalf("truncated %v, preview head %.300q", result.Truncated, result.Preview)
	}
}
