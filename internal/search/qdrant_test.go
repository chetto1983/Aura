package search

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
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
		SchemaVersion: wiki.CurrentSchemaVersion,
		PromptVersion: "v1",
		CreatedAt:     time.Now().UTC().Format(time.RFC3339),
		UpdatedAt:     time.Now().UTC().Format(time.RFC3339),
	})
	writeTestMDPage(t, wikiDir, &wiki.Page{
		Title:         "Beta Review",
		Body:          "Review links to [[alpha-contract]].",
		Category:      "project",
		Related:       []string{"alpha-contract"},
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
