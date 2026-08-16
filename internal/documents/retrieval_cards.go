package documents

import (
	"context"
	"errors"

	"github.com/chetto1983/aura/internal/arcadedb"
)

var errRetrievalControlPlaneUnset = errors.New("documents: retrieval control plane has no document index")

// ArcadeRetrievalControlPlane routes cards and bounds scope from ArcadeDB.
//
// It replaces PostgresRetrievalStore, whose two SQL statements joined aura.documents to
// its versions and assets to find "ready" documents -- a shape that only ever had rows for
// the upload path, so a document reconciled from the bucket was invisible to the card leg
// no matter what it contained. Both jobs are full-text ranking over records the reconciler
// already writes, and ArcadeDB indexes those with the same analyzer as the passages, so
// one engine now answers both legs of the cascade instead of two disagreeing about how a
// query tokenises.
type ArcadeRetrievalControlPlane struct {
	Index *arcadedb.DocumentIndex
}

// ResolveDocumentScope narrows caller-supplied ids to the ones this identity has.
func (c *ArcadeRetrievalControlPlane) ResolveDocumentScope(
	ctx context.Context, identityID string, documentIDs []string,
) ([]string, error) {
	if c == nil || c.Index == nil {
		return nil, errRetrievalControlPlaneUnset
	}
	return c.Index.ResolveDocumentScope(ctx, identityID, documentIDs)
}

// DocumentNames resolves display names for documents the card leg did not rank.
func (c *ArcadeRetrievalControlPlane) DocumentNames(
	ctx context.Context, identityID string, documentIDs []string,
) (map[string]string, error) {
	if c == nil || c.Index == nil {
		return nil, errRetrievalControlPlaneUnset
	}
	return c.Index.DocumentNames(ctx, identityID, documentIDs)
}

// RouteDocumentCards ranks documents by their own description.
//
// The scope argument is accepted and ignored, and that is not an oversight: the card leg
// answers "which documents look relevant at all", so narrowing it to a scope the caller
// already named would only return what the caller already had. The scope constrains the
// passage legs, where it decides which passages may be searched.
func (c *ArcadeRetrievalControlPlane) RouteDocumentCards(
	ctx context.Context, identityID, query string, _ []string, limit int,
) ([]RetrievalCard, error) {
	if c == nil || c.Index == nil {
		return nil, errRetrievalControlPlaneUnset
	}
	found, err := c.Index.DocumentCards(ctx, identityID, query, limit)
	if err != nil {
		return nil, err
	}
	cards := make([]RetrievalCard, 0, len(found))
	for _, card := range found {
		cards = append(cards, RetrievalCard{
			DocumentID: card.SearchDocumentID,
			// The file name IS the title. There is no catalog row to hold a nicer one, and
			// a name is what a person uses to ask for a file.
			Title:          card.FileName,
			SourceKind:     card.SourceKind,
			SourceKey:      card.SourceKey,
			Card:           card.Card,
			Rank:           card.Score,
			OriginalSHA256: card.RawSHA256,
		})
	}
	return cards, nil
}
