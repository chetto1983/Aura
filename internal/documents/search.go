package documents

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"strings"
)

const (
	defaultSearchLimit = 8
	maxSearchLimit     = 20
)

// Searcher runs sparse full-text search against indexed document chunks.
type Searcher struct {
	Client KnowledgeClient
}

// Search returns ranked chunk hits for a document search request.
func (s *Searcher) Search(ctx context.Context, req SearchRequest) ([]SearchHit, error) {
	if s.Client == nil {
		return nil, fmt.Errorf("document searcher has no knowledge client")
	}
	query := sanitizeFulltextQuery(req.Query)
	if query == "" {
		return nil, fmt.Errorf("document search query is empty")
	}
	limit := req.Limit
	if limit <= 0 {
		limit = defaultSearchLimit
	}
	if limit > maxSearchLimit {
		limit = maxSearchLimit
	}
	if req.DocumentID != "" {
		return s.scopedSearch(ctx, query, req.DocumentID, limit)
	}
	rows, err := s.Client.Read(ctx, sparseSearchQuery, map[string]any{
		"query":           query,
		"document_id":     "",
		"limit":           limit,
		"candidate_limit": limit * 3,
	})
	if err != nil {
		return nil, err
	}
	return hitsFromRows(rows)
}

// scopedSearch is the per-document sparse seed (spike 075): with a document_id supplied it
// ranks that document's OWN chunks by query-term overlap — a native metadata pre-filter on
// the indexed document_id — instead of the global db.index.fulltext.queryNodes(k)-THEN-
// filter, so a small or freshly-uploaded document is never crowded out of the global
// fulltext top-k by a large corpus (the spike-075 crowding case). The fulltext index cannot
// be restricted to a node set, so relevance is the count of distinct query terms present;
// the reranker reorders these seeds. It is the sparse twin of docScopedVectorSeedQuery —
// together they keep a scoped document reachable on both the dense and the fallback path.
func (s *Searcher) scopedSearch(ctx context.Context, query, documentID string, limit int) ([]SearchHit, error) {
	rows, err := s.Client.Read(ctx, docScopedSparseQuery, map[string]any{
		"document_id": documentID,
		"terms":       strings.Fields(strings.ToLower(query)),
		"limit":       limit,
	})
	if err != nil {
		return nil, err
	}
	return hitsFromRows(rows)
}

// hitsFromRows projects Neo4j result rows into SearchHits. Shared by the sparse
// Searcher and the two-stage Retrieve seed/expand stages so all three produce
// identical SearchHit shapes.
func hitsFromRows(rows []map[string]any) ([]SearchHit, error) {
	hits := make([]SearchHit, 0, len(rows))
	for _, row := range rows {
		hit, err := searchHitFromRow(row)
		if err != nil {
			return nil, err
		}
		hits = append(hits, hit)
	}
	return hits, nil
}

func sanitizeFulltextQuery(query string) string {
	query = strings.TrimSpace(query)
	query = strings.Map(func(r rune) rune {
		switch r {
		case '+', '-', '&', '|', '!', '(', ')', '{', '}', '[', ']', '^', '"', '~', '*', '?', ':', '\\', '/':
			return ' '
		default:
			if r < 32 {
				return ' '
			}
			return r
		}
	}, query)
	return strings.Join(strings.Fields(query), " ")
}

func searchHitFromRow(row map[string]any) (SearchHit, error) {
	var locator Locator
	if raw := stringValue(row["locator_json"]); raw != "" {
		if err := json.Unmarshal([]byte(raw), &locator); err != nil {
			return SearchHit{}, fmt.Errorf("decode locator: %w", err)
		}
	}
	return SearchHit{
		DocumentID:  stringValue(row["document_id"]),
		ChunkID:     stringValue(row["chunk_id"]),
		FileName:    stringValue(row["file_name"]),
		Score:       floatValue(row["score"]),
		Text:        stringValue(row["text"]),
		Locator:     locator,
		HeadingPath: stringSliceValue(row["heading_path"]),
	}, nil
}

func stringValue(v any) string {
	switch s := v.(type) {
	case string:
		return s
	case fmt.Stringer:
		return s.String()
	default:
		return ""
	}
}

func floatValue(v any) float64 {
	switch n := v.(type) {
	case float64:
		return n
	case float32:
		return float64(n)
	case int:
		return float64(n)
	case int64:
		return float64(n)
	case int32:
		return float64(n)
	case json.Number:
		f, _ := n.Float64()
		return f
	default:
		return math.NaN()
	}
}

func stringSliceValue(v any) []string {
	switch values := v.(type) {
	case []string:
		return append([]string(nil), values...)
	case []any:
		out := make([]string, 0, len(values))
		for _, value := range values {
			if s := stringValue(value); s != "" {
				out = append(out, s)
			}
		}
		return out
	default:
		return nil
	}
}

const sparseSearchQuery = `
CALL db.index.fulltext.queryNodes('chunk_text', $query, {limit: $candidate_limit})
YIELD node, score
WHERE ($document_id = "" OR node.document_id = $document_id)
  AND coalesce(node.active, true) = true
  AND node.deleted_at IS NULL
RETURN
  node.document_id AS document_id,
  coalesce(node.file_name, "") AS file_name,
  node.id AS chunk_id,
  node.text AS text,
  node.locator_json AS locator_json,
  node.heading_path AS heading_path,
  score AS score
ORDER BY score DESC
LIMIT $limit
`

// docScopedSparseQuery is the scoped sparse seed (spike 075): with a document_id supplied
// it ranks that document's OWN chunks by query-term overlap (a native metadata pre-filter
// on the indexed document_id), so a small or freshly-uploaded document's chunks are always
// reachable regardless of the global corpus size. The fulltext index cannot be restricted
// to a node set, so score = the count of distinct query terms present; chunks with no term
// match are dropped and the reranker reorders the rest. The projection matches
// sparseSearchQuery so hitsFromRows and the rerank/expand stages are identical.
const docScopedSparseQuery = `
MATCH (node:Chunk {document_id: $document_id})
WHERE node.text IS NOT NULL
  AND coalesce(node.active, true) = true
  AND node.deleted_at IS NULL
WITH node, size([term IN $terms WHERE toLower(node.text) CONTAINS term]) AS score
WHERE score > 0
RETURN
  node.document_id AS document_id,
  coalesce(node.file_name, "") AS file_name,
  node.id AS chunk_id,
  node.text AS text,
  node.locator_json AS locator_json,
  node.heading_path AS heading_path,
  score AS score
ORDER BY score DESC
LIMIT $limit
`
