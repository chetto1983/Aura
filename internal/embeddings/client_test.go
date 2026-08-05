package embeddings

import (
	"context"
	"encoding/json"
	"math"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestClientUsesOpenAIWireAndResponseIndexes(t *testing.T) {
	var request embeddingRequest
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/embeddings" {
			t.Fatalf("path = %q", r.URL.Path)
		}
		if got := r.Header.Get("Authorization"); got != "Bearer secret" {
			t.Fatalf("authorization = %q", got)
		}
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			t.Fatalf("decode request: %v", err)
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"data": []map[string]any{
			{"index": 1, "embedding": []float64{3, 4}},
			{"index": 0, "embedding": []float64{1, 2}},
		}})
	}))
	t.Cleanup(server.Close)

	client := &Client{
		BaseURL: server.URL + "/v1", Model: "model", APIKey: "secret",
		Client: server.Client(), Dimensions: 2,
	}
	vectors, err := client.Embed(t.Context(), []string{"first", "second"})
	if err != nil {
		t.Fatalf("Embed: %v", err)
	}
	if request.Model != "model" || request.Dimensions != 2 || len(request.Input) != 2 {
		t.Fatalf("request = %+v", request)
	}
	if vectors[0][0] != 1 || vectors[1][0] != 3 {
		t.Fatalf("vectors = %v", vectors)
	}
}

func TestClientBatchesWithoutLosingGlobalOrder(t *testing.T) {
	var mu sync.Mutex
	var batches [][]string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var request embeddingRequest
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			t.Fatalf("decode request: %v", err)
		}
		mu.Lock()
		batches = append(batches, append([]string(nil), request.Input...))
		mu.Unlock()
		data := make([]map[string]any, len(request.Input))
		for index := range request.Input {
			data[len(data)-1-index] = map[string]any{
				"index": index, "embedding": []float64{float64(index + 1), 0},
			}
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"data": data})
	}))
	t.Cleanup(server.Close)

	client := &Client{BaseURL: server.URL, Client: server.Client(), Dimensions: 2, BatchSize: 2}
	vectors, err := client.Embed(t.Context(), []string{"a", "b", "c", "d", "e"})
	if err != nil {
		t.Fatalf("Embed: %v", err)
	}
	if len(batches) != 3 || len(batches[0]) != 2 || len(batches[2]) != 1 {
		t.Fatalf("batches = %v", batches)
	}
	if len(vectors) != 5 || vectors[0][0] != 1 || vectors[1][0] != 2 || vectors[4][0] != 1 {
		t.Fatalf("vectors = %v", vectors)
	}
}

func TestClientRejectsInvalidResponseIndexes(t *testing.T) {
	tests := map[string]string{
		"duplicate":    `{"data":[{"index":0,"embedding":[1]},{"index":0,"embedding":[2]}]}`,
		"missing":      `{"data":[{"embedding":[1]}]}`,
		"out of range": `{"data":[{"index":2,"embedding":[1]}]}`,
		"wrong count":  `{"data":[]}`,
	}
	for name, response := range tests {
		t.Run(name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				_, _ = w.Write([]byte(response))
			}))
			t.Cleanup(server.Close)
			inputs := []string{"one"}
			if name == "duplicate" {
				inputs = append(inputs, "two")
			}
			_, err := (&Client{BaseURL: server.URL, Client: server.Client(), Dimensions: 1}).Embed(t.Context(), inputs)
			if err == nil {
				t.Fatal("invalid response accepted")
			}
		})
	}
}

func TestClientBoundsRequestsAndDoesNotEchoText(t *testing.T) {
	transport := roundTripFunc(func(request *http.Request) (*http.Response, error) {
		<-request.Context().Done()
		return nil, request.Context().Err()
	})
	client := &Client{
		BaseURL: "http://embed.invalid", Client: &http.Client{Transport: transport},
		Dimensions: 1, Timeout: 10 * time.Millisecond,
	}
	started := time.Now()
	_, err := client.Embed(context.Background(), []string{"private document text"})
	if err == nil || !strings.Contains(err.Error(), "deadline exceeded") {
		t.Fatalf("deadline error = %v", err)
	}
	if elapsed := time.Since(started); elapsed > time.Second {
		t.Fatalf("request took %v", elapsed)
	}
	if strings.Contains(err.Error(), "private document text") {
		t.Fatalf("error exposed input text: %v", err)
	}
}

func TestClientEmptyInputDoesNotCallTransport(t *testing.T) {
	called := false
	client := &Client{
		BaseURL: "http://embed.invalid",
		Client: &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
			called = true
			return nil, nil
		})},
	}
	vectors, err := client.Embed(t.Context(), nil)
	if err != nil || vectors != nil || called {
		t.Fatalf("empty input = %v, %v, called=%v", vectors, err, called)
	}
}

func TestTruncateMRL(t *testing.T) {
	vector, err := TruncateMRL([]float64{3, 4, 99}, 2)
	if err != nil {
		t.Fatalf("TruncateMRL: %v", err)
	}
	if math.Abs(vector[0]-0.6) > 1e-9 || math.Abs(vector[1]-0.8) > 1e-9 {
		t.Fatalf("vector = %v", vector)
	}
	for name, vector := range map[string][]float64{
		"short": {1}, "zero": {0, 0, 1}, "not finite": {math.Inf(1), 1, 1},
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := TruncateMRL(vector, 2); err == nil {
				t.Fatal("invalid vector accepted")
			}
		})
	}
}

func TestRetrievalTaskPrefixes(t *testing.T) {
	input := []string{"value"}
	if got := RetrievalQueries(input); got[0] != QueryPrefix+"value" {
		t.Fatalf("query = %v", got)
	}
	if got := RetrievalDocuments("  Report  ", input); got[0] != "title: Report | text: value" {
		t.Fatalf("document = %v", got)
	}
	if got := RetrievalDocuments(" ", input); got[0] != UntitledDocumentPrefix+"value" {
		t.Fatalf("untitled document = %v", got)
	}
	if input[0] != "value" {
		t.Fatalf("input mutated: %v", input)
	}
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (fn roundTripFunc) RoundTrip(request *http.Request) (*http.Response, error) {
	return fn(request)
}
