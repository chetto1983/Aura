package documents

import (
	"context"

	"github.com/chetto1983/aura/internal/arcadedb"
)

// Lexical mode (spec §8). When the dense legs cannot run -- the library holds vectors from
// another embedding space, the gate that says so could not be read, or the query could not be
// embedded -- both legs read the full-text indexes instead, and the status says the answer is
// lexical: candidates matched by their words. This deliberately amends the measured "no
// lexical-only path" (arcadedb RetrievalLegFused): that measurement compared two fusions, not
// one leg answering while the other is unavailable.

// routeCards runs the card leg the query can have: ranked against its vector, or lexically.
func (r *HostRetriever) routeCards(ctx context.Context, card CardQuery, dense queryVector) ([]RetrievalCard, error) {
	if dense.vector == nil {
		return r.ControlPlane.LexicalDocumentCards(ctx, card)
	}
	card.Vector, card.Space = dense.vector, dense.space
	return r.ControlPlane.RouteDocumentCards(ctx, card)
}

// passages runs the passage leg the query can have: fused against its vector, or lexically.
func (r *HostRetriever) passages(
	ctx context.Context,
	filter arcadedb.CandidateFilter,
	query string,
	dense queryVector,
	strategy arcadedb.FusionStrategy,
) ([]arcadedb.PassageCandidate, error) {
	if dense.vector == nil {
		return r.PassageIndex.LexicalCandidates(ctx, filter, query)
	}
	return r.PassageIndex.FusedCandidates(ctx, arcadedb.FusedCandidateQuery{
		CandidateFilter: filter, Query: query, Embedding: dense.vector, Space: dense.space, Strategy: strategy,
	})
}
