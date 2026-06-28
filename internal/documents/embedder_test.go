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

// TestEmbeddingClientCloudRoute asserts the D-28 cloud swap: APIKey set sends a
// Bearer header AND the Matryoshka `dimensions` param; the local route (no key)
// sends neither — the request stays byte-identical to the granite-sidecar shape.
func TestEmbeddingClientCloudRoute(t *testing.T) {
	capture := func(t *testing.T, apiKey string, dims int) (auth string, body map[string]any) {
		t.Helper()
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			auth = r.Header.Get("Authorization")
			_ = json.NewDecoder(r.Body).Decode(&body)
			_ = json.NewEncoder(w).Encode(map[string]any{
				"data": []map[string]any{{"embedding": []float64{1, 2, 3, 4}}},
			})
		}))
		defer srv.Close()
		client := &EmbeddingClient{BaseURL: srv.URL, Client: srv.Client(), APIKey: apiKey, Model: "qwen/qwen3-embedding-8b", Dimensions: dims}
		if _, err := client.Embed(t.Context(), []string{"x"}); err != nil {
			t.Fatalf("Embed: %v", err)
		}
		return auth, body
	}

	t.Run("cloud sends bearer + dimensions", func(t *testing.T) {
		auth, body := capture(t, "shared-key", 4)
		if auth != "Bearer shared-key" {
			t.Errorf("Authorization = %q, want Bearer shared-key", auth)
		}
		if body["dimensions"] != float64(4) {
			t.Errorf("dimensions = %v, want 4", body["dimensions"])
		}
		if body["model"] != "qwen/qwen3-embedding-8b" {
			t.Errorf("model = %v", body["model"])
		}
	})

	t.Run("local sends neither", func(t *testing.T) {
		auth, body := capture(t, "", 4)
		if auth != "" {
			t.Errorf("local Authorization = %q, want none", auth)
		}
		if _, has := body["dimensions"]; has {
			t.Errorf("local request sent a dimensions param %v, want none", body["dimensions"])
		}
	})
}
