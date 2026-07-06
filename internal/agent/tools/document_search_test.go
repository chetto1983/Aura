package tools

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/chetto1983/aura/internal/documents"
	"github.com/chetto1983/aura/internal/identityctx"
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

// TestDocumentSearchToolThreadsOwnerIdentity locks ME-01: the tool maps an empty
// CLI/no-principal ctx to the seeded `local` UUID (…001) via ownerFromContext — so the
// operator's own local-owned documents stay reachable with AURA_MUSR_ISOLATION on — and
// threads an authenticated web principal verbatim.
func TestDocumentSearchToolThreadsOwnerIdentity(t *testing.T) {
	t.Run("empty principal resolves to local UUID", func(t *testing.T) {
		backend := &fakeDocumentSearchBackend{}
		tool := &DocumentSearch{Searcher: backend}
		if _, err := tool.Execute(toolTestContext(t), json.RawMessage(`{"query":"hello"}`)); err != nil {
			t.Fatal(err)
		}
		if backend.req.IdentityID != localOwnerID {
			t.Fatalf("empty-principal search IdentityID = %q, want local UUID %q", backend.req.IdentityID, localOwnerID)
		}
	})
	t.Run("web principal threaded verbatim", func(t *testing.T) {
		const principal = "00000000-0000-0000-0000-0000000000ab"
		backend := &fakeDocumentSearchBackend{}
		tool := &DocumentSearch{Searcher: backend}
		ctx := identityctx.WithIdentityID(toolTestContext(t), principal)
		if _, err := tool.Execute(ctx, json.RawMessage(`{"query":"hello"}`)); err != nil {
			t.Fatal(err)
		}
		if backend.req.IdentityID != principal {
			t.Fatalf("web-principal search IdentityID = %q, want %q", backend.req.IdentityID, principal)
		}
	})
}

// TestDocumentSearchToolNoRerankerMatchesSearchOrder wires a REAL documents.Service
// with no reranker configured and asserts the tool returns the sparse fulltext Search
// order unchanged (Retrieve degrades to Search — no regression vs today's behaviour).
func TestDocumentSearchToolNoRerankerMatchesSearchOrder(t *testing.T) {
	svc := &documents.Service{Searcher: &stubSearchBackend{hits: []documents.SearchHit{
		{DocumentID: "doc-1", ChunkID: "ft-0", Text: "first"},
		{DocumentID: "doc-1", ChunkID: "ft-1", Text: "second"},
	}}}
	tool := &DocumentSearch{Searcher: svc}
	result, err := tool.Execute(toolTestContext(t), json.RawMessage(`{"query":"hello"}`))
	if err != nil {
		t.Fatal(err)
	}
	first := strings.Index(result.Preview, `"chunk_id":"ft-0"`)
	second := strings.Index(result.Preview, `"chunk_id":"ft-1"`)
	if first < 0 || second < 0 || first > second {
		t.Fatalf("want fulltext Search order ft-0 then ft-1, preview = %s", result.Preview)
	}
}

type fakeDocumentSearchBackend struct {
	req  documents.SearchRequest
	hits []documents.SearchHit
	err  error
}

func (f *fakeDocumentSearchBackend) Retrieve(_ context.Context, req documents.SearchRequest) ([]documents.SearchHit, error) {
	f.req = req
	if f.err != nil {
		return nil, f.err
	}
	return f.hits, nil
}

// stubSearchBackend is a sparse SearchBackend used to drive a real documents.Service
// (no reranker) through the tool, proving the no-regression Retrieve==Search path.
type stubSearchBackend struct {
	hits []documents.SearchHit
}

func (s *stubSearchBackend) Search(_ context.Context, _ documents.SearchRequest) ([]documents.SearchHit, error) {
	return s.hits, nil
}
