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
	"github.com/chetto1983/aura/internal/documents"
	"github.com/chetto1983/aura/internal/llm"
	"github.com/jackc/pgx/v5/pgxpool"
)

// Without ArcadeDB there is NO retrieval at all, and Retrieve must say so rather than
// answer emptily.
//
// This test asserted the opposite until 2026-08-08: that a PostgreSQL control plane
// survived so the cascade could still degrade to card-only. That fallback is gone with the
// store behind it -- the cards ARE ArcadeDB records now, so when ArcadeDB is unreachable
// there is nothing left to rank, not merely a thinner ranking.
//
// Failing is the point. A retriever with no control plane that returned an empty success
// would report "no matching documents" for a corpus it never searched, which is the
// skip-as-green shape this codebase refuses everywhere else.
func TestNewHostDocumentRetrieverHasNoControlPlaneWithoutArcadeDB(t *testing.T) {
	retriever, err := newHostDocumentRetriever(&config.Config{}, &pgxpool.Pool{})
	if err != nil {
		t.Fatal(err)
	}
	if retriever == nil || retriever.ControlPlane != nil || retriever.PassageIndex != nil {
		t.Fatalf("retriever = %#v, want neither a control plane nor a projection", retriever)
	}
	if _, err := retriever.Retrieve(t.Context(), documents.RetrievalRequest{
		IdentityID: "00000000-0000-0000-0000-000000000001", Query: "clienti",
	}); err == nil {
		t.Fatal("Retrieve answered without a control plane instead of failing")
	}
}

// Review Focus 4: a key rotated in the cockpit replaces the LLM profile in place, and the
// retriever the registry built before that runtime existed must send the new key on its next
// request (spec §5).
func TestDocumentQueryEmbedderReadsTheRotatedKey(t *testing.T) {
	var mu sync.Mutex
	var seen []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// A hosted route reads the model's input limit from the catalogue first (fit.go),
		// as TestMemoryEmbedderReadsTheRotatedKey's server answers it.
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
	cfg := &config.Config{Embed: config.EmbedConfig{CloudModel: "vendor/embed", CloudBaseURL: server.URL, Dimensions: 768}}
	handles := runtimeToolHandles{Documents: newDocumentLibrary(&pgxpool.Pool{}, cfg)}
	runtime := llm.NewRuntime(nil, llm.Config{APIKey: "boot-key"})
	wireDocumentQueryEmbedder(&handles, newQueryEmbedder(cfg, liveKey(runtime)))

	embedder := handles.Documents.retriever.Embedder
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
		t.Fatalf("authorization = %q, want the key read live at each request", seen)
	}
}

// The daemon must read documents in the space the ingest supervisor stamps them with, or the
// documents gate never opens. Both sides meet at one literal: the supervisor's
// TestRouteResolverNamesTheLocalSidecarsSpace and arcadedb's
// TestEmbeddingGemmaFloorsAreKeyedByTheSpaceTheVMAttests pin the same attestation to it.
func TestDocumentQuerySpaceIsTheSpaceIngestStamps(t *testing.T) {
	sidecar := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.WriteString(w, `{"data":[{"id":"/models/embeddinggemma-300M-Q8_0.gguf",`+
			`"meta":{"n_embd":768,"n_params":307581696,"size":327060480,"ftype":"Q8_0"}}]}`)
	}))
	t.Cleanup(sidecar.Close)
	space, err := newQueryEmbedder(&config.Config{Embed: config.EmbedConfig{BaseURL: sidecar.URL, Dimensions: 768}}, nil).
		Space(t.Context())
	if err != nil || space.ID != "es1-e0aa6accf0b79c6b" {
		t.Fatalf("daemon reads documents in %q (err %v), want es1-e0aa6accf0b79c6b", space.ID, err)
	}
}
