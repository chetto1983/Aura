package search

import (
	"context"
	"database/sql"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"

	_ "github.com/jackc/pgx/v5/stdlib"
)

// pgvectorSearcher provides vector search via PostgreSQL + pgvector.
type pgvectorSearcher struct {
	db     *sql.DB
	logger *slog.Logger
}

// newPgvectorSearcher creates a pgvector-backed searcher.
func newPgvectorSearcher(connString string, logger *slog.Logger) (*pgvectorSearcher, error) {
	db, err := sql.Open("pgx", connString)
	if err != nil {
		return nil, fmt.Errorf("opening pgvector connection: %w", err)
	}

	if err := db.Ping(); err != nil {
		db.Close()
		return nil, fmt.Errorf("pinging pgvector database: %w", err)
	}

	// Ensure pgvector extension and table exist
	if err := setupPgvectorSchema(db); err != nil {
		db.Close()
		return nil, fmt.Errorf("setting up pgvector schema: %w", err)
	}

	return &pgvectorSearcher{db: db, logger: logger}, nil
}

func setupPgvectorSchema(db *sql.DB) error {
	// Create extension if not exists
	if _, err := db.Exec(`CREATE EXTENSION IF NOT EXISTS vector`); err != nil {
		return fmt.Errorf("creating vector extension: %w", err)
	}

	// Create wiki_documents table if not exists
	_, err := db.Exec(`
		CREATE TABLE IF NOT EXISTS wiki_documents (
			id TEXT PRIMARY KEY,
			content TEXT NOT NULL,
			metadata JSONB DEFAULT '{}',
			embedding vector(1536)
		)
	`)
	if err != nil {
		return fmt.Errorf("creating wiki_documents table: %w", err)
	}

	return nil
}

func (pg *pgvectorSearcher) indexDocument(ctx context.Context, id, content string, metadata map[string]string) error {
	// Store document without embedding for now (embedding is done by the primary chromem engine)
	// The pgvector fallback stores documents and can do text-based search
	// Full vector search requires the embedding to be generated and stored separately
	metaJSON := metadataToJSON(metadata)

	_, err := pg.db.ExecContext(ctx, `
		INSERT INTO wiki_documents (id, content, metadata)
		VALUES ($1, $2, $3)
		ON CONFLICT (id) DO UPDATE SET content = $2, metadata = $3
	`, id, content, metaJSON)

	if err != nil {
		return fmt.Errorf("upserting document %s: %w", id, err)
	}

	return nil
}

func (pg *pgvectorSearcher) search(ctx context.Context, query string, topK int) ([]Result, error) {
	// Text-based search as fallback (since embeddings require the embedding function)
	// This uses PostgreSQL full-text search on the content column
	rows, err := pg.db.QueryContext(ctx, `
		SELECT id, content, metadata,
			   ts_rank_cd(to_tsvector('english', content), plainto_tsquery('english', $1)) AS score
		FROM wiki_documents
		WHERE to_tsvector('english', content) @@ plainto_tsquery('english', $1)
		ORDER BY score DESC
		LIMIT $2
	`, query, topK)

	if err != nil {
		return nil, fmt.Errorf("pgvector search: %w", err)
	}
	defer rows.Close()

	var results []Result
	for rows.Next() {
		var id, content string
		var metaJSON string
		var score float32
		if err := rows.Scan(&id, &content, &metaJSON, &score); err != nil {
			pg.logger.Warn("scanning search result", "error", err)
			continue
		}

		title := extractMetaField(metaJSON, "title")
		slug := extractMetaField(metaJSON, "slug")

		results = append(results, Result{
			Slug:    slug,
			Title:   title,
			Content: content,
			Score:   score,
		})
	}

	return results, rows.Err()
}

func (pg *pgvectorSearcher) indexWikiDir(ctx context.Context, wikiDir string, logger *slog.Logger) (int, error) {
	entries, err := os.ReadDir(wikiDir)
	if err != nil {
		return 0, fmt.Errorf("reading wiki directory: %w", err)
	}

	count := 0
	for _, entry := range entries {
		if entry.IsDir() || filepath.Ext(entry.Name()) != ".yaml" {
			continue
		}

		slug := entry.Name()[:len(entry.Name())-5]
		filePath := filepath.Join(wikiDir, entry.Name())

		data, err := os.ReadFile(filePath)
		if err != nil {
			logger.Warn("failed to read wiki page for pgvector indexing", "slug", slug, "error", err)
			continue
		}

		title := extractTitle(data)
		content := title + "\n" + string(data)

		if err := pg.indexDocument(ctx, slug, content, map[string]string{"slug": slug, "title": title}); err != nil {
			logger.Warn("failed to index in pgvector", "slug", slug, "error", err)
			continue
		}
		count++
	}

	return count, nil
}

func metadataToJSON(m map[string]string) string {
	if m == nil {
		return "{}"
	}
	parts := make([]string, 0, len(m))
	for k, v := range m {
		parts = append(parts, fmt.Sprintf(`"%s":"%s"`, k, v))
	}
	return "{" + strings.Join(parts, ",") + "}"
}

func extractMetaField(jsonStr, field string) string {
	// Simple JSON field extraction without importing encoding/json
	needle := `"` + field + `":"`
	idx := strings.Index(jsonStr, needle)
	if idx == -1 {
		return ""
	}
	start := idx + len(needle)
	end := strings.Index(jsonStr[start:], `"`)
	if end == -1 {
		return jsonStr[start:]
	}
	return jsonStr[start : start+end]
}
