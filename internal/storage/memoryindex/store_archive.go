package memoryindex

import (
	"context"
	"database/sql"
	"fmt"
	"log/slog"
	"strings"
	"time"
)

func (s *Store) ReplaceKind(ctx context.Context, kind string, docs []Document) error {
	if s == nil || s.db == nil {
		return fmt.Errorf("memoryindex: store unavailable")
	}
	kind = strings.TrimSpace(kind)
	if kind == "" {
		return fmt.Errorf("memoryindex: kind required")
	}

	// Read existing hashes for drift detection (before delete).
	var existingHashes map[string]string
	if s.freshnessStore != nil {
		existingHashes = s.readKindHashes(ctx, kind)
	}

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("memoryindex: begin replace: %w", err)
	}
	defer func() { _ = tx.Rollback() }()
	if _, err := tx.ExecContext(ctx, `DELETE FROM compact_memory_fts WHERE kind = ?`, kind); err != nil {
		return fmt.Errorf("memoryindex: delete old fts kind: %w", err)
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM compact_memory_documents WHERE kind = ?`, kind); err != nil {
		return fmt.Errorf("memoryindex: delete old documents kind: %w", err)
	}
	changedCount := 0
	for _, doc := range docs {
		doc.Kind = kind
		doc.ID = strings.TrimSpace(doc.ID)
		doc.Body = strings.TrimSpace(doc.Body)
		if doc.ID == "" || doc.Body == "" {
			continue
		}
		if doc.Handle == "" {
			doc.Handle = doc.ID
		}
		doc.Priority = NormalizePriority(doc.Priority)
		if doc.UpdatedAt.IsZero() {
			doc.UpdatedAt = s.now()
		}
		entitiesJSON := stringListJSON(doc.Entities)
		tagsJSON := stringListJSON(doc.Tags)
		updatedAt := doc.UpdatedAt.UTC().Format(time.RFC3339Nano)
		if err := insertDocumentRow(ctx, tx, "INSERT", doc, entitiesJSON, tagsJSON, updatedAt); err != nil {
			return fmt.Errorf("memoryindex: insert document %s: %w", doc.ID, err)
		}
		if err := insertFTSRow(ctx, tx, doc); err != nil {
			return fmt.Errorf("memoryindex: insert fts %s: %w", doc.ID, err)
		}
		// Count hash changes for freshness bump (only when tracking is enabled).
		if existingHashes != nil && doc.ContentHash != "" {
			if old, existed := existingHashes[doc.ID]; !existed || old != doc.ContentHash {
				changedCount++
			}
		}
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("memoryindex: commit replace: %w", err)
	}
	if s.freshnessStore != nil && changedCount > 0 {
		if err := s.freshnessStore.BumpPending(ctx, compactMemoryProjectionID, changedCount); err != nil && s.logger != nil {
			s.logger.Warn("memoryindex: freshness bump failed", "kind", kind, "changed", changedCount, "error", err)
		} else {
			slog.Debug("memoryindex: freshness pending bumped", "kind", kind, "changed", changedCount)
		}
	}
	return nil
}

func (s *Store) PurgeArchiveByChat(ctx context.Context, chatID int64) error {
	if chatID <= 0 {
		return nil
	}
	return s.deleteWhere(ctx, `kind = ? AND chat_id = ?`, KindArchive, chatID)
}

func (s *Store) PurgeArchiveOlderThan(ctx context.Context, cutoff time.Time) error {
	if cutoff.IsZero() {
		return nil
	}
	return s.deleteWhere(ctx, `kind = ? AND updated_at < ?`, KindArchive, cutoff.UTC().Format(time.RFC3339Nano))
}

func (s *Store) PurgeArchiveAll(ctx context.Context) error {
	return s.deleteWhere(ctx, `kind = ?`, KindArchive)
}

// PurgeSource removes every memoryindex entry that points at the given
// source_id (typically multiple page-level documents for an OCR'd PDF). The
// Qdrant mirror is updated in the same call via deleteWhere's vector
// cascade, so callers don't need to re-sync afterwards.
func (s *Store) PurgeSource(ctx context.Context, sourceID string) error {
	sourceID = strings.TrimSpace(sourceID)
	if sourceID == "" {
		return nil
	}
	return s.deleteWhere(ctx, `kind = ? AND source_id = ?`, KindSource, sourceID)
}

// DeleteDocument removes a single compact-memory document and its projection
// rows. Used by post-turn reconciliation when a candidate is judged duplicate
// or stale.
func (s *Store) DeleteDocument(ctx context.Context, id string) error {
	id = strings.TrimSpace(id)
	if id == "" {
		return nil
	}
	return s.deleteWhere(ctx, `id = ?`, id)
}

// FetchRecentOperational returns the N most recently updated kind=operational
// documents, ordered by updated_at DESC. Used by the system-prompt overlay to
// inject top-N lessons at conversation start (US-OP03). limit <= 0 defaults to 10.
func (s *Store) FetchRecentOperational(ctx context.Context, limit int) ([]Document, error) {
	if limit <= 0 {
		limit = 10
	}
	return s.fetchOperationalBySQL(ctx, `
SELECT id, kind, title, body, handle, source_id, page, chunk_index, byte_start, byte_end, chat_id, conversation_id, proposal_id, status, priority, last_recalled_at, recall_count, entities_json, tags_json, updated_at, content_hash, embedding_model_id, index_build_id
FROM compact_memory_documents
WHERE kind = ?
ORDER BY updated_at DESC
LIMIT ?
`, KindOperational, limit)
}

// FetchPinnedOperational returns Critical and High operational lessons in prompt
// priority order. Normal lessons are intentionally excluded: they remain
// available through recall/search instead of being pinned every turn.
func (s *Store) FetchPinnedOperational(ctx context.Context, limit int) ([]Document, error) {
	if limit <= 0 {
		limit = 30
	}
	return s.fetchOperationalBySQL(ctx, `
SELECT id, kind, title, body, handle, source_id, page, chunk_index, byte_start, byte_end, chat_id, conversation_id, proposal_id, status, priority, last_recalled_at, recall_count, entities_json, tags_json, updated_at, content_hash, embedding_model_id, index_build_id
FROM compact_memory_documents
WHERE kind = ?
  AND priority IN (?, ?)
  AND status <> 'quarantine'
ORDER BY CASE priority WHEN 'critical' THEN 0 WHEN 'high' THEN 1 ELSE 2 END,
         updated_at DESC,
         id ASC
LIMIT ?
`, KindOperational, PriorityCritical, PriorityHigh, limit)
}

// FetchOperationalForJudge returns active operational lessons considered by
// the post-turn ADD/UPDATE/DELETE judge. Rows are newest-first and capped so
// the judge prompt stays bounded.
func (s *Store) FetchOperationalForJudge(ctx context.Context, limit int) ([]Document, error) {
	if limit <= 0 {
		limit = 50
	}
	return s.fetchOperationalBySQL(ctx, `
SELECT id, kind, title, body, handle, source_id, page, chunk_index, byte_start, byte_end, chat_id, conversation_id, proposal_id, status, priority, last_recalled_at, recall_count, entities_json, tags_json, updated_at, content_hash, embedding_model_id, index_build_id
FROM compact_memory_documents
WHERE kind = ?
  AND status = 'active'
ORDER BY updated_at DESC, id ASC
LIMIT ?
`, KindOperational, limit)
}

// MarkOperationalRecalled records that operational lessons were shown to the
// model. It increments recall_count and sets last_recalled_at for active rows.
func (s *Store) MarkOperationalRecalled(ctx context.Context, ids []string, recalledAt time.Time) error {
	if s == nil || s.db == nil {
		return fmt.Errorf("memoryindex: store unavailable")
	}
	cleaned := uniqueTrimmed(ids)
	if len(cleaned) == 0 {
		return nil
	}
	if recalledAt.IsZero() {
		recalledAt = s.now()
	}
	stamp := recalledAt.UTC().Format(time.RFC3339Nano)
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("memoryindex: begin mark recalled: %w", err)
	}
	defer func() { _ = tx.Rollback() }()
	for _, id := range cleaned {
		if _, err := tx.ExecContext(ctx, `
UPDATE compact_memory_documents
SET recall_count = recall_count + 1,
    last_recalled_at = ?
WHERE id = ?
  AND kind = ?
  AND status = 'active'
`, stamp, id, KindOperational); err != nil {
			return fmt.Errorf("memoryindex: mark recalled %s: %w", id, err)
		}
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("memoryindex: commit mark recalled: %w", err)
	}
	return nil
}

type OperationalDecaySummary struct {
	Scanned int
	Deleted int
	Kept    int
}

// DecayOperationalLessons deletes stale Normal-priority operational lessons.
// Critical and High rows are excluded regardless of recall age.
func (s *Store) DecayOperationalLessons(ctx context.Context, now time.Time, unrecalledTTL, updatedGrace time.Duration) (OperationalDecaySummary, error) {
	if s == nil || s.db == nil {
		return OperationalDecaySummary{}, fmt.Errorf("memoryindex: store unavailable")
	}
	if now.IsZero() {
		now = s.now()
	}
	if unrecalledTTL <= 0 {
		unrecalledTTL = 30 * 24 * time.Hour
	}
	if updatedGrace <= 0 {
		updatedGrace = 7 * 24 * time.Hour
	}
	rows, err := s.db.QueryContext(ctx, `
SELECT id, updated_at, last_recalled_at, recall_count
FROM compact_memory_documents
WHERE kind = ?
  AND status = 'active'
  AND priority = ?
`, KindOperational, PriorityNormal)
	if err != nil {
		return OperationalDecaySummary{}, fmt.Errorf("memoryindex: list decay candidates: %w", err)
	}
	type candidate struct {
		id             string
		updatedAt      time.Time
		lastRecalledAt time.Time
		recallCount    int
	}
	var candidates []candidate
	for rows.Next() {
		var c candidate
		var updatedAt string
		var lastRecalledAt sql.NullString
		if err := rows.Scan(&c.id, &updatedAt, &lastRecalledAt, &c.recallCount); err != nil {
			_ = rows.Close()
			return OperationalDecaySummary{}, fmt.Errorf("memoryindex: scan decay candidate: %w", err)
		}
		c.updatedAt = parseRFC3339OrZero(updatedAt)
		if lastRecalledAt.Valid {
			c.lastRecalledAt = parseRFC3339OrZero(lastRecalledAt.String)
		}
		candidates = append(candidates, c)
	}
	if err := rows.Err(); err != nil {
		_ = rows.Close()
		return OperationalDecaySummary{}, fmt.Errorf("memoryindex: iterate decay candidates: %w", err)
	}
	_ = rows.Close()

	summary := OperationalDecaySummary{Scanned: len(candidates)}
	updateCutoff := now.UTC().Add(-updatedGrace)
	recallCutoff := now.UTC().Add(-unrecalledTTL)
	for _, c := range candidates {
		if c.updatedAt.IsZero() || c.updatedAt.After(updateCutoff) {
			summary.Kept++
			continue
		}
		if c.recallCount > 0 && !c.lastRecalledAt.IsZero() && c.lastRecalledAt.After(recallCutoff) {
			summary.Kept++
			continue
		}
		if err := s.DeleteDocument(ctx, c.id); err != nil {
			return summary, fmt.Errorf("memoryindex: decay delete %s: %w", c.id, err)
		}
		summary.Deleted++
	}
	return summary, nil
}
