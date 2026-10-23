//go:build arcadedb_integration

package docfields

import (
	"context"
	"errors"
	"slices"
	"testing"

	"github.com/chetto1983/aura/internal/arcadedb"
)

// officeLibrary indexes three documents, an hour apart: two invoices the operator typed, one of
// them also suggested urgent, and a note nobody labelled.
func officeLibrary(t *testing.T) (*Store, context.Context, string, Label) {
	t.Helper()
	s, ctx, id, c := indexedLibrary(t)
	fattura, _ := officeLabels(t, s, ctx, id, c)
	indexDocument(t, c, "chat/a.pdf", "2026-10-18 10:00:00.000", "Fattura A")
	indexDocument(t, c, "chat/b.xlsx", "2026-10-18 11:00:00.000", "Fattura B")
	indexDocument(t, c, "chat/c.txt", "2026-10-18 12:00:00.000", "Una nota")
	if err := s.ApplyToDocuments(ctx, id, []string{docID("chat/a.pdf"), docID("chat/b.xlsx")},
		Diff{Set: []Value{{Field: FieldType, ValueKey: fattura.ID}}}); err != nil {
		t.Fatal(err)
	}
	b, err := s.Record(ctx, id, "chat/b.xlsx")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.Suggest(ctx, id, b.RecordID, []Value{{Field: FieldTag, ValueKey: "lbl-urgente", SetBy: "rule:lbl-urgente"}}); err != nil {
		t.Fatal(err)
	}
	return s, ctx, id, fattura
}

func keysOf(rows []DocumentRow) []string {
	out := make([]string, 0, len(rows))
	for _, row := range rows {
		out = append(out, row.SourceKey)
	}
	return out
}

func TestLibraryListFiltersSortsAndCounts(t *testing.T) {
	s, ctx, id, fattura := officeLibrary(t)
	all, err := s.List(ctx, id, ListQuery{Sort: "-indexed_at", Limit: 2})
	if err != nil {
		t.Fatal(err)
	}
	if all.Total != 3 || !slices.Equal(keysOf(all.Rows), []string{"chat/c.txt", "chat/b.xlsx"}) {
		t.Fatalf("page = %v of %d", keysOf(all.Rows), all.Total)
	}
	if all.Facets["type"][fattura.ID] != 2 || all.Facets["tags"]["lbl-urgente"] != 1 {
		t.Fatalf("facets = %v", all.Facets)
	}
	if c := all.Rows[0]; c.RecordID != "" || c.Values == nil || len(c.Values) != 0 || c.NeedsReview {
		t.Fatalf("an unlabelled document = %+v", c)
	}
	if b := all.Rows[1]; b.RecordID == "" || len(b.Values) != 2 || !b.NeedsReview || b.ID != docID("chat/b.xlsx") {
		t.Fatalf("a labelled document = %+v", b)
	}

	typed, err := s.List(ctx, id, ListQuery{Fields: arcadedb.FieldFilter{Types: []string{fattura.ID}, IncludeUnreviewed: true}, Sort: "file_name"})
	if err != nil || typed.Total != 2 || !slices.Equal(keysOf(typed.Rows), []string{"chat/a.pdf", "chat/b.xlsx"}) {
		t.Fatalf("typed = %v of %d, %v", keysOf(typed.Rows), typed.Total, err)
	}
	review, err := s.List(ctx, id, ListQuery{NeedsReview: true})
	if err != nil || !slices.Equal(keysOf(review.Rows), []string{"chat/b.xlsx"}) {
		t.Fatalf("to review = %v, %v", keysOf(review.Rows), err)
	}
	if _, err := s.List(ctx, id, ListQuery{Sort: "size; DROP"}); !errors.Is(err, ErrInvalid) {
		t.Fatalf("an unknown sort: err = %v, want ErrInvalid", err)
	}
}

// The lists name what documents hold: Contratto is a type no document holds, and lbl-urgente is a
// suggestion with no label behind it, so neither is listed.
func TestLibraryListsNameTypesAndTagsWithCounts(t *testing.T) {
	s, ctx, id, _ := officeLibrary(t)
	interno, err := s.SaveLabel(ctx, id, Label{Kind: KindTag, Name: "Interno"})
	if err != nil {
		t.Fatal(err)
	}
	if err := s.ApplyToDocuments(ctx, id, []string{docID("chat/a.pdf")}, Diff{Set: []Value{{Field: FieldTag, ValueKey: interno.ID}}}); err != nil {
		t.Fatal(err)
	}
	lists, err := s.LibraryLists(ctx, id)
	if err != nil {
		t.Fatal(err)
	}
	want := LibraryLists{Types: []ListedName{{Name: "Fattura", Documents: 2}}, Tags: []ListedName{{Name: "Interno", Documents: 1}}}
	if !slices.Equal(lists.Types, want.Types) || !slices.Equal(lists.Tags, want.Tags) || lists.OmittedTypes+lists.OmittedTags != 0 {
		t.Fatalf("lists = %+v, want %+v", lists, want)
	}
}

func TestLibraryDocumentDetail(t *testing.T) {
	s, ctx, id, fattura := officeLibrary(t)
	detail, err := s.Document(ctx, id, docID("chat/b.xlsx"))
	if err != nil {
		t.Fatal(err)
	}
	if detail.SourceKey != "chat/b.xlsx" || len(detail.History) != 2 || detail.History[0].ValueKey != fattura.ID {
		t.Fatalf("detail = %+v", detail)
	}
	if _, err := s.Document(ctx, id, "doc:chat/nowhere.pdf"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("an unknown id: err = %v, want ErrNotFound", err)
	}
	other, _, otherID, _ := indexedLibrary(t)
	if _, err := other.Document(ctx, otherID, docID("chat/b.xlsx")); !errors.Is(err, ErrNotFound) {
		t.Fatalf("another identity's document: err = %v, want ErrNotFound", err)
	}
	if err := other.ApplyToDocuments(ctx, otherID, []string{docID("chat/b.xlsx")}, Diff{Set: []Value{{Field: FieldTag, ValueKey: "x"}}}); !errors.Is(err, ErrNotFound) {
		t.Fatalf("writing another identity's document: err = %v, want ErrNotFound", err)
	}
}

// Select all in a filter, minus what the operator unticked; review clears "da rivedere".
func TestLibrarySelectApplyAndReview(t *testing.T) {
	s, ctx, id, fattura := officeLibrary(t)
	ids, err := s.Select(ctx, id, ListQuery{Fields: arcadedb.FieldFilter{Types: []string{fattura.ID}, IncludeUnreviewed: true}},
		[]string{docID("chat/a.pdf")})
	if err != nil || !slices.Equal(ids, []string{docID("chat/b.xlsx")}) {
		t.Fatalf("selection = %v, %v", ids, err)
	}
	if err := s.ApplyToDocuments(ctx, id, []string{docID("chat/a.pdf"), docID("chat/c.txt")},
		Diff{Set: []Value{{Field: FieldTag, ValueKey: "lbl-archivio"}}}); err != nil {
		t.Fatal(err)
	}
	archived, err := s.List(ctx, id, ListQuery{Fields: arcadedb.FieldFilter{Tags: []string{"lbl-archivio"}}, Sort: "file_name"})
	if err != nil || !slices.Equal(keysOf(archived.Rows), []string{"chat/a.pdf", "chat/c.txt"}) {
		t.Fatalf("archived = %v, %v", keysOf(archived.Rows), err)
	}
	if err := s.ReviewDocuments(ctx, id, []string{docID("chat/b.xlsx")}); err != nil {
		t.Fatal(err)
	}
	if review, err := s.List(ctx, id, ListQuery{NeedsReview: true}); err != nil || review.Total != 0 {
		t.Fatalf("still to review: %v, %v", keysOf(review.Rows), err)
	}
}

func TestCustomFieldLifecycle(t *testing.T) {
	s, ctx := liveStore(t)
	id := "custom-fields-" + docID("x")
	stato, err := s.SaveCustomField(ctx, id, CustomField{Name: "Stato", DataType: CustomSelect, Options: []string{"pagata", "da pagare"}})
	if err != nil {
		t.Fatal(err)
	}
	for name, bad := range map[string]CustomField{
		"duplicate name":        {Name: " stato ", DataType: CustomText},
		"unknown type":          {Name: "Importo", DataType: "currency"},
		"select, no options":    {Name: "Esito", DataType: CustomSelect},
		"options on non-select": {Name: "Numero", DataType: CustomText, Options: []string{"a"}},
	} {
		if _, err := s.SaveCustomField(ctx, id, bad); !errors.Is(err, ErrInvalid) {
			t.Errorf("%s: err = %v, want ErrInvalid", name, err)
		}
	}
	stato.Options = []string{"pagata", "da pagare", "stornata"}
	if _, err := s.SaveCustomField(ctx, id, stato); err != nil {
		t.Fatal(err)
	}
	fields, err := s.CustomFields(ctx, id)
	if err != nil || len(fields) != 1 || len(fields[0].Options) != 3 {
		t.Fatalf("custom fields = %+v, %v", fields, err)
	}
	if err := s.DeleteCustomField(ctx, id, stato.ID); err != nil {
		t.Fatal(err)
	}
	if err := s.DeleteCustomField(ctx, id, stato.ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("second delete: err = %v, want ErrNotFound", err)
	}
}

// The filter bar's Exclude and "Non assegnata", the facets' unassigned counts, and the search box
// over file names.
func TestLibraryListExcludesUnassignedAndSearches(t *testing.T) {
	s, ctx, id, fattura := officeLibrary(t)
	excluded, err := s.List(ctx, id, ListQuery{Exclude: arcadedb.FieldFilter{Types: []string{fattura.ID}}, Fields: arcadedb.FieldFilter{IncludeUnreviewed: true}})
	if err != nil || !slices.Equal(keysOf(excluded.Rows), []string{"chat/c.txt"}) {
		t.Fatalf("excluding Fattura = %v, %v", keysOf(excluded.Rows), err)
	}
	untagged, err := s.List(ctx, id, ListQuery{Unassigned: []string{"tags"}, Fields: arcadedb.FieldFilter{IncludeUnreviewed: true}, Sort: "file_name"})
	if err != nil || !slices.Equal(keysOf(untagged.Rows), []string{"chat/a.pdf", "chat/c.txt"}) {
		t.Fatalf("untagged = %v, %v", keysOf(untagged.Rows), err)
	}
	notUrgent, err := s.List(ctx, id, ListQuery{Exclude: arcadedb.FieldFilter{Tags: []string{"lbl-urgente"}}, Fields: arcadedb.FieldFilter{IncludeUnreviewed: true}, Sort: "file_name"})
	if err != nil || !slices.Equal(keysOf(notUrgent.Rows), []string{"chat/a.pdf", "chat/c.txt"}) {
		t.Fatalf("not urgent = %v, %v", keysOf(notUrgent.Rows), err)
	}
	found, err := s.List(ctx, id, ListQuery{Search: "B.XL", Fields: arcadedb.FieldFilter{IncludeUnreviewed: true}})
	if err != nil || !slices.Equal(keysOf(found.Rows), []string{"chat/b.xlsx"}) {
		t.Fatalf("search = %v, %v", keysOf(found.Rows), err)
	}
	if literal, err := s.List(ctx, id, ListQuery{Search: "%"}); err != nil || literal.Total != 0 {
		t.Fatalf("a %% in the search matched as a wildcard: %v, %v", keysOf(literal.Rows), err)
	}
	all, err := s.List(ctx, id, ListQuery{Fields: arcadedb.FieldFilter{IncludeUnreviewed: true}})
	if err != nil || all.Facets["unassigned"]["type"] != 1 || all.Facets["unassigned"]["tags"] != 2 || all.Facets["unassigned"]["counterparty"] != 3 {
		t.Fatalf("unassigned facets = %v, %v", all.Facets["unassigned"], err)
	}
	if _, err := s.List(ctx, id, ListQuery{Unassigned: []string{"colour"}}); !errors.Is(err, ErrInvalid) {
		t.Fatalf("an unknown unassigned field: err = %v, want ErrInvalid", err)
	}
}
