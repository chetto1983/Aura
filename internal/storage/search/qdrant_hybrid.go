package search

import (
	"context"
	"database/sql"
	"fmt"
	"log/slog"
	"strconv"
	"strings"
	"sync"

	"github.com/aura/aura/internal/stringx"
)

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

// exactMatchDB returns pages from wiki_documents whose title contains any
// diacritic-stripped query token (case-insensitive substring match).
// ScoreExact is 1.0; missing pages get no entry (not score 0.0).
func exactMatchDB(ctx context.Context, db *sql.DB, query string, topK int, logger *slog.Logger) []Result {
	tokens := significantSearchTerms(query)
	if len(tokens) == 0 {
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

	var results []Result
	for rows.Next() {
		var id, content, metaJSON, title string
		if err := rows.Scan(&id, &content, &metaJSON, &title); err != nil {
			continue
		}
		titleStripped := stringx.StripDiacritics(strings.ToLower(title))
		hit := false
		for _, tok := range tokens {
			if strings.Contains(titleStripped, tok) {
				hit = true
				break
			}
		}
		if !hit {
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
			Score:         1.0,
			ScoreExact:    1.0,
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
		if len(results) >= topK {
			break
		}
	}
	return results
}
