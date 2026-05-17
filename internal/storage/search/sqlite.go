package search

import (
	"context"
	"database/sql"
	"fmt"
	"log/slog"
	"strconv"
	"strings"
	"unicode"

	auradb "github.com/aura/aura/internal/db"
	"github.com/aura/aura/internal/db/migrations"
)

// sqliteSearcher provides full-text search via SQLite + FTS5.
type sqliteSearcher struct {
	db     *sql.DB
	logger *slog.Logger
	owned  bool
}

// newSqliteSearcher creates a SQLite-backed searcher.
func newSqliteSearcher(dbPath string, logger *slog.Logger) (*sqliteSearcher, error) {
	db, err := auradb.Open(dbPath)
	if err != nil {
		return nil, fmt.Errorf("opening sqlite connection: %w", err)
	}
	if err := migrations.Run(context.Background(), db); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("migrating sqlite schema: %w", err)
	}

	s, err := newSqliteSearcherWithDB(db, logger)
	if err != nil {
		_ = db.Close()
		return nil, err
	}
	s.owned = true
	return s, nil
}

func newSqliteSearcherWithDB(db *sql.DB, logger *slog.Logger) (*sqliteSearcher, error) {
	if db == nil {
		return nil, fmt.Errorf("sqlite searcher: db required")
	}

	s := &sqliteSearcher{db: db, logger: logger, owned: false}

	return s, nil
}

func (s *sqliteSearcher) Close() error {
	if s == nil || !s.owned {
		return nil
	}
	return s.db.Close()
}

func (s *sqliteSearcher) clear(ctx context.Context) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM wiki_documents`)
	if err != nil {
		return fmt.Errorf("clearing wiki_documents: %w", err)
	}
	return nil
}

func (s *sqliteSearcher) clearOrRecreate(ctx context.Context) error {
	if err := s.clear(ctx); err != nil {
		if recoverableWikiDocumentsError(err) {
			return s.recreateWikiDocuments(ctx)
		}
		return err
	}
	return nil
}

func (s *sqliteSearcher) recreateWikiDocuments(ctx context.Context) error {
	if _, err := s.db.ExecContext(ctx, `DROP TABLE IF EXISTS wiki_documents`); err != nil {
		if !recoverableWikiDocumentsError(err) {
			return fmt.Errorf("dropping wiki_documents: %w", err)
		}
		if repairErr := s.forgetWikiDocumentsSchema(ctx); repairErr != nil {
			return fmt.Errorf("dropping wiki_documents: %w; forced schema repair failed: %v", err, repairErr)
		}
	}
	if _, err := s.db.ExecContext(ctx, `CREATE VIRTUAL TABLE wiki_documents USING fts5(id, content, metadata, title)`); err != nil {
		return fmt.Errorf("creating wiki_documents: %w", err)
	}
	return nil
}

func (s *sqliteSearcher) forgetWikiDocumentsSchema(ctx context.Context) error {
	var version int
	if err := s.db.QueryRowContext(ctx, `PRAGMA schema_version`).Scan(&version); err != nil {
		return fmt.Errorf("reading schema_version: %w", err)
	}
	if _, err := s.db.ExecContext(ctx, `PRAGMA writable_schema=ON`); err != nil {
		return fmt.Errorf("enable writable_schema: %w", err)
	}
	if _, err := s.db.ExecContext(ctx, `
		DELETE FROM sqlite_schema
		WHERE name = 'wiki_documents'
		   OR name LIKE 'wiki_documents_%'
		   OR tbl_name = 'wiki_documents'
	`); err != nil {
		_, _ = s.db.ExecContext(ctx, `PRAGMA writable_schema=OFF`)
		return fmt.Errorf("delete wiki_documents schema rows: %w", err)
	}
	if _, err := s.db.ExecContext(ctx, fmt.Sprintf(`PRAGMA schema_version=%d`, version+1)); err != nil {
		_, _ = s.db.ExecContext(ctx, `PRAGMA writable_schema=OFF`)
		return fmt.Errorf("bump schema_version: %w", err)
	}
	if _, err := s.db.ExecContext(ctx, `PRAGMA writable_schema=OFF`); err != nil {
		return fmt.Errorf("disable writable_schema: %w", err)
	}
	if _, err := s.db.ExecContext(ctx, `VACUUM`); err != nil {
		return fmt.Errorf("vacuum after wiki_documents schema repair: %w", err)
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

func recoverableWikiDocumentsError(err error) bool {
	if err == nil {
		return false
	}
	msg := strings.ToLower(err.Error())
	return strings.Contains(msg, "database disk image is malformed") ||
		strings.Contains(msg, "corruption") ||
		strings.Contains(msg, "has no column named") ||
		strings.Contains(msg, "no such table: wiki_documents") ||
		strings.Contains(msg, "not an fts5")
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
		updatedAt, _ := parseSearchPayloadTime(extractMetaField(metaJSON, "updated_at"))
		size, _ := strconv.ParseInt(extractMetaField(metaJSON, "size"), 10, 64)

		results = append(results, Result{
			Kind:      kind,
			Slug:      slug,
			Title:     title,
			Content:   content,
			Score:     float32(-rank),
			UpdatedAt: updatedAt,
			FilePath:  extractMetaField(metaJSON, "filepath"),
			Category:  extractMetaField(metaJSON, "category"),
			Tags:      splitCSVPayloadField(extractMetaField(metaJSON, "tags")),
			Related:   splitCSVPayloadField(extractMetaField(metaJSON, "related")),
			Sources:   splitCSVPayloadField(extractMetaField(metaJSON, "sources")),
			SizeBytes: size,
		})
	}

	return results, rows.Err()
}

// escapeFTS5Query converts free-form user text into simple FTS5 tokens.
// FTS5 query syntax is operator-rich, so punctuation such as /, comma, and
// apostrophes must not be passed through as query syntax.
func escapeFTS5Query(query string) string {
	fields := strings.FieldsFunc(query, func(r rune) bool {
		return !unicode.IsLetter(r) && !unicode.IsDigit(r) && r != '_'
	})
	if len(fields) == 0 {
		return ""
	}
	out := make([]string, 0, len(fields))
	seen := make(map[string]bool, len(fields))
	for _, field := range fields {
		field = strings.ToLower(strings.TrimSpace(field))
		if field == "" || seen[field] {
			continue
		}
		seen[field] = true
		out = append(out, field)
	}
	return strings.Join(out, " OR ")
}
