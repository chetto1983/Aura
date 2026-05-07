package main

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/aura/aura/internal/search"
	"github.com/aura/aura/internal/wiki"
)

func TestLoadDotEnvForDebugQdrant(t *testing.T) {
	path := filepath.Join(t.TempDir(), ".env")
	if err := os.WriteFile(path, []byte("QDRANT_URL=http://qdrant:6333\nQDRANT_COLLECTION='aura_memory_v1'\n"), 0o644); err != nil {
		t.Fatalf("write env: %v", err)
	}
	t.Setenv("QDRANT_URL", "")
	t.Setenv("QDRANT_COLLECTION", "")
	if err := loadDotEnv(path); err != nil {
		t.Fatalf("loadDotEnv: %v", err)
	}
	if os.Getenv("QDRANT_URL") != "http://qdrant:6333" {
		t.Fatalf("QDRANT_URL = %q", os.Getenv("QDRANT_URL"))
	}
	if os.Getenv("QDRANT_COLLECTION") != "aura_memory_v1" {
		t.Fatalf("QDRANT_COLLECTION = %q", os.Getenv("QDRANT_COLLECTION"))
	}
}

func TestRunQuerySmokeComparesQdrantAndLocalChromem(t *testing.T) {
	wikiDir := t.TempDir()
	writeQuerySmokePage(t, wikiDir, &wiki.Page{
		Title:         "Alpha Contract",
		Body:          "Alpha body with project contract notes.",
		Category:      "project",
		SchemaVersion: wiki.CurrentSchemaVersion,
		PromptVersion: "v1",
		CreatedAt:     time.Now().UTC().Format(time.RFC3339),
		UpdatedAt:     time.Now().UTC().Format(time.RFC3339),
	})

	var queryBody struct {
		Query       []float32 `json:"query"`
		Limit       int       `json:"limit"`
		WithPayload bool      `json:"with_payload"`
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/collections/aura_memory_v1/points/query" {
			t.Fatalf("unexpected request: %s %s", r.Method, r.URL.String())
		}
		if err := json.NewDecoder(r.Body).Decode(&queryBody); err != nil {
			t.Fatalf("decode query: %v", err)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
			"status": "ok",
			"result": {
				"points": [{
					"score": 0.91,
					"payload": {
						"doc_id": "alpha-contract",
						"slug": "alpha-contract",
						"title": "Alpha Contract",
						"kind": "wiki_page",
						"content": "Alpha body"
					}
				}]
			}
		}`))
	}))
	defer server.Close()

	report, err := runQuerySmoke(context.Background(), querySmokeConfig{
		Query:   "alpha project",
		TopK:    2,
		Compare: true,
		WikiDir: wikiDir,
		Qdrant: search.QdrantConfig{
			BaseURL:    server.URL,
			Collection: "aura_memory_v1",
		},
		EmbedFn: querySmokeEmbedding,
		Logger:  slog.New(slog.NewTextHandler(io.Discard, nil)),
	})
	if err != nil {
		t.Fatalf("runQuerySmoke: %v", err)
	}
	if len(queryBody.Query) != 5 || queryBody.Limit != 2 || !queryBody.WithPayload {
		t.Fatalf("query body = %+v", queryBody)
	}
	if !report.Qdrant.OK || report.Qdrant.Count != 1 || report.Qdrant.Results[0].Slug != "alpha-contract" {
		t.Fatalf("qdrant report = %+v", report.Qdrant)
	}
	if report.Local == nil || !report.Local.OK || report.Local.Count == 0 {
		t.Fatalf("local report = %+v", report.Local)
	}
	if report.Recommendation == "" {
		t.Fatal("recommendation should be populated")
	}
}

func TestQueryRecommendationDetectsStaleQdrantIndex(t *testing.T) {
	report := queryReport{
		Qdrant: queryProbe{OK: true, Count: 0},
		Local:  &queryProbe{OK: true, Count: 3},
	}
	if got := queryRecommendation(report); got != "rebuild_qdrant" {
		t.Fatalf("recommendation = %q, want rebuild_qdrant", got)
	}
}

func writeQuerySmokePage(t *testing.T, dir string, page *wiki.Page) {
	t.Helper()
	data, err := wiki.MarshalMD(page)
	if err != nil {
		t.Fatalf("MarshalMD: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, wiki.Slug(page.Title)+".md"), data, 0o644); err != nil {
		t.Fatalf("write page: %v", err)
	}
}

func querySmokeEmbedding(_ context.Context, text string) ([]float32, error) {
	keywords := []string{"alpha", "project", "contract", "body"}
	vector := make([]float32, len(keywords)+1)
	for i, keyword := range keywords {
		if containsFold(text, keyword) {
			vector[i] = 1
		}
	}
	allZero := true
	for _, v := range vector {
		if v != 0 {
			allZero = false
			break
		}
	}
	if allZero {
		vector[len(vector)-1] = 1
	}
	return vector, nil
}

func containsFold(text, needle string) bool {
	return strings.Contains(strings.ToLower(text), strings.ToLower(needle))
}
