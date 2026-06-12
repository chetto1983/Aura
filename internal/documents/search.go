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

type Searcher struct {
	Client KnowledgeClient
}

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
	candidateLimit := limit
	if req.DocumentID == "" {
		candidateLimit = limit * 3
	}
	rows, err := s.Client.Read(ctx, sparseSearchQuery, map[string]any{
		"query":           query,
		"document_id":     req.DocumentID,
		"limit":           limit,
		"candidate_limit": candidateLimit,
	})
	if err != nil {
		return nil, err
	}
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
