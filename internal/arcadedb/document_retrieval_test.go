package arcadedb

import (
	"encoding/json"
	"math"
	"strings"
	"testing"
)

// resultBody moved here with document_projection_test.go's deletion; the retrieval tests
// are its only remaining callers.
func resultBody(rows any) string {
	encoded, _ := json.Marshal(map[string]any{"result": rows})
	return string(encoded)
}

// candidateFixture shapes the complete record CocoIndex writes today.
func candidateFixture(index *DocumentIndex, passageID, documentID string, ordinal int64, scoreKey string, score float64) map[string]any {
	return map[string]any{
		"passage_key": passageID, "search_document_id": "search-" + documentID,
		"source_kind": "s3", "source_key": "fatture/" + documentID + ".pdf",
		"raw_sha256": strings.Repeat("a", 64), "schema_version": index.schemaVersion(), "ordinal": ordinal,
		"text": "bounded passage text", "normalized_text_sha256": strings.Repeat("b", 64),
		scoreKey: score,
	}
}

func TestCandidateLocatorRoundTripsStrictly(t *testing.T) {
	var index *DocumentIndex
	index, _ = testDocumentIndex(t, func(recordedRequest) testResponse {
		row := candidateFixture(index, "p-1", "doc-a", 1, "fused_score", 2)
		row["heading_path"] = []any{"Quarterly", "Revenue"}
		row["char_start"], row["char_end"] = 10, 25
		return testResponse{Body: resultBody([]any{row})}
	})
	candidates, err := index.FusedCandidates(t.Context(), FusedCandidateQuery{
		CandidateFilter: CandidateFilter{IdentityID: documentTestIdentity, Limit: 1},
		Query:           "revenue", Embedding: []float64{1, 0, 0},
	})
	if err != nil {
		t.Fatalf("FusedCandidates: %v", err)
	}
	candidate := candidates[0]
	if candidate.CharacterSpan == nil || candidate.CharacterSpan.End != 25 ||
		len(candidate.HeadingPath) != 2 {
		t.Fatalf("candidate locator = %+v", candidate)
	}
}

func TestCandidateQueriesRejectInvalidRequestsBeforeIO(t *testing.T) {
	tests := []struct {
		name string
		run  func(*DocumentIndex) error
	}{
		{"empty query", func(index *DocumentIndex) error {
			_, err := index.FusedCandidates(t.Context(), fusedFixtureQuery(""))
			return err
		}},
		{"long query", func(index *DocumentIndex) error {
			_, err := index.FusedCandidates(t.Context(), fusedFixtureQuery(strings.Repeat("x", 41)))
			return err
		}},
		{"wrong dimension", func(index *DocumentIndex) error {
			request := fusedFixtureQuery("x")
			request.Embedding = []float64{1}
			_, err := index.FusedCandidates(t.Context(), request)
			return err
		}},
		{"non-finite embedding", func(index *DocumentIndex) error {
			request := fusedFixtureQuery("x")
			request.Embedding = []float64{1, math.NaN(), 0}
			_, err := index.FusedCandidates(t.Context(), request)
			return err
		}},
		{"limit above cap", func(index *DocumentIndex) error {
			request := fusedFixtureQuery("x")
			request.Limit = 5
			_, err := index.FusedCandidates(t.Context(), request)
			return err
		}},
		{"too many documents", func(index *DocumentIndex) error {
			request := fusedFixtureQuery("x")
			request.DocumentIDs = []string{"a", "b", "c", "d"}
			_, err := index.FusedCandidates(t.Context(), request)
			return err
		}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			index, requests := testDocumentIndex(t, func(recordedRequest) testResponse {
				t.Fatal("invalid request reached ArcadeDB")
				return testResponse{}
			})
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
		"missing key":        func(row map[string]any) { delete(row, "passage_key") },
		"wrong schema":       func(row map[string]any) { row["schema_version"] = "old" },
		"fractional ordinal": func(row map[string]any) { row["ordinal"] = 1.5 },
		"negative score":     func(row map[string]any) { row["fused_score"] = -1 },
		"partial span":       func(row map[string]any) { row["char_start"] = 1 },
		"bad headings":       func(row map[string]any) { row["heading_path"] = []any{7} },
	}
	for name, mutate := range mutations {
		t.Run(name, func(t *testing.T) {
			var index *DocumentIndex
			index, _ = testDocumentIndex(t, func(recordedRequest) testResponse {
				row := candidateFixture(index, "p-1", "doc-a", 1, "fused_score", 1)
				mutate(row)
				return testResponse{Body: resultBody([]any{row})}
			})
			_, err := index.FusedCandidates(t.Context(), FusedCandidateQuery{
				CandidateFilter: CandidateFilter{IdentityID: documentTestIdentity, Limit: 1},
				Query:           "x", Embedding: []float64{1, 0, 0},
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
				row := candidateFixture(index, "p-1", "doc-a", 1, "fused_score", 1)
				return testResponse{Body: resultBody([]any{row, row})}
			})
			_, err := index.FusedCandidates(t.Context(), FusedCandidateQuery{
				CandidateFilter: CandidateFilter{IdentityID: documentTestIdentity, Limit: limit},
				Query:           "x", Embedding: []float64{1, 0, 0},
			})
			if err == nil {
				t.Fatal("invalid candidate response accepted")
			}
		})
	}
}

// The query is the measurement: it scored 0.850 recall@1 where the Go tier ladder it
// replaced scored 0.300, and it did so at these exact parameters. This test pins the
// shape so a later edit cannot drift it away from the measured one in silence.
func TestFusedCandidatesSendTheMeasuredQueryAndKeepEngineOrder(t *testing.T) {
	var index *DocumentIndex
	var statement string
	index, _ = testDocumentIndex(t, func(request recordedRequest) testResponse {
		statement, _ = request.Payload["command"].(string)
		return testResponse{Body: resultBody([]any{
			candidateFixture(index, "p-best", "doc-b", 2, "fused_score", 0.031),
			candidateFixture(index, "p-next", "doc-a", 1, "fused_score", 0.019),
		})}
	})
	candidates, err := index.FusedCandidates(t.Context(), FusedCandidateQuery{
		CandidateFilter: CandidateFilter{IdentityID: documentTestIdentity, Limit: 4},
		Query:           "codice cliente", Embedding: []float64{1, 0, 0},
	})
	if err != nil {
		t.Fatalf("FusedCandidates: %v", err)
	}
	// Engine order, NOT re-sorted by document id: the row the engine put first stays
	// first even though its document sorts after the other's. Re-imposing an order here
	// is what made the fused ranking arrive correct and leave grouped and ascending.
	if len(candidates) != 2 || candidates[0].PassageID != "p-best" {
		t.Fatalf("engine order not preserved: %#v", candidates)
	}
	if candidates[0].FusedScore == nil || *candidates[0].FusedScore != 0.031 {
		t.Fatalf("fused score not decoded: %#v", candidates[0])
	}
	for _, want := range []string{
		"`vector.fuse`(", "`vector.neighbors`('Passage[embedding]', :embedding, :fetch,", "1 = 1",
		"SEARCH_INDEX('Passage[text]', :query)", "fusion: 'RRF'",
		"groupBy: 'search_document_id'", "groupSize: 1",
	} {
		if !strings.Contains(statement, want) {
			t.Fatalf("statement lost %q: %s", want, statement)
		}
	}
	if strings.Contains(statement, "ORDER BY") {
		t.Fatalf("outer ORDER BY re-imposed on an already ordered fusion: %s", statement)
	}
}

func TestFusedCandidatesRejectAnUnknownStrategy(t *testing.T) {
	index, _ := testDocumentIndex(t, func(recordedRequest) testResponse {
		return testResponse{Body: resultBody([]any{})}
	})
	_, err := index.FusedCandidates(t.Context(), FusedCandidateQuery{
		CandidateFilter: CandidateFilter{IdentityID: documentTestIdentity, Limit: 4},
		Query:           "q", Embedding: []float64{1, 0, 0}, Strategy: "COSINE",
	})
	if err == nil {
		t.Fatal("a strategy the engine does not implement was accepted")
	}
}

// fusedFixtureQuery is a request that passes every check except the one under test, so
// each case above fails for its own named reason and not incidentally.
func fusedFixtureQuery(query string) FusedCandidateQuery {
	return FusedCandidateQuery{
		CandidateFilter: CandidateFilter{IdentityID: documentTestIdentity},
		Query:           query,
		Embedding:       []float64{1, 0, 0},
	}
}
