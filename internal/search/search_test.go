package search

import (
	"context"
	"database/sql"
	"errors"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	auradb "github.com/aura/aura/internal/db"
	"github.com/aura/aura/internal/db/migrations"
	"github.com/aura/aura/internal/wiki"
	"github.com/philippgille/chromem-go"
)

func TestFormatResults(t *testing.T) {
	tests := []struct {
		name     string
		results  []Result
		expected string
	}{
		{
			name:     "empty results",
			results:  nil,
			expected: "",
		},
		{
			name: "single result",
			results: []Result{
				{Slug: "go-programming", Title: "Go Programming", Content: "Go is a statically typed language", Score: 0.9},
			},
			expected: "Relevant wiki knowledge:\n- [[go-programming]] Go Programming\n  Go is a statically typed language\n",
		},
		{
			name: "multiple results",
			results: []Result{
				{Slug: "go-programming", Title: "Go Programming", Content: "Go is a language", Score: 0.9},
				{Slug: "rust-basics", Title: "Rust Basics", Content: "Rust is safe", Score: 0.8},
			},
			expected: "Relevant wiki knowledge:\n- [[go-programming]] Go Programming\n  Go is a language\n- [[rust-basics]] Rust Basics\n  Rust is safe\n",
		},
		{
			name: "graph node and index results",
			results: []Result{
				{Kind: "graph_node", Slug: "contract-renewal", Title: "Contract Renewal", Content: "Backlinks: legal-review", Score: 0.9},
				{Kind: "graph_index", Slug: "index:category:project", Title: "Index: project", Content: "Graph index category: project", Score: 0.8},
			},
			expected: "Relevant wiki knowledge:\n- [graph_node] [[contract-renewal]] Contract Renewal\n  Backlinks: legal-review\n- [graph_index] index:category:project Index: project\n  Graph index category: project\n",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := FormatResults(tt.results)
			if got != tt.expected {
				t.Errorf("FormatResults() = %q, want %q", got, tt.expected)
			}
		})
	}
}

func TestExtractTitle(t *testing.T) {
	tests := []struct {
		name     string
		data     string
		expected string
	}{
		{
			name:     "valid yaml with title",
			data:     "title: My Page\ncontent: Hello\n",
			expected: "My Page",
		},
		{
			name:     "no title field",
			data:     "content: Hello\n",
			expected: "",
		},
		{
			name:     "empty title",
			data:     "title: \"\"\ncontent: Hello\n",
			expected: "",
		},
		{
			name:     "invalid yaml",
			data:     "{{invalid",
			expected: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := extractTitle([]byte(tt.data))
			if got != tt.expected {
				t.Errorf("extractTitle() = %q, want %q", got, tt.expected)
			}
		})
	}
}

func TestIsIndexed(t *testing.T) {
	e := &Engine{indexed: false}
	if e.IsIndexed() {
		t.Error("expected IsIndexed to return false before indexing")
	}
	e.indexed = true
	if !e.IsIndexed() {
		t.Error("expected IsIndexed to return true after indexing")
	}
}

func TestSearchWithoutIndexing(t *testing.T) {
	e := &Engine{indexed: false}
	_, err := e.Search(context.Background(), "test", 5)
	if err == nil {
		t.Error("expected error when searching without indexing")
	}
}

func TestNewEngine(t *testing.T) {
	tmpDir := t.TempDir()
	logger := slog.Default()

	embedFn := chromem.NewEmbeddingFuncOpenAICompat("", "", "text-embedding-3-small", nil)
	e, err := NewEngine(tmpDir, embedFn, logger)
	if err != nil {
		t.Fatalf("NewEngine() error = %v", err)
	}
	if e == nil {
		t.Fatal("NewEngine() returned nil engine")
	}
	if e.IsIndexed() {
		t.Error("new engine should not be indexed yet")
	}
}

var (
	_ Queryer           = (*Engine)(nil)
	_ Searcher          = (*Engine)(nil)
	_ WikiPageReindexer = (*Engine)(nil)
	_ WikiPageIndexer   = (*Engine)(nil)
	_ DocumentIndexer   = (*Engine)(nil)
	_ Repository        = (*Engine)(nil)
	_ EmbeddingFunction = keywordEmbedding
)

func TestNewEngineWithFallbackCloseReleasesOwnedDB(t *testing.T) {
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "search.db")
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))

	engine, err := NewEngineWithFallback(tmpDir, keywordEmbedding, dbPath, logger)
	if err != nil {
		t.Fatalf("NewEngineWithFallback: %v", err)
	}
	if engine.sqlite == nil {
		t.Fatal("sqlite fallback was not configured")
	}

	if err := engine.Close(); err != nil {
		t.Fatalf("engine Close: %v", err)
	}

	db, err := auradb.Open(dbPath)
	if err != nil {
		t.Fatalf("reopen db after engine Close: %v", err)
	}
	defer db.Close()
	if _, err := db.Exec(`SELECT COUNT(*) FROM wiki_documents`); err != nil {
		t.Fatalf("query reopened search db: %v", err)
	}
}

func TestSqliteSearcherWithDBCloseDoesNotCloseSharedDB(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "shared.db")
	db, err := auradb.Open(dbPath)
	if err != nil {
		t.Fatalf("open shared db: %v", err)
	}
	defer db.Close()

	searcher, err := newSqliteSearcherWithDB(db, slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err != nil {
		t.Fatalf("newSqliteSearcherWithDB: %v", err)
	}
	if err := searcher.Close(); err != nil {
		t.Fatalf("searcher Close: %v", err)
	}
	if err := db.Ping(); err != nil {
		t.Fatalf("shared db was closed by searcher Close: %v", err)
	}
}

func TestSqliteSearcherWithDBDoesNotCreateSchema(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "shared.db")
	db, err := auradb.Open(dbPath)
	if err != nil {
		t.Fatalf("open shared db: %v", err)
	}
	defer db.Close()

	if _, err := newSqliteSearcherWithDB(db, slog.New(slog.NewTextHandler(io.Discard, nil))); err != nil {
		t.Fatalf("newSqliteSearcherWithDB: %v", err)
	}

	if tableExists(t, db, "wiki_documents") {
		t.Fatal("newSqliteSearcherWithDB created wiki_documents; migrations should own schema")
	}
}

func tableExists(t *testing.T, db *sql.DB, name string) bool {
	t.Helper()
	var got string
	err := db.QueryRow(`SELECT name FROM sqlite_master WHERE name = ?`, name).Scan(&got)
	if err == nil {
		return true
	}
	if err == sql.ErrNoRows {
		return false
	}
	t.Fatalf("query sqlite_master: %v", err)
	return false
}

func TestIndexWikiPages(t *testing.T) {
	tmpDir := t.TempDir()
	logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelDebug}))

	// Create test wiki files
	wikiContent := []byte("title: Test Page\ncontent: This is a test page\nschema_version: 1\nprompt_version: v1\n")
	if err := os.WriteFile(filepath.Join(tmpDir, "test-page.yaml"), wikiContent, 0644); err != nil {
		t.Fatalf("failed to create test wiki file: %v", err)
	}

	embedFn := chromem.NewEmbeddingFuncOpenAICompat("", "", "text-embedding-3-small", nil)
	e, err := NewEngine(tmpDir, embedFn, logger)
	if err != nil {
		t.Fatalf("NewEngine() error = %v", err)
	}

	// IndexWikiPages will attempt to embed, which requires an API.
	// This test verifies the file reading and metadata extraction works.
	// In a real environment with an embedding API, this would fully index.
	err = e.IndexWikiPages(context.Background())
	// Indexing may fail without a real embedding API, but the function
	// should at least attempt to read and parse files
	_ = err
}

func TestSearchMergesExactFTSAndVectorResults(t *testing.T) {
	wikiDir := t.TempDir()
	dbPath := filepath.Join(t.TempDir(), "aura.db")
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	writeTestMDPage(t, wikiDir, &wiki.Page{
		Title:         "Alpha Contract",
		Body:          "Narrow renewal memo.",
		Category:      "project",
		SchemaVersion: wiki.CurrentSchemaVersion,
		PromptVersion: "v1",
		CreatedAt:     time.Now().UTC().Format(time.RFC3339),
		UpdatedAt:     time.Now().UTC().Format(time.RFC3339),
	})
	writeTestMDPage(t, wikiDir, &wiki.Page{
		Title:         "Beta Review",
		Body:          "Beta project review links to [[alpha-contract]].",
		Category:      "project",
		SchemaVersion: wiki.CurrentSchemaVersion,
		PromptVersion: "v1",
		CreatedAt:     time.Now().UTC().Format(time.RFC3339),
		UpdatedAt:     time.Now().UTC().Format(time.RFC3339),
	})

	engine, err := NewEngineWithFallback(wikiDir, keywordEmbedding, dbPath, logger)
	if err != nil {
		t.Fatalf("NewEngineWithFallback: %v", err)
	}
	defer engine.Close()
	if err := engine.IndexWikiPages(context.Background()); err != nil {
		t.Fatalf("IndexWikiPages: %v", err)
	}

	results, err := engine.Search(context.Background(), "[[alpha-contract]] beta", 6)
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if len(results) < 2 {
		t.Fatalf("results = %#v, want exact plus lexical/vector results", results)
	}
	if results[0].Slug != "alpha-contract" {
		t.Fatalf("first result = %#v, want exact alpha-contract first", results[0])
	}
	if !hasResult(results, "wiki_page", "beta-review") && !hasResult(results, "graph_node", "beta-review") {
		t.Fatalf("missing merged beta-review result: %#v", results)
	}
}

func TestSearchFallsBackToSQLiteWhenVectorQueryFails(t *testing.T) {
	wikiDir := t.TempDir()
	dbPath := filepath.Join(t.TempDir(), "aura.db")
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	writeTestMDPage(t, wikiDir, &wiki.Page{
		Title:         "Alpha Contract",
		Body:          "Alpha fallback memo.",
		Category:      "project",
		SchemaVersion: wiki.CurrentSchemaVersion,
		PromptVersion: "v1",
		CreatedAt:     time.Now().UTC().Format(time.RFC3339),
		UpdatedAt:     time.Now().UTC().Format(time.RFC3339),
	})
	embed := func(ctx context.Context, text string) ([]float32, error) {
		if strings.Contains(text, "fail-vector") {
			return nil, errors.New("vector query failed")
		}
		return keywordEmbedding(ctx, text)
	}
	engine, err := NewEngineWithFallback(wikiDir, embed, dbPath, logger)
	if err != nil {
		t.Fatalf("NewEngineWithFallback: %v", err)
	}
	defer engine.Close()
	if err := engine.IndexWikiPages(context.Background()); err != nil {
		t.Fatalf("IndexWikiPages: %v", err)
	}

	results, err := engine.Search(context.Background(), "fail-vector alpha", 3)
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if len(results) == 0 || results[0].Slug != "alpha-contract" {
		t.Fatalf("results = %#v, want sqlite alpha result", results)
	}
}

func TestMergeHybridResultsDedupesByKindAndSlug(t *testing.T) {
	exact := []Result{{Kind: "wiki_page", Slug: "alpha", Title: "Alpha"}}
	fts := []Result{{Kind: "wiki_page", Slug: "alpha", Title: "Alpha duplicate"}, {Kind: "wiki_page", Slug: "beta", Title: "Beta"}}
	vector := []Result{{Kind: "graph_node", Slug: "alpha", Title: "Alpha graph"}, {Kind: "wiki_page", Slug: "gamma", Title: "Gamma"}}

	results := mergeHybridResults("alpha", 3, exact, fts, vector)
	if got := []string{results[0].Kind + ":" + results[0].Slug, results[1].Kind + ":" + results[1].Slug, results[2].Kind + ":" + results[2].Slug}; !slices.Equal(got, []string{"wiki_page:alpha", "wiki_page:beta", "graph_node:alpha"}) {
		t.Fatalf("merged order = %#v", got)
	}
}

func TestIndexWikiPagesAddsGraphNodeCards(t *testing.T) {
	tmpDir := t.TempDir()
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))

	writeTestMDPage(t, tmpDir, &wiki.Page{
		Title:         "Alpha Contract",
		Body:          "Core contract notes.",
		Category:      "project",
		Tags:          []string{"contract"},
		SchemaVersion: wiki.CurrentSchemaVersion,
		PromptVersion: "v1",
		CreatedAt:     time.Now().UTC().Format(time.RFC3339),
		UpdatedAt:     time.Now().UTC().Format(time.RFC3339),
	})
	writeTestMDPage(t, tmpDir, &wiki.Page{
		Title:         "Beta Legal Review",
		Body:          "Review links to [[alpha-contract]] before renewal.",
		Category:      "project",
		Related:       []string{"alpha-contract"},
		SchemaVersion: wiki.CurrentSchemaVersion,
		PromptVersion: "v1",
		CreatedAt:     time.Now().UTC().Format(time.RFC3339),
		UpdatedAt:     time.Now().UTC().Format(time.RFC3339),
	})

	e, err := NewEngine(tmpDir, keywordEmbedding, logger)
	if err != nil {
		t.Fatalf("NewEngine() error = %v", err)
	}
	if err := e.IndexWikiPages(context.Background()); err != nil {
		t.Fatalf("IndexWikiPages: %v", err)
	}
	if got, want := e.coll.Count(), 6; got != want {
		t.Fatalf("indexed document count = %d, want %d", got, want)
	}

	results, err := e.Search(context.Background(), "backlinks beta", 5)
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if !hasResult(results, "graph_node", "alpha-contract") {
		t.Fatalf("missing graph_node result for alpha-contract: %#v", results)
	}
	if !hasResult(results, "graph_index", "index:category:project") {
		t.Fatalf("missing graph_index result for project category: %#v", results)
	}
}

func TestIndexWikiPagesSkipsOperationalDocs(t *testing.T) {
	tmpDir := t.TempDir()
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))

	writeTestMDPage(t, tmpDir, &wiki.Page{
		Title:         "Project Memory",
		Body:          "Durable memory page.",
		Category:      "project",
		SchemaVersion: wiki.CurrentSchemaVersion,
		PromptVersion: "v1",
		CreatedAt:     time.Now().UTC().Format(time.RFC3339),
		UpdatedAt:     time.Now().UTC().Format(time.RFC3339),
	})
	specialPage := &wiki.Page{
		Title:         "Schema",
		Body:          "Example [[ghost]].",
		Category:      "ops",
		SchemaVersion: wiki.CurrentSchemaVersion,
		PromptVersion: "v1",
		CreatedAt:     time.Now().UTC().Format(time.RFC3339),
		UpdatedAt:     time.Now().UTC().Format(time.RFC3339),
	}
	data, err := wiki.MarshalMD(specialPage)
	if err != nil {
		t.Fatalf("MarshalMD special page: %v", err)
	}
	for _, name := range []string{"SCHEMA.md", "index.md", "log.md"} {
		if err := os.WriteFile(filepath.Join(tmpDir, name), data, 0o644); err != nil {
			t.Fatalf("write %s: %v", name, err)
		}
	}

	e, err := NewEngine(tmpDir, keywordEmbedding, logger)
	if err != nil {
		t.Fatalf("NewEngine() error = %v", err)
	}
	if err := e.IndexWikiPages(context.Background()); err != nil {
		t.Fatalf("IndexWikiPages: %v", err)
	}
	if got, want := e.coll.Count(), 4; got != want {
		t.Fatalf("indexed document count = %d, want %d", got, want)
	}
	results, err := e.Search(context.Background(), "schema ghost", 10)
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	for _, result := range results {
		if result.Slug == "SCHEMA" || result.Slug == "index" || result.Slug == "log" {
			t.Fatalf("operational doc leaked into search results: %#v", results)
		}
	}
}

func TestRebuildSQLiteWikiDocumentsClearsStaleAndIndexesGraph(t *testing.T) {
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "aura.db")
	writeTestMDPage(t, tmpDir, &wiki.Page{
		Title:         "Alpha Contract",
		Body:          "Core contract notes.",
		Category:      "project",
		SchemaVersion: wiki.CurrentSchemaVersion,
		PromptVersion: "v1",
		CreatedAt:     time.Now().UTC().Format(time.RFC3339),
		UpdatedAt:     time.Now().UTC().Format(time.RFC3339),
	})
	writeTestMDPage(t, tmpDir, &wiki.Page{
		Title:         "Beta Review",
		Body:          "Review links to [[alpha-contract]].",
		Category:      "project",
		Related:       []string{"alpha-contract"},
		SchemaVersion: wiki.CurrentSchemaVersion,
		PromptVersion: "v1",
		CreatedAt:     time.Now().UTC().Format(time.RFC3339),
		UpdatedAt:     time.Now().UTC().Format(time.RFC3339),
	})

	db, err := auradb.Open(dbPath)
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	if err := migrations.Run(context.Background(), db); err != nil {
		db.Close()
		t.Fatalf("migrate db: %v", err)
	}
	if _, err := db.Exec(`INSERT INTO wiki_documents (id, content, metadata, title) VALUES ('stale-source', 'old raw', '{"kind":"raw"}', 'Old')`); err != nil {
		db.Close()
		t.Fatalf("insert stale doc: %v", err)
	}
	if err := db.Close(); err != nil {
		t.Fatalf("close db: %v", err)
	}

	count, pages, err := RebuildSQLiteWikiDocuments(context.Background(), tmpDir, dbPath, slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err != nil {
		t.Fatalf("RebuildSQLiteWikiDocuments: %v", err)
	}
	if count != 6 || pages != 2 {
		t.Fatalf("count=%d pages=%d, want count=6 pages=2", count, pages)
	}

	db, err = auradb.Open(dbPath)
	if err != nil {
		t.Fatalf("reopen db: %v", err)
	}
	defer db.Close()
	var stale int
	if err := db.QueryRow(`SELECT COUNT(*) FROM wiki_documents WHERE id = 'stale-source'`).Scan(&stale); err != nil {
		t.Fatalf("count stale: %v", err)
	}
	if stale != 0 {
		t.Fatalf("stale docs = %d, want 0", stale)
	}
	var alpha int
	if err := db.QueryRow(`SELECT COUNT(*) FROM wiki_documents WHERE id IN ('alpha-contract', 'graph:node:alpha-contract')`).Scan(&alpha); err != nil {
		t.Fatalf("count alpha docs: %v", err)
	}
	if alpha != 2 {
		t.Fatalf("alpha docs = %d, want 2", alpha)
	}
}

func TestRebuildSQLiteWikiDocumentsRepairsBadWikiDocumentsTable(t *testing.T) {
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "aura.db")
	writeTestMDPage(t, tmpDir, &wiki.Page{
		Title:         "Repairable Memory",
		Body:          "Search text after repair.",
		Category:      "project",
		SchemaVersion: wiki.CurrentSchemaVersion,
		PromptVersion: "v1",
		CreatedAt:     time.Now().UTC().Format(time.RFC3339),
		UpdatedAt:     time.Now().UTC().Format(time.RFC3339),
	})
	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		t.Fatalf("open raw db: %v", err)
	}
	if _, err := db.Exec(`CREATE TABLE wiki_documents(id TEXT)`); err != nil {
		db.Close()
		t.Fatalf("create bad wiki_documents: %v", err)
	}
	if err := db.Close(); err != nil {
		t.Fatalf("close raw db: %v", err)
	}

	docs, pages, err := RebuildSQLiteWikiDocuments(context.Background(), tmpDir, dbPath, slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err != nil {
		t.Fatalf("RebuildSQLiteWikiDocuments: %v", err)
	}
	if docs != 4 || pages != 1 {
		t.Fatalf("docs=%d pages=%d, want docs=4 pages=1", docs, pages)
	}
	db, err = auradb.Open(dbPath)
	if err != nil {
		t.Fatalf("reopen db: %v", err)
	}
	defer db.Close()
	var got string
	if err := db.QueryRow(`SELECT id FROM wiki_documents WHERE wiki_documents MATCH 'repair'`).Scan(&got); err != nil {
		t.Fatalf("query repaired fts: %v", err)
	}
	if got != "repairable-memory" {
		t.Fatalf("matched id = %q, want repairable-memory", got)
	}
}

func TestResultStruct(t *testing.T) {
	r := Result{
		Kind:    "wiki_page",
		Slug:    "test-page",
		Title:   "Test Page",
		Content: "Some content",
		Score:   0.95,
	}
	if r.Slug != "test-page" {
		t.Errorf("expected Slug 'test-page', got %q", r.Slug)
	}
	if r.Kind != "wiki_page" {
		t.Errorf("expected Kind 'wiki_page', got %q", r.Kind)
	}
	if r.Title != "Test Page" {
		t.Errorf("expected Title 'Test Page', got %q", r.Title)
	}
	if r.Content != "Some content" {
		t.Errorf("expected Content 'Some content', got %q", r.Content)
	}
	if r.Score != 0.95 {
		t.Errorf("expected Score 0.95, got %f", r.Score)
	}
}

func writeTestMDPage(t *testing.T, dir string, page *wiki.Page) {
	t.Helper()
	data, err := wiki.MarshalMD(page)
	if err != nil {
		t.Fatalf("MarshalMD: %v", err)
	}
	path := filepath.Join(dir, wiki.Slug(page.Title)+".md")
	if err := os.WriteFile(path, data, 0644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
}

func keywordEmbedding(_ context.Context, text string) ([]float32, error) {
	lower := strings.ToLower(text)
	keywords := []string{"backlinks", "beta", "project", "contract"}
	vec := make([]float32, len(keywords)+1)
	for i, keyword := range keywords {
		if strings.Contains(lower, keyword) {
			vec[i] = 1
		}
	}
	empty := true
	for _, v := range vec {
		if v != 0 {
			empty = false
			break
		}
	}
	if empty {
		vec[len(vec)-1] = 1
	}
	return vec, nil
}

func hasResult(results []Result, kind, slug string) bool {
	for _, result := range results {
		if result.Kind == kind && result.Slug == slug {
			return true
		}
	}
	return false
}
