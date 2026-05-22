package search

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/aura/aura/internal/storage/memoryindex"
)

func TestCompactMemoryQdrantIndexRecreatesCollectionAndUpsertsPayloads(t *testing.T) {
	var sawDelete, sawCreate, sawUpsert bool
	var createBody struct {
		Vectors struct {
			Size     int    `json:"size"`
			Distance string `json:"distance"`
		} `json:"vectors"`
	}
	var pointsBody struct {
		Points []struct {
			ID      string            `json:"id"`
			Vector  []float32         `json:"vector"`
			Payload map[string]string `json:"payload"`
		} `json:"points"`
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("api-key") != "secret" {
			t.Fatalf("missing api-key header")
		}
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/collections/aura_memory_v1_compact":
			// Simulate cold start (collection not yet present) so rebuild proceeds.
			w.WriteHeader(http.StatusNotFound)
		case r.Method == http.MethodDelete && r.URL.Path == "/collections/aura_memory_v1_compact":
			sawDelete = true
			w.WriteHeader(http.StatusOK)
		case r.Method == http.MethodPut && r.URL.Path == "/collections/aura_memory_v1_compact":
			sawCreate = true
			if err := json.NewDecoder(r.Body).Decode(&createBody); err != nil {
				t.Fatalf("decode create body: %v", err)
			}
			w.WriteHeader(http.StatusOK)
		case r.Method == http.MethodPut && r.URL.Path == "/collections/aura_memory_v1_compact/points":
			sawUpsert = true
			if r.URL.Query().Get("wait") != "true" {
				t.Fatalf("wait query = %q, want true", r.URL.RawQuery)
			}
			if err := json.NewDecoder(r.Body).Decode(&pointsBody); err != nil {
				t.Fatalf("decode points body: %v", err)
			}
			w.WriteHeader(http.StatusOK)
		default:
			t.Fatalf("unexpected qdrant request: %s %s", r.Method, r.URL.String())
		}
	}))
	defer server.Close()

	index, err := NewCompactMemoryQdrantIndex(QdrantConfig{
		BaseURL:    server.URL,
		Collection: CompactMemoryQdrantCollection("aura_memory_v1"),
		APIKey:     "secret",
		BatchSize:  100,
	}, keywordEmbedding, slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err != nil {
		t.Fatalf("NewCompactMemoryQdrantIndex: %v", err)
	}
	report, err := index.Recreate(context.Background(), []memoryindex.Document{{
		ID:             "source:src_abc#page=2",
		Kind:           memoryindex.KindSource,
		Title:          "agreement.pdf",
		Body:           "The cancellation clause requires thirty days notice.",
		Handle:         "source:src_abc#page=2",
		SourceID:       "src_abc",
		Page:           2,
		ChatID:         10,
		ConversationID: 12,
		ProposalID:     34,
		Status:         "ocr_complete",
		Entities:       []string{"contract"},
		Tags:           []string{"pdf"},
		UpdatedAt:      time.Date(2026, 5, 8, 12, 0, 0, 0, time.UTC),
	}})
	if err != nil {
		t.Fatalf("Recreate: %v", err)
	}
	if !sawDelete || !sawCreate || !sawUpsert {
		t.Fatalf("requests delete:%v create:%v upsert:%v", sawDelete, sawCreate, sawUpsert)
	}
	if createBody.Vectors.Size != 5 || createBody.Vectors.Distance != "Cosine" {
		t.Fatalf("create vectors = %+v", createBody.Vectors)
	}
	if report.Collection != "aura_memory_v1_compact" || report.DocsIndexed != 1 || report.VectorSize != 5 {
		t.Fatalf("report = %+v", report)
	}
	payload := pointsBody.Points[0].Payload
	for _, key := range []string{"doc_id", "kind", "title", "content", "body", "handle", "source_id", "page", "chat_id", "conversation_id", "proposal_id", "status", "entities", "tags", "updated_at"} {
		if payload[key] == "" {
			t.Fatalf("missing payload[%s] in %+v", key, payload)
		}
	}
	if payload["kind"] != memoryindex.KindSource || payload["page"] != "2" || payload["chat_id"] != "10" {
		t.Fatalf("bad payload = %+v", payload)
	}
}

func TestCompactMemoryQdrantIndexUsesBatchEmbeddings(t *testing.T) {
	batchCalls := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/collections/aura_memory_v1_compact":
			// Simulate cold start (collection not yet present) so rebuild proceeds.
			w.WriteHeader(http.StatusNotFound)
		case r.Method == http.MethodDelete && r.URL.Path == "/collections/aura_memory_v1_compact":
			w.WriteHeader(http.StatusOK)
		case r.Method == http.MethodPut && r.URL.Path == "/collections/aura_memory_v1_compact":
			w.WriteHeader(http.StatusOK)
		case r.Method == http.MethodPut && r.URL.Path == "/collections/aura_memory_v1_compact/points":
			w.WriteHeader(http.StatusOK)
		default:
			t.Fatalf("unexpected qdrant request: %s %s", r.Method, r.URL.String())
		}
	}))
	defer server.Close()
	index, err := NewCompactMemoryQdrantIndexWithBatch(QdrantConfig{
		BaseURL:    server.URL,
		Collection: "aura_memory_v1_compact",
	}, keywordEmbedding, func(ctx context.Context, texts []string) ([][]float32, error) {
		batchCalls++
		vectors := make([][]float32, len(texts))
		for i, text := range texts {
			vec, err := keywordEmbedding(ctx, text)
			if err != nil {
				return nil, err
			}
			vectors[i] = vec
		}
		return vectors, nil
	}, slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err != nil {
		t.Fatalf("NewCompactMemoryQdrantIndexWithBatch: %v", err)
	}
	_, err = index.Recreate(context.Background(), []memoryindex.Document{
		{ID: "source:1", Kind: memoryindex.KindSource, Body: "first compact source", UpdatedAt: time.Now().UTC()},
		{ID: "archive:2", Kind: memoryindex.KindArchive, Body: "second compact archive", UpdatedAt: time.Now().UTC()},
	})
	if err != nil {
		t.Fatalf("Recreate: %v", err)
	}
	if batchCalls != 1 {
		t.Fatalf("batchCalls = %d, want 1", batchCalls)
	}
}

func TestCompactMemoryQdrantRecreateWarmCacheHit(t *testing.T) {
	var sawDelete, sawCreate, sawUpsert, sawCollectionInfo bool
	embedCalls := 0

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/collections/aura_memory_v1_compact":
			sawCollectionInfo = true
			w.WriteHeader(http.StatusOK)
			_, _ = io.WriteString(w, `{"result":{"status":"green","points_count":42,"indexed_vectors_count":42}}`)
		case r.Method == http.MethodDelete && r.URL.Path == "/collections/aura_memory_v1_compact":
			sawDelete = true
			w.WriteHeader(http.StatusOK)
		case r.Method == http.MethodPut && r.URL.Path == "/collections/aura_memory_v1_compact":
			sawCreate = true
			w.WriteHeader(http.StatusOK)
		case r.Method == http.MethodPut && r.URL.Path == "/collections/aura_memory_v1_compact/points":
			sawUpsert = true
			w.WriteHeader(http.StatusOK)
		default:
			t.Fatalf("unexpected qdrant request: %s %s", r.Method, r.URL.String())
		}
	}))
	defer server.Close()

	countingEmbed := func(ctx context.Context, text string) ([]float32, error) {
		embedCalls++
		return keywordEmbedding(ctx, text)
	}

	index, err := NewCompactMemoryQdrantIndex(QdrantConfig{
		BaseURL:    server.URL,
		Collection: "aura_memory_v1_compact",
		APIKey:     "secret",
	}, countingEmbed, slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err != nil {
		t.Fatalf("NewCompactMemoryQdrantIndex: %v", err)
	}

	report, err := index.Recreate(context.Background(), []memoryindex.Document{{
		ID:   "archive:1",
		Kind: memoryindex.KindArchive,
		Body: "some body",
	}})
	if err != nil {
		t.Fatalf("Recreate: %v", err)
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
	if report.DocsIndexed != 0 {
		t.Fatalf("report.DocsIndexed = %d, want 0 on warm-cache hit", report.DocsIndexed)
	}
	if report.VectorSize != 0 {
		t.Fatalf("report.VectorSize = %d, want 0 on warm-cache hit", report.VectorSize)
	}
	if report.Collection != "aura_memory_v1_compact" {
		t.Fatalf("report.Collection = %q, want aura_memory_v1_compact", report.Collection)
	}
}

func TestCompactMemoryQdrantRecreateColdPath(t *testing.T) {
	var sawDelete, sawCreate, sawUpsert, sawCollectionInfo bool

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/collections/aura_memory_v1_compact":
			sawCollectionInfo = true
			w.WriteHeader(http.StatusOK)
			_, _ = io.WriteString(w, `{"result":{"status":"green","points_count":0,"indexed_vectors_count":0}}`)
		case r.Method == http.MethodDelete && r.URL.Path == "/collections/aura_memory_v1_compact":
			sawDelete = true
			w.WriteHeader(http.StatusOK)
		case r.Method == http.MethodPut && r.URL.Path == "/collections/aura_memory_v1_compact":
			sawCreate = true
			w.WriteHeader(http.StatusOK)
		case r.Method == http.MethodPut && r.URL.Path == "/collections/aura_memory_v1_compact/points":
			sawUpsert = true
			w.WriteHeader(http.StatusOK)
		default:
			t.Fatalf("unexpected qdrant request: %s %s", r.Method, r.URL.String())
		}
	}))
	defer server.Close()

	index, err := NewCompactMemoryQdrantIndex(QdrantConfig{
		BaseURL:    server.URL,
		Collection: "aura_memory_v1_compact",
	}, keywordEmbedding, slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err != nil {
		t.Fatalf("NewCompactMemoryQdrantIndex: %v", err)
	}

	_, err = index.Recreate(context.Background(), []memoryindex.Document{{
		ID:   "archive:1",
		Kind: memoryindex.KindArchive,
		Body: "some body",
	}})
	if err != nil {
		t.Fatalf("Recreate: %v", err)
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
}

func TestCompactMemoryQdrantRecreateCollectionNotFound(t *testing.T) {
	var sawDelete, sawCreate, sawUpsert, sawCollectionInfo bool

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/collections/aura_memory_v1_compact":
			sawCollectionInfo = true
			w.WriteHeader(http.StatusNotFound)
		case r.Method == http.MethodDelete && r.URL.Path == "/collections/aura_memory_v1_compact":
			sawDelete = true
			w.WriteHeader(http.StatusOK)
		case r.Method == http.MethodPut && r.URL.Path == "/collections/aura_memory_v1_compact":
			sawCreate = true
			w.WriteHeader(http.StatusOK)
		case r.Method == http.MethodPut && r.URL.Path == "/collections/aura_memory_v1_compact/points":
			sawUpsert = true
			w.WriteHeader(http.StatusOK)
		default:
			t.Fatalf("unexpected qdrant request: %s %s", r.Method, r.URL.String())
		}
	}))
	defer server.Close()

	index, err := NewCompactMemoryQdrantIndex(QdrantConfig{
		BaseURL:    server.URL,
		Collection: "aura_memory_v1_compact",
	}, keywordEmbedding, slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err != nil {
		t.Fatalf("NewCompactMemoryQdrantIndex: %v", err)
	}

	_, err = index.Recreate(context.Background(), []memoryindex.Document{{
		ID:   "archive:1",
		Kind: memoryindex.KindArchive,
		Body: "some body",
	}})
	if err != nil {
		t.Fatalf("Recreate: %v", err)
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
}

func TestCompactMemoryQdrantRecreateCollectionInfoErrorFallsBack(t *testing.T) {
	var sawDelete, sawCreate, sawUpsert, sawCollectionInfo bool

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/collections/aura_memory_v1_compact":
			sawCollectionInfo = true
			w.WriteHeader(http.StatusInternalServerError)
			_, _ = io.WriteString(w, `{"status":{"error":"internal"}}`)
		case r.Method == http.MethodDelete && r.URL.Path == "/collections/aura_memory_v1_compact":
			sawDelete = true
			w.WriteHeader(http.StatusOK)
		case r.Method == http.MethodPut && r.URL.Path == "/collections/aura_memory_v1_compact":
			sawCreate = true
			w.WriteHeader(http.StatusOK)
		case r.Method == http.MethodPut && r.URL.Path == "/collections/aura_memory_v1_compact/points":
			sawUpsert = true
			w.WriteHeader(http.StatusOK)
		default:
			t.Fatalf("unexpected qdrant request: %s %s", r.Method, r.URL.String())
		}
	}))
	defer server.Close()

	index, err := NewCompactMemoryQdrantIndex(QdrantConfig{
		BaseURL:    server.URL,
		Collection: "aura_memory_v1_compact",
	}, keywordEmbedding, slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err != nil {
		t.Fatalf("NewCompactMemoryQdrantIndex: %v", err)
	}

	_, err = index.Recreate(context.Background(), []memoryindex.Document{{
		ID:   "archive:1",
		Kind: memoryindex.KindArchive,
		Body: "some body",
	}})
	if err != nil {
		t.Fatalf("Recreate: %v", err)
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
}

func TestCompactMemoryQdrantIndexSearchMapsPayloadAndFilters(t *testing.T) {
	var queryBody struct {
		Query       []float32 `json:"query"`
		Limit       int       `json:"limit"`
		WithPayload bool      `json:"with_payload"`
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/collections/aura_memory_v1_compact/points/query" {
			t.Fatalf("unexpected qdrant query request: %s %s", r.Method, r.URL.String())
		}
		if err := json.NewDecoder(r.Body).Decode(&queryBody); err != nil {
			t.Fatalf("decode query body: %v", err)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
			"status": "ok",
			"result": {
				"points": [{
					"id": "11111111-1111-5111-8111-111111111111",
					"score": 0.88,
					"payload": {
						"doc_id": "archive:12",
						"kind": "archive",
						"title": "chat=42 turn=3",
						"content": "archive content",
						"body": "Remember the contract deadline.",
						"handle": "conversation:12",
						"chat_id": "42",
						"conversation_id": "12",
						"tags": "user"
					}
				}, {
					"id": "22222222-2222-5222-8222-222222222222",
					"score": 0.80,
					"payload": {
						"doc_id": "archive:13",
						"kind": "archive",
						"title": "chat=99 turn=1",
						"body": "Other chat private deadline.",
						"handle": "conversation:13",
						"chat_id": "99",
						"conversation_id": "13"
					}
				}]
			}
		}`))
	}))
	defer server.Close()

	index, err := NewCompactMemoryQdrantIndex(QdrantConfig{
		BaseURL:    server.URL,
		Collection: "aura_memory_v1_compact",
	}, keywordEmbedding, slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err != nil {
		t.Fatalf("NewCompactMemoryQdrantIndex: %v", err)
	}
	results, err := index.Search(context.Background(), "contract deadline", memoryindex.Filter{
		Kinds:  []string{memoryindex.KindArchive},
		ChatID: 42,
		Limit:  1,
	})
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if len(queryBody.Query) != 5 || queryBody.Limit != 3 || !queryBody.WithPayload {
		t.Fatalf("query body = %+v", queryBody)
	}
	if len(results) != 1 {
		t.Fatalf("results = %d, want 1", len(results))
	}
	got := results[0]
	if got.ID != "archive:12" || got.Kind != memoryindex.KindArchive || got.Handle != "conversation:12" || got.ChatID != 42 || got.ConversationID != 12 || got.Score < 0.87 {
		t.Fatalf("mapped result = %+v", got)
	}
}

func TestCompactMemoryQdrantIndexUpsertCreatesCollectionWhenMissing(t *testing.T) {
	var createSeen bool
	upsertAttempts := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodPut && r.URL.Path == "/collections/aura_memory_v1_compact/points":
			upsertAttempts++
			if upsertAttempts == 1 {
				http.Error(w, "collection not found", http.StatusNotFound)
				return
			}
			w.WriteHeader(http.StatusOK)
		case r.Method == http.MethodPut && r.URL.Path == "/collections/aura_memory_v1_compact":
			createSeen = true
			w.WriteHeader(http.StatusOK)
		default:
			t.Fatalf("unexpected qdrant request: %s %s", r.Method, r.URL.String())
		}
	}))
	defer server.Close()

	index, err := NewCompactMemoryQdrantIndex(QdrantConfig{
		BaseURL:    server.URL,
		Collection: "aura_memory_v1_compact",
	}, keywordEmbedding, slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err != nil {
		t.Fatalf("NewCompactMemoryQdrantIndex: %v", err)
	}
	err = index.Upsert(context.Background(), []memoryindex.Document{{
		ID:        "archive:1",
		Kind:      memoryindex.KindArchive,
		Body:      "fresh compact archive row",
		Handle:    "conversation:1",
		UpdatedAt: time.Now().UTC(),
	}})
	if err != nil {
		t.Fatalf("Upsert: %v", err)
	}
	if !createSeen || upsertAttempts != 2 {
		t.Fatalf("createSeen=%v upsertAttempts=%d", createSeen, upsertAttempts)
	}
}

func TestCompactMemoryQdrantIndexDeletePoints(t *testing.T) {
	var deleteSeen bool
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/collections/aura_memory_v1_compact/points/delete" {
			t.Fatalf("unexpected qdrant delete request: %s %s", r.Method, r.URL.String())
		}
		if r.URL.Query().Get("wait") != "true" {
			t.Fatalf("wait query = %q, want true", r.URL.RawQuery)
		}
		var body struct {
			Points []string `json:"points"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatalf("decode delete body: %v", err)
		}
		if len(body.Points) != 2 || body.Points[0] == "" || body.Points[1] == "" {
			t.Fatalf("delete body = %+v", body)
		}
		deleteSeen = true
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	index, err := NewCompactMemoryQdrantIndex(QdrantConfig{
		BaseURL:    server.URL,
		Collection: "aura_memory_v1_compact",
	}, keywordEmbedding, slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err != nil {
		t.Fatalf("NewCompactMemoryQdrantIndex: %v", err)
	}
	if err := index.Delete(context.Background(), []string{"archive:1", "archive:2"}); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	if !deleteSeen {
		t.Fatalf("delete request was not sent")
	}
}

func TestCompactMemoryQdrantCollectionIsSeparateFromWikiCollection(t *testing.T) {
	if got := CompactMemoryQdrantCollection("aura_memory_v1"); got != "aura_memory_v1_compact" {
		t.Fatalf("collection = %q", got)
	}
	if got := CompactMemoryQdrantCollection("aura_memory_v1_compact"); got != "aura_memory_v1_compact_compact" {
		t.Fatalf("collection = %q", got)
	}
}

func TestCompactMemoryQdrantDimMismatchTriggersRebuild(t *testing.T) {
	var sawDelete, sawCreate, sawUpsert bool

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/collections/aura_memory_v1_compact":
			// Existing collection: 10 points, dim=5 (old model).
			w.WriteHeader(http.StatusOK)
			_, _ = io.WriteString(w, `{"result":{"status":"green","points_count":10,"indexed_vectors_count":10,"config":{"params":{"vectors":{"size":5}}}}}`)
		case r.Method == http.MethodDelete && r.URL.Path == "/collections/aura_memory_v1_compact":
			sawDelete = true
			w.WriteHeader(http.StatusOK)
		case r.Method == http.MethodPut && r.URL.Path == "/collections/aura_memory_v1_compact":
			sawCreate = true
			w.WriteHeader(http.StatusOK)
		case r.Method == http.MethodPut && r.URL.Path == "/collections/aura_memory_v1_compact/points":
			sawUpsert = true
			w.WriteHeader(http.StatusOK)
		default:
			t.Fatalf("unexpected qdrant request: %s %s", r.Method, r.URL.String())
		}
	}))
	defer server.Close()

	index, err := NewCompactMemoryQdrantIndexWithBatch(QdrantConfig{
		BaseURL:    server.URL,
		Collection: "aura_memory_v1_compact",
		// expectedDim=8 differs from stored dim=5 → must trigger rebuild.
		OutputDim: 8,
	}, keywordEmbedding, nil, slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err != nil {
		t.Fatalf("NewCompactMemoryQdrantIndexWithBatch: %v", err)
	}
	_, err = index.Recreate(context.Background(), []memoryindex.Document{{
		ID:   "archive:dm1",
		Kind: memoryindex.KindArchive,
		Body: "dim mismatch test body",
	}})
	if err != nil {
		t.Fatalf("Recreate: %v", err)
	}
	if !sawDelete || !sawCreate || !sawUpsert {
		t.Fatalf("dim-mismatch rebuild: delete=%v create=%v upsert=%v", sawDelete, sawCreate, sawUpsert)
	}
}

func TestCompactMemoryQdrantDimMismatchSkippedByEnv(t *testing.T) {
	var sawDelete, sawCreate, sawUpsert bool

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/collections/aura_memory_v1_compact":
			// Existing collection: 10 points, dim=5 (old model).
			w.WriteHeader(http.StatusOK)
			_, _ = io.WriteString(w, `{"result":{"status":"green","points_count":10,"indexed_vectors_count":10,"config":{"params":{"vectors":{"size":5}}}}}`)
		case r.Method == http.MethodDelete && r.URL.Path == "/collections/aura_memory_v1_compact":
			sawDelete = true
			w.WriteHeader(http.StatusOK)
		case r.Method == http.MethodPut && r.URL.Path == "/collections/aura_memory_v1_compact":
			sawCreate = true
			w.WriteHeader(http.StatusOK)
		case r.Method == http.MethodPut && r.URL.Path == "/collections/aura_memory_v1_compact/points":
			sawUpsert = true
			w.WriteHeader(http.StatusOK)
		default:
			t.Fatalf("unexpected qdrant request: %s %s", r.Method, r.URL.String())
		}
	}))
	defer server.Close()

	index, err := NewCompactMemoryQdrantIndexWithBatch(QdrantConfig{
		BaseURL:                server.URL,
		Collection:             "aura_memory_v1_compact",
		OutputDim:              8,
		SkipDimMismatchRebuild: true,
	}, keywordEmbedding, nil, slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err != nil {
		t.Fatalf("NewCompactMemoryQdrantIndexWithBatch: %v", err)
	}
	_, err = index.Recreate(context.Background(), []memoryindex.Document{{
		ID:   "archive:dm2",
		Kind: memoryindex.KindArchive,
		Body: "dim mismatch skip test body",
	}})
	if err != nil {
		t.Fatalf("Recreate: %v", err)
	}
	// With SkipDimMismatchRebuild=true the collection must NOT be touched.
	if sawDelete || sawCreate || sawUpsert {
		t.Fatalf("skip-dim-rebuild: expected no Qdrant writes, got delete=%v create=%v upsert=%v", sawDelete, sawCreate, sawUpsert)
	}
}
