package search

import (
	"context"
	"database/sql"
	"fmt"
	"log/slog"
	"strconv"
	"strings"

	"github.com/aura/aura/internal/wiki"
)

// WikiFTS5Syncer implements wiki.FTS5PageSyncer by writing directly to the
// SQLite wiki_documents FTS5 virtual table in the Aura main DB pool.
// Constructed via NewWikiFTS5Syncer and wired onto wiki.Store via SetFTS5Syncer.
type WikiFTS5Syncer struct {
	db     *sql.DB
	logger *slog.Logger
}

// NewWikiFTS5Syncer constructs a WikiFTS5Syncer from the shared Aura DB pool.
func NewWikiFTS5Syncer(db *sql.DB, logger *slog.Logger) *WikiFTS5Syncer {
	if logger == nil {
		logger = slog.Default()
	}
	return &WikiFTS5Syncer{db: db, logger: logger}
}

// SyncPage upserts the page into wiki_documents under key slug.
// FTS5 has no ON CONFLICT clause so this is a delete-then-insert.
func (s *WikiFTS5Syncer) SyncPage(ctx context.Context, slug string, page *wiki.Page) error {
	if _, err := s.db.ExecContext(ctx, `DELETE FROM wiki_documents WHERE id = ?`, slug); err != nil {
		return fmt.Errorf("fts5 sync delete %s: %w", slug, err)
	}
	content := page.Title + "\n" + page.Body
	meta := buildPageFTS5Meta(slug, page)
	metaJSON := metadataToJSON(meta)
	if _, err := s.db.ExecContext(ctx,
		`INSERT INTO wiki_documents (id, content, metadata, title) VALUES (?, ?, ?, ?)`,
		slug, content, metaJSON, page.Title,
	); err != nil {
		return fmt.Errorf("fts5 sync insert %s: %w", slug, err)
	}
	return nil
}

// RemovePage deletes the slug's row from wiki_documents.
func (s *WikiFTS5Syncer) RemovePage(ctx context.Context, slug string) error {
	if _, err := s.db.ExecContext(ctx, `DELETE FROM wiki_documents WHERE id = ?`, slug); err != nil {
		return fmt.Errorf("fts5 sync remove %s: %w", slug, err)
	}
	return nil
}

// CountDocs returns the total number of rows in wiki_documents.
func (s *WikiFTS5Syncer) CountDocs(ctx context.Context) (int, error) {
	var n int
	if err := s.db.QueryRowContext(ctx, `SELECT count(*) FROM wiki_documents`).Scan(&n); err != nil {
		return 0, fmt.Errorf("fts5 count: %w", err)
	}
	return n, nil
}

// buildPageFTS5Meta builds the metadata map stored in wiki_documents.metadata
// from a wiki.Page. Mirrors wikiDocumentFromPage but omits filesystem metadata
// (size, filepath) unavailable from the Page struct.
func buildPageFTS5Meta(slug string, page *wiki.Page) map[string]string {
	meta := map[string]string{
		"slug":           slug,
		"title":          page.Title,
		"kind":           "wiki_page",
		"category":       strings.TrimSpace(page.Category),
		"tags":           strings.Join(page.Tags, ","),
		"related":        strings.Join(wiki.RelatedSlugs(page.Related), ","),
		"sources":        strings.Join(page.Sources, ","),
		"schema_version": strconv.Itoa(page.SchemaVersion),
		"prompt_version": strings.TrimSpace(page.PromptVersion),
		"created_at":     strings.TrimSpace(page.CreatedAt),
		"updated_at":     strings.TrimSpace(page.UpdatedAt),
	}
	if page.Unversioned {
		meta["unversioned"] = "true"
	}
	for k, v := range meta {
		if v == "" || (k == "schema_version" && v == "0") {
			delete(meta, k)
		}
	}
	return meta
}
