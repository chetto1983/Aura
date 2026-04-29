package search

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"github.com/philippgille/chromem-go"
	"gopkg.in/yaml.v3"
)

// Result represents a search result with relevance score.
type Result struct {
	Slug    string
	Title   string
	Content string
	Score   float32
}

// Engine provides vector search over wiki pages, with chromem-go as primary
// and SQLite FTS5 as fallback.
type Engine struct {
	coll    *chromem.Collection
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
			e.logger.Warn("failed to index in pgvector fallback", "id", id, "error", err)
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

	entries, err := os.ReadDir(e.wikiDir)
	if err != nil {
		return fmt.Errorf("reading wiki directory: %w", err)
	}

	// Build slug -> file mapping, preferring .md over .yaml
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
		// Skip special files
		if slug == "index" || slug == "log" {
			continue
		}
		// Prefer .md over .yaml
		if existing, ok := slugFiles[slug]; ok && existing.ext == ".md" {
			continue
		}
		slugFiles[slug] = fileInfo{name: name, ext: ext}
	}

	count := 0
	for slug, fi := range slugFiles {
		filePath := filepath.Join(e.wikiDir, fi.name)

		data, err := os.ReadFile(filePath)
		if err != nil {
			e.logger.Warn("failed to read wiki page for indexing", "slug", slug, "error", err)
			continue
		}

		var title, content string
		if fi.ext == ".md" {
			title, content = extractFromMD(data)
		} else {
			title = extractTitle(data)
			content = title + "\n" + string(data)
		}

		if err := e.coll.AddDocument(ctx, chromem.Document{
			ID:       slug,
			Content:  content,
			Metadata: map[string]string{"slug": slug, "title": title},
		}); err != nil {
			e.logger.Warn("failed to index wiki page", "slug", slug, "error", err)
			continue
		}

		if e.sqlite != nil {
			if err := e.sqlite.indexDocument(ctx, slug, content, map[string]string{"slug": slug, "title": title}); err != nil {
				e.logger.Warn("failed to index in sqlite", "slug", slug, "error", err)
			}
		}

		count++
	}

	e.indexed = true
	e.logger.Info("wiki pages indexed", "count", count)
	return nil
}

// Search performs a vector similarity search and returns the top-k results.
// Falls back to pgvector if chromem search fails.
func (e *Engine) Search(ctx context.Context, query string, topK int) ([]Result, error) {
	e.mu.RLock()
	defer e.mu.RUnlock()

	if !e.indexed {
		return nil, fmt.Errorf("no documents indexed, call IndexWikiPages first")
	}

	if topK <= 0 {
		topK = 5
	}

	// Try primary (chromem) first
	results, err := e.queryChromem(ctx, query, topK)
	if err == nil {
		return results, nil
	}

	e.logger.Warn("chromem search failed, trying sqlite fallback", "error", err)

	// Try fallback (pgvector) if available
	if e.sqlite != nil {
		results, pgErr := e.sqlite.search(ctx, query, topK)
		if pgErr == nil {
			return results, nil
		}
		e.logger.Warn("sqlite fallback also failed", "error", pgErr)
	}

	return nil, fmt.Errorf("search failed: both chromem and sqlite unavailable")
}

// IsIndexed returns whether pages have been indexed.
func (e *Engine) IsIndexed() bool {
	e.mu.RLock()
	defer e.mu.RUnlock()
	return e.indexed
}

// ReindexWikiPage removes and re-indexes a single wiki page.
func (e *Engine) ReindexWikiPage(ctx context.Context, slug string) error {
	// Try .md first, fall back to .yaml
	var data []byte
	var isMD bool
	mdPath := filepath.Join(e.wikiDir, slug+".md")
	yamlPath := filepath.Join(e.wikiDir, slug+".yaml")

	if d, err := os.ReadFile(mdPath); err == nil {
		data = d
		isMD = true
	} else if d, err := os.ReadFile(yamlPath); err == nil {
		data = d
		isMD = false
	} else {
		return fmt.Errorf("reading wiki page %s: file not found", slug)
	}

	var title, content string
	if isMD {
		title, content = extractFromMD(data)
	} else {
		title = extractTitle(data)
		content = title + "\n" + string(data)
	}

	return e.Index(ctx, slug, content, map[string]string{"slug": slug, "title": title})
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
		sb.WriteString(fmt.Sprintf("- [[%s]] %s\n", r.Slug, r.Title))
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

// extractFromMD parses a markdown file with frontmatter and returns title and body content.
func extractFromMD(data []byte) (title, content string) {
	content = string(data)

	// Extract title from frontmatter
	if strings.HasPrefix(content, "---") {
		end := findMDBodyEnd(content)
		if end != -1 && end < len(content) {
			body := strings.TrimSpace(content[end:])
			content = strings.TrimSpace(body)
		}
		// Parse just the title from frontmatter
		title = extractTitle(data)
	} else {
		title = extractTitle(data)
	}

	content = title + "\n" + content
	return title, content
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
		if r.Metadata != nil {
			title = r.Metadata["title"]
		}
		searchResults = append(searchResults, Result{
			Slug:    r.ID,
			Title:   title,
			Content: r.Content,
			Score:   r.Similarity,
		})
	}

	return searchResults, nil
}
