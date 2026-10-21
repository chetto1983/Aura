package agui

import (
	"context"
	"net/http"
	"net/url"
	"strings"
	"testing"

	"github.com/chetto1983/aura/internal/docfields"
)

type fakeWorkspaceStore struct {
	fakeDocumentsStore
	view          docfields.SavedView
	offset, limit int
}

func (f *fakeWorkspaceStore) Views(_ context.Context, id string) ([]docfields.SavedView, error) {
	return []docfields.SavedView{}, f.note(id, "views")
}
func (f *fakeWorkspaceStore) SaveView(_ context.Context, id string, v docfields.SavedView) (docfields.SavedView, error) {
	f.view = v
	if v.ID == "" {
		v.ID = "new-view"
	}
	return v, f.note(id, "save view", v.ID)
}
func (f *fakeWorkspaceStore) DeleteView(_ context.Context, id, viewID string) error {
	return f.note(id, "delete view", viewID)
}
func (f *fakeWorkspaceStore) Content(_ context.Context, id, documentID string, offset, limit int) (docfields.ContentPage, error) {
	f.offset, f.limit = offset, limit
	return docfields.ContentPage{Passages: []docfields.ContentPassage{{Ordinal: offset, Text: "Cedar"}}, Offset: offset}, f.note(id, "content", documentID)
}

func TestDocumentWorkspaceRoutes(t *testing.T) {
	f := &fakeWorkspaceStore{}
	s := documentsServer(f)
	for _, c := range []struct{ method, path, body, key string }{
		{"GET", "/api/documents/views", "", "views"},
		{"POST", "/api/documents/views", `{"name":"Cedar","version":1,"query":"q=Cedar","display":"cards","pinned":true,"dashboard":true}`, "view"},
		{"PUT", "/api/documents/views/v1", `{"id":"spoof","name":"New","version":1,"query":"inbox=true","display":"table"}`, "view"},
		{"DELETE", "/api/documents/views/v1", "", "deleted"},
		{"GET", "/api/documents/d1/content?offset=25&limit=25", "", "passages"},
	} {
		code, body := documentsRequest(t, s, c.method, c.path, c.body)
		if code != 200 || body[c.key] == nil {
			t.Fatalf("%s %s = %d %v", c.method, c.path, code, body)
		}
		if f.identity != documentsOwner {
			t.Fatalf("identity=%s", f.identity)
		}
		if c.method == "PUT" && f.view.ID != "v1" {
			t.Fatalf("path id=%q", f.view.ID)
		}
	}
	if f.offset != 25 || f.limit != 25 {
		t.Fatalf("content paging=%d %d", f.offset, f.limit)
	}
	f.missing = []string{"foreign"}
	for _, c := range []struct{ method, path, body string }{{"GET", "/api/documents/foreign/content", ""}, {"PUT", "/api/documents/views/foreign", `{"name":"x","version":1,"display":"table"}`}, {"DELETE", "/api/documents/views/foreign", ""}} {
		if code, _ := documentsRequest(t, s, c.method, c.path, c.body); code != 404 {
			t.Fatalf("foreign %s=%d", c.path, code)
		}
	}
}

func TestDocumentWorkspaceQueryParsing(t *testing.T) {
	f := &fakeWorkspaceStore{}
	s := documentsServer(f)
	custom := url.QueryEscape(`[{"field":"custom:status","operator":"eq","value":"paid"}]`)
	code, _ := documentsRequest(t, s, "GET", "/api/documents?search_mode=content&q=Cedar&inbox=true&source_key=a&source_key=b&custom="+custom, "")
	if code != 200 || f.query.SearchMode != "content" || !f.query.Inbox || len(f.query.SourceKeys) != 2 || len(f.query.Custom) != 1 {
		t.Fatalf("query=%+v code=%d", f.query, code)
	}
	for _, query := range []string{"search_mode=invalid", "inbox=maybe", "custom=%7B%7D", "custom=" + url.QueryEscape(`[{"field":"custom:x","operator":"gt","value":"2"}]`), "offset=-1"} {
		if code, _ := documentsRequest(t, s, "GET", "/api/documents?"+query, ""); code != 400 {
			t.Errorf("%s=%d", query, code)
		}
	}
	for _, query := range []string{"limit=51", "offset=-1", "limit=oops"} {
		if code, _ := documentsRequest(t, s, "GET", "/api/documents/d1/content?"+query, ""); code != 400 {
			t.Errorf("content %s=%d", query, code)
		}
	}
	for _, path := range []string{"/api/documents/views", "/api/documents/d1/content"} {
		if code, _ := documentsRequest(t, documentsServer(nil), http.MethodGet, path, ""); code != 503 {
			t.Errorf("unavailable %s=%d", path, code)
		}
	}
}

func TestDocumentWorkspaceBulkPreservesFilters(t *testing.T) {
	f := &fakeWorkspaceStore{}
	s := documentsServer(f)
	body := `{"selection":{"filter":{"q":"Cedar","search_mode":"content","inbox":true,"source_key":["a"],"custom":[{"field":"custom:status","operator":"unassigned"}]},"expected_count":0},"diff":{}}`
	code, response := documentsRequest(t, s, "POST", "/api/documents/bulk", body)
	if code != 200 || !f.query.Inbox || f.query.SearchMode != "content" || len(f.query.SourceKeys) != 1 || len(f.query.Custom) != 1 {
		t.Fatalf("bulk query=%+v %d %v", f.query, code, response)
	}
	if code, _ := documentsRequest(t, s, "POST", "/api/documents/views", strings.Repeat("x", 10)); code != 400 {
		t.Fatal(code)
	}
}
