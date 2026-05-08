package search

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"github.com/aura/aura/internal/wiki"
	"github.com/philippgille/chromem-go"
	"gopkg.in/yaml.v3"
)

// indexConcurrency caps how many wiki pages are embedded in parallel
// during IndexWikiPages. 4 is a sweet spot: cuts cold-start time ~4x
// over serial without hitting Mistral's free-tier rate limits.
// Each goroutine still hits the embed cache first (slice 11h), so
// warm restarts make this constant moot.
const indexConcurrency = 4

// Result represents a search result with relevance score.
type Result struct {
	Kind    string
	Slug    string
	Title   string
	Content string
	Score   float32
}

// EmbeddingFunction is Aura's embedding provider boundary. Provider setup can
// swap Mistral/OpenAI-compatible/local implementations without coupling search
// callers to chromem-go directly.
type EmbeddingFunction = chromem.EmbeddingFunc

// Queryer is the minimal semantic retrieval boundary.
type Queryer interface {
	Search(ctx context.Context, query string, topK int) ([]Result, error)
}

// Searcher is the read-only wiki retrieval boundary used by tools and Telegram
// context injection.
type Searcher interface {
	Queryer
	IsIndexed() bool
}

// WikiPageReindexer is the wiki index maintenance boundary used after one wiki
// page changes.
type WikiPageReindexer interface {
	ReindexWikiPage(ctx context.Context, slug string) error
}

// WikiPageIndexer is the startup/full-rebuild wiki index boundary.
type WikiPageIndexer interface {
	IndexWikiPages(ctx context.Context) error
	WikiPageReindexer
}

// DocumentIndexer is the lower-level document indexing boundary used by debug
// and future non-wiki index feeds.
type DocumentIndexer interface {
	Index(ctx context.Context, id string, content string, metadata map[string]string) error
}

// Repository is the full search/index boundary implemented by Engine.
type Repository interface {
	Searcher
	WikiPageIndexer
	DocumentIndexer
}

// Engine provides vector search over wiki pages, with chromem-go as primary
// and SQLite FTS5 as fallback.
type Engine struct {
	coll    *chromem.Collection
	embedFn chromem.EmbeddingFunc
	sqlite  *sqliteSearcher // nil if not configured
	wikiDir string
	mu      sync.RWMutex
	logger  *slog.Logger
	indexed bool
}

// NewEngine creates a new search engine using chromem-go with an embedding function.
func NewEngine(wikiDir string, embedFn chromem.EmbeddingFunc, logger *slog.Logger) (*Engine, error) {
	db := chromem.NewDB()

	coll, err := db.CreateCollection("wiki", nil, embedFn)
	if err != nil {
		return nil, fmt.Errorf("creating chromem collection: %w", err)
	}

	return &Engine{
		embedFn: embedFn,
		coll:    coll,
		wikiDir: wikiDir,
		logger:  logger,
	}, nil
}

// NewEngineWithFallback creates a search engine with chromem-go (primary) and
// SQLite FTS5 (fallback). If SQLite connection fails, falls back to chromem-only.
func NewEngineWithFallback(wikiDir string, embedFn chromem.EmbeddingFunc, dbPath string, logger *slog.Logger) (*Engine, error) {
	engine, err := NewEngine(wikiDir, embedFn, logger)
	if err != nil {
		return nil, err
	}

	sq, err := newSqliteSearcher(dbPath, logger)
	if err != nil {
		logger.Warn("sqlite fallback unavailable, proceeding with chromem only", "error", err)
		return engine, nil
	}

	engine.sqlite = sq
	logger.Info("sqlite fallback search enabled")
	return engine, nil
}

// NewEngineWithFallbackWithDB creates a search engine using a caller-owned
// SQLite pool for the FTS5 fallback.
func NewEngineWithFallbackWithDB(wikiDir string, embedFn chromem.EmbeddingFunc, db *sql.DB, logger *slog.Logger) (*Engine, error) {
	engine, err := NewEngine(wikiDir, embedFn, logger)
	if err != nil {
		return nil, err
	}

	sq, err := newSqliteSearcherWithDB(db, logger)
	if err != nil {
		logger.Warn("sqlite fallback unavailable, proceeding with chromem only", "error", err)
		return engine, nil
	}

	engine.sqlite = sq
	logger.Info("sqlite fallback search enabled")
	return engine, nil
}

// Close releases resources owned by the engine. Engines built with
// NewEngineWithFallbackWithDB keep the caller-owned SQLite pool open.
func (e *Engine) Close() error {
	if e == nil || e.sqlite == nil {
		return nil
	}
	return e.sqlite.Close()
}

// Index adds or updates a document in the search index.
func (e *Engine) Index(ctx context.Context, id string, content string, metadata map[string]string) error {
	e.mu.Lock()
	defer e.mu.Unlock()

	if err := e.coll.AddDocument(ctx, chromem.Document{
		ID:       id,
		Content:  content,
		Metadata: metadata,
	}); err != nil {
		return fmt.Errorf("indexing document %s: %w", id, err)
	}

	if e.sqlite != nil {
		if err := e.sqlite.indexDocument(ctx, id, content, metadata); err != nil {
			e.logger.Warn("failed to index in sqlite search fallback", "id", id, "error", err)
		}
	}

	e.logger.Debug("document indexed", "id", id)
	return nil
}

// IndexWikiPages reads all wiki .md files and indexes them.
// Skips special files (index.md, log.md) and falls back to .yaml for legacy pages.
func (e *Engine) IndexWikiPages(ctx context.Context) error {
	e.mu.Lock()
	defer e.mu.Unlock()

	docs, pageCount, err := loadWikiDocuments(e.wikiDir, e.logger)
	if err != nil {
		return err
	}

	// Slice 11i: switch from a serial AddDocument loop to chromem-go's
	// AddDocuments(concurrency=indexConcurrency) so the per-page Mistral
	// round trips run in parallel goroutines. With 8 wiki pages × ~1 s
	// per embed serial = ~8 s; concurrency=4 = ~2 s. Higher concurrency
	// risks Mistral rate-limit pushback on free tiers.
	count := 0
	if err := e.resetCollectionLocked(); err != nil {
		return err
	}
	if len(docs) > 0 {
		if err := e.coll.AddDocuments(ctx, docs, indexConcurrency); err != nil {
			// Atomic failure on the batch — fall back to serial so a
			// single bad doc doesn't lose the rest of the index.
			e.logger.Warn("batch index failed, falling back to serial", "error", err, "docs", len(docs))
			for _, doc := range docs {
				if addErr := e.coll.AddDocument(ctx, doc); addErr != nil {
					e.logger.Warn("failed to index wiki page", "slug", doc.ID, "error", addErr)
					continue
				}
				count++
			}
		} else {
			count = len(docs)
		}

		// SQLite full-text mirror: keep this serial — local SQLite writes
		// are cheap and concurrent inserts on the same FTS table fight.
		if e.sqlite != nil {
			if err := indexSQLiteDocuments(ctx, e.sqlite, docs); err != nil {
				e.logger.Warn("failed to rebuild sqlite search mirror", "error", err)
			}
		}
	}

	e.indexed = true
	e.logger.Info("wiki pages indexed", "count", count, "pages", pageCount, "concurrency", indexConcurrency)
	return nil
}

// RebuildSQLiteWikiDocuments rebuilds the SQLite FTS mirror from the wiki
// files without requiring an embedding model. It is used by closure/debug
// flows after deterministic wiki cleanup so the manifest can be verified in
// one pass even when vector embeddings are unavailable.
func RebuildSQLiteWikiDocuments(ctx context.Context, wikiDir, dbPath string, logger *slog.Logger) (docsIndexed int, pagesIndexed int, err error) {
	sqlite, err := newSqliteSearcher(dbPath, logger)
	if err != nil {
		return 0, 0, err
	}
	defer sqlite.Close()

	docs, pageCount, err := loadWikiDocuments(wikiDir, logger)
	if err != nil {
		return 0, 0, err
	}
	if err := indexSQLiteDocuments(ctx, sqlite, docs); err != nil {
		return 0, 0, err
	}
	return len(docs), pageCount, nil
}

func indexSQLiteDocuments(ctx context.Context, sqlite *sqliteSearcher, docs []chromem.Document) error {
	if sqlite == nil {
		return nil
	}
	for attempt := 0; attempt < 2; attempt++ {
		if err := sqlite.clearOrRecreate(ctx); err != nil {
			return err
		}
		needsRepair := false
		for _, doc := range docs {
			if err := sqlite.indexDocument(ctx, doc.ID, doc.Content, doc.Metadata); err != nil {
				if attempt == 0 && recoverableWikiDocumentsError(err) {
					if repairErr := sqlite.recreateWikiDocuments(ctx); repairErr != nil {
						return repairErr
					}
					needsRepair = true
					break
				}
				return err
			}
		}
		if !needsRepair {
			return nil
		}
	}
	return fmt.Errorf("rebuilding wiki_documents: repair retry exhausted")
}

func loadWikiDocuments(wikiDir string, logger *slog.Logger) ([]chromem.Document, int, error) {
	if logger == nil {
		logger = slog.Default()
	}
	entries, err := os.ReadDir(wikiDir)
	if err != nil {
		return nil, 0, fmt.Errorf("reading wiki directory: %w", err)
	}

	type fileInfo struct {
		name string
		ext  string
	}
	slugFiles := make(map[string]fileInfo)
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		name := entry.Name()
		var slug, ext string
		if strings.HasSuffix(name, ".md") {
			slug = strings.TrimSuffix(name, ".md")
			ext = ".md"
		} else if strings.HasSuffix(name, ".yaml") {
			slug = strings.TrimSuffix(name, ".yaml")
			ext = ".yaml"
		} else {
			continue
		}
		if wiki.IsOperationalSlug(slug) {
			continue
		}
		if existing, ok := slugFiles[slug]; ok && existing.ext == ".md" {
			continue
		}
		slugFiles[slug] = fileInfo{name: name, ext: ext}
	}

	pages := make(map[string]indexedWikiPage, len(slugFiles))
	docs := make([]chromem.Document, 0, len(slugFiles)*3)
	for slug, fi := range slugFiles {
		filePath := filepath.Join(wikiDir, fi.name)
		data, err := os.ReadFile(filePath)
		if err != nil {
			logger.Warn("failed to read wiki page for indexing", "slug", slug, "error", err)
			continue
		}
		page, err := parseIndexedWikiPage(slug, fi.ext, data)
		if err != nil {
			logger.Warn("failed to parse wiki page for indexing", "slug", slug, "error", err)
			continue
		}
		pages[slug] = page
		title, content := page.Title, page.Title+"\n"+page.Body
		docs = append(docs, chromem.Document{
			ID:       slug,
			Content:  content,
			Metadata: map[string]string{"slug": slug, "title": title, "kind": "wiki_page"},
		})
	}
	docs = append(docs, buildGraphDocuments(pages)...)
	return docs, len(pages), nil
}

func (e *Engine) resetCollectionLocked() error {
	db := chromem.NewDB()
	coll, err := db.CreateCollection("wiki", nil, e.embedFn)
	if err != nil {
		return fmt.Errorf("resetting chromem collection: %w", err)
	}
	e.coll = coll
	return nil
}

// Search performs hybrid wiki retrieval. Exact slug/title and SQLite FTS
// results always get a chance before vector similarity; vector errors only
// fail the request when the local lexical index cannot answer either.
func (e *Engine) Search(ctx context.Context, query string, topK int) ([]Result, error) {
	e.mu.RLock()
	defer e.mu.RUnlock()

	if !e.indexed {
		return nil, fmt.Errorf("no documents indexed, call IndexWikiPages first")
	}

	if topK <= 0 {
		topK = 5
	}

	var exactResults []Result
	var ftsResults []Result
	var sqliteErr error
	if e.sqlite != nil {
		if exactResults, sqliteErr = e.sqlite.exactSearch(ctx, query, topK); sqliteErr != nil {
			e.logger.Warn("sqlite exact search failed", "error", sqliteErr)
		}
		var ftsErr error
		if ftsResults, ftsErr = e.sqlite.search(ctx, query, topK); ftsErr != nil {
			e.logger.Warn("sqlite FTS search failed", "error", ftsErr)
			if sqliteErr == nil {
				sqliteErr = ftsErr
			}
		}
	}

	vectorResults, vectorErr := e.queryChromem(ctx, query, topK)
	if vectorErr != nil {
		e.logger.Warn("chromem search failed", "error", vectorErr)
	}

	results := mergeHybridResults(query, topK, exactResults, ftsResults, vectorResults)
	if len(results) > 0 {
		return results, nil
	}
	if vectorErr != nil {
		if sqliteErr != nil {
			return nil, fmt.Errorf("search failed: chromem: %v; sqlite: %v", vectorErr, sqliteErr)
		}
		return nil, fmt.Errorf("search failed: chromem: %w", vectorErr)
	}
	return nil, nil
}

func mergeHybridResults(_ string, topK int, groups ...[]Result) []Result {
	if topK <= 0 {
		topK = 5
	}
	out := make([]Result, 0, topK)
	seen := make(map[string]bool, topK)
	for _, group := range groups {
		for _, result := range group {
			key := resultKey(result)
			if key == "" || seen[key] {
				continue
			}
			seen[key] = true
			out = append(out, result)
			if len(out) >= topK {
				return out
			}
		}
	}
	return out
}

func resultKey(result Result) string {
	kind := strings.TrimSpace(result.Kind)
	if kind == "" {
		kind = "wiki_page"
	}
	slug := strings.TrimSpace(result.Slug)
	if slug == "" {
		slug = strings.TrimSpace(result.Title)
	}
	if slug == "" {
		return ""
	}
	return kind + "\x00" + strings.ToLower(slug)
}

// IsIndexed returns whether pages have been indexed.
func (e *Engine) IsIndexed() bool {
	e.mu.RLock()
	defer e.mu.RUnlock()
	return e.indexed
}

// ReindexWikiPage refreshes the semantic wiki index after one page changed.
// Graph node cards include backlinks and category/index summaries, so a single
// page change can affect neighboring nodes. We verify the changed page exists,
// then rebuild the in-memory collection. The embedding cache keeps unchanged
// documents cheap.
func (e *Engine) ReindexWikiPage(ctx context.Context, slug string) error {
	mdPath := filepath.Join(e.wikiDir, slug+".md")
	yamlPath := filepath.Join(e.wikiDir, slug+".yaml")

	if _, err := os.Stat(mdPath); err != nil {
		if _, yamlErr := os.Stat(yamlPath); yamlErr != nil {
			return fmt.Errorf("reading wiki page %s: file not found", slug)
		}
	}

	return e.IndexWikiPages(ctx)
}

// FormatResults formats search results as context for injection into LLM prompts.
// Includes first 200 chars of content as excerpt.
func FormatResults(results []Result) string {
	if len(results) == 0 {
		return ""
	}

	var sb strings.Builder
	sb.WriteString("Relevant wiki knowledge:\n")
	for _, r := range results {
		kind := resultKind(r)
		label := resultLabel(r)
		if kind == "wiki_page" {
			sb.WriteString(fmt.Sprintf("- %s %s\n", label, r.Title))
		} else {
			sb.WriteString(fmt.Sprintf("- [%s] %s %s\n", kind, label, r.Title))
		}
		excerpt := truncateExcerpt(r.Content, 200)
		if excerpt != "" {
			sb.WriteString(fmt.Sprintf("  %s\n", excerpt))
		}
	}
	return sb.String()
}

// truncateExcerpt returns the first n characters of content, cleaned for display.
func truncateExcerpt(content string, n int) string {
	// Strip frontmatter if present
	if strings.HasPrefix(content, "---") {
		if end := findMDBodyEnd(content); end != -1 {
			content = content[end:]
		}
	}
	content = strings.TrimSpace(content)
	content = strings.ReplaceAll(content, "\n", " ")
	content = strings.ReplaceAll(content, "  ", " ")
	if len(content) > n {
		content = content[:n] + "..."
	}
	return content
}

// findMDBodyEnd finds the position after the closing --- delimiter of frontmatter.
func findMDBodyEnd(content string) int {
	// Skip opening ---
	if !strings.HasPrefix(content, "---") {
		return -1
	}
	rest := content[3:]
	// Skip newline after opening ---
	if len(rest) > 0 && rest[0] == '\n' {
		rest = rest[1:]
	} else if len(rest) > 1 && rest[0] == '\r' && rest[1] == '\n' {
		rest = rest[2:]
	}
	// Find closing ---
	idx := strings.Index(rest, "\n---\n")
	if idx == -1 {
		idx = strings.Index(rest, "\n---\r\n")
	}
	if idx == -1 {
		return -1
	}
	// Position after closing ---\n
	return len(content) - len(rest) + idx + 5
}

// extractTitle parses just the title field from YAML bytes.
func extractTitle(data []byte) string {
	var partial struct {
		Title string `yaml:"title"`
	}
	if err := yaml.Unmarshal(data, &partial); err != nil {
		return ""
	}
	return partial.Title
}

// metadataToJSON serializes a metadata map to a JSON string.
func metadataToJSON(metadata map[string]string) string {
	b, err := json.Marshal(metadata)
	if err != nil {
		return "{}"
	}
	return string(b)
}

// extractMetaField extracts a field value from a JSON metadata string.
func extractMetaField(metaJSON, field string) string {
	var m map[string]string
	if err := json.Unmarshal([]byte(metaJSON), &m); err != nil {
		return ""
	}
	return m[field]
}

func (e *Engine) queryChromem(ctx context.Context, query string, topK int) ([]Result, error) {
	if e.coll.Count() == 0 {
		return nil, nil
	}

	// Clamp topK to collection size
	if topK > e.coll.Count() {
		topK = e.coll.Count()
	}

	results, err := e.coll.Query(ctx, query, topK, nil, nil)
	if err != nil {
		return nil, fmt.Errorf("querying chromem: %w", err)
	}

	searchResults := make([]Result, 0, len(results))
	for _, r := range results {
		title := ""
		slug := r.ID
		kind := "wiki_page"
		if r.Metadata != nil {
			title = r.Metadata["title"]
			if metaSlug := strings.TrimSpace(r.Metadata["slug"]); metaSlug != "" {
				slug = metaSlug
			}
			if metaKind := strings.TrimSpace(r.Metadata["kind"]); metaKind != "" {
				kind = metaKind
			}
		}
		searchResults = append(searchResults, Result{
			Kind:    kind,
			Slug:    slug,
			Title:   title,
			Content: r.Content,
			Score:   r.Similarity,
		})
	}

	return searchResults, nil
}

func resultKind(r Result) string {
	if strings.TrimSpace(r.Kind) == "" {
		return "wiki_page"
	}
	return r.Kind
}

func resultLabel(r Result) string {
	if r.Slug == "" {
		return ""
	}
	switch resultKind(r) {
	case "wiki_page", "graph_node":
		return "[[" + r.Slug + "]]"
	default:
		return r.Slug
	}
}
