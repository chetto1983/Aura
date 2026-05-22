package memoryindex

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"time"
	"unicode"
)

func NormalizePriority(value string) string {
	priority, ok := ParsePriority(value)
	if !ok {
		return PriorityNormal
	}
	return priority
}

func ParsePriority(value string) (string, bool) {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "", PriorityNormal:
		return PriorityNormal, true
	case PriorityHigh:
		return PriorityHigh, true
	case PriorityCritical:
		return PriorityCritical, true
	default:
		return "", false
	}
}

// acquireWithContext takes mu unless ctx fires first. Plain sync.Mutex.Lock
// is uncancellable; this wrapper lets a queued PurgeSource caller back out
// when its deadline expires instead of sitting indefinitely behind a long
// Qdrant DELETE roundtrip in front of it.
func acquireWithContext(ctx context.Context, mu *sync.Mutex) error {
	if ctx == nil {
		mu.Lock()
		return nil
	}
	acquired := make(chan struct{})
	go func() {
		mu.Lock()
		close(acquired)
	}()
	select {
	case <-acquired:
		return nil
	case <-ctx.Done():
		// The waiter goroutine still holds the position in line; once it
		// acquires we release immediately so the next caller can proceed.
		go func() {
			<-acquired
			mu.Unlock()
		}()
		return ctx.Err()
	}
}

func (s *Store) deleteWhere(ctx context.Context, where string, args ...any) error {
	if s == nil || s.db == nil {
		return fmt.Errorf("memoryindex: store unavailable")
	}
	// Single-writer serialization across all delete fan-outs (Purge*).
	// See Store.writeMu doc comment for rationale (mini-PC + Qdrant + SQLite
	// write-lock interaction). Honors ctx cancellation so a stuck caller
	// can't pin the queue past its deadline.
	if err := acquireWithContext(ctx, &s.writeMu); err != nil {
		return err
	}
	defer s.writeMu.Unlock()

	rows, err := s.db.QueryContext(ctx, `SELECT id FROM compact_memory_documents WHERE `+where, args...)
	if err != nil {
		return fmt.Errorf("memoryindex: list delete ids: %w", err)
	}
	var ids []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			_ = rows.Close()
			return fmt.Errorf("memoryindex: scan delete id: %w", err)
		}
		ids = append(ids, id)
	}
	if err := rows.Err(); err != nil {
		_ = rows.Close()
		return fmt.Errorf("memoryindex: iterate delete ids: %w", err)
	}
	_ = rows.Close()
	if len(ids) == 0 {
		return nil
	}
	if s.vector != nil {
		if err := s.vector.Delete(ctx, ids); err != nil {
			return fmt.Errorf("memoryindex: vector delete: %w", err)
		}
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("memoryindex: begin delete: %w", err)
	}
	defer func() { _ = tx.Rollback() }()
	for _, id := range ids {
		if _, err := tx.ExecContext(ctx, `DELETE FROM compact_memory_fts WHERE id = ?`, id); err != nil {
			return fmt.Errorf("memoryindex: delete fts %s: %w", id, err)
		}
		if _, err := tx.ExecContext(ctx, `DELETE FROM compact_memory_documents WHERE id = ?`, id); err != nil {
			return fmt.Errorf("memoryindex: delete document %s: %w", id, err)
		}
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("memoryindex: commit delete: %w", err)
	}
	return nil
}

// readKindHashes returns {id -> content_hash} for all docs of the given kind.
// Returns nil on error (freshness tracking is best-effort).
func (s *Store) readKindHashes(ctx context.Context, kind string) map[string]string {
	rows, err := s.db.QueryContext(ctx,
		`SELECT id, content_hash FROM compact_memory_documents WHERE kind = ?`, kind)
	if err != nil {
		return nil
	}
	defer func() { _ = rows.Close() }()
	hashes := make(map[string]string)
	for rows.Next() {
		var id, hash string
		if err := rows.Scan(&id, &hash); err != nil {
			return nil
		}
		hashes[id] = hash
	}
	if rows.Err() != nil {
		return nil
	}
	return hashes
}

func exactCandidates(query string) []string {
	query = strings.ToLower(strings.TrimSpace(query))
	if query == "" {
		return nil
	}
	values := []string{query}
	fields := strings.Fields(query)
	if len(fields) > 1 {
		values = append(values, strings.Join(fields, "-"))
	}
	return uniqueNonEmpty(values)
}

func escapeFTS5Query(query string) string {
	fields := strings.FieldsFunc(query, func(r rune) bool {
		return !unicode.IsLetter(r) && !unicode.IsDigit(r) && r != '_'
	})
	if len(fields) == 0 {
		return ""
	}
	return strings.Join(uniqueNonEmpty(fields), " OR ")
}

func stringListJSON(values []string) string {
	b, err := json.Marshal(uniqueNonEmpty(values))
	if err != nil {
		return "[]"
	}
	return string(b)
}

func parseStringList(raw string) []string {
	var values []string
	if err := json.Unmarshal([]byte(raw), &values); err != nil {
		return nil
	}
	return uniqueNonEmpty(values)
}

func nullableTimeString(value time.Time) any {
	if value.IsZero() {
		return ""
	}
	return value.UTC().Format(time.RFC3339Nano)
}

func parseRFC3339OrZero(value string) time.Time {
	value = strings.TrimSpace(value)
	if value == "" {
		return time.Time{}
	}
	if ts, err := time.Parse(time.RFC3339Nano, value); err == nil {
		return ts
	}
	return time.Time{}
}

func uniqueNonEmpty(values []string) []string {
	out := make([]string, 0, len(values))
	seen := map[string]bool{}
	for _, value := range values {
		value = strings.ToLower(strings.TrimSpace(value))
		if value == "" || seen[value] {
			continue
		}
		seen[value] = true
		out = append(out, value)
	}
	return out
}

func uniqueTrimmed(values []string) []string {
	out := make([]string, 0, len(values))
	seen := map[string]bool{}
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" || seen[value] {
			continue
		}
		seen[value] = true
		out = append(out, value)
	}
	return out
}

func cappedRatio(value, cap float64) float64 {
	if value <= 0 || cap <= 0 {
		return 0
	}
	if value > cap {
		return 1
	}
	return value / cap
}

// insertDocumentRow executes the main-table INSERT for one document inside an
// open transaction. conflictClause is "INSERT OR REPLACE" (Upsert) or "INSERT"
// (ReplaceKind, where the kind was already bulk-deleted before the loop).
func insertDocumentRow(ctx context.Context, tx *sql.Tx, conflictClause string, doc Document, entitiesJSON, tagsJSON, updatedAt string) error {
	_, err := tx.ExecContext(ctx, conflictClause+` INTO compact_memory_documents
  (id, kind, title, body, handle, source_id, page, chunk_index, byte_start, byte_end, chat_id, conversation_id, proposal_id, status, priority, last_recalled_at, recall_count, entities_json, tags_json, updated_at, content_hash, embedding_model_id, index_build_id)
VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		doc.ID, doc.Kind, doc.Title, doc.Body, doc.Handle, doc.SourceID,
		doc.Page, doc.ChunkIndex, doc.ByteStart, doc.ByteEnd,
		doc.ChatID, doc.ConversationID, doc.ProposalID,
		doc.Status, doc.Priority, nullableTimeString(doc.LastRecalledAt), doc.RecallCount,
		entitiesJSON, tagsJSON, updatedAt,
		doc.ContentHash, doc.EmbeddingModelID, doc.IndexBuildID,
	)
	return err
}

// insertFTSRow inserts the FTS projection row for one document inside an open
// transaction.
func insertFTSRow(ctx context.Context, tx *sql.Tx, doc Document) error {
	_, err := tx.ExecContext(ctx, `
INSERT INTO compact_memory_fts
  (id, kind, title, body, handle, source_id, status, entities, tags)
VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		doc.ID, doc.Kind, doc.Title, doc.Body, doc.Handle, doc.SourceID, doc.Status,
		strings.Join(doc.Entities, " "), strings.Join(doc.Tags, " "),
	)
	return err
}

// fetchOperationalBySQL executes query with args, scans results via
// scanDocuments, and closes the row-set. Shared by all FetchXxxOperational
// methods to eliminate boilerplate.
func (s *Store) fetchOperationalBySQL(ctx context.Context, query string, args ...any) ([]Document, error) {
	if s == nil || s.db == nil {
		return nil, fmt.Errorf("memoryindex: store unavailable")
	}
	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("memoryindex: fetch operational docs: %w", err)
	}
	defer func() { _ = rows.Close() }()
	return scanDocuments(rows, 1)
}
