//go:build arcadedb_integration

package docfields

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"testing"

	"github.com/chetto1983/aura/internal/arcadedb"
)

func TestWorkspaceContentSearchPagesAndSelection(t *testing.T) {
	s, ctx, id, c := indexedLibrary(t)
	for _, q := range []string{"CREATE PROPERTY Passage.text STRING", "CREATE INDEX ON Passage (text) FULL_TEXT"} {
		if _, err := c.Command(ctx, q, nil); err != nil {
			t.Fatal(err)
		}
	}
	for i := range 61 {
		key := fmt.Sprintf("uploads/batch/report-%03d.pdf", i)
		indexDocument(t, c, key, "2026-10-21 10:00:00.000", "Cedar contract")
		if _, err := c.Command(ctx, "INSERT INTO Passage SET source_key=:key, ordinal=1, text='Cedar second page'", map[string]any{"key": key}); err != nil {
			t.Fatal(err)
		}
	}
	q := ListQuery{Search: "Cedar", SearchMode: "content", Limit: 20, Sort: "file_name"}
	var got []string
	for offset := 0; offset < 61; offset += 20 {
		q.Offset = offset
		r, err := s.List(ctx, id, q)
		if err != nil {
			t.Fatal(err)
		}
		if r.Total != 61 {
			t.Fatalf("total=%d", r.Total)
		}
		got = append(got, keysOf(r.Rows)...)
	}
	if len(slices.Compact(slices.Clone(got))) != 61 || !slices.IsSorted(got) {
		t.Fatalf("pages: %v", got)
	}
	selected, err := s.Select(ctx, id, q, nil)
	if err != nil || len(selected) != 61 {
		t.Fatalf("selection=%d %v", len(selected), err)
	}
	name, err := s.List(ctx, id, ListQuery{Search: "Cedar"})
	if err != nil || name.Total != 0 {
		t.Fatalf("name=%+v %v", name, err)
	}
	for _, literal := range []string{`Cedar" OR *:*`, `"`, `%`, `' OR`} {
		result, err := s.List(ctx, id, ListQuery{Search: literal, SearchMode: "content"})
		if err != nil || result.Total != 0 {
			t.Fatalf("literal %q = %+v %v", literal, result, err)
		}
	}
	q.Offset = 0
	q.SourceKeys = []string{got[17]}
	r, err := s.List(ctx, id, q)
	if err != nil || r.Total != 1 || !slices.Equal(keysOf(r.Rows), q.SourceKeys) {
		t.Fatalf("exact key=%+v %v", r, err)
	}
	q.Unassigned = []string{"type"}
	r, err = s.List(ctx, id, q)
	if err != nil || r.Total != 1 {
		t.Fatalf("composed=%+v %v", r, err)
	}
}

func TestWorkspaceInboxCustomAndContent(t *testing.T) {
	s, ctx, id, _ := indexedLibrary(t)
	c, err := s.tenants.For(ctx, id)
	if err != nil {
		t.Fatal(err)
	}
	indexDocument(t, c, "a.pdf", "2026-10-21 10:00:00.000", "Cedar")
	indexDocument(t, c, "b.pdf", "2026-10-21 10:00:00.001", "Maple")
	field, err := s.SaveCustomField(ctx, id, CustomField{Name: "Status", DataType: CustomText})
	if err != nil {
		t.Fatal(err)
	}
	if err = s.ApplyToDocuments(ctx, id, []string{docID("a.pdf")}, Diff{Set: []Value{{Field: "custom:" + field.ID, ValueKey: "paid"}}}); err != nil {
		t.Fatal(err)
	}
	if err = s.ReviewDocuments(ctx, id, []string{docID("a.pdf")}); err != nil {
		t.Fatal(err)
	}
	for _, q := range []ListQuery{{Inbox: true}, {Custom: []CustomFilter{{Field: "custom:" + field.ID, Operator: "unassigned"}}}} {
		r, err := s.List(ctx, id, q)
		if err != nil || !slices.Equal(keysOf(r.Rows), []string{"b.pdf"}) {
			t.Fatalf("%+v=%+v %v", q, r, err)
		}
	}
	r, err := s.List(ctx, id, ListQuery{NeedsReview: true})
	if err != nil || r.Total != 0 {
		t.Fatalf("suggestions=%+v %v", r, err)
	}
	r, err = s.List(ctx, id, ListQuery{Custom: []CustomFilter{{Field: "custom:" + field.ID, Operator: "eq", Value: "paid"}}})
	if err != nil || r.Total != 1 || r.Rows[0].SourceKey != "a.pdf" {
		t.Fatalf("custom=%+v %v", r, err)
	}
	for i := 1; i < 56; i++ {
		if _, err := c.Command(ctx, "INSERT INTO Passage SET source_key='a.pdf', ordinal=:i, text=:text", map[string]any{"i": i, "text": fmt.Sprint("Page ", i)}); err != nil {
			t.Fatal(err)
		}
	}
	content, err := s.Content(ctx, id, docID("a.pdf"), 0, 50)
	if err != nil || len(content.Passages) != 50 || !content.HasMore || content.Passages[0].Text != "Cedar" {
		t.Fatalf("content=%+v %v", content, err)
	}
	content, err = s.Content(ctx, id, docID("a.pdf"), 50, 50)
	if err != nil || len(content.Passages) != 6 || content.HasMore || content.Passages[0].Ordinal != 50 {
		t.Fatalf("last=%+v %v", content, err)
	}
	if _, err = s.Content(ctx, "other", docID("a.pdf"), 0, 25); !errors.Is(err, ErrNotFound) {
		t.Fatalf("foreign content=%v", err)
	}
	if _, err = s.List(ctx, id, ListQuery{Search: "Cedar", SearchMode: "content"}); err == nil {
		t.Fatal("missing full-text index should expose failure")
	}
	record, err := s.Record(ctx, id, "b.pdf")
	if err != nil {
		t.Fatal(err)
	}
	if _, err = s.Suggest(ctx, id, record.RecordID, []Value{{Field: "custom:" + field.ID, ValueKey: "pending", SetBy: SetByExtractor}}); err != nil {
		t.Fatal(err)
	}
	for _, include := range []bool{false, true} {
		q := ListQuery{Fields: arcadedb.FieldFilter{IncludeUnreviewed: include}, Custom: []CustomFilter{{Field: "custom:" + field.ID, Operator: "eq", Value: "pending"}}}
		result, err := s.List(ctx, id, q)
		want := 0
		if include {
			want = 1
		}
		if err != nil || result.Total != want {
			t.Fatalf("suggested custom include=%t: %+v %v", include, result, err)
		}
		ids, err := s.Select(ctx, id, q, nil)
		if err != nil || len(ids) != want {
			t.Fatalf("custom selection=%v %v", ids, err)
		}
	}
}

func TestWorkspaceEmptyIndexAndSavedViewPersistence(t *testing.T) {
	s, ctx := liveStore(t)
	r, err := s.List(ctx, "empty", ListQuery{Search: "Cedar", SearchMode: "content"})
	if err != nil || r.Total != 0 {
		t.Fatalf("empty=%+v %v", r, err)
	}
	v, err := s.SaveView(ctx, "owner", SavedView{Name: " Cedar ", Version: 1, Query: "q=Cedar&search_mode=content&inbox=true", Display: "cards", Pinned: true, Dashboard: true})
	if err != nil {
		t.Fatal(err)
	}
	fresh := New(s.tenants)
	views, err := fresh.Views(ctx, "owner")
	if err != nil || len(views) != 1 || views[0] != v {
		t.Fatalf("views=%+v %v", views, err)
	}
	if _, err = s.SaveView(ctx, "owner", SavedView{Name: "cedar", Version: 1, Display: "table"}); !errors.Is(err, ErrInvalid) {
		t.Fatalf("duplicate=%v", err)
	}
	foreign, err := s.Views(ctx, "other")
	if err != nil || len(foreign) != 0 {
		t.Fatalf("foreign=%+v %v", foreign, err)
	}
	v.Name = "Renamed"
	v.Pinned = false
	if _, err = s.SaveView(ctx, "owner", v); err != nil {
		t.Fatal(err)
	}
	if _, err = s.SaveView(ctx, "other", v); !errors.Is(err, ErrNotFound) {
		t.Fatalf("foreign update=%v", err)
	}
	unknown := v
	unknown.ID = "unknown"
	if _, err = s.SaveView(ctx, "owner", unknown); !errors.Is(err, ErrNotFound) {
		t.Fatalf("unknown update=%v", err)
	}
	if err = s.DeleteView(ctx, "other", v.ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("foreign delete=%v", err)
	}
	if err = s.DeleteView(ctx, "owner", v.ID); err != nil {
		t.Fatal(err)
	}
	if err = s.DeleteView(context.Background(), "owner", v.ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("unknown delete=%v", err)
	}
}
