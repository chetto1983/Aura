package documents

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/chetto1983/aura/internal/db/sqlc"
)

// SetDigest records what a document IS, for the only question the index still
// answers: which file to open. Keyed by the SEARCH document id, because that is
// the id the ingestion path holds when the digest is built.
func (s *PostgresCatalogStore) SetDigest(ctx context.Context, searchDocumentID, digest string) error {
	id, err := s.catalogIDForSearchDocument(ctx, searchDocumentID)
	if err != nil {
		return err
	}
	if _, err := s.q.SetDocumentDigest(ctx, sqlc.SetDocumentDigestParams{
		ID:     id,
		Digest: digest,
	}); err != nil {
		return fmt.Errorf("set document digest: %w", err)
	}
	return nil
}

// SearchDigests ranks one identity's library by ts_rank over the weighted
// title/tags/digest vector. No embedding, no reranker, no graph — the whole
// retrieval stack this replaces existed to find a PASSAGE, and passages are no
// longer what the agent needs.
//
// The returned DocumentID is the SEARCH id (`doc_<hex>`) when the document has
// one, because that is what document_open and every retrieval-shaped caller
// expect; documents catalogued without one fall back to their catalog uuid,
// which document_open also accepts.
func (s *PostgresCatalogStore) SearchDigests(
	ctx context.Context,
	identityID, query string,
	limit int,
) ([]DigestHit, error) {
	pgIdentityID, err := pgUUID("identity_id", identityID)
	if err != nil {
		return nil, err
	}
	if limit <= 0 || limit > maxDigestLimit {
		limit = defaultDigestLimit
	}
	rows, err := s.q.SearchDocumentDigests(ctx, sqlc.SearchDocumentDigestsParams{
		IdentityID: pgIdentityID,
		Query:      strings.TrimSpace(query),
		RowLimit:   int32(limit), //nolint:gosec // bounded by maxCatalogDocs just above.
	})
	if err != nil {
		return nil, fmt.Errorf("search document digests: %w", err)
	}
	hits := make([]DigestHit, 0, len(rows))
	for _, row := range rows {
		hits = append(hits, DigestHit{
			DocumentID: digestDocumentID(row),
			Title:      row.Title,
			Tags:       append([]string(nil), row.Tags...),
			Digest:     row.Digest,
			Rank:       float64(row.Rank),
		})
	}
	SortDigestHits(hits)
	return hits, nil
}

const (
	defaultDigestLimit = 8
	// A library listing is for choosing one file, not for paging: past a few dozen
	// the model is picking from noise, and the right fix is a better query.
	maxDigestLimit = 50
)

// digestDocumentID prefers the search id the rest of the document surface speaks.
func digestDocumentID(row sqlc.SearchDocumentDigestsRow) string {
	var metadata map[string]any
	if len(row.Metadata) > 0 {
		if err := json.Unmarshal(row.Metadata, &metadata); err == nil {
			if id, ok := metadata["search_document_id"].(string); ok && strings.TrimSpace(id) != "" {
				return id
			}
		}
	}
	return uuidString(row.ID)
}
