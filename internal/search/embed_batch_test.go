package search

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestOpenAICompatBatchEmbeddingFunctionPreservesOrder(t *testing.T) {
	var request struct {
		Model string   `json:"model"`
		Input []string `json:"input"`
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/v1/embeddings" {
			t.Fatalf("unexpected request: %s %s", r.Method, r.URL.String())
		}
		if got := r.Header.Get("Authorization"); got != "Bearer secret" {
			t.Fatalf("authorization = %q", got)
		}
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			t.Fatalf("decode request: %v", err)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"data":[{"index":1,"embedding":[2,0]},{"index":0,"embedding":[1,0]}]}`))
	}))
	defer server.Close()

	embed := NewOpenAICompatBatchEmbeddingFunction(server.URL+"/v1", "secret", "mistral-embed", server.Client())
	vectors, err := embed(context.Background(), []string{"one", "two"})
	if err != nil {
		t.Fatalf("embed: %v", err)
	}
	if request.Model != "mistral-embed" || len(request.Input) != 2 || request.Input[0] != "one" || request.Input[1] != "two" {
		t.Fatalf("request = %+v", request)
	}
	if len(vectors) != 2 || vectors[0][0] != 1 || vectors[1][0] != 2 {
		t.Fatalf("vectors = %#v", vectors)
	}
}
