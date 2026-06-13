package documents

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/chetto1983/aura/internal/knowledge"
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
			"data": []map[string]any{{"embedding": []float64{1, 2}}},
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
	dim := knowledge.DefaultEmbedDimensions
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"data": []map[string]any{{"embedding": make([]float64, dim)}},
		})
	}))
	defer srv.Close()

	// Dimensions unset (0) -> defaults to knowledge.DefaultEmbedDimensions.
	client := &EmbeddingClient{BaseURL: srv.URL, Client: srv.Client()}
	got, err := client.Embed(t.Context(), []string{"a"})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || len(got[0]) != dim {
		t.Fatalf("embedding dims = %d, want default %d", len(got[0]), dim)
	}
}

// --- indexer.go UpsertSparse guard + error branches ---

func TestUpsertSparseRejectsMissingClient(t *testing.T) {
	_, err := (&Indexer{}).UpsertSparse(t.Context(), testDocumentWithChunks(t, 1))
	if err == nil || !strings.Contains(err.Error(), "knowledge client") {
		t.Fatalf("want missing client error, got %v", err)
	}
}

func TestUpsertSparseRejectsDocumentWithoutChunks(t *testing.T) {
	_, err := (&Indexer{Client: &fakeKnowledgeClient{}}).UpsertSparse(t.Context(), ExtractedDocument{ID: "doc_1"})
	if err == nil || !strings.Contains(err.Error(), "no chunks") {
		t.Fatalf("want no-chunks error, got %v", err)
	}
}

func TestUpsertSparseFailsWhenDocumentUpsertFails(t *testing.T) {
	// First Write (the document MERGE) fails.
	fake := &fakeKnowledgeClient{failWriteAt: 1, failErr: errors.New("neo4j down")}
	_, err := (&Indexer{Client: fake}).UpsertSparse(t.Context(), testDocumentWithChunks(t, 1))
	if err == nil || !strings.Contains(err.Error(), "upsert document") {
		t.Fatalf("want upsert document error, got %v", err)
	}
}

func TestUpsertSparseFailsWhenMarkSearchableFails(t *testing.T) {
	// Doc MERGE (1) + chunk batch (2) succeed; the searchable mark (3) fails.
	fake := &fakeKnowledgeClient{failWriteAt: 3, failErr: errors.New("neo4j down")}
	_, err := (&Indexer{Client: fake}).UpsertSparse(t.Context(), testDocumentWithChunks(t, 1))
	if err == nil || !strings.Contains(err.Error(), "mark document searchable") {
		t.Fatalf("want mark searchable error, got %v", err)
	}
}

func TestUpsertEmbeddingsPropagatesWriteError(t *testing.T) {
	fake := &fakeKnowledgeClient{failWriteAt: 1, failErr: errors.New("neo4j down")}
	_, err := (&Indexer{Client: fake}).UpsertEmbeddings(t.Context(), "doc_1", []EmbeddedChunk{{ID: "c1"}}, JobComplete, 1)
	if err == nil || !strings.Contains(err.Error(), "upsert embeddings") {
		t.Fatalf("want upsert embeddings error, got %v", err)
	}
}

// --- worker.go embedWithRetry exhaustion ---

func TestEmbeddingWorkerReturnsLastErrorAfterRetriesExhausted(t *testing.T) {
	doc := testDocumentWithChunks(t, 1)
	gen := &fakeEmbeddingGenerator{failures: 10}
	worker := &EmbeddingWorker{
		Generator:  gen,
		Indexer:    &fakeEmbeddingIndexer{},
		BatchSize:  1,
		MaxRetries: 2,
		Backoff:    time.Nanosecond,
	}
	err := worker.Process(t.Context(), doc)
	if err == nil || !strings.Contains(err.Error(), "embed failed") {
		t.Fatalf("want embed failed error after exhausting retries, got %v", err)
	}
	if gen.calls != 2 {
		t.Fatalf("generator calls = %d, want 2 (MaxRetries)", gen.calls)
	}
}

func TestEmbeddingWorkerUsesDefaultBatchAndRetryWhenUnset(t *testing.T) {
	doc := testDocumentWithChunks(t, 2)
	indexer := &fakeEmbeddingIndexer{}
	// BatchSize and MaxRetries left at zero -> defaults apply (32 / 3).
	worker := &EmbeddingWorker{Generator: &fakeEmbeddingGenerator{}, Indexer: indexer, Backoff: time.Nanosecond}
	if err := worker.Process(t.Context(), doc); err != nil {
		t.Fatal(err)
	}
	// Two chunks fit one default batch of 32.
	if indexer.calls != 1 {
		t.Fatalf("indexer calls = %d, want 1 default batch", indexer.calls)
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

func TestServiceIngestPathRejectsMissingDependencies(t *testing.T) {
	path := writeNamedTempFile(t, "manual.pdf", "payload")

	if _, err := (&Service{}).IngestPath(t.Context(), IngestRequest{}, path); err == nil || !strings.Contains(err.Error(), "job store") {
		t.Fatalf("missing job store error = %v", err)
	}
	noExtractor := &Service{Jobs: newFakeJobStore()}
	if _, err := noExtractor.IngestPath(t.Context(), IngestRequest{}, path); err == nil || !strings.Contains(err.Error(), "extractor") {
		t.Fatalf("missing extractor error = %v", err)
	}
	noIndexer := &Service{Jobs: newFakeJobStore(), Extractor: &fakeExtractor{resp: oneChunkResponse()}}
	if _, err := noIndexer.IngestPath(t.Context(), IngestRequest{}, path); err == nil || !strings.Contains(err.Error(), "indexer") {
		t.Fatalf("missing indexer error = %v", err)
	}
}

func TestServiceIngestPathRejectsMissingFile(t *testing.T) {
	service := &Service{
		Jobs:      newFakeJobStore(),
		Extractor: &fakeExtractor{resp: oneChunkResponse()},
		Indexer:   &fakeSparseIndexer{count: 1},
	}
	_, err := service.IngestPath(t.Context(), IngestRequest{}, t.TempDir()+"/missing.pdf")
	if err == nil {
		t.Fatal("want stat error for missing file")
	}
}
