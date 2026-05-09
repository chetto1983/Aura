package memoryindex

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"log/slog"
	"sort"
	"strings"
	"time"
	"unicode"
)

const (
	KindSource   = "source"
	KindArchive  = "archive"
	KindProposal = "proposal"
)

const defaultSearchLimit = 8

const compactMemoryFTSCreateSQL = `
CREATE VIRTUAL TABLE compact_memory_fts
USING fts5(id UNINDEXED, kind, title, body, handle, source_id, status, entities, tags);
`

type Document struct {
	ID             string
	Kind           string
	Title          string
	Body           string
	Handle         string
	SourceID       string
	Page           int
	ChatID         int64
	ConversationID int64
	ProposalID     int64
	Status         string
	Entities       []string
	Tags           []string
	UpdatedAt      time.Time
	Score          float64
}

type Filter struct {
	Kinds  []string
	ChatID int64
	Limit  int
}

type VectorReport struct {
	Collection  string
	DocsIndexed int
	VectorSize  int
}

type VectorIndex interface {
	Upsert(ctx context.Context, docs []Document) error
	Recreate(ctx context.Context, docs []Document) (VectorReport, error)
	Search(ctx context.Context, query string, filter Filter) ([]Document, error)
	Delete(ctx context.Context, docIDs []string) error
}

type Store struct {
	db     *sql.DB
	vector VectorIndex
	logger *slog.Logger
	now    func() time.Time
}

func NewStore(db *sql.DB) (*Store, error) {
	return NewStoreWithVector(db, nil, nil)
}

func NewStoreWithVector(db *sql.DB, vector VectorIndex, logger *slog.Logger) (*Store, error) {
	if db == nil {
		return nil, fmt.Errorf("memoryindex: db required")
	}
	if logger == nil {
		logger = slog.Default()
	}
	return &Store{
		db:     db,
		vector: vector,
		logger: logger,
		now:    func() time.Time { return time.Now().UTC() },
	}, nil
}

func (s *Store) Upsert(ctx context.Context, doc Document) error {
	if s == nil || s.db == nil {
		return fmt.Errorf("memoryindex: store unavailable")
	}
	doc.ID = strings.TrimSpace(doc.ID)
	doc.Kind = strings.TrimSpace(doc.Kind)
	doc.Body = strings.TrimSpace(doc.Body)
	if doc.ID == "" {
		return fmt.Errorf("memoryindex: document id required")
	}
	if doc.Kind == "" {
		return fmt.Errorf("memoryindex: document kind required")
	}
	if doc.Body == "" {
		return fmt.Errorf("memoryindex: document body required")
	}
	if doc.Handle == "" {
		doc.Handle = doc.ID
	}
	if doc.UpdatedAt.IsZero() {
		doc.UpdatedAt = s.now()
	}
	entitiesJSON := stringListJSON(doc.Entities)
	tagsJSON := stringListJSON(doc.Tags)
	updatedAt := doc.UpdatedAt.UTC().Format(time.RFC3339Nano)

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("memoryindex: begin upsert: %w", err)
	}
	defer tx.Rollback()

	if _, err := tx.ExecContext(ctx, `DELETE FROM compact_memory_fts WHERE id = ?`, doc.ID); err != nil {
		return fmt.Errorf("memoryindex: delete old fts row: %w", err)
	}
	if _, err := tx.ExecContext(ctx, `
INSERT OR REPLACE INTO compact_memory_documents
  (id, kind, title, body, handle, source_id, page, chat_id, conversation_id, proposal_id, status, entities_json, tags_json, updated_at)
VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
`,
		doc.ID,
		doc.Kind,
		doc.Title,
		doc.Body,
		doc.Handle,
		doc.SourceID,
		doc.Page,
		doc.ChatID,
		doc.ConversationID,
		doc.ProposalID,
		doc.Status,
		entitiesJSON,
		tagsJSON,
		updatedAt,
	); err != nil {
		return fmt.Errorf("memoryindex: upsert document: %w", err)
	}
	if _, err := tx.ExecContext(ctx, `
INSERT INTO compact_memory_fts
  (id, kind, title, body, handle, source_id, status, entities, tags)
VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)
`,
		doc.ID,
		doc.Kind,
		doc.Title,
		doc.Body,
		doc.Handle,
		doc.SourceID,
		doc.Status,
		strings.Join(doc.Entities, " "),
		strings.Join(doc.Tags, " "),
	); err != nil {
		return fmt.Errorf("memoryindex: upsert fts row: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("memoryindex: commit upsert: %w", err)
	}
	if s.vector != nil {
		if err := s.vector.Upsert(ctx, []Document{doc}); err != nil && s.logger != nil {
			s.logger.Warn("memoryindex: vector upsert failed", "id", doc.ID, "error", err)
		}
	}
	return nil
}

func (s *Store) ReplaceKind(ctx context.Context, kind string, docs []Document) error {
	if s == nil || s.db == nil {
		return fmt.Errorf("memoryindex: store unavailable")
	}
	kind = strings.TrimSpace(kind)
	if kind == "" {
		return fmt.Errorf("memoryindex: kind required")
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("memoryindex: begin replace: %w", err)
	}
	defer tx.Rollback()
	if _, err := tx.ExecContext(ctx, `DELETE FROM compact_memory_fts WHERE kind = ?`, kind); err != nil {
		return fmt.Errorf("memoryindex: delete old fts kind: %w", err)
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM compact_memory_documents WHERE kind = ?`, kind); err != nil {
		return fmt.Errorf("memoryindex: delete old documents kind: %w", err)
	}
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
		if doc.UpdatedAt.IsZero() {
			doc.UpdatedAt = s.now()
		}
		entitiesJSON := stringListJSON(doc.Entities)
		tagsJSON := stringListJSON(doc.Tags)
		updatedAt := doc.UpdatedAt.UTC().Format(time.RFC3339Nano)
		if _, err := tx.ExecContext(ctx, `
INSERT INTO compact_memory_documents
  (id, kind, title, body, handle, source_id, page, chat_id, conversation_id, proposal_id, status, entities_json, tags_json, updated_at)
VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
`,
			doc.ID, doc.Kind, doc.Title, doc.Body, doc.Handle, doc.SourceID, doc.Page,
			doc.ChatID, doc.ConversationID, doc.ProposalID, doc.Status, entitiesJSON, tagsJSON, updatedAt,
		); err != nil {
			return fmt.Errorf("memoryindex: insert document %s: %w", doc.ID, err)
		}
		if _, err := tx.ExecContext(ctx, `
INSERT INTO compact_memory_fts
  (id, kind, title, body, handle, source_id, status, entities, tags)
VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)
`,
			doc.ID, doc.Kind, doc.Title, doc.Body, doc.Handle, doc.SourceID, doc.Status,
			strings.Join(doc.Entities, " "), strings.Join(doc.Tags, " "),
		); err != nil {
			return fmt.Errorf("memoryindex: insert fts %s: %w", doc.ID, err)
		}
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("memoryindex: commit replace: %w", err)
	}
	return nil
}

func (s *Store) RebuildFTS(ctx context.Context) error {
	if s == nil || s.db == nil {
		return fmt.Errorf("memoryindex: store unavailable")
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("memoryindex: begin fts rebuild: %w", err)
	}
	defer tx.Rollback()
	if _, err := tx.ExecContext(ctx, `DROP TABLE IF EXISTS compact_memory_fts`); err != nil {
		return fmt.Errorf("memoryindex: drop compact fts: %w", err)
	}
	if _, err := tx.ExecContext(ctx, compactMemoryFTSCreateSQL); err != nil {
		return fmt.Errorf("memoryindex: create compact fts: %w", err)
	}
	if _, err := tx.ExecContext(ctx, `
INSERT INTO compact_memory_fts
  (id, kind, title, body, handle, source_id, status, entities, tags)
SELECT id, kind, title, body, handle, source_id, status, entities_json, tags_json
FROM compact_memory_documents
ORDER BY kind, updated_at DESC, id
`); err != nil {
		return fmt.Errorf("memoryindex: repopulate compact fts: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("memoryindex: commit fts rebuild: %w", err)
	}
	return nil
}

func (s *Store) Search(ctx context.Context, query string, filter Filter) ([]Document, error) {
	if s == nil || s.db == nil {
		return nil, fmt.Errorf("memoryindex: store unavailable")
	}
	limit := filter.Limit
	if limit <= 0 {
		limit = defaultSearchLimit
	}
	candidates, err := s.exactSearch(ctx, query, filter, limit)
	if err != nil {
		return nil, err
	}
	ftsResults, err := s.ftsSearch(ctx, query, filter, limit)
	if err != nil {
		return nil, err
	}
	candidates = append(candidates, ftsResults...)
	if s.vector != nil {
		vectorResults, err := s.vector.Search(ctx, query, filter)
		if err != nil {
			if s.logger != nil {
				s.logger.Warn("memoryindex: vector search failed; using local compact index", "error", err)
			}
		} else {
			candidates = append(candidates, vectorResults...)
		}
	}
	return mergeDocuments(candidates, limit), nil
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

func (s *Store) deleteWhere(ctx context.Context, where string, args ...any) error {
	if s == nil || s.db == nil {
		return fmt.Errorf("memoryindex: store unavailable")
	}
	rows, err := s.db.QueryContext(ctx, `SELECT id FROM compact_memory_documents WHERE `+where, args...)
	if err != nil {
		return fmt.Errorf("memoryindex: list delete ids: %w", err)
	}
	var ids []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			rows.Close()
			return fmt.Errorf("memoryindex: scan delete id: %w", err)
		}
		ids = append(ids, id)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return fmt.Errorf("memoryindex: iterate delete ids: %w", err)
	}
	rows.Close()
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
	defer tx.Rollback()
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

func (s *Store) SyncVector(ctx context.Context) (VectorReport, error) {
	if s == nil || s.db == nil {
		return VectorReport{}, fmt.Errorf("memoryindex: store unavailable")
	}
	if s.vector == nil {
		return VectorReport{}, nil
	}
	docs, err := s.allDocuments(ctx)
	if err != nil {
		return VectorReport{}, err
	}
	return s.vector.Recreate(ctx, docs)
}

func (s *Store) allDocuments(ctx context.Context) ([]Document, error) {
	rows, err := s.db.QueryContext(ctx, `
SELECT d.id, d.kind, d.title, d.body, d.handle, d.source_id, d.page, d.chat_id, d.conversation_id, d.proposal_id, d.status, d.entities_json, d.tags_json, d.updated_at
FROM compact_memory_documents d
ORDER BY d.kind, d.updated_at DESC, d.id
`)
	if err != nil {
		return nil, fmt.Errorf("memoryindex: list all documents: %w", err)
	}
	defer rows.Close()
	return scanDocuments(rows, 1)
}

func (s *Store) exactSearch(ctx context.Context, query string, filter Filter, limit int) ([]Document, error) {
	values := exactCandidates(query)
	if len(values) == 0 {
		return nil, nil
	}
	clauses := make([]string, 0, len(values)*3)
	args := make([]any, 0, len(values)*3+len(filter.Kinds)+2)
	for _, value := range values {
		clauses = append(clauses, `lower(d.id) = ?`, `lower(d.handle) = ?`, `lower(d.title) = ?`)
		args = append(args, value, value, value)
	}
	where, filterArgs := filterWhere(filter)
	args = append(args, filterArgs...)
	args = append(args, limit)
	rows, err := s.db.QueryContext(ctx, `
SELECT d.id, d.kind, d.title, d.body, d.handle, d.source_id, d.page, d.chat_id, d.conversation_id, d.proposal_id, d.status, d.entities_json, d.tags_json, d.updated_at
FROM compact_memory_documents d
WHERE (`+strings.Join(clauses, " OR ")+`)`+where+`
ORDER BY length(d.id), d.id
LIMIT ?
`, args...)
	if err != nil {
		return nil, fmt.Errorf("memoryindex: exact search: %w", err)
	}
	defer rows.Close()
	return scanDocuments(rows, 1)
}

func (s *Store) ftsSearch(ctx context.Context, query string, filter Filter, limit int) ([]Document, error) {
	safeQuery := escapeFTS5Query(query)
	if safeQuery == "" {
		return nil, nil
	}
	where, args := filterWhere(filter)
	args = append([]any{safeQuery}, args...)
	args = append(args, limit)
	rows, err := s.db.QueryContext(ctx, `
SELECT d.id, d.kind, d.title, d.body, d.handle, d.source_id, d.page, d.chat_id, d.conversation_id, d.proposal_id, d.status, d.entities_json, d.tags_json, d.updated_at, f.rank
FROM compact_memory_fts f
JOIN compact_memory_documents d ON d.id = f.id
WHERE compact_memory_fts MATCH ?`+where+`
ORDER BY f.rank
LIMIT ?
`, args...)
	if err != nil {
		return nil, fmt.Errorf("memoryindex: fts search: %w", err)
	}
	defer rows.Close()
	return scanDocuments(rows, 0)
}

func filterWhere(filter Filter) (string, []any) {
	var clauses []string
	var args []any
	kinds := uniqueNonEmpty(filter.Kinds)
	if len(kinds) > 0 {
		placeholders := make([]string, len(kinds))
		for i, kind := range kinds {
			placeholders[i] = "?"
			args = append(args, kind)
		}
		clauses = append(clauses, "d.kind IN ("+strings.Join(placeholders, ",")+")")
	}
	if filter.ChatID > 0 {
		clauses = append(clauses, "(d.kind <> ? OR d.chat_id = ?)")
		args = append(args, KindArchive, filter.ChatID)
	}
	if len(clauses) == 0 {
		return "", args
	}
	return " AND " + strings.Join(clauses, " AND "), args
}

func scanDocuments(rows *sql.Rows, fixedScore float64) ([]Document, error) {
	var out []Document
	for rows.Next() {
		var doc Document
		var entitiesJSON, tagsJSON, updatedAt string
		if fixedScore > 0 {
			if err := rows.Scan(&doc.ID, &doc.Kind, &doc.Title, &doc.Body, &doc.Handle, &doc.SourceID, &doc.Page, &doc.ChatID, &doc.ConversationID, &doc.ProposalID, &doc.Status, &entitiesJSON, &tagsJSON, &updatedAt); err != nil {
				return nil, err
			}
			doc.Score = fixedScore
		} else {
			var rank float64
			if err := rows.Scan(&doc.ID, &doc.Kind, &doc.Title, &doc.Body, &doc.Handle, &doc.SourceID, &doc.Page, &doc.ChatID, &doc.ConversationID, &doc.ProposalID, &doc.Status, &entitiesJSON, &tagsJSON, &updatedAt, &rank); err != nil {
				return nil, err
			}
			doc.Score = 0.35 + cappedRatio(-rank, 12)*0.50
		}
		doc.Entities = parseStringList(entitiesJSON)
		doc.Tags = parseStringList(tagsJSON)
		if ts, err := time.Parse(time.RFC3339Nano, updatedAt); err == nil {
			doc.UpdatedAt = ts
		}
		out = append(out, doc)
	}
	return out, rows.Err()
}

func mergeDocuments(docs []Document, limit int) []Document {
	if limit <= 0 {
		limit = defaultSearchLimit
	}
	byID := map[string]Document{}
	for _, doc := range docs {
		if doc.ID == "" {
			continue
		}
		if existing, ok := byID[doc.ID]; ok && existing.Score >= doc.Score {
			continue
		}
		byID[doc.ID] = doc
	}
	out := make([]Document, 0, len(byID))
	for _, doc := range byID {
		out = append(out, doc)
	}
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].Score == out[j].Score {
			return out[i].ID < out[j].ID
		}
		return out[i].Score > out[j].Score
	})
	if len(out) > limit {
		out = out[:limit]
	}
	return out
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
		return !(unicode.IsLetter(r) || unicode.IsDigit(r) || r == '_')
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

func cappedRatio(value, cap float64) float64 {
	if value <= 0 || cap <= 0 {
		return 0
	}
	if value > cap {
		return 1
	}
	return value / cap
}
