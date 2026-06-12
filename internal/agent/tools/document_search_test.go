package tools

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/chetto1983/aura/internal/documents"
)

func TestDocumentSearchToolValidatesQuery(t *testing.T) {
	tool := &DocumentSearch{Searcher: &fakeDocumentSearchBackend{}}
	_, err := tool.Execute(toolTestContext(t), json.RawMessage(`{"query":"   "}`))
	if err == nil || !strings.Contains(err.Error(), "query is required") {
		t.Fatalf("want query error, got %v", err)
	}
}

func TestDocumentSearchToolReturnsCitedHits(t *testing.T) {
	backend := &fakeDocumentSearchBackend{
		hits: []documents.SearchHit{{DocumentID: "doc-1", ChunkID: "chunk-1", FileName: "manual.pdf", Text: "hello", Locator: documents.Locator{Page: 7}}},
	}
	tool := &DocumentSearch{Searcher: backend}
	result, err := tool.Execute(toolTestContext(t), json.RawMessage(`{"query":"hello","document_id":"doc-1","limit":5}`))
	if err != nil {
		t.Fatal(err)
	}
	if backend.req.DocumentID != "doc-1" || backend.req.Limit != 5 {
		t.Fatalf("request = %#v", backend.req)
	}
	if !strings.Contains(result.Preview, `"chunk_id":"chunk-1"`) {
		t.Fatalf("preview = %s", result.Preview)
	}
	if result.Provenance == nil || result.Provenance.Source != "document_search" {
		t.Fatalf("provenance = %#v", result.Provenance)
	}
}

func TestDocumentSearchToolCapsLimit(t *testing.T) {
	backend := &fakeDocumentSearchBackend{}
	tool := &DocumentSearch{Searcher: backend}
	if _, err := tool.Execute(toolTestContext(t), json.RawMessage(`{"query":"hello","limit":99}`)); err != nil {
		t.Fatal(err)
	}
	if backend.req.Limit != 20 {
		t.Fatalf("limit = %d", backend.req.Limit)
	}
}

func TestDocumentSearchToolPropagatesSearchError(t *testing.T) {
	tool := &DocumentSearch{Searcher: &fakeDocumentSearchBackend{err: errors.New("neo4j down")}}
	_, err := tool.Execute(toolTestContext(t), json.RawMessage(`{"query":"hello"}`))
	if err == nil || !strings.Contains(err.Error(), "neo4j down") {
		t.Fatalf("want neo4j down error, got %v", err)
	}
}

func toolTestContext(t *testing.T) context.Context {
	t.Helper()
	return WithToolCallContext(t.Context(), "session", "toolcall", t.TempDir(), 4096)
}

type fakeDocumentSearchBackend struct {
	req  documents.SearchRequest
	hits []documents.SearchHit
	err  error
}

func (f *fakeDocumentSearchBackend) Search(_ context.Context, req documents.SearchRequest) ([]documents.SearchHit, error) {
	f.req = req
	if f.err != nil {
		return nil, f.err
	}
	return f.hits, nil
}
