package search

import (
	"context"
	"database/sql"
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
)

// These tests cover the chromem-free search layer:
//
//   - Pure helpers (FormatResults, extractTitle, mergeHybridResults).
//   - loadWikiDocuments + buildGraphDocuments — the wiki → indexable docs
//     pipeline that both Qdrant and SQLite consume.
//   - SQLite FTS mirror via RebuildSQLiteWikiDocuments and the underlying
//     sqliteSearcher.
//
// Tests that previously exercised the chromem-go in-memory `Engine` were
// deleted when the Engine was removed. The production wiring uses
// NewQdrantRepository, which is integration-tested against the live
// Qdrant container; we do not stub the Qdrant HTTP API here.

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
				{Kind: "graph_node", Slug: "alpha-contract", Title: "Alpha Contract", Content: "Graph node body."},
				{Kind: "graph_index", Slug: "index:category:project", Title: "Index: project", Content: "Index body."},
			},
			expected: "Relevant wiki knowledge:\n- [graph_node] [[alpha-contract]] Alpha Contract\n  Graph node body.\n- [graph_index] index:category:project Index: project\n  Index body.\n",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := FormatResults(tt.results); got != tt.expected {
				t.Errorf("FormatResults() =\n%q\nwant:\n%q", got, tt.expected)
			}
		})
	}
}

func TestExtractTitle(t *testing.T) {
	cases := []struct {
		name string
		yaml string
		want string
	}{
		{"valid yaml", "title: My Page\nbody: text", "My Page"},
		{"missing title", "body: text", ""},
		{"empty input", "", ""},
		{"invalid yaml", "title: [unclosed\nbody: text", ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := extractTitle([]byte(tc.yaml)); got != tc.want {
				t.Errorf("extractTitle(%q) = %q, want %q", tc.yaml, got, tc.want)
			}
		})
	}
}

// TestMergeHybridResultsRRFFusion verifies that mergeHybridResults applies
// Reciprocal Rank Fusion across the three positional channels (exact, FTS,
// vector) and dedupes on (kind, slug). Expected RRF math with k=60,
// weights {1.0, 0.6, 0.8}:
//
//	wiki_page:alpha  = 1.0/61 + 0.6/61 = 1.6/61 ≈ 0.02623  (exact + fts duplicate)
//	graph_node:alpha = 0.8/61          ≈ 0.01311           (vector rank 1)
//	wiki_page:gamma  = 0.8/62          ≈ 0.01290           (vector rank 2)
//	wiki_page:beta   = 0.6/62          ≈ 0.00968           (fts rank 2 only)
//
// Top-3 are therefore [wiki_page:alpha, graph_node:alpha, wiki_page:gamma].
// The first-seen Result wins on metadata, so wiki_page:alpha keeps the
// exact-channel title "Alpha", not the fts "Alpha duplicate".
func TestMergeHybridResultsRRFFusion(t *testing.T) {
	exact := []Result{{Kind: "wiki_page", Slug: "alpha", Title: "Alpha"}}
	fts := []Result{{Kind: "wiki_page", Slug: "alpha", Title: "Alpha duplicate"}, {Kind: "wiki_page", Slug: "beta", Title: "Beta"}}
	vector := []Result{{Kind: "graph_node", Slug: "alpha", Title: "Alpha graph"}, {Kind: "wiki_page", Slug: "gamma", Title: "Gamma"}}

	results := mergeHybridResults("alpha", 3, exact, fts, vector)
	if got := []string{results[0].Kind + ":" + results[0].Slug, results[1].Kind + ":" + results[1].Slug, results[2].Kind + ":" + results[2].Slug}; !slices.Equal(got, []string{"wiki_page:alpha", "graph_node:alpha", "wiki_page:gamma"}) {
		t.Fatalf("merged order = %#v", got)
	}
	if results[0].Title != "Alpha" {
		t.Fatalf("first-seen metadata lost: title = %q, want %q", results[0].Title, "Alpha")
	}
	// Fused scores should be monotonically decreasing.
	for i := 1; i < len(results); i++ {
		if results[i-1].Score < results[i].Score {
			t.Fatalf("score not descending at i=%d: %f then %f", i, results[i-1].Score, results[i].Score)
		}
	}
}

func TestLoadWikiDocumentsBuildsGraphCards(t *testing.T) {
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
		Related:       wiki.RelatedFromSlugs([]string{"alpha-contract"}),
		SchemaVersion: wiki.CurrentSchemaVersion,
		PromptVersion: "v1",
		CreatedAt:     time.Now().UTC().Format(time.RFC3339),
		UpdatedAt:     time.Now().UTC().Format(time.RFC3339),
	})

	docs, pages, err := loadWikiDocuments(tmpDir, logger)
	if err != nil {
		t.Fatalf("loadWikiDocuments: %v", err)
	}
	if pages != 2 {
		t.Fatalf("pages = %d, want 2", pages)
	}
	// 2 wiki_page + 2 graph_node + 1 graph_index (category) + 1 graph_index (all) = 6
	if len(docs) != 6 {
		t.Fatalf("docs = %d, want 6", len(docs))
	}
	if !hasDoc(docs, "graph:node:alpha-contract") {
		t.Fatalf("missing graph_node card for alpha-contract")
	}
	if !hasDoc(docs, "graph:index:category:project") {
		t.Fatalf("missing graph_index card for project category")
	}
	if !hasDoc(docs, "graph:index:all") {
		t.Fatalf("missing graph_index overview card")
	}
}

func TestLoadWikiDocumentsPromotesFrontmatterMetadata(t *testing.T) {
	tmpDir := t.TempDir()
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	created := "2026-05-01T10:00:00Z"
	updated := "2026-05-02T11:30:00Z"

	writeTestMDPage(t, tmpDir, &wiki.Page{
		Title:         "Phase07F Metadata",
		Body:          "Phase07F sentinel frontmatter body linked to [[graph-target]].",
		Category:      "concept",
		Tags:          []string{"rag", "frontmatter"},
		Related:       wiki.RelatedFromSlugs([]string{"graph-target"}),
		Sources:       []string{"src_phase07f", "https://example.test/ref"},
		SchemaVersion: wiki.CurrentSchemaVersion,
		PromptVersion: "ingest_v2",
		CreatedAt:     created,
		UpdatedAt:     updated,
		Unversioned:   true,
	})

	docs, _, err := loadWikiDocuments(tmpDir, logger)
	if err != nil {
		t.Fatalf("loadWikiDocuments: %v", err)
	}

	pageDoc := findDoc(t, docs, "phase07f-metadata")
	for key, want := range map[string]string{
		"schema_version": "3",
		"prompt_version": "ingest_v2",
		"created_at":     created,
		"updated_at":     updated,
		"sources":        "src_phase07f,https://example.test/ref",
		"unversioned":    "true",
	} {
		if got := pageDoc.Metadata[key]; got != want {
			t.Fatalf("wiki metadata %s = %q, want %q; meta=%v", key, got, want, pageDoc.Metadata)
		}
	}

	graphDoc := findDoc(t, docs, "graph:node:phase07f-metadata")
	for key, want := range map[string]string{
		"schema_version": "3",
		"prompt_version": "ingest_v2",
		"created_at":     created,
		"sources":        "src_phase07f,https://example.test/ref",
		"unversioned":    "true",
	} {
		if got := graphDoc.Metadata[key]; got != want {
			t.Fatalf("graph metadata %s = %q, want %q; meta=%v", key, got, want, graphDoc.Metadata)
		}
	}
	for _, want := range []string{
		"Schema version: 3",
		"Prompt version: ingest_v2",
		"Created: " + created,
		"Unversioned: true",
		"Sources: src_phase07f, https://example.test/ref",
	} {
		if !strings.Contains(graphDoc.Content, want) {
			t.Fatalf("graph content missing %q:\n%s", want, graphDoc.Content)
		}
	}
}

func TestLoadWikiDocumentsSkipsOperationalDocs(t *testing.T) {
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

	docs, pages, err := loadWikiDocuments(tmpDir, logger)
	if err != nil {
		t.Fatalf("loadWikiDocuments: %v", err)
	}
	if pages != 1 {
		t.Fatalf("pages = %d, want 1 (only project-memory)", pages)
	}
	for _, doc := range docs {
		for _, skipID := range []string{"SCHEMA", "index", "log"} {
			if doc.ID == skipID {
				t.Fatalf("operational doc leaked into index: %+v", doc)
			}
		}
	}
}

func TestSQLiteSearchTokenizesPunctuationQueries(t *testing.T) {
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "aura.db")
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))

	writeTestMDPage(t, tmpDir, &wiki.Page{
		Title:         "S7-1200 PLC",
		Body:          "Programmable logic controller from Siemens.",
		Category:      "industrial",
		SchemaVersion: wiki.CurrentSchemaVersion,
		PromptVersion: "v1",
		CreatedAt:     time.Now().UTC().Format(time.RFC3339),
		UpdatedAt:     time.Now().UTC().Format(time.RFC3339),
	})
	if _, _, err := RebuildSQLiteWikiDocuments(context.Background(), tmpDir, dbPath, logger); err != nil {
		t.Fatalf("RebuildSQLiteWikiDocuments: %v", err)
	}

	db, err := auradb.Open(dbPath)
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	defer db.Close()
	sq, err := newSqliteSearcherWithDB(db, logger)
	if err != nil {
		t.Fatalf("newSqliteSearcherWithDB: %v", err)
	}
	// "S7-1200" has a hyphen — escapeFTS5Query must handle this without
	// crashing the FTS5 parser. The matched doc is the wiki_page row.
	results, err := sq.search(context.Background(), "S7-1200", 5)
	if err != nil {
		t.Fatalf("sqliteSearcher.search: %v", err)
	}
	if !hasResult(results, "wiki_page", "s7-1200-plc") {
		t.Fatalf("results = %#v, want s7-1200-plc hit", results)
	}
}

func TestEscapeFTS5QueryDropsLowSignalQuestionTerms(t *testing.T) {
	got := escapeFTS5Query("Qual è il numero totale di casi COVID registrati per la regione Lombardia?")
	for _, noisy := range []string{"qual", "e", "il", "di", "per", "la"} {
		if strings.Contains(" OR "+got+" OR ", " OR "+noisy+" OR ") {
			t.Fatalf("query %q still contains low-signal term %q", got, noisy)
		}
	}
	for _, want := range []string{"numero", "totale", "casi", "covid", "registrati", "regione", "lombardia"} {
		if !strings.Contains(" OR "+got+" OR ", " OR "+want+" OR ") {
			t.Fatalf("query %q missing term %q", got, want)
		}
	}
}

func TestSQLiteSearchReturnsFrontmatterMetadata(t *testing.T) {
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "aura.db")
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	created := "2026-05-01T10:00:00Z"
	updated := "2026-05-02T11:30:00Z"

	writeTestMDPage(t, tmpDir, &wiki.Page{
		Title:         "Phase07F SQLite Metadata",
		Body:          "Unique phase07f sqlite metadata retrieval body.",
		Category:      "concept",
		Sources:       []string{"src_sqlite07f"},
		SchemaVersion: wiki.CurrentSchemaVersion,
		PromptVersion: "proposal_v3",
		CreatedAt:     created,
		UpdatedAt:     updated,
		Unversioned:   true,
	})
	if _, _, err := RebuildSQLiteWikiDocuments(context.Background(), tmpDir, dbPath, logger); err != nil {
		t.Fatalf("RebuildSQLiteWikiDocuments: %v", err)
	}

	db, err := auradb.Open(dbPath)
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	defer db.Close()
	sq, err := newSqliteSearcherWithDB(db, logger)
	if err != nil {
		t.Fatalf("newSqliteSearcherWithDB: %v", err)
	}
	results, err := sq.search(context.Background(), "phase07f sqlite metadata", 5)
	if err != nil {
		t.Fatalf("sqliteSearcher.search: %v", err)
	}
	if len(results) == 0 {
		t.Fatal("expected at least one result")
	}
	got := results[0]
	if got.SchemaVersion != wiki.CurrentSchemaVersion {
		t.Fatalf("schema version = %d, want %d", got.SchemaVersion, wiki.CurrentSchemaVersion)
	}
	if got.PromptVersion != "proposal_v3" {
		t.Fatalf("prompt version = %q", got.PromptVersion)
	}
	wantCreated, _ := time.Parse(time.RFC3339, created)
	if !got.CreatedAt.Equal(wantCreated) {
		t.Fatalf("created_at = %s, want %s", got.CreatedAt.Format(time.RFC3339), created)
	}
	if !got.Unversioned {
		t.Fatal("unversioned metadata was not returned")
	}
	if !slices.Equal(got.Sources, []string{"src_sqlite07f"}) {
		t.Fatalf("sources = %v", got.Sources)
	}
}

func TestSqliteSearcherWithDBCloseDoesNotCloseSharedDB(t *testing.T) {
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "aura.db")
	pool, err := auradb.Open(dbPath)
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	defer pool.Close()
	if err := migrations.Run(context.Background(), pool); err != nil {
		t.Fatalf("migrate: %v", err)
	}

	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	sq, err := newSqliteSearcherWithDB(pool, logger)
	if err != nil {
		t.Fatalf("newSqliteSearcherWithDB: %v", err)
	}
	if err := sq.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	// pool is still usable
	if err := pool.PingContext(context.Background()); err != nil {
		t.Fatalf("pool ping after searcher close: %v", err)
	}
}

func TestSqliteSearcherWithDBDoesNotCreateSchema(t *testing.T) {
	// When constructed against a caller-owned pool, the searcher trusts the
	// caller to have applied migrations. If migrations were not run, the
	// FTS table won't exist; the first indexDocument call will return an
	// error rather than silently creating an alternate schema.
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "aura.db")
	pool, err := sql.Open("sqlite", dbPath)
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	defer pool.Close()

	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	sq, err := newSqliteSearcherWithDB(pool, logger)
	if err != nil {
		t.Fatalf("newSqliteSearcherWithDB: %v", err)
	}
	defer sq.Close()
	if tableExists(t, pool, "wiki_documents") {
		t.Fatal("wiki_documents was created without migrations")
	}
}

func tableExists(t *testing.T, db *sql.DB, name string) bool {
	t.Helper()
	var got string
	err := db.QueryRow(`SELECT name FROM sqlite_master WHERE type='table' AND name = ?`, name).Scan(&got)
	if err == sql.ErrNoRows {
		return false
	}
	if err != nil {
		t.Fatalf("query sqlite_master: %v", err)
	}
	return got == name
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
		Related:       wiki.RelatedFromSlugs([]string{"alpha-contract"}),
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

func hasResult(results []Result, kind, slug string) bool {
	for _, r := range results {
		if r.Kind == kind && r.Slug == slug {
			return true
		}
	}
	return false
}

func hasDoc(docs []Document, id string) bool {
	for _, doc := range docs {
		if doc.ID == id {
			return true
		}
	}
	return false
}

func findDoc(t *testing.T, docs []Document, id string) Document {
	t.Helper()
	for _, doc := range docs {
		if doc.ID == id {
			return doc
		}
	}
	t.Fatalf("document %q not found in %+v", id, docs)
	return Document{}
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

// keywordEmbedding is referenced indirectly by other tests in this package
// that build a synthetic embed func; the assignment keeps go vet happy if
// no test in this file calls it directly after edits.
var _ EmbeddingFunc = keywordEmbedding
