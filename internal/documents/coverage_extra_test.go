package documents

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/chetto1983/aura/internal/config"
)

// --- embedder.go error paths ---

func TestEmbeddingClientRejectsNon2xxStatus(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "boom", http.StatusServiceUnavailable)
	}))
	defer srv.Close()

	client := &EmbeddingClient{BaseURL: srv.URL, Client: srv.Client(), Dimensions: 2}
	_, err := client.Embed(t.Context(), []string{"a"})
	if err == nil || !strings.Contains(err.Error(), "HTTP 503") {
		t.Fatalf("want HTTP 503 error, got %v", err)
	}
}

func TestEmbeddingClientRejectsCountMismatch(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		// Asked for 2 inputs, sidecar returns 1 embedding.
		_ = json.NewEncoder(w).Encode(map[string]any{
			"data": []map[string]any{{"index": 0, "embedding": []float64{1, 2}}},
		})
	}))
	defer srv.Close()

	client := &EmbeddingClient{BaseURL: srv.URL, Client: srv.Client(), Dimensions: 2}
	_, err := client.Embed(t.Context(), []string{"a", "b"})
	if err == nil || !strings.Contains(err.Error(), "1 embeddings for 2 inputs") {
		t.Fatalf("want count-mismatch error, got %v", err)
	}
}

func TestEmbeddingClientRejectsMalformedJSON(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte("{not json"))
	}))
	defer srv.Close()

	client := &EmbeddingClient{BaseURL: srv.URL, Client: srv.Client(), Dimensions: 2}
	_, err := client.Embed(t.Context(), []string{"a"})
	if err == nil || !strings.Contains(err.Error(), "decode") {
		t.Fatalf("want decode error, got %v", err)
	}
}

func TestEmbeddingClientPropagatesTransportError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	url := srv.URL
	srv.Close() // server is down, so client.Do fails at the transport layer.

	client := &EmbeddingClient{BaseURL: url, Dimensions: 2}
	_, err := client.Embed(t.Context(), []string{"a"})
	if err == nil {
		t.Fatal("want transport error against a closed server")
	}
}

func TestEmbeddingClientUsesDefaultDimensionsWhenUnset(t *testing.T) {
	dim := config.DefaultEmbedDimensions
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"data": []map[string]any{{"index": 0, "embedding": make([]float64, dim)}},
		})
	}))
	defer srv.Close()

	// Dimensions unset (0) -> defaults to config.DefaultEmbedDimensions.
	client := &EmbeddingClient{BaseURL: srv.URL, Client: srv.Client()}
	got, err := client.Embed(t.Context(), []string{"a"})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || len(got[0]) != dim {
		t.Fatalf("embedding dims = %d, want default %d", len(got[0]), dim)
	}
}

// --- service.go GetJob error path ---

func TestServiceGetJobPropagatesStoreError(t *testing.T) {
	service := &Service{Jobs: newFakeJobStore()}
	_, err := service.GetJob(t.Context(), "missing-job")
	if err == nil || !strings.Contains(err.Error(), "not found") {
		t.Fatalf("want not-found error, got %v", err)
	}
}

// --- service.go IngestPath dependency guards ---

func TestServiceIngestPathRejectsMissingJobStore(t *testing.T) {
	path := writeNamedTempFile(t, "manual.pdf", "payload")
	if _, err := (&Service{}).IngestPath(t.Context(), IngestRequest{}, path); err == nil ||
		!strings.Contains(err.Error(), "job store") {
		t.Fatalf("missing job store error = %v", err)
	}
}

func TestServiceIngestPathRejectsMissingFile(t *testing.T) {
	service := &Service{Jobs: newFakeJobStore()}
	_, err := service.IngestPath(t.Context(), IngestRequest{}, t.TempDir()+"/missing.pdf")
	if err == nil {
		t.Fatal("want stat error for missing file")
	}
}
