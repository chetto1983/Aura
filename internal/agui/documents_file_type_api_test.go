package agui

import (
	"fmt"
	"testing"

	"github.com/chetto1983/aura/internal/docfields"
)

func TestDocumentFileTypeHTTPBoundary(t *testing.T) {
	f := &fakeWorkspaceStore{}
	s := documentsServer(f)
	for _, kind := range []string{"all", "documents", "pdf", "word", "spreadsheet", "presentation", "text", "image", "video", "audio", "code"} {
		t.Run(kind, func(t *testing.T) {
			if code, body := documentsRequest(t, s, "GET", "/api/documents?file_type="+kind, ""); code != 200 {
				t.Fatalf("GET file type=%d %v", code, body)
			}
			if f.query.FileType != kind {
				t.Fatalf("GET filter=%q, want %s", f.query.FileType, kind)
			}
			body := fmt.Sprintf(`{"selection":{"filter":{"file_type":%q,"q":"Cedar","search_mode":"content","inbox":true,"source_key":["a"],"custom":[{"field":"custom:status","operator":"unassigned"}]},"expected_count":0},"diff":{}}`, kind)
			if code, response := documentsRequest(t, s, "POST", "/api/documents/bulk", body); code != 200 {
				t.Fatalf("bulk file type=%d %v", code, response)
			}
			if f.query.FileType != kind || f.query.Search != "Cedar" || f.query.SearchMode != "content" || !f.query.Inbox || len(f.query.SourceKeys) != 1 || len(f.query.Custom) != 1 {
				t.Fatalf("bulk filters=%+v", f.query)
			}
		})
	}
	if code, _ := documentsRequest(t, s, "GET", "/api/documents?file_type=other", ""); code != 400 {
		t.Fatalf("unknown file type=%d", code)
	}
	for _, path := range []string{"/api/documents", "/api/documents?file_type="} {
		if code, _ := documentsRequest(t, s, "GET", path, ""); code != 200 || f.query.FileType != "documents" {
			t.Fatalf("default documents query=%+v status=%d", f.query, code)
		}
	}
	for _, filter := range []string{`{}`, `{"file_type":""}`} {
		body := fmt.Sprintf(`{"selection":{"filter":%s,"expected_count":0},"diff":{}}`, filter)
		if code, _ := documentsRequest(t, s, "POST", "/api/documents/bulk", body); code != 200 || f.query.FileType != "documents" {
			t.Fatalf("default bulk query=%+v status=%d", f.query, code)
		}
	}
	body := `{"selection":{"filter":{"file_type":"other"},"expected_count":0},"diff":{}}`
	if code, _ := documentsRequest(t, documentsServer(docfields.New(nil)), "POST", "/api/documents/bulk", body); code != 400 {
		t.Fatalf("unknown bulk file type=%d", code)
	}
}
