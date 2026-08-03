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
	hits, err := client.SearchFactsHybrid(context.Background(), "cliente?", 1, time.Time{})
	if err != nil {
		t.Fatalf("SearchFactsHybrid: %v", err)
	}
	if len(hits) != 1 || hits[0].Statement != "first" {
		t.Fatalf("hits = %+v", hits)
	}
	if len(embedder.calls) != 1 || embedder.calls[0][0] != taskQueryPrefix+"cliente?" {
		t.Fatalf("embedding input = %v", embedder.calls)
	}
	if len(*requests) != 2 {
		t.Fatalf("requests = %d", len(*requests))
	}
	params := (*requests)[0].Payload["params"].(map[string]any)
	if params["query"] != `cliente\?` || params["candidates"] != float64(20) {
		t.Fatalf("fusion params = %v", params)
	}
}

func TestSearchFactsHybridFallsBackToLexical(t *testing.T) {
	tests := []struct {
		name      string
		embedder  Embedder
		fusion    testResponse
		hydration testResponse
	}{
		{name: "no embedder"},
		{name: "embed error", embedder: &stubEmbedder{err: errors.New("down")}},
		{name: "wrong dimensions", embedder: &stubEmbedder{vectors: [][][]float64{{{1}}}}},
		{name: "fusion error", embedder: &stubEmbedder{vectors: [][][]float64{{vectorOf(1)}}}, fusion: testResponse{Status: 500, Body: `{}`}},
		{name: "empty fusion", embedder: &stubEmbedder{vectors: [][][]float64{{vectorOf(1)}}}, fusion: testResponse{Body: `{"result":[]}`}},
		{name: "empty rid", embedder: &stubEmbedder{vectors: [][][]float64{{vectorOf(1)}}}, fusion: testResponse{Body: `{"result":[{"rid":null}]}`}},
		{name: "hydration error", embedder: &stubEmbedder{vectors: [][][]float64{{vectorOf(1)}}}, fusion: testResponse{Body: `{"result":[{"rid":"#3:1"}]}`}, hydration: testResponse{Status: 500, Body: `{}`}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			client, _ := routedClient(t, func(request recordedRequest) testResponse {
				statement, _ := request.Payload["command"].(string)
				switch {
				case strings.Contains(statement, "vector.fuse"):
					return tt.fusion
				case strings.Contains(statement, "@rid IN"):
					return tt.hydration
				default:
					return testResponse{Body: oneFactRow}
				}
			})
			client.WithEmbedder(tt.embedder)
			hits, err := client.SearchFactsHybrid(context.Background(), "where", 2, now)
			if err != nil || len(hits) != 1 || hits[0].Subject != "Davide" {
				t.Fatalf("hits=%+v err=%v", hits, err)
			}
		})
	}
	client, _ := recordingClient(t, `{"result":[]}`)
	if _, err := client.SearchFactsHybrid(context.Background(), " ", 1, now); err == nil {
		t.Fatal("blank hybrid query accepted")
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
	if len(*requests) != 5 {
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

func TestEscapeLuceneEscapesOperators(t *testing.T) {
	input := `a+b-(c) && d:e? "x" / y\\z`
	got := escapeLucene(input)
	for _, escaped := range []string{`\+`, `\-`, `\(`, `\)`, `\&`, `\:`, `\?`, `\"`, `\/`, `\\`} {
		if !strings.Contains(got, escaped) {
			t.Fatalf("escapeLucene(%q) = %q, missing %q", input, got, escaped)
		}
	}
}
