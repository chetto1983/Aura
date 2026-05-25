// Package search is Aura's read-side wiki retrieval. Qdrant is the only vector
// backend (the in-memory chromem-go path that used to live here was deleted
// once Qdrant became required infrastructure). SQLite FTS5 remains as the
// keyword-side companion: every wiki page is mirrored there at index time.
//
// Hybrid fusion state (2026-05-21): mergeHybridResults below implements
// reciprocal-rank fusion across ScoreExact + ScoreFTS + ScoreVector, but
// qdrantRepository.Search ships vector-only — only compact-memory currently
// drives the hybrid path. Wiki search through qdrantRepository.Search hits
// Qdrant cosine alone. Phase-WIKI-B US-WIKI-B02 wires FTS5 + exact into the
// wiki path so the fusion actually fuses for the wiki use case too.
package search

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"github.com/aura/aura/internal/wiki"
)

// RebuildSQLiteWikiDocuments rebuilds the SQLite FTS mirror from the wiki
// files without requiring an embedding model. It is used by closure/debug
// flows after deterministic wiki cleanup so the manifest can be verified in
// one pass even when vector embeddings are unavailable.
func RebuildSQLiteWikiDocuments(ctx context.Context, wikiDir, dbPath string, logger *slog.Logger) (docsIndexed int, pagesIndexed int, err error) {
	sqlite, err := newSqliteSearcher(dbPath, logger)
	if err != nil {
		return 0, 0, err
	}
	defer func() {
		if closeErr := sqlite.Close(); closeErr != nil && err == nil {
			err = closeErr
		}
	}()

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
	for attempt := range 2 {
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

type wikiFileInfo struct {
	name string
	ext  string
}

func loadWikiPageDocument(wikiDir, slug string, _ *slog.Logger) (Document, bool, error) {
	slug = strings.TrimSpace(slug)
	if slug == "" {
		return Document{}, false, fmt.Errorf("wiki slug is required")
	}
	if wiki.IsOperationalSlug(slug) {
		return Document{}, false, nil
	}
	for _, fi := range []wikiFileInfo{
		{name: slug + ".md", ext: ".md"},
		{name: slug + ".yaml", ext: ".yaml"},
	} {
		filePath := filepath.Join(wikiDir, fi.name)
		data, err := os.ReadFile(filePath)
		if err != nil {
			if os.IsNotExist(err) {
				continue
			}
			return Document{}, false, fmt.Errorf("reading wiki page %s: %w", slug, err)
		}
		page, err := parseIndexedWikiPage(slug, fi.ext, data)
		if err != nil {
			return Document{}, false, fmt.Errorf("parsing wiki page %s: %w", slug, err)
		}
		return wikiDocumentFromPage(slug, fi, filePath, page), true, nil
	}
	return Document{}, false, nil
}

func loadWikiDocuments(wikiDir string, logger *slog.Logger) ([]Document, int, error) {
	if logger == nil {
		logger = slog.Default()
	}
	entries, err := os.ReadDir(wikiDir)
	if err != nil {
		return nil, 0, fmt.Errorf("reading wiki directory: %w", err)
	}

	slugFiles := make(map[string]wikiFileInfo)
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		name := entry.Name()
		var slug, ext string
		if s, ok := strings.CutSuffix(name, ".md"); ok {
			slug, ext = s, ".md"
		} else if s, ok := strings.CutSuffix(name, ".yaml"); ok {
			slug, ext = s, ".yaml"
		} else {
			continue
		}
		if wiki.IsOperationalSlug(slug) {
			continue
		}
		if existing, ok := slugFiles[slug]; ok && existing.ext == ".md" {
			continue
		}
		slugFiles[slug] = wikiFileInfo{name: name, ext: ext}
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
		stat, _ := os.Stat(filePath)
		var sizeStr string
		if stat != nil {
			sizeStr = strconv.FormatInt(stat.Size(), 10)
		}
		docs = append(docs, Document{
			ID:       slug,
			Content:  content,
			Metadata: buildWikiMeta(slug, title, sizeStr, fi, page),
		})
	}
	docs = append(docs, buildGraphDocuments(pages)...)
	return docs, len(pages), nil
}

func wikiDocumentFromPage(slug string, fi wikiFileInfo, filePath string, page indexedWikiPage) Document {
	title, content := page.Title, page.Title+"\n"+page.Body
	stat, _ := os.Stat(filePath)
	var sizeStr string
	if stat != nil {
		sizeStr = strconv.FormatInt(stat.Size(), 10)
	}
	return Document{
		ID:       slug,
		Content:  content,
		Metadata: buildWikiMeta(slug, title, sizeStr, fi, page),
	}
}

// buildWikiMeta constructs the Qdrant/SQLite metadata payload for a single
// wiki page. sizeStr is the decimal byte count from os.Stat; pass "" when
// unknown. Empty strings and schema_version=0 are stripped so the payload
// stays compact.
func buildWikiMeta(slug, title, sizeStr string, fi wikiFileInfo, page indexedWikiPage) map[string]string {
	meta := map[string]string{
		"slug":           slug,
		"title":          title,
		"kind":           "wiki_page",
		"filepath":       filepath.ToSlash(fi.name),
		"size":           sizeStr,
		"category":       strings.TrimSpace(page.Category),
		"tags":           strings.Join(page.Tags, ","),
		"related":        strings.Join(page.Related, ","),
		"sources":        strings.Join(page.Sources, ","),
		"schema_version": strconv.Itoa(page.SchemaVersion),
		"prompt_version": strings.TrimSpace(page.PromptVersion),
		"created_at":     strings.TrimSpace(page.Created),
	}
	if page.Unversioned {
		meta["unversioned"] = "true"
	}
	if updated := strings.TrimSpace(page.Updated); updated != "" {
		meta["updated_at"] = updated
	}
	for k, v := range meta {
		if v == "" || (k == "schema_version" && v == "0") {
			delete(meta, k)
		}
	}
	return meta
}

// mergeHybridResults fuses ranked Result groups via Reciprocal Rank Fusion,
// then normalises every component into [0,1] so the LLM-facing score reads
// as a confidence, not a raw RRF fraction.
//
// Raw RRF gives each Result Σ_g (w_g / (k + rank_in_g + 1)). With k=60 and
// weights {1.0, 0.6, 0.8} the maximum possible fused score is 2.4/61 ≈ 0.0393
// — a perfect top-1 hit across all three channels. A naïve display surfaces
// that as score=0.04, which the agent reads as "almost zero" and over-searches
// to compensate (observed in chat 1148481707 turns 263–286, four redundant
// search calls for the same query). Dividing every component by the same
// rrfMaxScore preserves component additivity (Score == Σ components) and
// rescales the perfect hit to 1.00.
//
// Recency decay is applied downstream in memory_search.go; this layer only
// handles the join.
func mergeHybridResults(query string, topK int, groups ...[]Result) []Result {
	if topK <= 0 {
		topK = 5
	}
	pinned, groups := splitPinnedExactSlug(query, groups)
	type entry struct {
		result      Result
		score       float32
		scoreExact  float32
		scoreFTS    float32
		scoreVector float32
	}
	byKey := map[string]*entry{}
	totalWeight := 0.0
	for groupIdx, group := range groups {
		weight := rrfGroupWeight(groupIdx)
		totalWeight += weight
		for rank, result := range group {
			key := resultKey(result)
			if key == "" {
				continue
			}
			contribution := float32(weight / float64(rrfK+rank+1))
			existing, ok := byKey[key]
			if !ok {
				existing = &entry{result: result}
				byKey[key] = existing
			}
			existing.score += contribution
			switch groupIdx {
			case 0:
				existing.scoreExact += contribution
			case 1:
				existing.scoreFTS += contribution
			case 2:
				existing.scoreVector += contribution
			}
		}
	}
	maxRRF := float32(totalWeight / float64(rrfK+1))
	if maxRRF <= 0 {
		maxRRF = 1
	}
	out := make([]Result, 0, len(byKey))
	for _, e := range byKey {
		r := e.result
		r.Score = e.score / maxRRF
		r.ScoreExact = e.scoreExact / maxRRF
		r.ScoreFTS = e.scoreFTS / maxRRF
		r.ScoreVector = e.scoreVector / maxRRF
		out = append(out, r)
	}
	sortHybridResultsByScore(out)
	if pinned != nil {
		pin := *pinned
		pin.Score = 1
		pin.ScoreExact = 1
		pin.ScoreFTS = 0
		pin.ScoreVector = 0
		out = append([]Result{pin}, out...)
	}
	if len(out) > topK {
		out = out[:topK]
	}
	return out
}

func splitPinnedExactSlug(query string, groups [][]Result) (*Result, [][]Result) {
	if len(groups) == 0 {
		return nil, groups
	}
	target := normalizeExactSlugish(query)
	if target == "" {
		return nil, groups
	}
	var pinned *Result
	for _, result := range groups[0] {
		if normalizeExactSlugish(result.Slug) == target {
			copied := result
			pinned = &copied
			break
		}
	}
	if pinned == nil {
		return nil, groups
	}
	pinKey := resultKey(*pinned)
	filtered := make([][]Result, len(groups))
	for i, group := range groups {
		for _, result := range group {
			if resultKey(result) != pinKey {
				filtered[i] = append(filtered[i], result)
			}
		}
	}
	return pinned, filtered
}

// rrfGroupWeight returns the RRF channel weight for a group at the given
// positional index. Group 0 is the exact-match channel, 1 is FTS, 2 is
// vector; any further groups fall back to a smaller "extra" weight so an
// over-fanning caller still gets sane fusion without code changes here.
func rrfGroupWeight(groupIdx int) float64 {
	switch groupIdx {
	case 0:
		return rrfWeightExactResult
	case 1:
		return rrfWeightFTSResult
	case 2:
		return rrfWeightVectorResult
	default:
		return rrfWeightExtraResult
	}
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
			fmt.Fprintf(&sb, "- %s %s\n", label, r.Title)
		} else {
			fmt.Fprintf(&sb, "- [%s] %s %s\n", kind, label, r.Title)
		}
		excerpt := truncateExcerpt(r.Content, 200)
		if excerpt != "" {
			fmt.Fprintf(&sb, "  %s\n", excerpt)
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
