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

// CardQuery is one card-leg request: the query and its scope, and for the dense leg the
// query's vector and the space it is in.
type CardQuery struct {
	IdentityID   string
	Query        string
	Vector       []float64
	Space        string
	DocumentIDs  []string
	SourceScopes []SourceScope
	Limit        int
}

func (q CardQuery) filter() arcadedb.CandidateFilter {
	sourceKeys, sourcePrefixes := ArcadeSourceFilters(q.SourceScopes)
	return arcadedb.CandidateFilter{
		IdentityID: q.IdentityID, Limit: q.Limit, DocumentIDs: q.DocumentIDs,
		SourceKeys: sourceKeys, SourcePrefixes: sourcePrefixes,
	}
}

// DocumentsDenseOpen asks the identity's documents gate (arcadedb embedding_space.go).
func (c *ArcadeRetrievalControlPlane) DocumentsDenseOpen(
	ctx context.Context, identityID, space string,
) (bool, error) {
	if c == nil || c.Index == nil {
		return false, errRetrievalControlPlaneUnset
	}
	return c.Index.DocumentsDenseOpen(ctx, identityID, space)
}

// RouteDocumentCards ranks documents by their own description inside the exact same scope
// as the passage leg. An ignored filter here would turn a scoped search into an unscoped one.
func (c *ArcadeRetrievalControlPlane) RouteDocumentCards(ctx context.Context, q CardQuery) ([]RetrievalCard, error) {
	if c == nil || c.Index == nil {
		return nil, errRetrievalControlPlaneUnset
	}
	found, err := c.Index.DocumentCardsScoped(ctx, q.filter(), q.Query, q.Vector, q.Space)
	if err != nil {
		return nil, err
	}
	return retrievalCards(found), nil
}

// LexicalDocumentCards ranks documents by their card and file name alone, inside the same
// scope, for lexical mode (spec §8).
func (c *ArcadeRetrievalControlPlane) LexicalDocumentCards(ctx context.Context, q CardQuery) ([]RetrievalCard, error) {
	if c == nil || c.Index == nil {
		return nil, errRetrievalControlPlaneUnset
	}
	found, err := c.Index.LexicalDocumentCards(ctx, q.filter(), q.Query)
	if err != nil {
		return nil, err
	}
	return retrievalCards(found), nil
}

// retrievalCards is ArcadeDB's card record as the ranking reads it.
func retrievalCards(found []arcadedb.DocumentCard) []RetrievalCard {
	cards := make([]RetrievalCard, 0, len(found))
	for _, card := range found {
		cards = append(cards, RetrievalCard{
			DocumentID: card.SearchDocumentID,
			// The file name IS the title. There is no catalog row to hold a nicer one, and
			// a name is what a person uses to ask for a file.
			Title:            card.FileName,
			SourceKind:       card.SourceKind,
			SourceKey:        card.SourceKey,
			Card:             card.Card,
			Rank:             card.Score,
			OriginalSHA256:   card.RawSHA256,
			NormalizedSHA256: card.NormalizedSHA256,
			SizeBytes:        card.SizeBytes,
			PassageCount:     card.PassageCount,
			IndexedAt:        card.IndexedAt,
		})
	}
	return cards
}
