// Package search is Aura's read-side wiki retrieval. Qdrant is the only vector
// backend (the in-memory chromem-go path that used to live here was deleted
// once Qdrant became required infrastructure). SQLite FTS5 remains as the
// keyword-side companion: every wiki page is mirrored there at index time so
// search() can hybridize cosine similarity with BM25-style lexical hits.
package search

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/aura/aura/internal/wiki"
	"gopkg.in/yaml.v3"
)

// indexConcurrency caps how many wiki pages are embedded in parallel during
// a rebuild. The host is a 16-core mini-PC shared with the user's daily
// work — saturating the CPU starves IDE/browser/other services, so we cap
// at 2: matches llama-embed's --parallel 2 slot count with no over-subscription.
// Reindex takes ~5-10 min for ~1400 vectors at this setting; that's the
// intended trade-off (see feedback_minipc_cpu_budget memory).
const indexConcurrency = 2

// splitCSVPayloadField reverses the comma-join used at index time for
// list-valued frontmatter fields (tags, related, sources). Empty entries are
// dropped; whitespace around items is trimmed. Returns nil when the value is
// empty so callers do not have to distinguish "absent" from "empty list".
func splitCSVPayloadField(raw string) []string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil
	}
	parts := strings.Split(raw, ",")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		if v := strings.TrimSpace(p); v != "" {
			out = append(out, v)
		}
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

// parseSearchPayloadTime accepts the RFC3339 and a couple of common YAML
// frontmatter layouts (older pages used a space separator). Empty / unparseable
// values surface as the zero time, which the recency multiplier knows to skip.
func parseSearchPayloadTime(raw string) (time.Time, bool) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return time.Time{}, false
	}
	for _, layout := range []string{
		time.RFC3339Nano,
		time.RFC3339,
		"2006-01-02T15:04:05",
		"2006-01-02 15:04:05",
		"2006-01-02",
	} {
		if ts, err := time.Parse(layout, raw); err == nil {
			return ts.UTC(), true
		}
	}
	return time.Time{}, false
}

// Result is one wiki hit returned by Search.
//
// UpdatedAt carries the page's frontmatter updated_at when the backend can
// supply it (zero time when unknown). Callers that score with a recency factor
// must guard against a zero time and choose whether to apply recency=1.0 or
// skip the multiplier.
//
// FilePath, Category, Tags, Related, Sources, and SizeBytes round out the
// per-hit metadata so search_memory can satisfy the LLM in one round-trip —
// previously the model had to follow up with list_files/read_file to map a
// slug back to a .md path or read the page's relations.
type Result struct {
	Kind        string
	Slug        string
	Title       string
	Content     string
	Score       float32
	ScoreExact  float32
	ScoreFTS    float32
	ScoreVector float32
	UpdatedAt   time.Time
	FilePath    string
	Category    string
	Tags        []string
	Related     []string
	Sources     []string
	SizeBytes   int64
}

// EmbeddingFunc is Aura's embedding provider boundary. Same signature as
// chromem-go's func type so wrappers (EmbedCache, fakes) port cleanly; the
// dependency on chromem itself is gone.
type EmbeddingFunc func(ctx context.Context, text string) ([]float32, error)

// BatchEmbeddingFunction returns one vector per input text, in order.
type BatchEmbeddingFunction func(ctx context.Context, texts []string) ([][]float32, error)

// Document is the input shape for both Qdrant upserts and the SQLite mirror.
type Document struct {
	ID       string
	Content  string
	Metadata map[string]string
}

// EmbeddingFunction is the historical alias retained for one release while
// callers migrate. New code should use EmbeddingFunc directly.
type EmbeddingFunction = EmbeddingFunc

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

// Repository is the full search/index boundary implemented by qdrantRepository.
type Repository interface {
	Searcher
	WikiPageIndexer
	DocumentIndexer
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

// RebuildSQLiteWikiDocumentsWithDB is the caller-owned-pool variant.
func RebuildSQLiteWikiDocumentsWithDB(ctx context.Context, wikiDir string, db *sql.DB, logger *slog.Logger) (docsIndexed int, pagesIndexed int, err error) {
	sqlite, err := newSqliteSearcherWithDB(db, logger)
	if err != nil {
		return 0, 0, err
	}
	docs, pageCount, err := loadWikiDocuments(wikiDir, logger)
	if err != nil {
		return 0, 0, err
	}
	if err := indexSQLiteDocuments(ctx, sqlite, docs); err != nil {
		return 0, 0, err
	}
	return len(docs), pageCount, nil
}

func indexSQLiteDocuments(ctx context.Context, sqlite *sqliteSearcher, docs []Document) error {
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

func loadWikiDocuments(wikiDir string, logger *slog.Logger) ([]Document, int, error) {
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
	docs := make([]Document, 0, len(slugFiles)*3)
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
		// Payload metadata kept in Qdrant so search_memory can return more
		// than slug+snippet — the LLM previously had to round-trip via
		// list_files/read_file to find the on-disk file. The model itself
		// flagged this gap on 2026-05-11 turn 2393: "so cosa c'è scritto in
		// [[davide]] ma non so che file .md corrisponde". Including filepath,
		// updated_at, category, tags, related, and sources collapses that
		// detour into the same retrieval call.
		stat, _ := os.Stat(filePath)
		var sizeStr string
		if stat != nil {
			sizeStr = strconv.FormatInt(stat.Size(), 10)
		}
		meta := map[string]string{
			"slug":      slug,
			"title":     title,
			"kind":      "wiki_page",
			"filepath":  filepath.ToSlash(fi.name),
			"size":      sizeStr,
			"category":  strings.TrimSpace(page.Category),
			"tags":      strings.Join(page.Tags, ","),
			"related":   strings.Join(page.Related, ","),
			"sources":   strings.Join(page.Sources, ","),
		}
		if updated := strings.TrimSpace(page.Updated); updated != "" {
			meta["updated_at"] = updated
		}
		// Empty values stripped so the payload stays compact in Qdrant.
		for k, v := range meta {
			if v == "" {
				delete(meta, k)
			}
		}
		docs = append(docs, Document{
			ID:       slug,
			Content:  content,
			Metadata: meta,
		})
	}
	docs = append(docs, buildGraphDocuments(pages)...)
	return docs, len(pages), nil
}

// rrfMergeConstants for search.Result fusion. Mirrors the constants in
// internal/memoryindex (rrfK=60, exact/fts/vector weights 1.0/0.6/0.8).
// Group weights are positional: first group treated as the exact-match
// channel, second as FTS, third as vector. Extra groups beyond the third
// fall back to weight=0.5 so a caller that fans out into more channels
// still gets sane fusion without a code change here.
const (
	rrfK                  = 60
	rrfWeightExactResult  = 1.0
	rrfWeightFTSResult    = 0.6
	rrfWeightVectorResult = 0.8
	rrfWeightExtraResult  = 0.5
)

// mergeHybridResults fuses ranked Result groups via Reciprocal Rank Fusion.
// Each Result gets a fused score Σ_g (w_g / (k + rank_in_g + 1)). Results
// with the same (kind, slug) accumulate contributions across groups; the
// first-seen Result keeps its metadata. Returns the top-K by fused score.
//
// The Telegram-facing search call downstream of this still applies recency
// decay; this layer only handles the join. The fused score overwrites
// Result.Score so the recency multiplier sees a value comparable across
// backends.
func mergeHybridResults(_ string, topK int, groups ...[]Result) []Result {
	if topK <= 0 {
		topK = 5
	}
	type entry struct {
		result      Result
		score       float32
		scoreExact  float32
		scoreFTS    float32
		scoreVector float32
	}
	byKey := map[string]*entry{}
	for groupIdx, group := range groups {
		weight := rrfWeightExtraResult
		switch groupIdx {
		case 0:
			weight = rrfWeightExactResult
		case 1:
			weight = rrfWeightFTSResult
		case 2:
			weight = rrfWeightVectorResult
		}
		for rank, result := range group {
			key := resultKey(result)
			if key == "" {
				continue
			}
			contribution := float32(weight / float64(rrfK+rank+1))
			if existing, ok := byKey[key]; ok {
				existing.score += contribution
				switch groupIdx {
				case 0:
					existing.scoreExact += contribution
				case 1:
					existing.scoreFTS += contribution
				case 2:
					existing.scoreVector += contribution
				}
				continue
			}
			e := &entry{result: result, score: contribution}
			switch groupIdx {
			case 0:
				e.scoreExact = contribution
			case 1:
				e.scoreFTS = contribution
			case 2:
				e.scoreVector = contribution
			}
			byKey[key] = e
		}
	}
	out := make([]Result, 0, len(byKey))
	for _, e := range byKey {
		r := e.result
		r.Score = e.score
		r.ScoreExact = e.scoreExact
		r.ScoreFTS = e.scoreFTS
		r.ScoreVector = e.scoreVector
		out = append(out, r)
	}
	sortHybridResultsByScore(out)
	if len(out) > topK {
		out = out[:topK]
	}
	return out
}

// sortHybridResultsByScore orders fused results by descending Score with a
// stable tiebreaker on the canonical (kind, slug) key. Extracted so future
// callers that fuse out-of-band can reuse the ordering.
func sortHybridResultsByScore(results []Result) {
	sort.SliceStable(results, func(i, j int) bool {
		if results[i].Score == results[j].Score {
			return resultKey(results[i]) < resultKey(results[j])
		}
		return results[i].Score > results[j].Score
	})
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

// FormatResults formats search results as context for injection into LLM
// prompts. Includes a 200-char excerpt per hit.
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

func truncateExcerpt(content string, n int) string {
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

func findMDBodyEnd(content string) int {
	if !strings.HasPrefix(content, "---") {
		return -1
	}
	rest := content[3:]
	if len(rest) > 0 && rest[0] == '\n' {
		rest = rest[1:]
	} else if len(rest) > 1 && rest[0] == '\r' && rest[1] == '\n' {
		rest = rest[2:]
	}
	idx := strings.Index(rest, "\n---\n")
	if idx == -1 {
		idx = strings.Index(rest, "\n---\r\n")
	}
	if idx == -1 {
		return -1
	}
	return len(content) - len(rest) + idx + 5
}

func extractTitle(data []byte) string {
	var partial struct {
		Title string `yaml:"title"`
	}
	if err := yaml.Unmarshal(data, &partial); err != nil {
		return ""
	}
	return partial.Title
}

func metadataToJSON(metadata map[string]string) string {
	b, err := json.Marshal(metadata)
	if err != nil {
		return "{}"
	}
	return string(b)
}

func extractMetaField(metaJSON, field string) string {
	var m map[string]string
	if err := json.Unmarshal([]byte(metaJSON), &m); err != nil {
		return ""
	}
	return m[field]
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
