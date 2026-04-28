package search

import (
	"context"
	"database/sql"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"

	_ "modernc.org/sqlite"
)

// sqliteSearcher provides full-text search via SQLite + FTS5.
type sqliteSearcher struct {
	db     *sql.DB
	logger *slog.Logger
}

// newSqliteSearcher creates a SQLite-backed searcher.
func newSqliteSearcher(dbPath string, logger *slog.Logger) (*sqliteSearcher, error) {
	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		return nil, fmt.Errorf("opening sqlite connection: %w", err)
	}

	if err := db.Ping(); err != nil {
		db.Close()
		return nil, fmt.Errorf("pinging sqlite database: %w", err)
	}

	if err := setupSqliteSchema(db); err != nil {
		db.Close()
		return nil, fmt.Errorf("setting up sqlite schema: %w", err)
	}

	s := &sqliteSearcher{db: db, logger: logger}

	if err := s.createConversationsTable(); err != nil {
		db.Close()
		return nil, fmt.Errorf("creating conversations table: %w", err)
	}

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
	rows, err := s.db.QueryContext(ctx, `
		SELECT id, content, metadata, title, rank
		FROM wiki_documents
		WHERE wiki_documents MATCH ?
		ORDER BY rank
		LIMIT ?
	`, query, topK)
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

		results = append(results, Result{
			Slug:    slug,
			Title:   title,
			Content: content,
			Score:   float32(-rank),
		})
	}

	return results, rows.Err()
}

func (s *sqliteSearcher) indexWikiDir(ctx context.Context, wikiDir string, logger *slog.Logger) (int, error) {
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
			logger.Warn("failed to read wiki page for sqlite indexing", "slug", slug, "error", err)
			continue
		}

		title := extractTitle(data)
		content := title + "\n" + string(data)

		if err := s.indexDocument(ctx, slug, content, map[string]string{"slug": slug, "title": title}); err != nil {
			logger.Warn("failed to index in sqlite", "slug", slug, "error", err)
			continue
		}
		count++
	}

	return count, nil
}

// StoreConversation saves a conversation message to SQLite.
func (s *sqliteSearcher) StoreConversation(ctx context.Context, userID, role, content string) error {
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO conversations (user_id, role, content)
		VALUES (?, ?, ?)
	`, userID, role, content)
	return err
}

// createConversationsTable ensures the conversations table exists.
func (s *sqliteSearcher) createConversationsTable() error {
	_, err := s.db.Exec(`
		CREATE TABLE IF NOT EXISTS conversations (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			user_id TEXT NOT NULL,
			role TEXT NOT NULL,
			content TEXT NOT NULL,
			created_at DATETIME DEFAULT CURRENT_TIMESTAMP
		)
	`)
	return err
}
