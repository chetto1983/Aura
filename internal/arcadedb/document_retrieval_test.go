package arcadedb

import (
	"encoding/json"
	"math"
	"strconv"
	"strings"
	"testing"
)

// resultBody moved here with document_projection_test.go's deletion; the retrieval tests
// are its only remaining callers.
func resultBody(rows any) string {
	encoded, _ := json.Marshal(map[string]any{"result": rows})
	return string(encoded)
}

// candidateFixture shapes a row the way services/ingest actually writes one: passage_key is
// "<search_document_id>:<ordinal>", and projection_key / version_id / version_number are
// absent because the sidecar never sets them. It used to hash the projection keys, which
// tested the decoder against a writer that no longer exists.
func candidateFixture(index *DocumentIndex, passageID, documentID string, ordinal int64, scoreKey string, score float64) map[string]any {
	generation := "generation-1"
	return map[string]any{
		"passage_key": "search-" + documentID + ":" + strconv.FormatInt(ordinal, 10), "passage_id": passageID,
		"document_id": documentID, "search_document_id": "search-" + documentID,
		"raw_sha256":          strings.Repeat("a", 64),
		"pipeline_generation": generation, "schema_version": index.schemaVersion(), "ordinal": ordinal,
		"text": "bounded passage text", "normalized_text_sha256": strings.Repeat("b", 64),
		"active": true, scoreKey: score,
	}
}

func TestLexicalCandidatesBindEscapeFilterAndSort(t *testing.T) {
	var index *DocumentIndex
	index, requests := testDocumentIndex(t, func(recordedRequest) testResponse {
		return testResponse{Body: resultBody([]any{
			candidateFixture(index, "p-2", "doc-b", 2, "lexical_score", 3),
			candidateFixture(index, "p-1", "doc-a", 1, "lexical_score", 9),
		})}
	}, true)
	candidates, err := index.LexicalCandidates(t.Context(), LexicalCandidateQuery{
		CandidateFilter: CandidateFilter{
			IdentityID: documentTestIdentity, Limit: 2,
			DocumentIDs: []string{"doc-b", "doc-a", "doc-a"},
		},
		Query: `c++ impossible?`,
	})
	if err != nil {
		t.Fatalf("LexicalCandidates: %v", err)
	}
	if len(candidates) != 2 || candidates[0].PassageID != "p-1" ||
		candidates[0].LexicalScore == nil || candidates[0].DenseDistance != nil {
		t.Fatalf("candidates = %+v", candidates)
	}
	request := (*requests)[0]
	statement, _ := request.Payload["command"].(string)
	if !strings.Contains(statement, "SEARCH_INDEX('Passage[text]'") ||
		!strings.Contains(statement, "document_id IN :document_ids") ||
		!strings.Contains(statement, "ORDER BY lexical_score DESC") {
		t.Fatalf("statement = %s", statement)
	}
	params, _ := request.Payload["params"].(map[string]any)
	if params["query"] != `c\+\+ impossible\?` {
		t.Fatalf("escaped query = %#v", params["query"])
	}
	documents, _ := params["document_ids"].([]any)
	if len(documents) != 2 || documents[0] != "doc-a" || documents[1] != "doc-b" {
		t.Fatalf("document filters = %#v", params["document_ids"])
	}
}

func TestDenseCandidatesUseActiveRIDFilterAndSort(t *testing.T) {
	var index *DocumentIndex
	index, requests := testDocumentIndex(t, func(recordedRequest) testResponse {
		return testResponse{Body: resultBody([]any{
			candidateFixture(index, "p-far", "doc-b", 2, "dense_distance", 0.4),
			candidateFixture(index, "p-near", "doc-a", 1, "dense_distance", 0.1),
		})}
	}, true)
	candidates, err := index.DenseCandidates(t.Context(), DenseCandidateQuery{
		CandidateFilter: CandidateFilter{IdentityID: documentTestIdentity, Limit: 2},
		Embedding:       []float64{1, 0, 0},
	})
	if err != nil {
		t.Fatalf("DenseCandidates: %v", err)
	}
	if len(candidates) != 2 || candidates[0].PassageID != "p-near" ||
		candidates[0].DenseDistance == nil || candidates[0].LexicalScore != nil {
		t.Fatalf("candidates = %+v", candidates)
	}
	request := (*requests)[0]
	statement, _ := request.Payload["command"].(string)
	if !strings.Contains(statement, "`vector.neighbors`('Passage[embedding]'") ||
		!strings.Contains(statement, "WHERE active = true") ||
		!strings.Contains(statement, "ORDER BY dense_distance ASC") {
		t.Fatalf("statement = %s", statement)
	}
	params, _ := request.Payload["params"].(map[string]any)
	if params["fetch"] != float64(4) {
		t.Fatalf("fetch = %#v", params["fetch"])
	}
	if _, found := params["document_ids"]; found {
		t.Fatalf("empty filter emitted document_ids: %#v", params)
	}
}

func TestCandidateLocatorRoundTripsStrictly(t *testing.T) {
	var index *DocumentIndex
	index, _ = testDocumentIndex(t, func(recordedRequest) testResponse {
		row := candidateFixture(index, "p-1", "doc-a", 1, "lexical_score", 2)
		row["self_ref"] = "#/tables/1"
		row["heading_path"] = []any{"Quarterly", "Revenue"}
		row["captions"] = []any{"Amounts in EUR"}
		row["page_number"] = 3
		row["bbox_left"], row["bbox_top"], row["bbox_right"], row["bbox_bottom"] = 1.0, 2.0, 3.0, 4.0
		row["char_start"], row["char_end"] = 10, 25
		row["sheet_name"], row["table_name"] = "Sheet1", "Revenue"
		row["row_number"], row["column_number"], row["cell_reference"] = 4, 7, "G4"
		return testResponse{Body: resultBody([]any{row})}
	}, true)
	candidates, err := index.LexicalCandidates(t.Context(), LexicalCandidateQuery{
		CandidateFilter: CandidateFilter{IdentityID: documentTestIdentity, Limit: 1}, Query: "revenue",
	})
	if err != nil {
		t.Fatalf("LexicalCandidates: %v", err)
	}
	candidate := candidates[0]
	if candidate.PageNumber == nil || *candidate.PageNumber != 3 ||
		candidate.BoundingBox == nil || candidate.BoundingBox.Right != 3 ||
		candidate.CharacterSpan == nil || candidate.CharacterSpan.End != 25 ||
		candidate.CellReference != "G4" || len(candidate.HeadingPath) != 2 {
		t.Fatalf("candidate locator = %+v", candidate)
	}
}

func TestCandidateQueriesRejectInvalidRequestsBeforeIO(t *testing.T) {
	tests := []struct {
		name string
		run  func(*DocumentIndex) error
	}{
		{"empty query", func(index *DocumentIndex) error {
			_, err := index.LexicalCandidates(t.Context(), LexicalCandidateQuery{CandidateFilter: CandidateFilter{IdentityID: documentTestIdentity}})
			return err
		}},
		{"long query", func(index *DocumentIndex) error {
			_, err := index.LexicalCandidates(t.Context(), LexicalCandidateQuery{CandidateFilter: CandidateFilter{IdentityID: documentTestIdentity}, Query: strings.Repeat("x", 41)})
			return err
		}},
		{"wrong dimension", func(index *DocumentIndex) error {
			_, err := index.DenseCandidates(t.Context(), DenseCandidateQuery{CandidateFilter: CandidateFilter{IdentityID: documentTestIdentity}, Embedding: []float64{1}})
			return err
		}},
		{"non-finite embedding", func(index *DocumentIndex) error {
			_, err := index.DenseCandidates(t.Context(), DenseCandidateQuery{CandidateFilter: CandidateFilter{IdentityID: documentTestIdentity}, Embedding: []float64{1, math.NaN(), 0}})
			return err
		}},
		{"limit above cap", func(index *DocumentIndex) error {
			_, err := index.LexicalCandidates(t.Context(), LexicalCandidateQuery{CandidateFilter: CandidateFilter{IdentityID: documentTestIdentity, Limit: 5}, Query: "x"})
			return err
		}},
		{"too many documents", func(index *DocumentIndex) error {
			_, err := index.LexicalCandidates(t.Context(), LexicalCandidateQuery{CandidateFilter: CandidateFilter{IdentityID: documentTestIdentity, DocumentIDs: []string{"a", "b", "c", "d"}}, Query: "x"})
			return err
		}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			index, requests := testDocumentIndex(t, func(recordedRequest) testResponse {
				t.Fatal("invalid request reached ArcadeDB")
				return testResponse{}
			}, true)
			if err := test.run(index); err == nil {
				t.Fatal("invalid request accepted")
			}
			if len(*requests) != 0 {
				t.Fatalf("requests = %d", len(*requests))
			}
		})
	}
}

func TestCandidateDecoderRejectsMalformedOrStaleRows(t *testing.T) {
	mutations := map[string]func(map[string]any){
		"missing key":        func(row map[string]any) { delete(row, "passage_id") },
		"wrong schema":       func(row map[string]any) { row["schema_version"] = "old" },
		"inactive":           func(row map[string]any) { row["active"] = false },
		"fractional ordinal": func(row map[string]any) { row["ordinal"] = 1.5 },
		"negative score":     func(row map[string]any) { row["lexical_score"] = -1 },
		"partial bbox":       func(row map[string]any) { row["bbox_left"] = 1.0 },
		"partial span":       func(row map[string]any) { row["char_start"] = 1 },
		"bad headings":       func(row map[string]any) { row["heading_path"] = []any{7} },
		// "bad projection key" was here. It is gone with the invariant it protected: the
		// decoder no longer requires projection_key, because the generation model it keyed
		// was deleted and the only writer that exists never sets it. Keeping the case would
		// have meant keeping a check that rejected every real row.
	}
	for name, mutate := range mutations {
		t.Run(name, func(t *testing.T) {
			var index *DocumentIndex
			index, _ = testDocumentIndex(t, func(recordedRequest) testResponse {
				row := candidateFixture(index, "p-1", "doc-a", 1, "lexical_score", 1)
				mutate(row)
				return testResponse{Body: resultBody([]any{row})}
			}, true)
			_, err := index.LexicalCandidates(t.Context(), LexicalCandidateQuery{
				CandidateFilter: CandidateFilter{IdentityID: documentTestIdentity, Limit: 1}, Query: "x",
			})
			if err == nil {
				t.Fatal("malformed candidate accepted")
			}
		})
	}
}

func TestCandidateDecoderRejectsDuplicateAndOverLimitRows(t *testing.T) {
	for name, limit := range map[string]int{"duplicate": 2, "over limit": 1} {
		t.Run(name, func(t *testing.T) {
			var index *DocumentIndex
			index, _ = testDocumentIndex(t, func(recordedRequest) testResponse {
				row := candidateFixture(index, "p-1", "doc-a", 1, "lexical_score", 1)
				return testResponse{Body: resultBody([]any{row, row})}
			}, true)
			_, err := index.LexicalCandidates(t.Context(), LexicalCandidateQuery{
				CandidateFilter: CandidateFilter{IdentityID: documentTestIdentity, Limit: limit}, Query: "x",
			})
			if err == nil {
				t.Fatal("invalid candidate response accepted")
			}
		})
	}
}
