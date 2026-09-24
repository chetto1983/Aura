package main

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/chetto1983/aura/internal/config"
	"github.com/chetto1983/aura/internal/embeddings"
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

// The daemon and arcadedb-mcp must name the same memory space, or each re-stamps the other's
// writes and memory stays lexical; MCP logs its space at boot, and so must the daemon, or the
// operator can see only one side (final review recommendation).
func TestLogMemorySpaceNamesTheDaemonsSpace(t *testing.T) {
	var out bytes.Buffer
	logMemorySpace(slog.New(slog.NewTextHandler(&out, nil)), namedSpace("es1-daemon"), time.Second)
	if !strings.Contains(out.String(), "space=es1-daemon") {
		t.Fatalf("boot log = %q, want the daemon's memory space", out.String())
	}
}

type namedSpace string

func (s namedSpace) Embed(context.Context, []string) ([][]float64, error) { return nil, nil }

func (s namedSpace) Space(context.Context) (embeddings.Space, error) {
	return embeddings.Space{ID: string(s)}, nil
}

func TestMemoryEmbedderIsNilWithoutARoute(t *testing.T) {
	if embedder := memoryEmbedder(&config.Config{}, llm.NewRuntime(nil, llm.Config{})); embedder != nil {
		t.Fatalf("got %#v, want nil with no embedding base", embedder)
	}
}
