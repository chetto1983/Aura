package documents

import (
	"errors"
	"strings"
	"testing"
)

func TestSearchRejectsEmptyQuery(t *testing.T) {
	searcher := &Searcher{Client: &fakeKnowledgeClient{}}
	_, err := searcher.Search(t.Context(), SearchRequest{Query: "?!:"})
	if err == nil || !strings.Contains(err.Error(), "empty") {
		t.Fatalf("want empty query error, got %v", err)
	}
}

func TestSearchCapsLimit(t *testing.T) {
	fake := &fakeKnowledgeClient{}
	searcher := &Searcher{Client: fake}
	if _, err := searcher.Search(t.Context(), SearchRequest{Query: "safety", Limit: 99}); err != nil {
		t.Fatal(err)
	}
	got := fake.readCalls[0].params["limit"]
	if got != maxSearchLimit {
		t.Fatalf("limit = %#v, want %d", got, maxSearchLimit)
	}
	if got := fake.readCalls[0].params["candidate_limit"]; got != maxSearchLimit*3 {
		t.Fatalf("candidate_limit = %#v, want %d", got, maxSearchLimit*3)
	}
}

func TestSearchFiltersByDocumentID(t *testing.T) {
	fake := &fakeKnowledgeClient{}
	searcher := &Searcher{Client: fake}
	if _, err := searcher.Search(t.Context(), SearchRequest{Query: "reset", DocumentID: "doc_123"}); err != nil {
		t.Fatal(err)
	}
	if got := fake.readCalls[0].params["document_id"]; got != "doc_123" {
		t.Fatalf("document_id param = %#v", got)
	}
	if got := fake.readCalls[0].params["candidate_limit"]; got != defaultSearchLimit {
		t.Fatalf("candidate_limit = %#v, want %d", got, defaultSearchLimit)
	}
}

func TestSearchDecodesLocatorJSON(t *testing.T) {
	fake := &fakeKnowledgeClient{
		readRows: []map[string]any{
			{
				"document_id":  "doc_1",
				"chunk_id":     "chunk_1",
				"file_name":    "manual.pdf",
				"text":         "Reset the line.",
				"score":        12.5,
				"locator_json": `{"page":57}`,
				"heading_path": []any{"Safety", "Reset"},
			},
		},
	}
	searcher := &Searcher{Client: fake}
	hits, err := searcher.Search(t.Context(), SearchRequest{Query: "reset"})
	if err != nil {
		t.Fatal(err)
	}
	if len(hits) != 1 {
		t.Fatalf("hits = %d", len(hits))
	}
	if hits[0].Locator.Page != 57 {
		t.Fatalf("page = %d", hits[0].Locator.Page)
	}
	if got := strings.Join(hits[0].HeadingPath, "/"); got != "Safety/Reset" {
		t.Fatalf("heading path = %q", got)
	}
}

func TestSearchPropagatesSearchError(t *testing.T) {
	searcher := &Searcher{Client: &fakeKnowledgeClient{failRead: errors.New("neo4j down")}}
	_, err := searcher.Search(t.Context(), SearchRequest{Query: "reset"})
	if err == nil || !strings.Contains(err.Error(), "neo4j down") {
		t.Fatalf("want neo4j down error, got %v", err)
	}
}
