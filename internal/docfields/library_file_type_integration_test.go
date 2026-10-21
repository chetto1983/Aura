//go:build arcadedb_integration

package docfields

import (
	"fmt"
	"net/url"
	"slices"
	"testing"

	"github.com/chetto1983/aura/internal/arcadedb"
)

func TestLibraryFileTypeGroupsListSelectAndDetail(t *testing.T) {
	s, ctx, id, c := indexedLibrary(t)
	groups := []struct {
		kind string
		keys []string
	}{
		{"pdf", []string{"report.final.PDF"}},
		{"word", []string{"contract.docm", "letter.DOCX", "template.ott"}},
		{"spreadsheet", []string{"budget.XLSX", "data.csv", "template.xltm"}},
		{"presentation", []string{"deck.PPTX", "show.ppsm", "template.otp"}},
		{"text", []string{"note.TXT", "note.markdown"}},
		{"image", []string{"photo.JPEG", "report.pdf.PNG"}},
		{"video", []string{"film.MP4"}},
		{"audio", []string{"meeting.MP3"}},
		{"code", []string{"app.TSX", "config.yaml", "main.Go", "schema.json", "main.c++", "ui.sublime-build", "controller.iecst"}},
		{"other", []string{"README", "payload.bin", "report.pdf.exe", "report.pdf "}},
	}
	var all, documents []string
	wantTypes := map[string]string{}
	for _, group := range groups {
		for _, key := range group.keys {
			wantTypes[key] = group.kind
			indexDocument(t, c, key, "2026-10-21 10:00:00.000", "Cedar contract")
			all = append(all, key)
			if slices.Contains([]string{"pdf", "word", "spreadsheet", "presentation", "text"}, group.kind) {
				documents = append(documents, key)
			}
		}
	}
	checks := append(slices.Clone(groups[:len(groups)-1]), struct {
		kind string
		keys []string
	}{"documents", documents}, struct {
		kind string
		keys []string
	}{"all", all}, struct {
		kind string
		keys []string
	}{"", documents})
	for _, check := range checks {
		t.Run(check.kind, func(t *testing.T) {
			q, err := ParseListQuery(url.Values{"file_type": {check.kind}, "sort": {"file_name"}, "limit": {"200"}})
			if err != nil {
				t.Fatal(err)
			}
			result, err := s.List(ctx, id, q)
			want := slices.Sorted(slices.Values(check.keys))
			if err != nil || result.Total != len(want) || !slices.Equal(keysOf(result.Rows), want) {
				t.Fatalf("%s rows=%v total=%d want=%v err=%v", check.kind, keysOf(result.Rows), result.Total, want, err)
			}
			for _, row := range result.Rows {
				if row.FileType != wantTypes[row.SourceKey] {
					t.Fatalf("%s classified as %s, want %s", row.SourceKey, row.FileType, wantTypes[row.SourceKey])
				}
			}
			ids, err := s.Select(ctx, id, q, nil)
			wantIDs := make([]string, len(want))
			for i, key := range want {
				wantIDs[i] = docID(key)
			}
			if err != nil || !slices.Equal(ids, wantIDs) {
				t.Fatalf("%s selection=%v want=%v err=%v", check.kind, ids, wantIDs, err)
			}
		})
	}
	q, _ := ParseListQuery(url.Values{"file_type": {"documents"}, "limit": {"2"}, "offset": {"2"}})
	result, err := s.List(ctx, id, q)
	if err != nil || result.Total != len(documents) || len(result.Rows) != 2 {
		t.Fatalf("paged=%+v %v", result, err)
	}
	result, err = s.List(ctx, id, ListQuery{})
	if err != nil || result.Total != len(documents) || len(result.Rows) != len(documents) {
		t.Fatalf("zero-query default=%+v %v", result, err)
	}
	selected, err := s.Select(ctx, id, ListQuery{}, nil)
	if err != nil || len(selected) != len(documents) {
		t.Fatalf("zero-query selection=%v %v", selected, err)
	}
	for _, group := range groups {
		detail, err := s.Document(ctx, id, docID(group.keys[0]))
		if err != nil || detail.FileType != group.kind {
			t.Fatalf("detail %s type=%s %v", group.keys[0], detail.FileType, err)
		}
	}
	for _, kind := range []string{"documents", "code", "audio"} {
		v, err := s.SaveView(ctx, id, SavedView{Name: kind, Version: 1, Query: "file_type=" + kind, Display: "table"})
		if err != nil || v.Query != "file_type="+kind {
			t.Fatalf("save %s=%+v %v", kind, v, err)
		}
	}
	if _, err = s.SaveView(ctx, id, SavedView{Name: "Default", Version: 1, Display: "table"}); err != nil {
		t.Fatal(err)
	}
	views, err := New(s.tenants).Views(ctx, id)
	if err != nil || len(views) != 4 {
		t.Fatalf("persisted file type views=%+v %v", views, err)
	}
	for _, view := range views {
		if view.Name == "Default" {
			values, err := url.ParseQuery(view.Query)
			if err != nil {
				t.Fatal(err)
			}
			q, err := ParseListQuery(values)
			if err != nil || q.FileType != "documents" {
				t.Fatalf("default saved filter=%+v %v", q, err)
			}
			result, err := s.List(ctx, id, q)
			if err != nil || result.Total != len(documents) {
				t.Fatalf("default saved results=%+v %v", result, err)
			}
		}
	}
}

func TestLibraryFileTypeComposesWithContentAndMetadata(t *testing.T) {
	s, ctx, id, c := indexedLibrary(t)
	for _, query := range []string{"CREATE PROPERTY Passage.text STRING", "CREATE INDEX ON Passage (text) FULL_TEXT"} {
		if _, err := c.Command(ctx, query, nil); err != nil {
			t.Fatal(err)
		}
	}
	for _, key := range []string{"contract.PDF", "contract.PNG", "contract.MP4", "contract.MP3", "contract.GO", "other.PDF"} {
		indexDocument(t, c, key, "2026-10-21 10:00:00.000", "Cedar contract")
	}
	label, err := s.SaveLabel(ctx, id, Label{Kind: KindType, Name: "Contract"})
	if err != nil {
		t.Fatal(err)
	}
	field, err := s.SaveCustomField(ctx, id, CustomField{Name: "Status", DataType: CustomText})
	if err != nil {
		t.Fatal(err)
	}
	ids := []string{docID("contract.PDF"), docID("contract.PNG"), docID("contract.MP4"), docID("contract.MP3"), docID("contract.GO")}
	if err = s.ApplyToDocuments(ctx, id, ids, Diff{Set: []Value{{Field: FieldType, ValueKey: label.ID}, {Field: "custom:" + field.ID, ValueKey: "paid"}}}); err != nil {
		t.Fatal(err)
	}
	custom := fmt.Sprintf(`[{"field":%q,"operator":"eq","value":"paid"}]`, "custom:"+field.ID)
	q, err := ParseListQuery(url.Values{"file_type": {"documents"}, "q": {"Cedar"}, "search_mode": {"content"}, "type": {label.ID}, "inbox": {"true"}, "source_key": {"contract.PDF", "contract.PNG", "contract.MP4", "contract.MP3", "contract.GO"}, "custom": {custom}})
	if err != nil {
		t.Fatal(err)
	}
	result, err := s.List(ctx, id, q)
	if err != nil || result.Total != 1 || !slices.Equal(keysOf(result.Rows), []string{"contract.PDF"}) || result.Facets["type"][label.ID] != 1 {
		t.Fatalf("composed=%+v %v", result, err)
	}
	selected, err := s.Select(ctx, id, q, nil)
	if err != nil || !slices.Equal(selected, ids[:1]) {
		t.Fatalf("composed selection=%v %v", selected, err)
	}
	selected, err = s.Select(ctx, id, q, ids[:1])
	if err != nil || len(selected) != 0 {
		t.Fatalf("exclusion=%v %v", selected, err)
	}
	q.SearchMode, q.Search = "name", "contract"
	result, err = s.List(ctx, id, q)
	if err != nil || result.Total != 1 {
		t.Fatalf("name composition=%+v %v", result, err)
	}
	if err = s.ReviewDocuments(ctx, id, ids[:1]); err != nil {
		t.Fatal(err)
	}
	result, err = s.List(ctx, id, q)
	if err != nil || result.Total != 0 {
		t.Fatalf("reviewed inbox=%+v %v", result, err)
	}
	q.Fields = arcadedb.FieldFilter{Types: []string{label.ID}, IncludeUnreviewed: true}
	q.Inbox = false
	if _, err = s.List(ctx, "other", q); err != nil {
		t.Fatalf("foreign empty library=%v", err)
	}
}
