package documents

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/chetto1983/aura/internal/db/sqlc"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
)

// SetDigest records what a document IS, for the only question the index still
// answers: which file to open.
//
// documentID may be either namespace — the `doc_<hex>` search id a retrieval hit
// carries, or the catalog uuid — because the caller holds whichever its own
// caller gave it. The write is identity-scoped twice over: in SQL, and in the RLS
// session the transaction opens. A document id alone must never be enough to
// overwrite what another person's library says about their file, and this is
// reachable from a tool call.
func (s *PostgresCatalogStore) SetDigest(ctx context.Context, identityID, documentID, digest string) error {
	return s.setDescription(ctx, identityID, documentID, "digest",
		func(q *sqlc.Queries, id, owner pgtype.UUID) error {
			_, err := q.SetDocumentDigest(ctx, sqlc.SetDocumentDigestParams{
				ID: id, IdentityID: owner, Digest: digest,
			})
			return err
		})
}

// SetCard records what INGEST measured about a document, from the file itself.
//
// It is deliberately a different column and a different call from SetDigest: a
// re-ingest overwrites the machine's description and must leave the agent's note
// alone, and document_describe overwrites the note and must leave the structure
// alone. Same identity gate as SetDigest, for the same reason.
func (s *PostgresCatalogStore) SetCard(ctx context.Context, identityID, documentID, card string) error {
	return s.setDescription(ctx, identityID, documentID, "card",
		func(q *sqlc.Queries, id, owner pgtype.UUID) error {
			_, err := q.SetDocumentCard(ctx, sqlc.SetDocumentCardParams{
				ID: id, IdentityID: owner, Card: card,
			})
			return err
		})
}

// setDescription resolves the two ids both description writes need and turns a
// no-rows result into not-found. The two writes differ only in the column they
// touch; sharing the resolution keeps the identity gate in ONE place, which is
// the part that must not drift between them.
func (s *PostgresCatalogStore) setDescription(
	ctx context.Context,
	identityID, documentID, column string,
	write func(q *sqlc.Queries, id, owner pgtype.UUID) error,
) error {
	owner, err := pgUUID("identity_id", identityID)
	if err != nil {
		return err
	}
	return s.scoped(ctx, identityID, func(sc catalogTx) error {
		id, err := sc.digestTargetID(ctx, identityID, documentID)
		if err != nil {
			return err
		}
		if err := write(sc.q, id, owner); err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				return fmt.Errorf("%w: %s", ErrDocumentNotCatalogued, documentID)
			}
			return fmt.Errorf("set document %s: %w", column, err)
		}
		return nil
	})
}

// digestTargetID accepts either id namespace, same as OpenService does.
func (sc catalogTx) digestTargetID(ctx context.Context, identityID, documentID string) (pgtype.UUID, error) {
	documentID = strings.TrimSpace(documentID)
	if documentID == "" {
		return pgtype.UUID{}, fmt.Errorf("document_id is required")
	}
	if _, err := uuid.Parse(documentID); err == nil {
		return pgUUID("document_id", documentID)
	}
	return sc.catalogIDForSearchDocument(ctx, identityID, documentID)
}

// SearchDigests ranks one identity's library by ts_rank over the weighted
// title/tags/digest/card vector. No embedding, no reranker, no graph — the whole
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
	rowLimit := int32(defaultDigestLimit)
	if limit > 0 && limit <= maxDigestLimit {
		rowLimit = int32(limit)
	}
	return scopedValue(ctx, s, identityID, func(sc catalogTx) ([]DigestHit, error) {
		rows, err := sc.q.SearchDocumentDigests(ctx, sqlc.SearchDocumentDigestsParams{
			IdentityID: pgIdentityID,
			Query:      strings.TrimSpace(query),
			RowLimit:   rowLimit,
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
				Card:       row.Card,
				Rank:       float64(row.Rank),
				UpdatedAt:  row.UpdatedAt.Time,
			})
		}
		// The SQL already returns them in order. This re-sort exists so a caller that
		// merges hits from more than one source lands on the same order — it must
		// therefore agree with the statement, not quietly re-alphabetize it.
		SortDigestHits(hits)
		return hits, nil
	})
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
