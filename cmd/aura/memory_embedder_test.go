package main

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/chetto1983/aura/internal/config"
	"github.com/chetto1983/aura/internal/llm"
)

// A key rotated in the cockpit replaces the LLM profile in place (primaryLLMRouteReloader);
// the daemon never restarts for it, so the memory route must read it from there on every
// request (spec §5, "The key is read live").
func TestMemoryEmbedderReadsTheRotatedKey(t *testing.T) {
	var mu sync.Mutex
	var seen []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/models") {
			_, _ = io.WriteString(w, `{"data":[{"id":"vendor/embed","context_length":2048}]}`)
			return
		}
		mu.Lock()
		seen = append(seen, r.Header.Get("Authorization"))
		mu.Unlock()
		vector := make([]float64, 768)
		vector[0] = 1
		_ = json.NewEncoder(w).Encode(map[string]any{"data": []map[string]any{{"index": 0, "embedding": vector}}})
	}))
	t.Cleanup(server.Close)

	cfg := &config.Config{Embed: config.EmbedConfig{CloudModel: "vendor/embed", CloudBaseURL: server.URL}}
	runtime := llm.NewRuntime(nil, llm.Config{APIKey: "boot-key"})
	embedder := memoryEmbedder(cfg, runtime)
	if _, err := embedder.Embed(t.Context(), []string{"a"}); err != nil {
		t.Fatalf("Embed: %v", err)
	}
	runtime.Replace(nil, llm.Config{APIKey: "rotated-key"})
	if _, err := embedder.Embed(t.Context(), []string{"b"}); err != nil {
		t.Fatalf("Embed after rotation: %v", err)
	}
	mu.Lock()
	defer mu.Unlock()
	if len(seen) != 2 || seen[0] != "Bearer boot-key" || seen[1] != "Bearer rotated-key" {
		t.Fatalf("authorization = %q, want the key live at each request", seen)
	}
}

func TestMemoryEmbedderIsNilWithoutARoute(t *testing.T) {
	if embedder := memoryEmbedder(&config.Config{}, llm.NewRuntime(nil, llm.Config{})); embedder != nil {
		t.Fatalf("got %#v, want nil with no embedding base", embedder)
	}
}
