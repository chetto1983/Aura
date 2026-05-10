package tools

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestNewToolVectorIndexDefaults(t *testing.T) {
	idx := NewToolVectorIndex(ToolVectorConfig{}, nil)
	if idx == nil {
		t.Fatal("NewToolVectorIndex returned nil")
	}
	if idx.cfg.Backend != "fts" {
		t.Fatalf("default backend = %q, want fts", idx.cfg.Backend)
	}
	if idx.cfg.Collection != "aura_tool_search" {
		t.Fatalf("default collection = %q, want aura_tool_search", idx.cfg.Collection)
	}
}

func TestToolVectorIndexSearchWhenFTS(t *testing.T) {
	idx := NewToolVectorIndex(ToolVectorConfig{Backend: "fts"}, nil)
	results, err := idx.Search(context.Background(), "test query", 5)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(results) != 0 {
		t.Fatalf("expected no results for fts backend, got %d", len(results))
	}
}

func TestToolVectorIndexBuildWhenFTS(t *testing.T) {
	idx := NewToolVectorIndex(ToolVectorConfig{Backend: "fts"}, nil)
	docs := []toolVectorDoc{
		{name: "test_tool", text: "A tool for testing"},
	}
	if err := idx.Build(context.Background(), docs); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if idx.docCount != 0 {
		t.Fatalf("docCount = %d, want 0 for fts backend", idx.docCount)
	}
}

func TestToolVectorIndexNilReady(t *testing.T) {
	var idx *toolVectorIndex
	if err := idx.Ready(context.Background()); err != nil {
		t.Fatalf("nil index Ready should not error: %v", err)
	}
}

func TestToolVectorIndexHealth(t *testing.T) {
	idx := NewToolVectorIndex(ToolVectorConfig{
		Backend:    "hybrid",
		EmbedModel: "test-model",
	}, nil)
	h := idx.Health()
	if h.Backend != "hybrid" {
		t.Fatalf("Health.Backend = %q, want hybrid", h.Backend)
	}
	if h.DocCount != 0 {
		t.Fatalf("Health.DocCount = %d, want 0", h.DocCount)
	}
	if h.Fallback {
		t.Fatal("Health.Fallback = true, want false")
	}
}

func TestToolVectorIndexHealthWithError(t *testing.T) {
	idx := NewToolVectorIndex(ToolVectorConfig{Backend: "vector"}, nil)
	idx.lastError = context.DeadlineExceeded
	h := idx.Health()
	if h.LastError == "" {
		t.Fatal("expected LastError to be set")
	}
	if !h.Fallback {
		t.Fatal("expected Fallback = true when lastError is set")
	}
}

func TestToolVectorIndexNilHealth(t *testing.T) {
	var idx *toolVectorIndex
	h := idx.Health()
	if h.Backend != "fts" {
		t.Fatalf("nil Health.Backend = %q, want fts", h.Backend)
	}
	if !h.Fallback {
		t.Fatal("nil Health.Fallback = false, want true")
	}
}

func TestToolQdrantPointID(t *testing.T) {
	id1 := toolQdrantPointID("execute_code")
	id2 := toolQdrantPointID("execute_code")
	id3 := toolQdrantPointID("execute_shell")
	if id1 != id2 {
		t.Fatalf("same name produced different ids: %q vs %q", id1, id2)
	}
	if id1 == id3 {
		t.Fatal("different names produced same id")
	}
	if len(id1) != 36 {
		t.Fatalf("point id length = %d, want 36", len(id1))
	}
}

func TestToolVectorIndexBuildEmptyDocs(t *testing.T) {
	idx := NewToolVectorIndex(ToolVectorConfig{Backend: "vector"}, nil)
	if err := idx.Build(context.Background(), nil); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if idx.docCount != 0 {
		t.Fatalf("docCount = %d, want 0", idx.docCount)
	}
}

func TestToolVectorConfigDefaultCollection(t *testing.T) {
	idx := NewToolVectorIndex(ToolVectorConfig{Backend: "hybrid", Collection: ""}, nil)
	if idx.cfg.Collection != "aura_tool_search" {
		t.Fatalf("Collection = %q, want aura_tool_search", idx.cfg.Collection)
	}
}

func TestToolVectorConfigCustomCollection(t *testing.T) {
	idx := NewToolVectorIndex(ToolVectorConfig{
		Backend:    "vector",
		Collection: "custom_tools",
	}, nil)
	if idx.cfg.Collection != "custom_tools" {
		t.Fatalf("Collection = %q, want custom_tools", idx.cfg.Collection)
	}
}

// newToolVectorIndexForTest creates a toolVectorIndex backed by a qdrant test server and an
// optional embed test server. embedServerURL may be empty to assert embed is not called.
func newToolVectorIndexForTest(t *testing.T, qdrantServerURL, embedServerURL string) *toolVectorIndex {
	t.Helper()
	cfg := ToolVectorConfig{
		Backend:      "vector",
		Collection:   "aura_tool_search",
		QdrantURL:    qdrantServerURL,
		EmbedBaseURL: embedServerURL,
		EmbedAPIKey:  "embedkey",
		EmbedModel:   "text-embed-001",
	}
	return NewToolVectorIndex(cfg, slog.New(slog.NewTextHandler(io.Discard, nil)))
}

// buildEmbedServer creates an httptest server that returns a 5-dimensional embedding per input.
func buildEmbedServer(t *testing.T, embedCalls *int) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			Input []string `json:"input"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Fatalf("decode embed body: %v", err)
		}
		if embedCalls != nil {
			*embedCalls += len(req.Input)
		}
		data := make([]map[string]interface{}, len(req.Input))
		for i := range req.Input {
			data[i] = map[string]interface{}{
				"index":     i,
				"embedding": []float32{0.1, 0.2, 0.3, 0.4, 0.5},
			}
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]interface{}{"data": data})
	}))
}

func TestToolVectorBuildWarmCacheHit(t *testing.T) {
	var sawDelete, sawCreate, sawUpsert, sawCollectionInfo bool
	embedCalls := 0

	qdrantServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/collections/aura_tool_search":
			sawCollectionInfo = true
			w.WriteHeader(http.StatusOK)
			_, _ = io.WriteString(w, `{"result":{"status":"green","points_count":42,"indexed_vectors_count":42}}`)
		case r.Method == http.MethodDelete && r.URL.Path == "/collections/aura_tool_search":
			sawDelete = true
			w.WriteHeader(http.StatusOK)
		case r.Method == http.MethodPut && r.URL.Path == "/collections/aura_tool_search":
			sawCreate = true
			w.WriteHeader(http.StatusOK)
		case r.Method == http.MethodPut && r.URL.Path == "/collections/aura_tool_search/points":
			sawUpsert = true
			w.WriteHeader(http.StatusOK)
		default:
			t.Fatalf("unexpected qdrant request: %s %s", r.Method, r.URL.String())
		}
	}))
	defer qdrantServer.Close()

	// Embed server should NOT be called on warm-cache hit.
	embedServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		embedCalls++
		t.Fatalf("embed called unexpectedly on warm-cache hit: %s %s", r.Method, r.URL.Path)
	}))
	defer embedServer.Close()

	idx := newToolVectorIndexForTest(t, qdrantServer.URL, embedServer.URL)
	docs := []toolVectorDoc{
		{name: "web_search", text: "search the web"},
		{name: "store_source", text: "store a source document"},
	}

	if err := idx.Build(context.Background(), docs); err != nil {
		t.Fatalf("Build: %v", err)
	}
	if !sawCollectionInfo {
		t.Fatal("CollectionInfo was not called")
	}
	if sawDelete {
		t.Fatal("DeleteCollection was called on warm-cache hit")
	}
	if sawCreate {
		t.Fatal("CreateCollection was called on warm-cache hit")
	}
	if sawUpsert {
		t.Fatal("Upsert was called on warm-cache hit")
	}
	if embedCalls != 0 {
		t.Fatalf("embed called %d times on warm-cache hit, want 0", embedCalls)
	}
	if idx.docCount != len(docs) {
		t.Fatalf("idx.docCount = %d, want %d", idx.docCount, len(docs))
	}
	if idx.lastError != nil {
		t.Fatalf("idx.lastError = %v, want nil", idx.lastError)
	}
}

func TestToolVectorBuildColdPath(t *testing.T) {
	var sawDelete, sawCreate, sawUpsert, sawCollectionInfo bool
	embedCalls := 0

	embedServer := buildEmbedServer(t, &embedCalls)
	defer embedServer.Close()

	qdrantServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/collections/aura_tool_search":
			sawCollectionInfo = true
			w.WriteHeader(http.StatusOK)
			_, _ = io.WriteString(w, `{"result":{"status":"green","points_count":0,"indexed_vectors_count":0}}`)
		case r.Method == http.MethodDelete && r.URL.Path == "/collections/aura_tool_search":
			sawDelete = true
			w.WriteHeader(http.StatusOK)
		case r.Method == http.MethodPut && r.URL.Path == "/collections/aura_tool_search":
			sawCreate = true
			w.WriteHeader(http.StatusOK)
		case r.Method == http.MethodPut && r.URL.Path == "/collections/aura_tool_search/points":
			sawUpsert = true
			w.WriteHeader(http.StatusOK)
		default:
			t.Fatalf("unexpected qdrant request: %s %s", r.Method, r.URL.String())
		}
	}))
	defer qdrantServer.Close()

	idx := newToolVectorIndexForTest(t, qdrantServer.URL, embedServer.URL)
	docs := []toolVectorDoc{
		{name: "web_search", text: "search the web"},
	}

	if err := idx.Build(context.Background(), docs); err != nil {
		t.Fatalf("Build: %v", err)
	}
	if !sawCollectionInfo {
		t.Fatal("CollectionInfo was not called")
	}
	if !sawDelete {
		t.Fatal("DeleteCollection was not called on cold path")
	}
	if !sawCreate {
		t.Fatal("CreateCollection was not called on cold path")
	}
	if !sawUpsert {
		t.Fatal("Upsert was not called on cold path")
	}
	if embedCalls == 0 {
		t.Fatal("embed was not called on cold path")
	}
}

func TestToolVectorBuildCollectionNotFound(t *testing.T) {
	var sawDelete, sawCreate, sawUpsert, sawCollectionInfo bool
	embedCalls := 0

	embedServer := buildEmbedServer(t, &embedCalls)
	defer embedServer.Close()

	qdrantServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/collections/aura_tool_search":
			sawCollectionInfo = true
			w.WriteHeader(http.StatusNotFound)
		case r.Method == http.MethodDelete && r.URL.Path == "/collections/aura_tool_search":
			sawDelete = true
			w.WriteHeader(http.StatusOK)
		case r.Method == http.MethodPut && r.URL.Path == "/collections/aura_tool_search":
			sawCreate = true
			w.WriteHeader(http.StatusOK)
		case r.Method == http.MethodPut && r.URL.Path == "/collections/aura_tool_search/points":
			sawUpsert = true
			w.WriteHeader(http.StatusOK)
		default:
			t.Fatalf("unexpected qdrant request: %s %s", r.Method, r.URL.String())
		}
	}))
	defer qdrantServer.Close()

	idx := newToolVectorIndexForTest(t, qdrantServer.URL, embedServer.URL)
	docs := []toolVectorDoc{
		{name: "web_search", text: "search the web"},
	}

	if err := idx.Build(context.Background(), docs); err != nil {
		t.Fatalf("Build: %v", err)
	}
	if !sawCollectionInfo {
		t.Fatal("CollectionInfo was not called")
	}
	if !sawDelete {
		t.Fatal("DeleteCollection was not called on not-found path")
	}
	if !sawCreate {
		t.Fatal("CreateCollection was not called on not-found path")
	}
	if !sawUpsert {
		t.Fatal("Upsert was not called on not-found path")
	}
	if embedCalls == 0 {
		t.Fatal("embed was not called on not-found path")
	}
}

func TestToolVectorBuildCollectionInfoErrorFallsBack(t *testing.T) {
	var sawDelete, sawCreate, sawUpsert, sawCollectionInfo bool
	embedCalls := 0

	embedServer := buildEmbedServer(t, &embedCalls)
	defer embedServer.Close()

	qdrantServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/collections/aura_tool_search":
			sawCollectionInfo = true
			w.WriteHeader(http.StatusInternalServerError)
			_, _ = io.WriteString(w, `{"status":{"error":"internal"}}`)
		case r.Method == http.MethodDelete && r.URL.Path == "/collections/aura_tool_search":
			sawDelete = true
			w.WriteHeader(http.StatusOK)
		case r.Method == http.MethodPut && r.URL.Path == "/collections/aura_tool_search":
			sawCreate = true
			w.WriteHeader(http.StatusOK)
		case r.Method == http.MethodPut && r.URL.Path == "/collections/aura_tool_search/points":
			sawUpsert = true
			w.WriteHeader(http.StatusOK)
		default:
			t.Fatalf("unexpected qdrant request: %s %s", r.Method, r.URL.String())
		}
	}))
	defer qdrantServer.Close()

	idx := newToolVectorIndexForTest(t, qdrantServer.URL, embedServer.URL)
	docs := []toolVectorDoc{
		{name: "web_search", text: "search the web"},
	}

	if err := idx.Build(context.Background(), docs); err != nil {
		t.Fatalf("Build: %v", err)
	}
	if !sawCollectionInfo {
		t.Fatal("CollectionInfo was not called")
	}
	if !sawDelete {
		t.Fatal("DeleteCollection was not called on error-fallback path")
	}
	if !sawCreate {
		t.Fatal("CreateCollection was not called on error-fallback path")
	}
	if !sawUpsert {
		t.Fatal("Upsert was not called on error-fallback path")
	}
	if embedCalls == 0 {
		t.Fatal("embed was not called on error-fallback path")
	}
}
