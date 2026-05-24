package llm

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
)

func modelsResponse(models []map[string]any) []byte {
	b, _ := json.Marshal(map[string]any{"data": models})
	return b
}

func TestFetchModelMetadata_Success(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/models" || r.Method != http.MethodGet {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write(modelsResponse([]map[string]any{
			{"id": "other/model", "context_length": 32768, "max_completion_tokens": 4096},
			{"id": "deepseek/deepseek-v4-flash", "context_length": 163840, "max_completion_tokens": 8192},
		}))
	}))
	defer server.Close()

	// Ensure cache starts empty for this URL+model pair.
	cacheKey := server.URL + "\x00deepseek/deepseek-v4-flash"
	modelMetadataCache.Delete(cacheKey)
	defer modelMetadataCache.Delete(cacheKey)

	client := NewOpenAIClient(OpenAIConfig{APIKey: "test", BaseURL: server.URL})
	meta, err := client.FetchModelMetadata(context.Background(), "deepseek/deepseek-v4-flash")
	if err != nil {
		t.Fatalf("FetchModelMetadata: unexpected error: %v", err)
	}
	if meta.ID != "deepseek/deepseek-v4-flash" {
		t.Errorf("ID = %q, want deepseek/deepseek-v4-flash", meta.ID)
	}
	if meta.ContextLength != 163840 {
		t.Errorf("ContextLength = %d, want 163840", meta.ContextLength)
	}
	if meta.MaxCompletionTokens != 8192 {
		t.Errorf("MaxCompletionTokens = %d, want 8192", meta.MaxCompletionTokens)
	}
}

func TestFetchModelMetadata_CacheHit(t *testing.T) {
	var callCount atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/models" {
			http.NotFound(w, r)
			return
		}
		callCount.Add(1)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write(modelsResponse([]map[string]any{
			{"id": "cached-model", "context_length": 65536, "max_completion_tokens": 2048},
		}))
	}))
	defer server.Close()

	cacheKey := server.URL + "\x00cached-model"
	modelMetadataCache.Delete(cacheKey)
	defer modelMetadataCache.Delete(cacheKey)

	client := NewOpenAIClient(OpenAIConfig{APIKey: "test", BaseURL: server.URL})

	meta1, err := client.FetchModelMetadata(context.Background(), "cached-model")
	if err != nil {
		t.Fatalf("first call error: %v", err)
	}

	meta2, err := client.FetchModelMetadata(context.Background(), "cached-model")
	if err != nil {
		t.Fatalf("second call error: %v", err)
	}

	if meta1 != meta2 {
		t.Error("second call returned a different pointer — cache miss")
	}
	if n := callCount.Load(); n != 1 {
		t.Errorf("server received %d requests, want 1 (cache should absorb second call)", n)
	}
}

func TestFetchModelMetadata_404(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
		_, _ = w.Write([]byte(`{"error":"not found"}`))
	}))
	defer server.Close()

	cacheKey := server.URL + "\x00any-model"
	modelMetadataCache.Delete(cacheKey)
	defer modelMetadataCache.Delete(cacheKey)

	client := NewOpenAIClient(OpenAIConfig{APIKey: "test", BaseURL: server.URL})
	_, err := client.FetchModelMetadata(context.Background(), "any-model")
	if err == nil {
		t.Fatal("expected error on 404, got nil")
	}
}

func TestFetchModelMetadata_MalformedJSON(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{not valid json`))
	}))
	defer server.Close()

	cacheKey := server.URL + "\x00any-model"
	modelMetadataCache.Delete(cacheKey)
	defer modelMetadataCache.Delete(cacheKey)

	client := NewOpenAIClient(OpenAIConfig{APIKey: "test", BaseURL: server.URL})
	_, err := client.FetchModelMetadata(context.Background(), "any-model")
	if err == nil {
		t.Fatal("expected error on malformed JSON, got nil")
	}
}

func TestFetchModelMetadata_ModelNotInList(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write(modelsResponse([]map[string]any{
			{"id": "other/model", "context_length": 8192, "max_completion_tokens": 512},
		}))
	}))
	defer server.Close()

	cacheKey := server.URL + "\x00wanted-model"
	modelMetadataCache.Delete(cacheKey)
	defer modelMetadataCache.Delete(cacheKey)

	client := NewOpenAIClient(OpenAIConfig{APIKey: "test", BaseURL: server.URL})
	_, err := client.FetchModelMetadata(context.Background(), "wanted-model")
	if err == nil {
		t.Fatal("expected error when model not in list, got nil")
	}
}
