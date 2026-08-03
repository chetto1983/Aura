package arcadedb

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"testing"
	"time"
)

type stubEmbedder struct {
	vectors [][][]float64
	err     error
	calls   [][]string
}

func (s *stubEmbedder) Embed(_ context.Context, texts []string) ([][]float64, error) {
	s.calls = append(s.calls, append([]string(nil), texts...))
	if s.err != nil {
		return nil, s.err
	}
	if len(s.vectors) == 0 {
		return nil, nil
	}
	result := s.vectors[0]
	s.vectors = s.vectors[1:]
	return result, nil
}

func vectorOf(value float64) []float64 {
	vector := make([]float64, vectorDimensions)
	vector[0] = value
	return vector
}

func TestSearchFactsHybridRestoresFusionOrder(t *testing.T) {
	embedder := &stubEmbedder{vectors: [][][]float64{{vectorOf(1)}}}
	client, requests := routedClient(t, func(request recordedRequest) testResponse {
		statement, _ := request.Payload["command"].(string)
		if strings.Contains(statement, "vector.fuse") {
			return testResponse{Body: `{"result":[{"rid":"#3:1"},{"rid":"#3:2"}]}`}
		}
		if strings.Contains(statement, "@rid IN") {
			return testResponse{Body: `{"result":[
				{"@rid":"#3:2","statement":"second","subject":"B","object":"C"},
				{"@rid":"#3:1","statement":"first","subject":"A","object":"B"}]}`}
		}
		return testResponse{Status: http.StatusBadRequest, Body: `{"detail":"unexpected query"}`}
	})
	client.WithEmbedder(embedder)
	result, err := client.SearchFactsHybrid(context.Background(), "cliente torino?", 1, time.Time{})
	if err != nil {
		t.Fatalf("SearchFactsHybrid: %v", err)
	}
	if len(result.Facts) != 1 || result.Facts[0].Statement != "first" ||
		result.RetrievalPath != retrievalPathHybrid || result.Abstained || result.Reason != "" {
		t.Fatalf("result = %+v", result)
	}
	if len(embedder.calls) != 1 || embedder.calls[0][0] != taskQueryPrefix+"cliente torino?" {
		t.Fatalf("embedding input = %v", embedder.calls)
	}
	if len(*requests) != 2 {
		t.Fatalf("requests = %d", len(*requests))
	}
	params := (*requests)[0].Payload["params"].(map[string]any)
	if params["query"] != `cliente torino\?` || params["candidates"] != float64(20) ||
		params["as_of"] == nil || params["max_distance"] != float64(0.55) ||
		params["min_lexical_score"] != float64(2) {
		t.Fatalf("fusion params = %v", params)
	}
	fusion := (*requests)[0].Payload["command"].(string)
	vectorFilter := `{ filter: (SELECT @rid FROM FACT WHERE ` + asOfCondition +
		`).@rid, maxDistance: :max_distance }`
	if !strings.Contains(fusion, vectorFilter) {
		t.Fatalf("dense leg does not filter valid RIDs inline before ranking: %s", fusion)
	}
	if !strings.Contains(fusion, "maxDistance: :max_distance") {
		t.Fatalf("dense relevance gate missing: %s", fusion)
	}
	if strings.Count(fusion, asOfCondition) != 2 {
		t.Fatalf("validity filter must occur in both retrieval legs: %s", fusion)
	}
	lexical := "SEARCH_INDEX('FACT[statement]', :query) = true AND $score >= :min_lexical_score AND " + asOfCondition +
		" LIMIT :candidates"
	if !strings.Contains(fusion, lexical) {
		t.Fatalf("lexical validity filter must precede candidate LIMIT: %s", fusion)
	}
}

func TestSearchFactsHybridFallsBackToLexical(t *testing.T) {
	tests := []struct {
		name      string
		embedder  Embedder
		fusion    testResponse
		hydration testResponse
		reason    string
	}{
		{name: "no embedder", reason: reasonEmbedderNotConfigured},
		{name: "embed error", embedder: &stubEmbedder{err: errors.New("down")}, reason: reasonEmbeddingFailed},
		{name: "wrong dimensions", embedder: &stubEmbedder{vectors: [][][]float64{{{1}}}}, reason: reasonEmbeddingInvalid},
		{name: "fusion error", embedder: &stubEmbedder{vectors: [][][]float64{{vectorOf(1)}}}, fusion: testResponse{Status: 500, Body: `{}`}, reason: reasonFusionFailed},
		{name: "empty rid", embedder: &stubEmbedder{vectors: [][][]float64{{vectorOf(1)}}}, fusion: testResponse{Body: `{"result":[{"rid":null}]}`}, reason: reasonFusionResultInvalid},
		{name: "hydration error", embedder: &stubEmbedder{vectors: [][][]float64{{vectorOf(1)}}}, fusion: testResponse{Body: `{"result":[{"rid":"#3:1"}]}`}, hydration: testResponse{Status: 500, Body: `{}`}, reason: reasonHydrationFailed},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var lexicalQueries []any
			client, _ := routedClient(t, func(request recordedRequest) testResponse {
				statement, _ := request.Payload["command"].(string)
				switch {
				case strings.Contains(statement, "vector.fuse"):
					return tt.fusion
				case strings.Contains(statement, "@rid IN"):
					return tt.hydration
				default:
					params, _ := request.Payload["params"].(map[string]any)
					lexicalQueries = append(lexicalQueries, params["query"])
					return testResponse{Body: oneFactRow}
				}
			})
			client.WithEmbedder(tt.embedder)
			result, err := client.SearchFactsHybrid(context.Background(), "where?", 2, now)
			if err != nil || len(result.Facts) != 1 || result.Facts[0].Subject != "Davide" {
				t.Fatalf("result=%+v err=%v", result, err)
			}
			if result.RetrievalPath != retrievalPathLexical || result.Abstained || result.Reason != tt.reason {
				t.Fatalf("fallback metadata = %+v", result)
			}
			if len(lexicalQueries) != 1 || lexicalQueries[0] != `where\?` {
				t.Fatalf("fallback lexical queries = %#v, want one escaped query", lexicalQueries)
			}
		})
	}
	client, _ := recordingClient(t, `{"result":[]}`)
	if _, err := client.SearchFactsHybrid(context.Background(), " ", 1, now); err == nil {
		t.Fatal("blank hybrid query accepted")
	}
}

func TestSearchFactsHybridAbstainsWhenNoLegQualifies(t *testing.T) {
	client, requests := routedClient(t, func(request recordedRequest) testResponse {
		return testResponse{Body: `{"result":[]}`}
	})
	client.WithEmbedder(&stubEmbedder{vectors: [][][]float64{{vectorOf(1)}}})
	result, err := client.SearchFactsHybrid(context.Background(), "unrelated", 5, now)
	if err != nil {
		t.Fatalf("SearchFactsHybrid: %v", err)
	}
	if len(result.Facts) != 0 || result.RetrievalPath != retrievalPathHybrid ||
		!result.Abstained || result.Reason != reasonNoQualifiedCandidates {
		t.Fatalf("result = %+v", result)
	}
	if len(*requests) != 1 {
		t.Fatalf("requests = %d, want no lexical retry after a valid abstention", len(*requests))
	}
}

func TestSearchFactsHybridReportsLexicalFallbackAbstention(t *testing.T) {
	client, requests := routedClient(t, func(request recordedRequest) testResponse {
		return testResponse{Body: `{"result":[]}`}
	})
	result, err := client.SearchFactsHybrid(context.Background(), "unrelated", 5, now)
	if err != nil {
		t.Fatalf("SearchFactsHybrid: %v", err)
	}
	if len(result.Facts) != 0 || result.RetrievalPath != retrievalPathLexical ||
		!result.Abstained || result.Reason != reasonEmbedderNotConfigured {
		t.Fatalf("result = %+v", result)
	}
	if len(*requests) != 1 {
		t.Fatalf("requests = %d, want one bounded lexical fallback", len(*requests))
	}
}

func TestSearchFactsHybridCapsCandidatesAndQuery(t *testing.T) {
	embedder := &stubEmbedder{vectors: [][][]float64{{vectorOf(1)}}}
	client, requests := routedClient(t, func(request recordedRequest) testResponse {
		statement, _ := request.Payload["command"].(string)
		if strings.Contains(statement, "vector.fuse") {
			return testResponse{Body: `{"result":[{"rid":"#3:1"}]}`}
		}
		return testResponse{Body: `{"result":[]}`}
	})
	client.WithEmbedder(embedder)
	if _, err := client.SearchFactsHybrid(context.Background(), "bounded", 1000, now); err != nil {
		t.Fatalf("SearchFactsHybrid: %v", err)
	}
	params := (*requests)[0].Payload["params"].(map[string]any)
	if params["candidates"] != float64(400) {
		t.Fatalf("candidates = %v, want 400", params["candidates"])
	}
	if _, err := client.SearchFactsHybrid(
		context.Background(), strings.Repeat("界", 2049), 1, now,
	); err == nil {
		t.Fatal("oversized hybrid query accepted")
	}
}

func TestEmbedStatementIsFailSoft(t *testing.T) {
	var none *Client
	if none.embedStatement(context.Background(), "fact") != nil {
		t.Fatal("nil client embedded")
	}
	client := &Client{}
	if client.embedStatement(context.Background(), "fact") != nil {
		t.Fatal("client without embedder embedded")
	}
	for _, embedder := range []*stubEmbedder{
		{err: errors.New("down")},
		{vectors: [][][]float64{{}}},
		{vectors: [][][]float64{{{1}}}},
	} {
		client.embedder = embedder
		if got := client.embedStatement(context.Background(), "fact"); got != nil {
			t.Fatalf("invalid embedding accepted: %v", got)
		}
	}
	embedder := &stubEmbedder{vectors: [][][]float64{{vectorOf(2)}}}
	client.embedder = embedder
	if got := client.embedStatement(context.Background(), "fact"); len(got) != vectorDimensions {
		t.Fatalf("embedding length = %d", len(got))
	}
	if embedder.calls[0][0] != taskDocumentPrefix+"fact" {
		t.Fatalf("input = %v", embedder.calls)
	}
}

// The vector a write computes must reach the database. It did not: the edge statement
// carried no `embedding` in its SET while the parameter was bound anyway, ArcadeDB
// accepted the unused param without a word, and EVERY fact in every identity's memory
// was stored without its vector — from the MCP tool, the CLI and onboarding alike.
func TestUpsertFactStoresTheVectorItComputed(t *testing.T) {
	client, rec := recordingClient(t, `{"result":[]}`)
	client.WithEmbedder(&stubEmbedder{vectors: [][][]float64{{vectorOf(5)}}})

	if _, err := client.UpsertFact(context.Background(), validFact(), now); err != nil {
		t.Fatalf("UpsertFact: %v", err)
	}
	edge, params := rec.statements[len(rec.statements)-1], rec.params[len(rec.params)-1]
	if !strings.Contains(edge, "embedding = :embedding") {
		t.Fatalf("the edge does not store the vector it computed:\n%s", edge)
	}
	vector, ok := params["embedding"].([]any)
	if !ok || len(vector) != vectorDimensions {
		t.Fatalf("embedding param = %T with %d values, want %d floats",
			params["embedding"], len(vector), vectorDimensions)
	}
}

// With no embedder — or a sidecar that is down — the fact is still written, without the
// clause and without the parameter. The write must never fail because retrieval's dense
// leg is unavailable; the backfill sweep embeds it within minutes.
func TestUpsertFactWithoutAVectorOmitsTheClause(t *testing.T) {
	client, rec := recordingClient(t, `{"result":[]}`)

	if _, err := client.UpsertFact(context.Background(), validFact(), now); err != nil {
		t.Fatalf("UpsertFact: %v", err)
	}
	edge, params := rec.statements[len(rec.statements)-1], rec.params[len(rec.params)-1]
	if strings.Contains(edge, "embedding") {
		t.Fatalf("no vector to store, yet the statement names one:\n%s", edge)
	}
	if _, bound := params["embedding"]; bound {
		t.Fatal("an embedding parameter was bound with no vector to put in it")
	}
}

func TestEmbedMissingAndReembedFactsWriteOnlyValidVectors(t *testing.T) {
	embedder := &stubEmbedder{vectors: [][][]float64{
		{vectorOf(1), {1}},
		{vectorOf(2), vectorOf(3)},
	}}
	client, requests := routedClient(t, func(request recordedRequest) testResponse {
		statement, _ := request.Payload["command"].(string)
		if strings.HasPrefix(statement, "SELECT @rid AS rid") {
			return testResponse{Body: `{"result":[
				{"rid":"#3:1","statement":"one"},
				{"rid":"#3:2","statement":"two"},
				{"rid":"","statement":"ignored"}]}`}
		}
		if strings.HasPrefix(statement, "UPDATE FACT SET embedding") {
			return testResponse{Body: `{"result":[{"count":1}]}`}
		}
		return testResponse{Status: 400, Body: `{}`}
	})
	client.WithEmbedder(embedder)
	written, err := client.EmbedMissingFacts(context.Background(), 0)
	if err != nil || written != 1 {
		t.Fatalf("EmbedMissingFacts written=%d err=%v", written, err)
	}
	written, err = client.ReEmbedAllFacts(context.Background(), 2)
	if err != nil || written != 2 {
		t.Fatalf("ReEmbedAllFacts written=%d err=%v", written, err)
	}
	// Four, not five: each call is one SELECT plus ONE batched sqlscript, where it
	// used to be one UPDATE per fact. The count is asserted only to catch a stray
	// extra round trip — that a batch is a single request is pinned properly by
	// TestWriteVectorsSendsOneBoundScriptForTheWholeBatch, and index 2 below is
	// still the second selection either way.
	if len(*requests) != 4 {
		t.Fatalf("requests = %d", len(*requests))
	}
	if !strings.Contains((*requests)[0].Payload["command"].(string), "embedding IS NULL") ||
		!strings.Contains((*requests)[2].Payload["command"].(string), "statement IS NOT NULL") {
		t.Fatalf("selection queries = %+v", *requests)
	}
}

func TestEmbedFactsHandlesNoWorkAndFailures(t *testing.T) {
	client := &Client{}
	if _, err := client.EmbedMissingFacts(context.Background(), 1); err == nil {
		t.Fatal("missing embedder accepted")
	}
	empty, _ := routedClient(t, func(recordedRequest) testResponse {
		return testResponse{Body: `{"result":[]}`}
	})
	empty.WithEmbedder(&stubEmbedder{})
	if got, err := empty.EmbedMissingFacts(context.Background(), 1); err != nil || got != 0 {
		t.Fatalf("empty result=%d err=%v", got, err)
	}
	failed, _ := routedClient(t, func(recordedRequest) testResponse {
		return testResponse{Status: 500, Body: `{"detail":"select failed"}`}
	})
	failed.WithEmbedder(&stubEmbedder{})
	if _, err := failed.EmbedMissingFacts(context.Background(), 1); err == nil || !strings.Contains(err.Error(), "select facts") {
		t.Fatalf("select error = %v", err)
	}
	embedFailed, _ := routedClient(t, func(recordedRequest) testResponse {
		return testResponse{Body: `{"result":[{"rid":"#3:1","statement":"one"}]}`}
	})
	embedFailed.WithEmbedder(&stubEmbedder{err: errors.New("down")})
	if _, err := embedFailed.EmbedMissingFacts(context.Background(), 1); err == nil || !strings.Contains(err.Error(), "embed backfill") {
		t.Fatalf("embed error = %v", err)
	}
	writeFailed, _ := routedClient(t, func(request recordedRequest) testResponse {
		statement, _ := request.Payload["command"].(string)
		if strings.HasPrefix(statement, "SELECT") {
			return testResponse{Body: `{"result":[{"rid":"#3:1","statement":"one"}]}`}
		}
		return testResponse{Status: 500, Body: `{"detail":"write failed"}`}
	})
	writeFailed.WithEmbedder(&stubEmbedder{vectors: [][][]float64{{vectorOf(1)}}})
	if _, err := writeFailed.EmbedMissingFacts(context.Background(), 1); err == nil || !strings.Contains(err.Error(), "write embedding") {
		t.Fatalf("write error = %v", err)
	}
}

func TestEmbedFactsCapsMaintenanceBatch(t *testing.T) {
	client, requests := routedClient(t, func(recordedRequest) testResponse {
		return testResponse{Body: `{"result":[]}`}
	})
	client.WithEmbedder(&stubEmbedder{})
	if _, err := client.ReEmbedAllFacts(context.Background(), 1000); err != nil {
		t.Fatalf("ReEmbedAllFacts: %v", err)
	}
	statement := (*requests)[0].Payload["command"].(string)
	if !strings.Contains(statement, "LIMIT 100") {
		t.Fatalf("maintenance cap missing: %s", statement)
	}
}

func TestEscapeLuceneEscapesOperators(t *testing.T) {
	input := `a+b-(c) && d:e? "x" / y\\z`
	got := escapeLucene(input)
	for _, escaped := range []string{`\+`, `\-`, `\(`, `\)`, `\&`, `\:`, `\?`, `\"`, `\/`, `\\`} {
		if !strings.Contains(got, escaped) {
			t.Fatalf("escapeLucene(%q) = %q, missing %q", input, got, escaped)
		}
	}
}
