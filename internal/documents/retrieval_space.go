package documents

import (
	"context"
	"errors"
	"fmt"

	"github.com/chetto1983/aura/internal/embeddings"
)

// QueryEmbedder embeds a query and names the space its vector is in (embeddings.Route), so
// the dense legs run only over a library wholly in that space (spec §3).
type QueryEmbedder interface {
	Embed(ctx context.Context, texts []string) ([][]float64, error)
	Space(ctx context.Context) (embeddings.Space, error)
}

// queryVector is the query embedded for the dense legs, and the space its vector is in.
type queryVector struct {
	vector []float64
	space  string
}

var errNoQueryEmbedder = errors.New("documents: retrieval embedder is not configured")

// denseQuery is the dense legs' entry: the embedded query, or the degradation that keeps the
// read off them and its cause. The gate is asked before the query is embedded, so a closed
// gate costs no request. The space is read again after, because a model swapped in between
// would rank a new model's vector against a library checked in the old space.
func (r *HostRetriever) denseQuery(ctx context.Context, identityID, query string) (queryVector, string, error) {
	if r.Embedder == nil {
		return queryVector{}, DegradationEmbedding, errNoQueryEmbedder
	}
	space, err := r.Embedder.Space(ctx)
	if err != nil {
		return queryVector{}, DegradationEmbedding, err
	}
	open, err := r.ControlPlane.DocumentsDenseOpen(ctx, identityID, space.ID)
	if err != nil {
		return queryVector{}, DegradationSpaceCheck, err
	}
	if !open {
		return queryVector{}, DegradationSpaceMismatch,
			fmt.Errorf("the library holds vectors outside space %s", space.ID)
	}
	vectors, err := r.Embedder.Embed(ctx, embeddings.RetrievalQueries([]string{query}))
	if err != nil {
		return queryVector{}, DegradationEmbedding, err
	}
	if len(vectors) != 1 {
		return queryVector{}, DegradationEmbedding,
			fmt.Errorf("documents: embedder returned %d vectors, want 1", len(vectors))
	}
	after, err := r.Embedder.Space(ctx)
	if err != nil {
		return queryVector{}, DegradationEmbedding, err
	}
	if after.ID != space.ID {
		return queryVector{}, DegradationSpaceMismatch,
			fmt.Errorf("the embedding space moved from %s to %s during the query", space.ID, after.ID)
	}
	return queryVector{vector: vectors[0], space: space.ID}, "", nil
}
