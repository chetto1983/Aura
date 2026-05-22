package memoryindex

import (
	"context"
	"database/sql"
	"fmt"
	"sort"
	"strings"
	"time"
)

func (s *Store) Search(ctx context.Context, query string, filter Filter) ([]Document, error) {
	if s == nil || s.db == nil {
		return nil, fmt.Errorf("memoryindex: store unavailable")
	}
	limit := filter.Limit
	if limit <= 0 {
		limit = defaultSearchLimit
	}
	exact, err := s.exactSearch(ctx, query, filter, limit)
	if err != nil {
		return nil, err
	}
	fts, err := s.ftsSearch(ctx, query, filter, limit)
	if err != nil {
		return nil, err
	}
	var vector []Document
	if s.vector != nil {
		vectorResults, vecErr := s.vector.Search(ctx, query, filter)
		if vecErr != nil {
			if s.logger != nil {
				s.logger.Warn("memoryindex: vector search failed; using local compact index", "error", vecErr)
			}
		} else {
			vector = vectorResults
		}
	}
	docs := mergeDocumentsRRF(exact, fts, vector, limit)
	if s.freshnessStore != nil && len(docs) > 0 {
		s.annotateStaleness(ctx, docs)
	}
	return docs, nil
}

// annotateStaleness does a single freshness lookup for compactMemoryProjectionID
// and stamps StalenessSeconds + DegradedRead on every document in docs.
// It is best-effort: errors are silently ignored so Search always returns docs.
func (s *Store) annotateStaleness(ctx context.Context, docs []Document) {
	row, found, err := s.freshnessStore.Get(ctx, compactMemoryProjectionID)
	if err != nil || !found {
		return
	}
	lastIndexedAt := row.LastFullRebuildAt
	if row.LastIncrementalAt != nil && *row.LastIncrementalAt > lastIndexedAt {
		lastIndexedAt = *row.LastIncrementalAt
	}
	nowUnix := s.now().Unix()
	var stalenessSeconds int64
	if lastIndexedAt > 0 {
		diff := nowUnix - lastIndexedAt
		if diff < 0 {
			diff = 0
		}
		stalenessSeconds = diff
	} else {
		// Never indexed → treat as maximally stale.
		stalenessSeconds = nowUnix
	}
	threshold := s.staleThresholdSecs
	if threshold <= 0 {
		threshold = 3600
	}
	degraded := row.PendingCount > 0 || stalenessSeconds > threshold
	for i := range docs {
		docs[i].StalenessSeconds = stalenessSeconds
		docs[i].DegradedRead = degraded
	}
}

func (s *Store) RebuildFTS(ctx context.Context) error {
	if s == nil || s.db == nil {
		return fmt.Errorf("memoryindex: store unavailable")
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("memoryindex: begin fts rebuild: %w", err)
	}
	defer func() { _ = tx.Rollback() }()
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
SELECT d.id, d.kind, d.title, d.body, d.handle, d.source_id, d.page, d.chunk_index, d.byte_start, d.byte_end, d.chat_id, d.conversation_id, d.proposal_id, d.status, d.priority, d.last_recalled_at, d.recall_count, d.entities_json, d.tags_json, d.updated_at, d.content_hash, d.embedding_model_id, d.index_build_id
FROM compact_memory_documents d
ORDER BY d.kind, d.updated_at DESC, d.id
`)
	if err != nil {
		return nil, fmt.Errorf("memoryindex: list all documents: %w", err)
	}
	defer func() { _ = rows.Close() }()
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
SELECT d.id, d.kind, d.title, d.body, d.handle, d.source_id, d.page, d.chunk_index, d.byte_start, d.byte_end, d.chat_id, d.conversation_id, d.proposal_id, d.status, d.priority, d.last_recalled_at, d.recall_count, d.entities_json, d.tags_json, d.updated_at, d.content_hash, d.embedding_model_id, d.index_build_id
FROM compact_memory_documents d
WHERE (`+strings.Join(clauses, " OR ")+`)`+where+`
ORDER BY length(d.id), d.id
LIMIT ?
`, args...)
	if err != nil {
		return nil, fmt.Errorf("memoryindex: exact search: %w", err)
	}
	defer func() { _ = rows.Close() }()
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
SELECT d.id, d.kind, d.title, d.body, d.handle, d.source_id, d.page, d.chunk_index, d.byte_start, d.byte_end, d.chat_id, d.conversation_id, d.proposal_id, d.status, d.priority, d.last_recalled_at, d.recall_count, d.entities_json, d.tags_json, d.updated_at, d.content_hash, d.embedding_model_id, d.index_build_id, f.rank
FROM compact_memory_fts f
JOIN compact_memory_documents d ON d.id = f.id
WHERE compact_memory_fts MATCH ?`+where+`
ORDER BY f.rank
LIMIT ?
`, args...)
	if err != nil {
		return nil, fmt.Errorf("memoryindex: fts search: %w", err)
	}
	defer func() { _ = rows.Close() }()
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
	if filter.SourceID != "" {
		clauses = append(clauses, "d.source_id = ?")
		args = append(args, filter.SourceID)
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
		var lastRecalledAt sql.NullString
		if fixedScore > 0 {
			if err := rows.Scan(&doc.ID, &doc.Kind, &doc.Title, &doc.Body, &doc.Handle, &doc.SourceID, &doc.Page, &doc.ChunkIndex, &doc.ByteStart, &doc.ByteEnd, &doc.ChatID, &doc.ConversationID, &doc.ProposalID, &doc.Status, &doc.Priority, &lastRecalledAt, &doc.RecallCount, &entitiesJSON, &tagsJSON, &updatedAt, &doc.ContentHash, &doc.EmbeddingModelID, &doc.IndexBuildID); err != nil {
				return nil, err
			}
			doc.Score = fixedScore
		} else {
			var rank float64
			if err := rows.Scan(&doc.ID, &doc.Kind, &doc.Title, &doc.Body, &doc.Handle, &doc.SourceID, &doc.Page, &doc.ChunkIndex, &doc.ByteStart, &doc.ByteEnd, &doc.ChatID, &doc.ConversationID, &doc.ProposalID, &doc.Status, &doc.Priority, &lastRecalledAt, &doc.RecallCount, &entitiesJSON, &tagsJSON, &updatedAt, &doc.ContentHash, &doc.EmbeddingModelID, &doc.IndexBuildID, &rank); err != nil {
				return nil, err
			}
			doc.Score = 0.35 + cappedRatio(-rank, 12)*0.50
		}
		doc.Priority = NormalizePriority(doc.Priority)
		doc.Entities = parseStringList(entitiesJSON)
		doc.Tags = parseStringList(tagsJSON)
		if ts, err := time.Parse(time.RFC3339Nano, updatedAt); err == nil {
			doc.UpdatedAt = ts
		}
		if lastRecalledAt.Valid && strings.TrimSpace(lastRecalledAt.String) != "" {
			if ts, err := time.Parse(time.RFC3339Nano, lastRecalledAt.String); err == nil {
				doc.LastRecalledAt = ts
			}
		}
		out = append(out, doc)
	}
	return out, rows.Err()
}

// RRF constants. k=60 is the cookbook default
// (https://www.anthropic.com/news/contextual-retrieval, mirrored across
// Elastic/Vespa/Weaviate docs). Group weights bias toward exact-id matches
// and the semantic backend; FTS BM25 gets the smallest weight because its
// keyword signal spikes most often on incidental token overlap.
const (
	rrfK            = 60
	rrfWeightExact  = 1.0
	rrfWeightFTS    = 0.6
	rrfWeightVector = 0.8
)

// mergeDocumentsRRF fuses three ranked groups (exact, fts, vector) into a
// single ordered slice via Reciprocal Rank Fusion: score(doc) = Σ_g
// (w_g / (k + rank_in_g + 1)). Each input slice is assumed already ordered
// from most-relevant to least-relevant; rank is taken from slice position.
//
// The fused score overwrites Document.Score on the returned values so the
// downstream recency-decay multiplier (memory_search.go:185) sees a value
// that is comparable across backends. The previous per-backend Score
// calibration in scanDocuments is therefore retained but ignored here.
func mergeDocumentsRRF(exact, fts, vector []Document, limit int) []Document {
	if limit <= 0 {
		limit = defaultSearchLimit
	}
	type entry struct {
		doc         Document
		score       float64
		scoreExact  float64
		scoreFTS    float64
		scoreVector float64
	}
	byID := map[string]*entry{}
	type channel int
	const (
		chanExact  channel = 0
		chanFTS    channel = 1
		chanVector channel = 2
	)
	accumulate := func(group []Document, weight float64, ch channel) {
		for rank, doc := range group {
			if doc.ID == "" {
				continue
			}
			contribution := weight / float64(rrfK+rank+1)
			if existing, ok := byID[doc.ID]; ok {
				existing.score += contribution
				switch ch {
				case chanExact:
					existing.scoreExact += contribution
				case chanFTS:
					existing.scoreFTS += contribution
				case chanVector:
					existing.scoreVector += contribution
				}
				continue
			}
			// Copy the doc so later contributions cannot stomp on the
			// metadata we keep here.
			docCopy := doc
			e := &entry{doc: docCopy, score: contribution}
			switch ch {
			case chanExact:
				e.scoreExact = contribution
			case chanFTS:
				e.scoreFTS = contribution
			case chanVector:
				e.scoreVector = contribution
			}
			byID[doc.ID] = e
		}
	}
	accumulate(exact, rrfWeightExact, chanExact)
	accumulate(fts, rrfWeightFTS, chanFTS)
	accumulate(vector, rrfWeightVector, chanVector)

	out := make([]Document, 0, len(byID))
	for _, e := range byID {
		doc := e.doc
		doc.Score = e.score
		doc.ScoreExact = e.scoreExact
		doc.ScoreFTS = e.scoreFTS
		doc.ScoreVector = e.scoreVector
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
