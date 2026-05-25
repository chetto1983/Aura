package search

import (
	"context"
	"database/sql"
	"fmt"
	"log/slog"
	"math"
	"sort"
	"strconv"
	"strings"
	"sync"
	"unicode"

	"github.com/aura/aura/internal/stringx"
)

type exactMatchRow struct {
	id       string
	content  string
	metaJSON string
	title    string
	slug     string
	kind     string
}

// SearchHybrid fuses three signal channels via RRF:
//   - exact: title substring match with diacritic-strip (boolean, 1.0 hit)
//   - fts:   FTS5 BM25 from the wiki_documents SQLite mirror
//   - vector: cosine similarity from Qdrant
//
// Falls back to vector-only when db is nil (built via NewQdrantRepository).
func (r *qdrantRepository) SearchHybrid(ctx context.Context, query string, topK int) ([]Result, error) {
	if r.db == nil {
		return r.primary.Search(ctx, query, topK)
	}
	if topK <= 0 {
		topK = 5
	}
	fetch := topK * 2

	var (
		vectorResults []Result
		ftsResults    []Result
		exactResults  []Result
		vectorErr     error
		ftsErr        error
	)
	var wg sync.WaitGroup
	wg.Add(3)
	go func() {
		defer wg.Done()
		vectorResults, vectorErr = r.primary.Search(ctx, query, fetch)
	}()
	go func() {
		defer wg.Done()
		ftsResults, ftsErr = ftsSearchDB(ctx, r.db, query, fetch, r.logger)
	}()
	go func() {
		defer wg.Done()
		exactResults = exactMatchDB(ctx, r.db, query, fetch, r.logger)
	}()
	wg.Wait()

	if vectorErr != nil {
		return nil, vectorErr
	}
	if ftsErr != nil {
		r.logger.Warn("hybrid wiki search: FTS5 failed; using vector-only", "error", ftsErr)
		return vectorResults, nil
	}
	return mergeHybridResults(query, topK, exactResults, ftsResults, vectorResults), nil
}

// ftsSearchDB queries the wiki_documents FTS5 mirror using BM25. Re-uses the
// escapeFTS5Query tokeniser already used by sqliteSearcher.
func ftsSearchDB(ctx context.Context, db *sql.DB, query string, topK int, logger *slog.Logger) ([]Result, error) {
	safeQuery := escapeFTS5Query(query)
	if safeQuery == "" {
		return nil, nil
	}
	rows, err := db.QueryContext(ctx, `
		SELECT id, content, metadata, title, rank
		FROM wiki_documents
		WHERE wiki_documents MATCH ?
		ORDER BY rank
		LIMIT ?
	`, safeQuery, topK)
	if err != nil {
		return nil, fmt.Errorf("hybrid FTS5 search: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var results []Result
	for rows.Next() {
		var id, content, metaJSON, title string
		var rank float64
		if err := rows.Scan(&id, &content, &metaJSON, &title, &rank); err != nil {
			if logger != nil {
				logger.Warn("hybrid: scanning FTS5 result", "error", err)
			}
			continue
		}
		slug := extractMetaField(metaJSON, "slug")
		if slug == "" {
			slug = id
		}
		kind := extractMetaField(metaJSON, "kind")
		if kind == "" {
			kind = "wiki_page"
		}
		updatedAt, _ := parseSearchPayloadTime(extractMetaField(metaJSON, "updated_at"))
		createdAt, _ := parseSearchPayloadTime(extractMetaField(metaJSON, "created_at"))
		schemaVersion, _ := strconv.Atoi(strings.TrimSpace(extractMetaField(metaJSON, "schema_version")))
		size, _ := strconv.ParseInt(extractMetaField(metaJSON, "size"), 10, 64)
		results = append(results, Result{
			Kind:          kind,
			Slug:          slug,
			Title:         title,
			Content:       content,
			Score:         float32(-rank),
			UpdatedAt:     updatedAt,
			CreatedAt:     createdAt,
			SchemaVersion: schemaVersion,
			PromptVersion: extractMetaField(metaJSON, "prompt_version"),
			Unversioned:   parseSearchPayloadBool(extractMetaField(metaJSON, "unversioned")),
			FilePath:      extractMetaField(metaJSON, "filepath"),
			Category:      extractMetaField(metaJSON, "category"),
			Tags:          splitCSVPayloadField(extractMetaField(metaJSON, "tags")),
			Related:       splitCSVPayloadField(extractMetaField(metaJSON, "related")),
			Sources:       splitCSVPayloadField(extractMetaField(metaJSON, "sources")),
			SizeBytes:     size,
		})
	}
	return results, rows.Err()
}

// exactMatchDB returns pages from wiki_documents whose slug or title matches
// the query through a tiered exact/prefix/substring scorer.
func exactMatchDB(ctx context.Context, db *sql.DB, query string, topK int, logger *slog.Logger) []Result {
	terms := exactSearchTerms(query)
	if len(terms) == 0 {
		return nil
	}

	// Fetch all page rows; ~60 pages, cheap.
	rows, err := db.QueryContext(ctx, `
		SELECT id, content, metadata, title
		FROM wiki_documents
	`)
	if err != nil {
		if logger != nil {
			logger.Warn("hybrid: exact match fetch failed", "error", err)
		}
		return nil
	}
	defer func() { _ = rows.Close() }()

	var allRows []exactMatchRow
	for rows.Next() {
		var id, content, metaJSON, title string
		if err := rows.Scan(&id, &content, &metaJSON, &title); err != nil {
			continue
		}
		slug := extractMetaField(metaJSON, "slug")
		if slug == "" {
			slug = id
		}
		kind := extractMetaField(metaJSON, "kind")
		if kind == "" {
			kind = "wiki_page"
		}
		allRows = append(allRows, exactMatchRow{
			id:       id,
			content:  content,
			metaJSON: metaJSON,
			title:    title,
			slug:     slug,
			kind:     kind,
		})
	}
	if len(allRows) == 0 {
		return nil
	}

	idf := exactTermIDF(terms, allRows)
	type candidate struct {
		result Result
		raw    float64
	}
	candidates := make([]candidate, 0, len(allRows))
	for _, row := range allRows {
		raw := exactTierScore(row.slug, row.title, terms, idf)
		if raw <= 0 {
			continue
		}
		updatedAt, _ := parseSearchPayloadTime(extractMetaField(row.metaJSON, "updated_at"))
		createdAt, _ := parseSearchPayloadTime(extractMetaField(row.metaJSON, "created_at"))
		schemaVersion, _ := strconv.Atoi(strings.TrimSpace(extractMetaField(row.metaJSON, "schema_version")))
		size, _ := strconv.ParseInt(extractMetaField(row.metaJSON, "size"), 10, 64)
		candidates = append(candidates, candidate{
			raw: raw,
			result: Result{
				Kind:          row.kind,
				Slug:          row.slug,
				Title:         row.title,
				Content:       row.content,
				UpdatedAt:     updatedAt,
				CreatedAt:     createdAt,
				SchemaVersion: schemaVersion,
				PromptVersion: extractMetaField(row.metaJSON, "prompt_version"),
				Unversioned:   parseSearchPayloadBool(extractMetaField(row.metaJSON, "unversioned")),
				FilePath:      extractMetaField(row.metaJSON, "filepath"),
				Category:      extractMetaField(row.metaJSON, "category"),
				Tags:          splitCSVPayloadField(extractMetaField(row.metaJSON, "tags")),
				Related:       splitCSVPayloadField(extractMetaField(row.metaJSON, "related")),
				Sources:       splitCSVPayloadField(extractMetaField(row.metaJSON, "sources")),
				SizeBytes:     size,
			},
		})
	}
	sort.SliceStable(candidates, func(i, j int) bool {
		if candidates[i].raw == candidates[j].raw {
			return resultKey(candidates[i].result) < resultKey(candidates[j].result)
		}
		return candidates[i].raw > candidates[j].raw
	})
	if topK > 0 && len(candidates) > topK {
		candidates = candidates[:topK]
	}
	if len(candidates) == 0 {
		return nil
	}
	maxRaw := candidates[0].raw
	results := make([]Result, 0, len(candidates))
	for _, c := range candidates {
		score := float32(c.raw / maxRaw)
		c.result.Score = score
		c.result.ScoreExact = score
		results = append(results, c.result)
	}
	return results
}

func exactSearchTerms(query string) []string {
	terms := significantSearchTerms(query)
	if slug := normalizeExactSlugish(query); len(slug) >= 2 {
		terms = append([]string{slug}, terms...)
	}
	out := make([]string, 0, len(terms))
	seen := make(map[string]bool, len(terms))
	for _, term := range terms {
		term = strings.TrimSpace(term)
		if len(term) < 2 || seen[term] {
			continue
		}
		seen[term] = true
		out = append(out, term)
	}
	return out
}

func exactTermIDF(terms []string, rows []exactMatchRow) map[string]float64 {
	out := make(map[string]float64, len(terms))
	n := float64(len(rows))
	for _, term := range terms {
		var df float64
		for _, row := range rows {
			if exactTermTier(row.slug, row.title, term) > 0 {
				df++
			}
		}
		out[term] = math.Log(1 + n/(1+df))
	}
	return out
}

func exactTierScore(slug, title string, terms []string, idf map[string]float64) float64 {
	var score float64
	for _, term := range terms {
		tier := exactTermTier(slug, title, term)
		if tier == 0 {
			continue
		}
		score += float64(tier) * idf[term]
	}
	return score
}

func exactTermTier(slug, title, term string) int {
	slugNorm := normalizeExactSlugish(slug)
	titleNorm := normalizeExactText(title)
	titleSlug := normalizeExactSlugish(title)
	switch {
	case slugNorm == term || titleNorm == term || titleSlug == term:
		return 1000
	case strings.HasPrefix(slugNorm, term) || strings.HasPrefix(titleNorm, term) || strings.HasPrefix(titleSlug, term):
		return 100
	case strings.Contains(slugNorm, term) || strings.Contains(titleNorm, term) || strings.Contains(titleSlug, term):
		return 1
	default:
		return 0
	}
}

func normalizeExactText(s string) string {
	s = stringx.StripDiacritics(strings.ToLower(s))
	return strings.Join(strings.Fields(s), " ")
}

func normalizeExactSlugish(s string) string {
	s = stringx.StripDiacritics(strings.ToLower(s))
	var b strings.Builder
	lastSep := false
	for _, r := range s {
		switch {
		case unicode.IsLetter(r) || unicode.IsDigit(r):
			b.WriteRune(r)
			lastSep = false
		case b.Len() > 0 && !lastSep:
			b.WriteByte('-')
			lastSep = true
		}
	}
	return strings.Trim(b.String(), "-")
}
