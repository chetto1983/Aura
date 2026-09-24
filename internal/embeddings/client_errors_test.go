package embeddings

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/chetto1983/aura/internal/config"
)

// embedServer answers the catalogue and hands every embeddings request to respond.
func embedServer(t *testing.T, respond http.HandlerFunc) *httptest.Server {
	t.Helper()
	server := httptest.NewServer(withCatalogue(respond))
	t.Cleanup(server.Close)
	return server
}

func vectorsBody(w http.ResponseWriter, vectors ...[]float64) {
	data := make([]map[string]any, len(vectors))
	for i, vector := range vectors {
		data[i] = map[string]any{"index": i, "embedding": vector}
	}
	_ = json.NewEncoder(w).Encode(map[string]any{"data": data})
}

func TestClientRejectsAResponseOfTheWrongWidth(t *testing.T) {
	server := embedServer(t, func(w http.ResponseWriter, _ *http.Request) { vectorsBody(w, []float64{1}) })
	client := &Client{BaseURL: server.URL, Client: server.Client(), Dimensions: 2}
	if _, err := client.Embed(t.Context(), []string{"a"}); err == nil || !strings.Contains(err.Error(), "dimension") {
		t.Fatalf("want dimension error, got %v", err)
	}
}

func TestClientRequiresABaseURL(t *testing.T) {
	if _, err := (&Client{}).Embed(t.Context(), []string{"x"}); err == nil || !strings.Contains(err.Error(), "base URL") {
		t.Fatalf("want base URL error, got %v", err)
	}
}

// The hosted route sends a Bearer header and the Matryoshka `dimensions` parameter; the local
// route sends neither, so its request stays the sidecar's shape.
func TestClientSendsTheKeyAndWidthOnlyOnTheHostedRoute(t *testing.T) {
	capture := func(t *testing.T, apiKey string) (string, map[string]any) {
		t.Helper()
		var auth string
		var body map[string]any
		server := embedServer(t, func(w http.ResponseWriter, r *http.Request) {
			auth = r.Header.Get("Authorization")
			_ = json.NewDecoder(r.Body).Decode(&body)
			vectorsBody(w, []float64{1, 2, 3, 4})
		})
		client := &Client{BaseURL: server.URL, Client: server.Client(), APIKey: apiKey, Model: "model", Dimensions: 4}
		if _, err := client.Embed(t.Context(), []string{"x"}); err != nil {
			t.Fatalf("Embed: %v", err)
		}
		return auth, body
	}
	auth, body := capture(t, "shared-key")
	if auth != "Bearer shared-key" || body["dimensions"] != float64(4) || body["model"] != "model" {
		t.Fatalf("hosted request: auth %q body %v", auth, body)
	}
	auth, body = capture(t, "")
	if _, sent := body["dimensions"]; auth != "" || sent {
		t.Fatalf("local request: auth %q body %v, want neither key nor width", auth, body)
	}
}

func TestClientRejectsAFailedEmbeddingRequest(t *testing.T) {
	server := embedServer(t, func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "boom", http.StatusServiceUnavailable)
	})
	client := &Client{BaseURL: server.URL, Client: server.Client(), Dimensions: 2}
	if _, err := client.Embed(t.Context(), []string{"a"}); err == nil || !strings.Contains(err.Error(), "HTTP 503") {
		t.Fatalf("want HTTP 503 error, got %v", err)
	}
}

func TestClientRejectsFewerVectorsThanInputs(t *testing.T) {
	server := embedServer(t, func(w http.ResponseWriter, _ *http.Request) { vectorsBody(w, []float64{1, 2}) })
	client := &Client{BaseURL: server.URL, Client: server.Client(), Dimensions: 2}
	if _, err := client.Embed(t.Context(), []string{"a", "b"}); err == nil || !strings.Contains(err.Error(), "1 embeddings for 2 inputs") {
		t.Fatalf("want count-mismatch error, got %v", err)
	}
}

func TestClientRejectsMalformedJSON(t *testing.T) {
	server := embedServer(t, func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write([]byte("{not json")) })
	client := &Client{BaseURL: server.URL, Client: server.Client(), Dimensions: 2}
	if _, err := client.Embed(t.Context(), []string{"a"}); err == nil || !strings.Contains(err.Error(), "decode") {
		t.Fatalf("want decode error, got %v", err)
	}
}

func TestClientPropagatesATransportError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	url := server.URL
	server.Close()
	if _, err := (&Client{BaseURL: url, Dimensions: 2}).Embed(t.Context(), []string{"a"}); err == nil {
		t.Fatal("want transport error against a closed server")
	}
}

func TestClientDefaultsTheWidthWhenUnset(t *testing.T) {
	dim := config.DefaultEmbedDimensions
	server := embedServer(t, func(w http.ResponseWriter, _ *http.Request) { vectorsBody(w, make([]float64, dim)) })
	got, err := (&Client{BaseURL: server.URL, Client: server.Client()}).Embed(t.Context(), []string{"a"})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || len(got[0]) != dim {
		t.Fatalf("embedding width = %d, want the default %d", len(got[0]), dim)
	}
}
