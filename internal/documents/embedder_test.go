package documents

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestEmbeddingClientReturnsEmbeddings(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/embeddings" {
			t.Fatalf("path = %q", r.URL.Path)
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"data": []map[string]any{
				{"embedding": []float64{1, 2}},
				{"embedding": []float64{3, 4}},
			},
		})
	}))
	defer srv.Close()

	client := &EmbeddingClient{BaseURL: srv.URL, Client: srv.Client(), Dimensions: 2}
	got, err := client.Embed(t.Context(), []string{"a", "b"})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 || len(got[0]) != 2 {
		t.Fatalf("embeddings = %#v", got)
	}
}

func TestEmbeddingClientRejectsDimensionMismatch(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"data": []map[string]any{{"embedding": []float64{1}}},
		})
	}))
	defer srv.Close()

	client := &EmbeddingClient{BaseURL: srv.URL, Client: srv.Client(), Dimensions: 2}
	_, err := client.Embed(t.Context(), []string{"a"})
	if err == nil || !strings.Contains(err.Error(), "dimension") {
		t.Fatalf("want dimension error, got %v", err)
	}
}

func TestEmbeddingClientHandlesEmptyInputAndDefaultModel(t *testing.T) {
	client := &EmbeddingClient{BaseURL: "http://127.0.0.1:1"}
	got, err := client.Embed(t.Context(), nil)
	if err != nil {
		t.Fatal(err)
	}
	if got != nil {
		t.Fatalf("empty input embeddings = %#v", got)
	}
	if got := inputModel(""); got != "aura-local-embedding" {
		t.Fatalf("inputModel default = %q", got)
	}
	if got := inputModel("custom"); got != "custom" {
		t.Fatalf("inputModel custom = %q", got)
	}
}

func TestEmbeddingClientRequiresBaseURL(t *testing.T) {
	_, err := (&EmbeddingClient{}).Embed(t.Context(), []string{"x"})
	if err == nil || !strings.Contains(err.Error(), "base URL") {
		t.Fatalf("want base URL error, got %v", err)
	}
}
