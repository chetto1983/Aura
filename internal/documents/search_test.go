package documents

import (
	"encoding/json"
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

// TestSearchDocumentScopedUsesTermOverlapPreFilter proves the spike-075 sparse pre-filter:
// a scoped request ranks the document's OWN chunks by query-term overlap (MATCH on the
// indexed document_id) instead of the global db.index.fulltext.queryNodes(k)-then-filter,
// so a small/new doc is never crowded out. Supersedes the old post-filter assertion (the
// scoped path no longer uses the global fulltext index nor candidate_limit).
func TestSearchDocumentScopedUsesTermOverlapPreFilter(t *testing.T) {
	fake := &fakeKnowledgeClient{}
	searcher := &Searcher{Client: fake}
	if _, err := searcher.Search(t.Context(), SearchRequest{Query: "Reset The Line", DocumentID: "doc_123"}); err != nil {
		t.Fatal(err)
	}
	call := fake.readCalls[0]
	if !strings.Contains(call.query, "MATCH (node:Chunk {document_id: $document_id})") {
		t.Fatalf("scoped search must pre-filter the document's own chunks, got:\n%s", call.query)
	}
	if strings.Contains(call.query, "db.index.fulltext.queryNodes") {
		t.Fatalf("scoped search must NOT use the global fulltext top-k:\n%s", call.query)
	}
	if got := call.params["document_id"]; got != "doc_123" {
		t.Fatalf("document_id param = %#v, want doc_123", got)
	}
	terms, ok := call.params["terms"].([]string)
	if !ok || len(terms) != 3 || terms[0] != "reset" {
		t.Fatalf("terms param = %#v, want lowercased query terms [reset the line]", call.params["terms"])
	}
	if _, hasCand := call.params["candidate_limit"]; hasCand {
		t.Fatalf("scoped pre-filter must not carry candidate_limit (no global top-k):\n%#v", call.params)
	}
}

// TestSearchUnscopedUsesGlobalFulltext keeps the no-regression contract: with no
// document_id the global db.index.fulltext.queryNodes path (with candidate_limit) is used.
func TestSearchUnscopedUsesGlobalFulltext(t *testing.T) {
	fake := &fakeKnowledgeClient{}
	searcher := &Searcher{Client: fake}
	if _, err := searcher.Search(t.Context(), SearchRequest{Query: "reset"}); err != nil {
		t.Fatal(err)
	}
	call := fake.readCalls[0]
	if !strings.Contains(call.query, "db.index.fulltext.queryNodes") {
		t.Fatalf("unscoped search must use the global fulltext index:\n%s", call.query)
	}
	if got := call.params["candidate_limit"]; got != defaultSearchLimit*3 {
		t.Fatalf("candidate_limit = %#v, want %d", got, defaultSearchLimit*3)
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

func TestSearchValueHelpers(t *testing.T) {
	if got := stringValue(testStringer("boom")); got != "boom" {
		t.Fatalf("stringValue fmt.Stringer = %q", got)
	}
	for _, value := range []any{float32(1.5), int(2), int64(3), int32(4), json.Number("5.5")} {
		if got := floatValue(value); got == 0 {
			t.Fatalf("floatValue(%T) = %f", value, got)
		}
	}
	if got := stringSliceValue([]string{"a", "b"}); strings.Join(got, ",") != "a,b" {
		t.Fatalf("stringSliceValue([]string) = %#v", got)
	}
	if got := stringSliceValue(123); got != nil {
		t.Fatalf("stringSliceValue(default) = %#v", got)
	}
}

type testStringer string

func (s testStringer) String() string { return string(s) }
