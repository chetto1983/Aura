package search

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// TestOpenAICompatEmbeddingFunctionConnectionRefused verifies that when the
// embedding sidecar is unreachable (connection refused), Embed returns a
// descriptive error and does not panic. This is the US-QA-FIX14 failure mode:
// llama-embed container stops responding; search_memory must degrade
// gracefully via the "wiki search failed: …" warning path, not crash.
func TestOpenAICompatEmbeddingFunctionConnectionRefused(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	closedURL := server.URL
	server.Close() // port is now closed; next connect = ECONNREFUSED

	embed := NewOpenAICompatEmbeddingFunction(closedURL+"/v1", "test-key", "embeddinggemma", 0, false, nil)
	if embed == nil {
		t.Fatal("expected non-nil EmbeddingFunc for valid config")
	}

	_, err := embed(context.Background(), "query when sidecar is down")
	if err == nil {
		t.Fatal("expected error when embedding sidecar is unreachable, got nil")
	}
	// Error must carry the 'embeddings request' prefix so callers (EmbedCache,
	// qdrantSearcher) can surface a meaningful log line rather than a bare
	// transport error.
	if !strings.Contains(err.Error(), "embeddings request") {
		t.Fatalf("error missing 'embeddings request' prefix: %v", err)
	}
}

// TestOpenAICompatEmbeddingFunctionHTTPError verifies that a non-2xx response
// (e.g. 503 Service Unavailable) is surfaced as an error with the status code
// visible in the message — not silently swallowed or panicked over.
func TestOpenAICompatEmbeddingFunctionHTTPError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusServiceUnavailable)
		_, _ = w.Write([]byte(`{"error":"service unavailable"}`))
	}))
	defer server.Close()

	embed := NewOpenAICompatEmbeddingFunction(server.URL+"/v1", "test-key", "embeddinggemma", 0, false, server.Client())
	if embed == nil {
		t.Fatal("expected non-nil EmbeddingFunc")
	}

	_, err := embed(context.Background(), "query on 503")
	if err == nil {
		t.Fatal("expected error for HTTP 503, got nil")
	}
	if !strings.Contains(err.Error(), "503") {
		t.Fatalf("error missing HTTP status code: %v", err)
	}
}

// TestNewOpenAICompatEmbeddingFunctionNilOnMissingConfig verifies that a nil
// EmbeddingFunc is returned when any required config field is empty. Callers
// that receive nil must refuse to register the embedding-dependent searcher
// rather than registering a func that always errors at call time.
func TestNewOpenAICompatEmbeddingFunctionNilOnMissingConfig(t *testing.T) {
	cases := []struct {
		name    string
		baseURL string
		apiKey  string
		model   string
	}{
		{"empty baseURL", "", "key", "model"},
		{"empty apiKey", "http://localhost", "", "model"},
		{"empty model", "http://localhost", "key", ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if f := NewOpenAICompatEmbeddingFunction(tc.baseURL, tc.apiKey, tc.model, 0, false, nil); f != nil {
				t.Fatalf("expected nil EmbeddingFunc for invalid config, got non-nil")
			}
		})
	}
}
