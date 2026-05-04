package search

import (
	"context"
	"database/sql"
	"fmt"
	"log/slog"
	"strings"

	auradb "github.com/aura/aura/internal/db"
)

// sqliteSearcher provides full-text search via SQLite + FTS5.
type sqliteSearcher struct {
	db     *sql.DB
	logger *slog.Logger
}

// newSqliteSearcher creates a SQLite-backed searcher.
func newSqliteSearcher(dbPath string, logger *slog.Logger) (*sqliteSearcher, error) {
	db, err := auradb.Open(dbPath)
	if err != nil {
		return nil, fmt.Errorf("opening sqlite connection: %w", err)
	}

	s, err := newSqliteSearcherWithDB(db, logger)
	if err != nil {
		_ = db.Close()
		return nil, err
	}
	return s, nil
}

func newSqliteSearcherWithDB(db *sql.DB, logger *slog.Logger) (*sqliteSearcher, error) {
	if db == nil {
		return nil, fmt.Errorf("sqlite searcher: db required")
	}
	if err := setupSqliteSchema(db); err != nil {
		return nil, fmt.Errorf("setting up sqlite schema: %w", err)
	}

	s := &sqliteSearcher{db: db, logger: logger}

	return s, nil
}

func setupSqliteSchema(db *sql.DB) error {
	_, err := db.Exec(`
		CREATE VIRTUAL TABLE IF NOT EXISTS wiki_documents
		USING fts5(id, content, metadata, title)
	`)
	if err != nil {
		return fmt.Errorf("creating FTS5 table: %w", err)
	}
	return nil
}

func (s *sqliteSearcher) clear(ctx context.Context) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM wiki_documents`)
	if err != nil {
		return fmt.Errorf("clearing wiki_documents: %w", err)
	}
	return nil
}

func (s *sqliteSearcher) indexDocument(ctx context.Context, id, content string, metadata map[string]string) error {
	metaJSON := metadataToJSON(metadata)
	title := metadata["title"]

	// FTS5 doesn't support ON CONFLICT — delete then insert
	_, err := s.db.ExecContext(ctx, `DELETE FROM wiki_documents WHERE id = ?`, id)
	if err != nil {
		return fmt.Errorf("deleting old document %s: %w", id, err)
	}

	_, err = s.db.ExecContext(ctx, `
		INSERT INTO wiki_documents (id, content, metadata, title)
		VALUES (?, ?, ?, ?)
	`, id, content, metaJSON, title)
	if err != nil {
		return fmt.Errorf("inserting document %s: %w", id, err)
	}
	return nil
}

func (s *sqliteSearcher) search(ctx context.Context, query string, topK int) ([]Result, error) {
	safeQuery := escapeFTS5Query(query)
	if safeQuery == "" {
		return nil, nil
	}

	rows, err := s.db.QueryContext(ctx, `
		SELECT id, content, metadata, title, rank
		FROM wiki_documents
		WHERE wiki_documents MATCH ?
		ORDER BY rank
		LIMIT ?
	`, safeQuery, topK)
	if err != nil {
		return nil, fmt.Errorf("sqlite FTS5 search: %w", err)
	}
	defer rows.Close()

	var results []Result
	for rows.Next() {
		var id, content, metaJSON, title string
		var rank float64
		if err := rows.Scan(&id, &content, &metaJSON, &title, &rank); err != nil {
			s.logger.Warn("scanning search result", "error", err)
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

		results = append(results, Result{
			Kind:    kind,
			Slug:    slug,
			Title:   title,
			Content: content,
			Score:   float32(-rank),
		})
	}

	return results, rows.Err()
}

// escapeFTS5Query strips FTS5 special characters and operators from a query string.
// FTS5 treats ?, *, ", (, ), {, } as special syntax which causes errors if unescaped.
func escapeFTS5Query(query string) string {
	replacer := strings.NewReplacer(
		"?", " ",
		"*", " ",
		"\"", " ",
		"(", " ",
		")", " ",
		"{", " ",
		"}", " ",
		":", " ",
	)
	escaped := replacer.Replace(query)
	fields := strings.Fields(escaped)
	if len(fields) == 0 {
		return ""
	}
	return strings.Join(fields, " OR ")
}
