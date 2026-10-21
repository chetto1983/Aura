package docfields

import (
	"context"
	"fmt"

	"github.com/chetto1983/aura/internal/arcadedb"
)

// ContentPassage holds indexed text, which may overlap and omit original layout.
type ContentPassage struct {
	Ordinal int    `json:"ordinal"`
	Text    string `json:"text"`
}

// ContentPage uses one-row lookahead to report more content without loading a whole document.
type ContentPage struct {
	Passages []ContentPassage `json:"passages"`
	Offset   int              `json:"offset"`
	HasMore  bool             `json:"has_more"`
}

// Content checks tenant-local document membership before reading ordered indexed passages.
func (s *Store) Content(ctx context.Context, identityID, documentID string, offset, limit int) (ContentPage, error) {
	if offset < 0 || limit < 0 || limit > 50 {
		return ContentPage{}, invalid("content offset must be nonnegative and limit at most 50")
	}
	if limit == 0 {
		limit = 25
	}
	c, err := s.client(ctx, identityID)
	if err != nil {
		return ContentPage{}, err
	}
	docs, err := c.Query(ctx, "SELECT source_key FROM IndexedDocument WHERE search_document_id = :id", map[string]any{"id": documentID})
	if err != nil && !arcadedb.IsMissingType(err, arcadedb.IndexedDocumentType) {
		return ContentPage{}, fmt.Errorf("read document for content: %w", err)
	}
	if len(docs) == 0 {
		return ContentPage{}, fmt.Errorf("%w: document %s", ErrNotFound, documentID)
	}
	result := ContentPage{Passages: []ContentPassage{}, Offset: offset}
	rows, err := c.Query(ctx, fmt.Sprintf("SELECT ordinal, text FROM Passage WHERE source_key = :key ORDER BY ordinal, @rid SKIP %d LIMIT %d", offset, limit+1), map[string]any{"key": str(docs[0]["source_key"])})
	if err != nil {
		return ContentPage{}, fmt.Errorf("read indexed content: %w", err)
	}
	result.HasMore = len(rows) > limit
	for _, row := range rows[:min(len(rows), limit)] {
		ordinal, _ := row["ordinal"].(float64)
		result.Passages = append(result.Passages, ContentPassage{Ordinal: int(ordinal), Text: str(row["text"])})
	}
	return result, nil
}
