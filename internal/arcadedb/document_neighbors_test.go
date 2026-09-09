package arcadedb

import (
	"strings"
	"testing"
)

// The neighbour of a hit is the passage the ranking did NOT return, so it can only be asked
// for by position. services/ingest keys every passage <search_document_id>:<ordinal> and
// indexes that UNIQUE, which is what makes the lookup a direct hit rather than a scan.
func TestPassagesAtAsksForTheKeysItWasGivenOrdinals(t *testing.T) {
	var params map[string]any
	var index *DocumentIndex
	index, requests := testDocumentIndex(t, func(request recordedRequest) testResponse {
		params, _ = request.Payload["params"].(map[string]any)
		row := candidateFixture(index, "search-doc-a:41", "doc-a", 41, "ordinal", 41)
		row["passage_key"] = "search-doc-a:41"
		return testResponse{Body: resultBody([]any{row})}
	})

	passages, err := index.PassagesAt(t.Context(), documentTestIdentity, []PassageRef{
		{SearchDocumentID: "search-doc-a", Ordinal: 41},
		{SearchDocumentID: "search-doc-a", Ordinal: 43},
		{SearchDocumentID: "search-doc-a", Ordinal: 41}, // a shared neighbour is asked once
	})
	if err != nil {
		t.Fatalf("PassagesAt: %v", err)
	}
	keys, _ := params["passage_keys"].([]any)
	if len(keys) != 2 {
		t.Fatalf("passage_keys = %v, want the two distinct keys", params["passage_keys"])
	}
	if keys[0] != "search-doc-a:41" || keys[1] != "search-doc-a:43" {
		t.Fatalf("passage_keys = %v, want <document>:<ordinal>", keys)
	}
	// 43 does not exist: the hit sat at the end of its document, so it has no neighbour on
	// that side. That is an answer, not a failure.
	if len(passages) != 1 || passages[0].Ordinal != 41 {
		t.Fatalf("passages = %+v, want only the one that exists", passages)
	}
	// Unranked by construction, so it must not pretend to a score.
	if passages[0].FusedScore != nil {
		t.Fatalf("an unranked passage carried a score: %v", *passages[0].FusedScore)
	}
	statement, _ := (*requests)[0].Payload["command"].(string)
	if !strings.Contains(statement, "passage_key IN :passage_keys") {
		t.Fatalf("statement = %s", statement)
	}
}

func TestPassagesAtRefusesAnUnrequestedRow(t *testing.T) {
	var index *DocumentIndex
	index, _ = testDocumentIndex(t, func(recordedRequest) testResponse {
		row := candidateFixture(index, "search-doc-a:99", "doc-a", 99, "ordinal", 99)
		row["passage_key"] = "search-doc-a:99"
		return testResponse{Body: resultBody([]any{row})}
	})
	_, err := index.PassagesAt(t.Context(), documentTestIdentity,
		[]PassageRef{{SearchDocumentID: "search-doc-a", Ordinal: 41}})
	if err == nil {
		t.Fatal("a passage nobody asked for was accepted")
	}
}

func TestPassagesAtValidatesBeforeIO(t *testing.T) {
	for name, refs := range map[string][]PassageRef{
		"negative ordinal": {{SearchDocumentID: "search-doc-a", Ordinal: -1}},
		"blank document":   {{SearchDocumentID: "  ", Ordinal: 1}},
	} {
		t.Run(name, func(t *testing.T) {
			index, requests := testDocumentIndex(t, func(recordedRequest) testResponse {
				t.Fatal("an invalid request reached ArcadeDB")
				return testResponse{}
			})
			if _, err := index.PassagesAt(t.Context(), documentTestIdentity, refs); err == nil {
				t.Fatal("invalid reference accepted")
			}
			if len(*requests) != 0 {
				t.Fatalf("%d request(s) issued", len(*requests))
			}
		})
	}
}

// No references is not an error and must not become a query for everything.
func TestPassagesAtWithNoReferencesIssuesNoQuery(t *testing.T) {
	index, requests := testDocumentIndex(t, func(recordedRequest) testResponse {
		t.Fatal("an empty reference list issued a query")
		return testResponse{}
	})
	passages, err := index.PassagesAt(t.Context(), documentTestIdentity, nil)
	if err != nil || len(passages) != 0 || len(*requests) != 0 {
		t.Fatalf("passages = %+v, err = %v, requests = %d", passages, err, len(*requests))
	}
}
