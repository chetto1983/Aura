package documents

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/chetto1983/aura/internal/db/sqlc"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
)

// SetCard records what INGEST measured about a document, from the file itself
// (internal/documents/filecard). Every file gets one the moment it lands, which is the
// difference between a library and a list of things somebody already found.
//
// documentID may be either namespace — the `doc_<hex>` search id a retrieval hit carries,
// or the catalog uuid — because the caller holds whichever its own caller gave it.
//
// The write is identity-scoped twice over: in SQL, and in the RLS session the transaction
// opens. A document id alone must never be enough to overwrite what another person's
// library says about their file.
func (s *PostgresCatalogStore) SetCard(ctx context.Context, identityID, documentID, card string) error {
	owner, err := pgUUID("identity_id", identityID)
	if err != nil {
		return err
	}
	return s.scoped(ctx, identityID, func(sc catalogTx) error {
		id, err := sc.digestTargetID(ctx, identityID, documentID)
		if err != nil {
			return err
		}
		if _, err := sc.q.SetDocumentCard(ctx, sqlc.SetDocumentCardParams{
			ID: id, IdentityID: owner, Card: card,
		}); err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				return fmt.Errorf("%w: %s", ErrDocumentNotCatalogued, documentID)
			}
			return fmt.Errorf("set document card: %w", err)
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
