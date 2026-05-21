package search

import (
	"context"
	"database/sql"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	auradb "github.com/aura/aura/internal/db"
	"github.com/aura/aura/internal/wiki"
)

// hybridTestSetup returns a populated *sql.DB (in tmpdir) and a wiki dir.
func hybridTestSetup(t *testing.T, pages []*wiki.Page) (wikiDir string, db *sql.DB) {
	t.Helper()
	wikiDir = t.TempDir()
	dbDir := t.TempDir()
	for _, p := range pages {
		writeTestMDPage(t, wikiDir, p)
	}
	dbPath := filepath.Join(dbDir, "aura.db")
	if _, _, err := RebuildSQLiteWikiDocuments(context.Background(), wikiDir, dbPath, slog.New(slog.NewTextHandler(io.Discard, nil))); err != nil {
		t.Fatalf("RebuildSQLiteWikiDocuments: %v", err)
	}
	var err error
	db, err = auradb.Open(dbPath)
	if err != nil {
		t.Fatalf("open hybrid test db: %v", err)
	}
	t.Cleanup(func() { db.Close() })
	return wikiDir, db
}

// hybridQdrantServer builds a fake Qdrant HTTP server that returns the given
// scored payload slices for any /points/query POST. Also handles /readyz and
// /collections/* for the warm-cache check.
func hybridQdrantServer(t *testing.T, points []map[string]interface{}) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/readyz":
			_, _ = w.Write([]byte("ready"))
		case strings.Contains(r.URL.Path, "/points/query"):
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]interface{}{
				"status": "ok",
				"result": map[string]interface{}{"points": points},
			})
		case strings.Contains(r.URL.Path, "/collections/"):
			// Return >0 points so warm-cache skips rebuild; no embed calls needed.
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"status":"ok","result":{"points_count":2}}`))
		default:
			t.Logf("hybrid test: unhandled qdrant request %s %s", r.Method, r.URL.Path)
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	t.Cleanup(srv.Close)
	return srv
}

// TestSearchHybrid_NoisePageDownranked ensures a page with high vector
// similarity but no query token in its title is outranked by a page whose
// title literally contains the query term.
func TestSearchHybrid_NoisePageDownranked(t *testing.T) {
	now := time.Now().UTC().Format(time.RFC3339)

	legitimate := &wiki.Page{
		Title:         "Delta Automazioni SRL",
		Body:          "Cliente 615827. Referente: rossi@delta.it.",
		Category:      "client",
		SchemaVersion: wiki.CurrentSchemaVersion,
		PromptVersion: "v1",
		CreatedAt:     now,
		UpdatedAt:     now,
	}
	noise := &wiki.Page{
		Title:         "Prova Wiki Test Page",
		Body:          "Contenuto generico di test per verifica motore.",
		Category:      "test",
		SchemaVersion: wiki.CurrentSchemaVersion,
		PromptVersion: "v1",
		CreatedAt:     now,
		UpdatedAt:     now,
	}

	wikiDir, db := hybridTestSetup(t, []*wiki.Page{legitimate, noise})
	_ = wikiDir

	legitimateSlug := wiki.Slug(legitimate.Title)
	noiseSlug := wiki.Slug(noise.Title)

	// Qdrant intentionally returns noise page first (higher cosine) to simulate
	// the pure-vector failure mode. Hybrid fusion must correct this.
	qdrantPoints := []map[string]interface{}{
		{
			"id":    qdrantPointID(noiseSlug),
			"score": 0.92,
			"payload": map[string]string{
				"slug":    noiseSlug,
				"title":   noise.Title,
				"content": noise.Title + "\n" + noise.Body,
				"kind":    "wiki_page",
			},
		},
		{
			"id":    qdrantPointID(legitimateSlug),
			"score": 0.71,
			"payload": map[string]string{
				"slug":    legitimateSlug,
				"title":   legitimate.Title,
				"content": legitimate.Title + "\n" + legitimate.Body,
				"kind":    "wiki_page",
			},
		},
	}

	srv := hybridQdrantServer(t, qdrantPoints)
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))

	repo, err := NewQdrantRepositoryWithDB(QdrantConfig{
		BaseURL:    srv.URL,
		Collection: "aura_hybrid_test",
	}, keywordEmbedding, wikiDir, db, logger)
	if err != nil {
		t.Fatalf("NewQdrantRepositoryWithDB: %v", err)
	}

	results, err := repo.SearchHybrid(context.Background(), "Delta Automazioni", 5)
	if err != nil {
		t.Fatalf("SearchHybrid: %v", err)
	}
	if len(results) == 0 {
		t.Fatal("SearchHybrid returned no results")
	}

	legitRank, noiseRank := -1, -1
	for i, r := range results {
		switch r.Slug {
		case legitimateSlug:
			legitRank = i
		case noiseSlug:
			noiseRank = i
		}
	}
	if legitRank == -1 {
		t.Fatalf("legitimate page %q not in results: %+v", legitimateSlug, results)
	}
	if noiseRank != -1 && legitRank > noiseRank {
		t.Errorf("noise page at rank %d outranks legitimate page at rank %d — hybrid fusion failed; results: %+v",
			noiseRank+1, legitRank+1, results)
	}
}

// TestSearchHybrid_DiacriticInsensitiveMatch verifies that a query without
// accents matches a title that contains accented characters.
func TestSearchHybrid_DiacriticInsensitiveMatch(t *testing.T) {
	now := time.Now().UTC().Format(time.RFC3339)

	accentedPage := &wiki.Page{
		Title:         "Società Formazione Genova",
		Body:          "Ente di formazione professionale con sede a Genova.",
		Category:      "client",
		SchemaVersion: wiki.CurrentSchemaVersion,
		PromptVersion: "v1",
		CreatedAt:     now,
		UpdatedAt:     now,
	}

	_, db := hybridTestSetup(t, []*wiki.Page{accentedPage})

	slug := wiki.Slug(accentedPage.Title)
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))

	// Query without the grave accent — diacritic strip must match.
	exactResults := exactMatchDB(context.Background(), db, "societa formazione", 10, logger)
	found := false
	for _, r := range exactResults {
		if r.Slug == slug {
			found = true
		}
	}
	if !found {
		t.Errorf("diacritic-insensitive exact match did not return slug %q; exactResults: %+v", slug, exactResults)
	}
}

// TestSearchHybrid_FallbackWhenNoDatabase ensures SearchHybrid returns
// vector-only results when the repository has no SQLite DB wired.
func TestSearchHybrid_FallbackWhenNoDatabase(t *testing.T) {
	qdrantPoints := []map[string]interface{}{
		{
			"id":    qdrantPointID("alpha-page"),
			"score": 0.88,
			"payload": map[string]string{
				"slug":    "alpha-page",
				"title":   "Alpha Page",
				"content": "Alpha Page\nalpha content",
				"kind":    "wiki_page",
			},
		},
	}
	srv := hybridQdrantServer(t, qdrantPoints)
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))

	// Build repository WITHOUT db (via old constructor).
	repo, err := NewQdrantRepository(QdrantConfig{
		BaseURL:    srv.URL,
		Collection: "aura_hybrid_test",
	}, keywordEmbedding, t.TempDir(), logger)
	if err != nil {
		t.Fatalf("NewQdrantRepository: %v", err)
	}

	// The repo doesn't implement HybridSearcher, so memory_search.go falls back.
	// But we can cast to *qdrantRepository (same package) and call SearchHybrid directly.
	qrepo := repo.(*qdrantRepository)
	results, err := qrepo.SearchHybrid(context.Background(), "alpha", 5)
	if err != nil {
		t.Fatalf("SearchHybrid fallback: %v", err)
	}
	if len(results) != 1 || results[0].Slug != "alpha-page" {
		t.Errorf("fallback results = %+v, want alpha-page", results)
	}
}
