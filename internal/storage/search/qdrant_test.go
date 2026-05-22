package search

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/aura/aura/internal/wiki"
)

func TestRebuildQdrantWikiDocumentsCreatesCollectionAndUpsertsDocs(t *testing.T) {
	wikiDir := t.TempDir()
	writeTestMDPage(t, wikiDir, &wiki.Page{
		Title:         "Alpha Contract",
		Body:          "Core contract notes.",
		Category:      "project",
		Sources:       []string{"src_qdrant_phase07f"},
		SchemaVersion: wiki.CurrentSchemaVersion,
		PromptVersion: "v1",
		CreatedAt:     time.Now().UTC().Format(time.RFC3339),
		UpdatedAt:     time.Now().UTC().Format(time.RFC3339),
		Unversioned:   true,
	})
	writeTestMDPage(t, wikiDir, &wiki.Page{
		Title:         "Beta Review",
		Body:          "Review links to [[alpha-contract]].",
		Category:      "project",
		Related:       wiki.RelatedFromSlugs([]string{"alpha-contract"}),
		SchemaVersion: wiki.CurrentSchemaVersion,
		PromptVersion: "v1",
		CreatedAt:     time.Now().UTC().Format(time.RFC3339),
		UpdatedAt:     time.Now().UTC().Format(time.RFC3339),
	})

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
		case r.Method == http.MethodGet && r.URL.Path == "/readyz":
			_, _ = w.Write([]byte("ready"))
		case r.Method == http.MethodGet && r.URL.Path == "/collections/aura_memory_v1":
			// Simulate cold start (collection not yet present) so rebuild proceeds.
			w.WriteHeader(http.StatusNotFound)
		case r.Method == http.MethodDelete && r.URL.Path == "/collections/aura_memory_v1":
			sawDelete = true
			w.WriteHeader(http.StatusOK)
		case r.Method == http.MethodPut && r.URL.Path == "/collections/aura_memory_v1":
			sawCreate = true
			if err := json.NewDecoder(r.Body).Decode(&createBody); err != nil {
				t.Fatalf("decode create body: %v", err)
			}
			w.WriteHeader(http.StatusOK)
		case r.Method == http.MethodPut && r.URL.Path == "/collections/aura_memory_v1/points":
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

	report, err := RebuildQdrantWikiDocuments(context.Background(), wikiDir, keywordEmbedding, QdrantConfig{
		BaseURL:    server.URL,
		Collection: "aura_memory_v1",
		APIKey:     "secret",
		BatchSize:  100,
	}, slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err != nil {
		t.Fatalf("RebuildQdrantWikiDocuments: %v", err)
	}
	if !sawDelete || !sawCreate || !sawUpsert {
		t.Fatalf("requests delete:%v create:%v upsert:%v", sawDelete, sawCreate, sawUpsert)
	}
	if createBody.Vectors.Size != 5 || createBody.Vectors.Distance != "Cosine" {
		t.Fatalf("create vectors = %+v, want size=5 distance=Cosine", createBody.Vectors)
	}
	if got, want := len(pointsBody.Points), 6; got != want {
		t.Fatalf("upserted points = %d, want %d", got, want)
	}
	if report.DocsIndexed != 6 || report.PagesIndexed != 2 || report.VectorSize != 5 {
		t.Fatalf("report = %+v", report)
	}
	for _, point := range pointsBody.Points {
		if point.ID == "" || len(point.Vector) != 5 {
			t.Fatalf("bad point = %+v", point)
		}
		if point.Payload["doc_id"] == "" || point.Payload["kind"] == "" || point.Payload["content"] == "" {
			t.Fatalf("missing payload fields: %+v", point.Payload)
		}
		if point.Payload["doc_id"] == "alpha-contract" {
			for _, key := range []string{"schema_version", "prompt_version", "created_at", "updated_at", "sources", "unversioned"} {
				if point.Payload[key] == "" {
					t.Fatalf("alpha payload missing %s: %+v", key, point.Payload)
				}
			}
		}
	}
}

func TestRebuildQdrantWikiDocumentsWarmCacheHit(t *testing.T) {
	wikiDir := t.TempDir()
	writeTestMDPage(t, wikiDir, &wiki.Page{
		Title:         "Alpha Contract",
		Body:          "Core contract notes.",
		Category:      "project",
		SchemaVersion: wiki.CurrentSchemaVersion,
		PromptVersion: "v1",
		CreatedAt:     time.Now().UTC().Format(time.RFC3339),
		UpdatedAt:     time.Now().UTC().Format(time.RFC3339),
	})

	var sawDelete, sawCreate, sawUpsert, sawCollectionInfo bool
	embedCalls := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("api-key") != "secret" {
			t.Fatalf("missing api-key header")
		}
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/readyz":
			_, _ = w.Write([]byte("ready"))
		case r.Method == http.MethodGet && r.URL.Path == "/collections/aura_memory_v1":
			sawCollectionInfo = true
			w.WriteHeader(http.StatusOK)
			_, _ = io.WriteString(w, `{"result":{"status":"green","points_count":42,"indexed_vectors_count":42}}`)
		case r.Method == http.MethodDelete && r.URL.Path == "/collections/aura_memory_v1":
			sawDelete = true
			w.WriteHeader(http.StatusOK)
		case r.Method == http.MethodPut && r.URL.Path == "/collections/aura_memory_v1":
			sawCreate = true
			w.WriteHeader(http.StatusOK)
		case r.Method == http.MethodPut && r.URL.Path == "/collections/aura_memory_v1/points":
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

	report, err := RebuildQdrantWikiDocuments(context.Background(), wikiDir, countingEmbed, QdrantConfig{
		BaseURL:    server.URL,
		Collection: "aura_memory_v1",
		APIKey:     "secret",
	}, slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err != nil {
		t.Fatalf("RebuildQdrantWikiDocuments: %v", err)
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
	if report.Collection != "aura_memory_v1" {
		t.Fatalf("report.Collection = %q, want aura_memory_v1", report.Collection)
	}
	if report.DocsIndexed != 42 {
		t.Fatalf("report.DocsIndexed = %d, want 42 (live points count)", report.DocsIndexed)
	}
}

func TestQdrantRepositoryReindexWikiPageUpsertsChangedPageAfterWarmCache(t *testing.T) {
	wikiDir := t.TempDir()
	writeTestMDPage(t, wikiDir, &wiki.Page{
		Title:         "Phase07F Live Contract",
		Body:          "Initial body.",
		SchemaVersion: wiki.CurrentSchemaVersion,
		PromptVersion: "v1",
		CreatedAt:     "2026-05-17T10:00:00Z",
		UpdatedAt:     "2026-05-17T10:00:00Z",
	})

	var sawCollectionInfo, sawDelete, sawCreate bool
	var pointsBody struct {
		Points []struct {
			ID      string            `json:"id"`
			Vector  []float32         `json:"vector"`
			Payload map[string]string `json:"payload"`
		} `json:"points"`
	}
	embedCalls := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("api-key") != "secret" {
			t.Fatalf("missing api-key header")
		}
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/readyz":
			_, _ = w.Write([]byte("ready"))
		case r.Method == http.MethodGet && r.URL.Path == "/collections/aura_memory_v1":
			sawCollectionInfo = true
			w.WriteHeader(http.StatusOK)
			_, _ = io.WriteString(w, `{"result":{"status":"green","points_count":42,"indexed_vectors_count":42}}`)
		case r.Method == http.MethodDelete && r.URL.Path == "/collections/aura_memory_v1":
			sawDelete = true
			w.WriteHeader(http.StatusOK)
		case r.Method == http.MethodPut && r.URL.Path == "/collections/aura_memory_v1":
			sawCreate = true
			w.WriteHeader(http.StatusOK)
		case r.Method == http.MethodPut && r.URL.Path == "/collections/aura_memory_v1/points":
			if err := json.NewDecoder(r.Body).Decode(&pointsBody); err != nil {
				t.Fatalf("decode points body: %v", err)
			}
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
	repo, err := NewQdrantRepository(QdrantConfig{
		BaseURL:    server.URL,
		Collection: "aura_memory_v1",
		APIKey:     "secret",
	}, countingEmbed, wikiDir, slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err != nil {
		t.Fatalf("NewQdrantRepository: %v", err)
	}
	if err := repo.IndexWikiPages(context.Background()); err != nil {
		t.Fatalf("IndexWikiPages warm cache: %v", err)
	}
	writeTestMDPage(t, wikiDir, &wiki.Page{
		Title:         "Phase07F Live Contract",
		Body:          "Updated Phase07F metadata marker.",
		Sources:       []string{"src_phase07f_live"},
		SchemaVersion: wiki.CurrentSchemaVersion,
		PromptVersion: "v1",
		CreatedAt:     "2026-05-17T10:00:00Z",
		UpdatedAt:     "2026-05-17T11:00:00Z",
		Unversioned:   true,
	})
	if err := repo.ReindexWikiPage(context.Background(), "phase07f-live-contract"); err != nil {
		t.Fatalf("ReindexWikiPage: %v", err)
	}

	if !sawCollectionInfo {
		t.Fatal("warm-cache collection info was not checked")
	}
	if sawDelete {
		t.Fatal("ReindexWikiPage deleted the whole collection")
	}
	if sawCreate {
		t.Fatal("ReindexWikiPage recreated the warm collection")
	}
	if embedCalls != 1 {
		t.Fatalf("embed calls = %d, want exactly one single-page reindex embed", embedCalls)
	}
	if len(pointsBody.Points) != 1 {
		t.Fatalf("upsert points = %d, want 1", len(pointsBody.Points))
	}
	payload := pointsBody.Points[0].Payload
	if payload["doc_id"] != "phase07f-live-contract" || payload["kind"] != "wiki_page" {
		t.Fatalf("payload identity = %+v", payload)
	}
	for _, key := range []string{"schema_version", "prompt_version", "created_at", "updated_at", "sources", "unversioned"} {
		if payload[key] == "" {
			t.Fatalf("payload missing %s: %+v", key, payload)
		}
	}
	if !strings.Contains(payload["content"], "Updated Phase07F metadata marker.") {
		t.Fatalf("payload content did not use updated page: %q", payload["content"])
	}
}

func TestQdrantRepositoryReindexWikiPageDeletesMissingPagePoints(t *testing.T) {
	wikiDir := t.TempDir()
	var deleteBody struct {
		Points []string `json:"points"`
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/collections/aura_memory_v1/points/delete" {
			t.Fatalf("unexpected qdrant request: %s %s", r.Method, r.URL.String())
		}
		if r.URL.Query().Get("wait") != "true" {
			t.Fatalf("wait query = %q, want true", r.URL.RawQuery)
		}
		if err := json.NewDecoder(r.Body).Decode(&deleteBody); err != nil {
			t.Fatalf("decode delete body: %v", err)
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	repo, err := NewQdrantRepository(QdrantConfig{
		BaseURL:    server.URL,
		Collection: "aura_memory_v1",
	}, keywordEmbedding, wikiDir, slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err != nil {
		t.Fatalf("NewQdrantRepository: %v", err)
	}
	if err := repo.ReindexWikiPage(context.Background(), "deleted-page"); err != nil {
		t.Fatalf("ReindexWikiPage: %v", err)
	}
	want := []string{qdrantPointID("deleted-page"), qdrantPointID("graph:node:deleted-page")}
	if !slices.Equal(deleteBody.Points, want) {
		t.Fatalf("delete points = %v, want %v", deleteBody.Points, want)
	}
}

func TestRebuildQdrantWikiDocumentsColdPath(t *testing.T) {
	wikiDir := t.TempDir()
	writeTestMDPage(t, wikiDir, &wiki.Page{
		Title:         "Alpha Contract",
		Body:          "Core contract notes.",
		Category:      "project",
		SchemaVersion: wiki.CurrentSchemaVersion,
		PromptVersion: "v1",
		CreatedAt:     time.Now().UTC().Format(time.RFC3339),
		UpdatedAt:     time.Now().UTC().Format(time.RFC3339),
	})

	var sawDelete, sawCreate, sawUpsert, sawCollectionInfo bool
	embedCalls := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("api-key") != "secret" {
			t.Fatalf("missing api-key header")
		}
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/readyz":
			_, _ = w.Write([]byte("ready"))
		case r.Method == http.MethodGet && r.URL.Path == "/collections/aura_memory_v1":
			sawCollectionInfo = true
			w.WriteHeader(http.StatusOK)
			_, _ = io.WriteString(w, `{"result":{"status":"green","points_count":0,"indexed_vectors_count":0}}`)
		case r.Method == http.MethodDelete && r.URL.Path == "/collections/aura_memory_v1":
			sawDelete = true
			w.WriteHeader(http.StatusOK)
		case r.Method == http.MethodPut && r.URL.Path == "/collections/aura_memory_v1":
			sawCreate = true
			w.WriteHeader(http.StatusOK)
		case r.Method == http.MethodPut && r.URL.Path == "/collections/aura_memory_v1/points":
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

	_, err := RebuildQdrantWikiDocuments(context.Background(), wikiDir, countingEmbed, QdrantConfig{
		BaseURL:    server.URL,
		Collection: "aura_memory_v1",
		APIKey:     "secret",
	}, slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err != nil {
		t.Fatalf("RebuildQdrantWikiDocuments: %v", err)
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

func TestRebuildQdrantWikiDocumentsCollectionNotFound(t *testing.T) {
	wikiDir := t.TempDir()
	writeTestMDPage(t, wikiDir, &wiki.Page{
		Title:         "Alpha Contract",
		Body:          "Core contract notes.",
		Category:      "project",
		SchemaVersion: wiki.CurrentSchemaVersion,
		PromptVersion: "v1",
		CreatedAt:     time.Now().UTC().Format(time.RFC3339),
		UpdatedAt:     time.Now().UTC().Format(time.RFC3339),
	})

	var sawDelete, sawCreate, sawUpsert, sawCollectionInfo bool
	embedCalls := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("api-key") != "secret" {
			t.Fatalf("missing api-key header")
		}
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/readyz":
			_, _ = w.Write([]byte("ready"))
		case r.Method == http.MethodGet && r.URL.Path == "/collections/aura_memory_v1":
			sawCollectionInfo = true
			w.WriteHeader(http.StatusNotFound)
		case r.Method == http.MethodDelete && r.URL.Path == "/collections/aura_memory_v1":
			sawDelete = true
			w.WriteHeader(http.StatusOK)
		case r.Method == http.MethodPut && r.URL.Path == "/collections/aura_memory_v1":
			sawCreate = true
			w.WriteHeader(http.StatusOK)
		case r.Method == http.MethodPut && r.URL.Path == "/collections/aura_memory_v1/points":
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

	_, err := RebuildQdrantWikiDocuments(context.Background(), wikiDir, countingEmbed, QdrantConfig{
		BaseURL:    server.URL,
		Collection: "aura_memory_v1",
		APIKey:     "secret",
	}, slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err != nil {
		t.Fatalf("RebuildQdrantWikiDocuments: %v", err)
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

func TestRebuildQdrantWikiDocumentsCollectionInfoErrorFallsBack(t *testing.T) {
	wikiDir := t.TempDir()
	writeTestMDPage(t, wikiDir, &wiki.Page{
		Title:         "Alpha Contract",
		Body:          "Core contract notes.",
		Category:      "project",
		SchemaVersion: wiki.CurrentSchemaVersion,
		PromptVersion: "v1",
		CreatedAt:     time.Now().UTC().Format(time.RFC3339),
		UpdatedAt:     time.Now().UTC().Format(time.RFC3339),
	})

	var sawDelete, sawCreate, sawUpsert, sawCollectionInfo bool
	embedCalls := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("api-key") != "secret" {
			t.Fatalf("missing api-key header")
		}
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/readyz":
			_, _ = w.Write([]byte("ready"))
		case r.Method == http.MethodGet && r.URL.Path == "/collections/aura_memory_v1":
			sawCollectionInfo = true
			w.WriteHeader(http.StatusInternalServerError)
			_, _ = io.WriteString(w, `{"status":{"error":"internal"}}`)
		case r.Method == http.MethodDelete && r.URL.Path == "/collections/aura_memory_v1":
			sawDelete = true
			w.WriteHeader(http.StatusOK)
		case r.Method == http.MethodPut && r.URL.Path == "/collections/aura_memory_v1":
			sawCreate = true
			w.WriteHeader(http.StatusOK)
		case r.Method == http.MethodPut && r.URL.Path == "/collections/aura_memory_v1/points":
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

	_, err := RebuildQdrantWikiDocuments(context.Background(), wikiDir, countingEmbed, QdrantConfig{
		BaseURL:    server.URL,
		Collection: "aura_memory_v1",
		APIKey:     "secret",
	}, slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err != nil {
		t.Fatalf("RebuildQdrantWikiDocuments error: %v", err)
	}
	if !sawCollectionInfo {
		t.Fatal("CollectionInfo was not called")
	}
	if !sawDelete {
		t.Fatal("DeleteCollection was not called on fallback path")
	}
	if !sawCreate {
		t.Fatal("CreateCollection was not called on fallback path")
	}
	if !sawUpsert {
		t.Fatal("Upsert was not called on fallback path")
	}
	if embedCalls == 0 {
		t.Fatal("embed was not called on fallback path")
	}
}

func TestQdrantPointIDIsStableUUID(t *testing.T) {
	first := qdrantPointID("graph:node:alpha-contract")
	second := qdrantPointID("graph:node:alpha-contract")
	other := qdrantPointID("alpha-contract")
	if first != second {
		t.Fatalf("point id not stable: %q != %q", first, second)
	}
	if first == other {
		t.Fatalf("distinct doc ids produced same point id: %q", first)
	}
	if len(first) != 36 || strings.Count(first, "-") != 4 {
		t.Fatalf("point id = %q, want UUID string", first)
	}
}

func TestQdrantSearcherSearchQueriesPointsAndMapsPayload(t *testing.T) {
	var queryBody struct {
		Query       []float32 `json:"query"`
		Limit       int       `json:"limit"`
		WithPayload bool      `json:"with_payload"`
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("api-key") != "secret" {
			t.Fatalf("missing api-key header")
		}
		if r.Method != http.MethodPost || r.URL.Path != "/collections/aura_memory_v1/points/query" {
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
					"id": "7f1d5a80-2b6b-5c40-a310-6cab32bc7d4f",
					"score": 0.87,
					"payload": {
						"doc_id": "alpha-contract",
						"slug": "alpha-contract",
						"title": "Alpha Contract",
						"kind": "wiki_page",
						"content": "Alpha body",
						"schema_version": "3",
						"prompt_version": "ingest_v2",
						"created_at": "2026-05-01T10:00:00Z",
						"updated_at": "2026-05-02T11:30:00Z",
						"sources": "src_qdrant_phase07f,https://example.test/ref",
						"unversioned": "true"
					}
				}]
			}
		}`))
	}))
	defer server.Close()

	searcher, err := NewQdrantSearcher(QdrantConfig{
		BaseURL:    server.URL,
		Collection: "aura_memory_v1",
		APIKey:     "secret",
	}, keywordEmbedding)
	if err != nil {
		t.Fatalf("NewQdrantSearcher: %v", err)
	}
	results, err := searcher.Search(context.Background(), "alpha project", 3)
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
	if got.Kind != "wiki_page" || got.Slug != "alpha-contract" || got.Title != "Alpha Contract" || got.Content != "Alpha body" || got.Score != 0.87 {
		t.Fatalf("mapped result = %+v", got)
	}
	if got.SchemaVersion != wiki.CurrentSchemaVersion || got.PromptVersion != "ingest_v2" || !got.Unversioned {
		t.Fatalf("mapped frontmatter metadata = %+v", got)
	}
	wantCreated, _ := time.Parse(time.RFC3339, "2026-05-01T10:00:00Z")
	if !got.CreatedAt.Equal(wantCreated) {
		t.Fatalf("created_at = %s, want %s", got.CreatedAt.Format(time.RFC3339), wantCreated.Format(time.RFC3339))
	}
	if !slices.Equal(got.Sources, []string{"src_qdrant_phase07f", "https://example.test/ref"}) {
		t.Fatalf("sources = %v", got.Sources)
	}
}

func TestQdrantRepositoryReturnsQueryError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/collections/aura_memory_v1/points/query" {
			t.Fatalf("unexpected request: %s %s", r.Method, r.URL.String())
		}
		http.Error(w, "collection missing", http.StatusNotFound)
	}))
	defer server.Close()

	repo, err := NewQdrantRepository(QdrantConfig{
		BaseURL:    server.URL,
		Collection: "aura_memory_v1",
	}, keywordEmbedding, t.TempDir(), slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err != nil {
		t.Fatalf("NewQdrantRepository: %v", err)
	}
	if _, err := repo.Search(context.Background(), "alpha", 5); err == nil {
		t.Fatal("Search returned nil error")
	}
}

func TestQdrantRepositoryReturnsEmptyResults(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/collections/aura_memory_v1/points/query" {
			t.Fatalf("unexpected request: %s %s", r.Method, r.URL.String())
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"status":"ok","result":{"points":[]}}`))
	}))
	defer server.Close()

	repo, err := NewQdrantRepository(QdrantConfig{
		BaseURL:    server.URL,
		Collection: "aura_memory_v1",
	}, keywordEmbedding, t.TempDir(), slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err != nil {
		t.Fatalf("NewQdrantRepository: %v", err)
	}
	results, err := repo.Search(context.Background(), "alpha", 5)
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if len(results) != 0 {
		t.Fatalf("results = %+v", results)
	}
}

func TestQdrantRepositoryReturnsPrimaryResults(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/collections/aura_memory_v1/points/query" {
			t.Fatalf("unexpected request: %s %s", r.Method, r.URL.String())
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
			"status": "ok",
			"result": {
				"points": [{
					"id": "7f1d5a80-2b6b-5c40-a310-6cab32bc7d4f",
					"score": 0.77,
					"payload": {
						"doc_id": "vector-only",
						"slug": "vector-only",
						"title": "Vector Only",
						"kind": "wiki_page",
						"content": "qdrant result"
					}
				}]
			}
		}`))
	}))
	defer server.Close()

	repo, err := NewQdrantRepository(QdrantConfig{
		BaseURL:    server.URL,
		Collection: "aura_memory_v1",
	}, keywordEmbedding, t.TempDir(), slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err != nil {
		t.Fatalf("NewQdrantRepository: %v", err)
	}
	results, err := repo.Search(context.Background(), "alpha", 5)
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if len(results) != 1 || results[0].Slug != "vector-only" {
		t.Fatalf("results = %+v", results)
	}
}

// TestRebuildQdrantWikiDocumentsSkipOnEmbedError verifies that when an embed call
// fails for a specific doc, the rebuild skips that doc and continues indexing the
// rest — returning nil error as long as at least one doc was indexed.
func TestRebuildQdrantWikiDocumentsSkipOnEmbedError(t *testing.T) {
	wikiDir := t.TempDir()
	writeTestMDPage(t, wikiDir, &wiki.Page{
		Title:         "Alpha Success",
		Body:          "Normal content that embeds fine.",
		Category:      "project",
		SchemaVersion: wiki.CurrentSchemaVersion,
		PromptVersion: "v1",
		CreatedAt:     time.Now().UTC().Format(time.RFC3339),
		UpdatedAt:     time.Now().UTC().Format(time.RFC3339),
	})
	writeTestMDPage(t, wikiDir, &wiki.Page{
		Title:         "Beta Fail",
		Body:          "ZZZMUSTFAIL context too large for embed sidecar",
		Category:      "project",
		SchemaVersion: wiki.CurrentSchemaVersion,
		PromptVersion: "v1",
		CreatedAt:     time.Now().UTC().Format(time.RFC3339),
		UpdatedAt:     time.Now().UTC().Format(time.RFC3339),
	})

	// failingEmbed returns an error for any content containing the sentinel token.
	// This covers: wiki_page:beta-fail (body has token) and graph:node:beta-fail
	// (Summary line includes body excerpt). The graph index docs contain only slug+title,
	// so they do NOT contain the token and embed fine.
	failingEmbed := func(_ context.Context, text string) ([]float32, error) {
		if strings.Contains(text, "ZZZMUSTFAIL") {
			return nil, fmt.Errorf("input is larger than max context size (1024)")
		}
		return keywordEmbedding(context.Background(), text)
	}

	var upsertedCount int
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/readyz":
			_, _ = w.Write([]byte("ready"))
		case r.Method == http.MethodGet && r.URL.Path == "/collections/aura_memory_v1":
			w.WriteHeader(http.StatusNotFound)
		case r.Method == http.MethodDelete && r.URL.Path == "/collections/aura_memory_v1":
			w.WriteHeader(http.StatusOK)
		case r.Method == http.MethodPut && r.URL.Path == "/collections/aura_memory_v1":
			w.WriteHeader(http.StatusOK)
		case r.Method == http.MethodPut && r.URL.Path == "/collections/aura_memory_v1/points":
			var body struct {
				Points []json.RawMessage `json:"points"`
			}
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				t.Fatalf("decode upsert body: %v", err)
			}
			upsertedCount += len(body.Points)
			w.WriteHeader(http.StatusOK)
		default:
			t.Fatalf("unexpected qdrant request: %s %s", r.Method, r.URL.String())
		}
	}))
	defer server.Close()

	report, err := RebuildQdrantWikiDocuments(context.Background(), wikiDir, failingEmbed, QdrantConfig{
		BaseURL:    server.URL,
		Collection: "aura_memory_v1",
		BatchSize:  100,
	}, slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err != nil {
		t.Fatalf("RebuildQdrantWikiDocuments returned error for partial embed failure: %v", err)
	}

	// 2 pages × (1 wiki_page + 1 graph:node) + 1 graph:index:category:project + 1 graph:index:all = 6 total docs.
	// beta-fail wiki_page + graph:node:beta-fail both contain the sentinel → 2 skipped.
	if len(report.SkippedDocs) != 2 {
		t.Fatalf("skipped docs count = %d, want 2; skipped = %v", len(report.SkippedDocs), report.SkippedDocs)
	}
	skippedIDs := make(map[string]string, len(report.SkippedDocs))
	for _, s := range report.SkippedDocs {
		skippedIDs[s.DocID] = s.Reason
	}
	if _, ok := skippedIDs["beta-fail"]; !ok {
		t.Fatalf("expected beta-fail in skipped docs; got %v", report.SkippedDocs)
	}
	if _, ok := skippedIDs["graph:node:beta-fail"]; !ok {
		t.Fatalf("expected graph:node:beta-fail in skipped docs; got %v", report.SkippedDocs)
	}
	for id, reason := range skippedIDs {
		if reason == "" {
			t.Fatalf("skipped doc %q has empty reason", id)
		}
	}

	// 4 docs succeed: wiki_page:alpha-success, graph:node:alpha-success, graph:index:category:project, graph:index:all.
	if report.DocsIndexed != 4 {
		t.Fatalf("docs indexed = %d, want 4 (alpha wiki+graph + 2 index docs)", report.DocsIndexed)
	}
	if upsertedCount != 4 {
		t.Fatalf("upserted points count = %d, want 4", upsertedCount)
	}
}

// TestRebuildQdrantWikiDocumentsDimMismatchAutoRebuild verifies that when the
// existing collection reports a different vector size than cfg.OutputDim, the
// warm-cache is bypassed and a full rebuild is triggered.
func TestRebuildQdrantWikiDocumentsDimMismatchAutoRebuild(t *testing.T) {
	wikiDir := t.TempDir()
	writeTestMDPage(t, wikiDir, &wiki.Page{
		Title:         "Page One",
		Body:          "Content for dim mismatch test.",
		Category:      "project",
		SchemaVersion: wiki.CurrentSchemaVersion,
		PromptVersion: "v1",
		CreatedAt:     time.Now().UTC().Format(time.RFC3339),
		UpdatedAt:     time.Now().UTC().Format(time.RFC3339),
	})

	var sawDelete, sawCreate, sawUpsert bool
	embedCalls := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/readyz":
			_, _ = w.Write([]byte("ready"))
		case r.Method == http.MethodGet && r.URL.Path == "/collections/aura_memory_v1":
			// Return existing collection with VectorSize=128, but cfg.OutputDim=5.
			w.WriteHeader(http.StatusOK)
			_, _ = io.WriteString(w, `{"result":{"status":"green","points_count":10,"indexed_vectors_count":10,"config":{"params":{"vectors":{"size":128,"distance":"Cosine"}}}}}`)
		case r.Method == http.MethodDelete && r.URL.Path == "/collections/aura_memory_v1":
			sawDelete = true
			w.WriteHeader(http.StatusOK)
		case r.Method == http.MethodPut && r.URL.Path == "/collections/aura_memory_v1":
			sawCreate = true
			w.WriteHeader(http.StatusOK)
		case r.Method == http.MethodPut && r.URL.Path == "/collections/aura_memory_v1/points":
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

	// cfg.OutputDim=5; server returns existing VectorSize=128 → mismatch → full rebuild.
	report, err := RebuildQdrantWikiDocuments(context.Background(), wikiDir, countingEmbed, QdrantConfig{
		BaseURL:    server.URL,
		Collection: "aura_memory_v1",
		OutputDim:  5, // keywordEmbedding returns 5-dim vectors
	}, slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err != nil {
		t.Fatalf("RebuildQdrantWikiDocuments: %v", err)
	}
	if !sawDelete {
		t.Fatal("expected DeleteCollection to be called on dim mismatch")
	}
	if !sawCreate {
		t.Fatal("expected CreateCollection to be called on dim mismatch")
	}
	if !sawUpsert {
		t.Fatal("expected Upsert to be called on dim mismatch")
	}
	if embedCalls == 0 {
		t.Fatal("expected embed calls on dim-mismatch rebuild")
	}
	if report.PriorVectorSize != 128 {
		t.Fatalf("PriorVectorSize = %d, want 128", report.PriorVectorSize)
	}
}

// TestRebuildQdrantWikiDocumentsDimMismatchSkippedByEnv verifies that when
// cfg.SkipDimMismatchRebuild is true, the warm-cache is still used despite the
// dimension mismatch.
func TestRebuildQdrantWikiDocumentsDimMismatchSkippedByEnv(t *testing.T) {
	wikiDir := t.TempDir()
	writeTestMDPage(t, wikiDir, &wiki.Page{
		Title:         "Page One",
		Body:          "Content for skip-env test.",
		Category:      "project",
		SchemaVersion: wiki.CurrentSchemaVersion,
		PromptVersion: "v1",
		CreatedAt:     time.Now().UTC().Format(time.RFC3339),
		UpdatedAt:     time.Now().UTC().Format(time.RFC3339),
	})

	var sawDelete bool
	embedCalls := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/readyz":
			_, _ = w.Write([]byte("ready"))
		case r.Method == http.MethodGet && r.URL.Path == "/collections/aura_memory_v1":
			w.WriteHeader(http.StatusOK)
			_, _ = io.WriteString(w, `{"result":{"status":"green","points_count":10,"indexed_vectors_count":10,"config":{"params":{"vectors":{"size":128,"distance":"Cosine"}}}}}`)
		case r.Method == http.MethodDelete && r.URL.Path == "/collections/aura_memory_v1":
			sawDelete = true
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

	// SkipDimMismatchRebuild=true → warm-cache hit despite VectorSize=128 vs OutputDim=5.
	report, err := RebuildQdrantWikiDocuments(context.Background(), wikiDir, countingEmbed, QdrantConfig{
		BaseURL:                server.URL,
		Collection:             "aura_memory_v1",
		OutputDim:              5,
		SkipDimMismatchRebuild: true,
	}, slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err != nil {
		t.Fatalf("RebuildQdrantWikiDocuments: %v", err)
	}
	if sawDelete {
		t.Fatal("expected NO DeleteCollection when SkipDimMismatchRebuild=true")
	}
	if embedCalls != 0 {
		t.Fatalf("embed called %d times on skipped-env warm-cache, want 0", embedCalls)
	}
	if report.PriorVectorSize != 128 {
		t.Fatalf("PriorVectorSize = %d, want 128", report.PriorVectorSize)
	}
}
