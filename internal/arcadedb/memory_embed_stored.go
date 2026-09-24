package arcadedb

import (
	"context"
	"fmt"
	"strings"

	"github.com/chetto1983/aura/internal/embeddings"
)

// controlInput tells a refused text from a refused route. An endpoint answers 400 to every
// input when the model id is wrong; read as "these texts are bad", one typo in a route
// would stamp a whole memory refused. A route that embeds this and refuses a text has
// refused the text.
const controlInput = "Aura memory embedding route check."

// embedStored embeds texts as stored documents: one storedVector per text, in order.
//
// The space is read BEFORE the request, and that order is the safe one. A model swapped
// during the request can then only make a vector claim the older space, which the pass
// re-embeds; read afterwards, an old model's vector could claim the new space and open the
// gate over it.
//
// A request the endpoint refuses as input (embeddings.RejectsInput) is split in halves
// until each refused text stands alone, and only that text is marked refused -- its
// storedVector carries the space and no vector -- once controlInput has shown the route
// itself works. Any other failure belongs to the route and is returned; nothing is marked.
// A vector of the wrong width is neither, and that text gets an empty storedVector.
func (c *Client) embedStored(ctx context.Context, texts []string) ([]storedVector, error) {
	space, err := c.embedder.Space(ctx)
	if err != nil {
		return nil, err
	}
	if space.ID == "" {
		return nil, fmt.Errorf("arcadedb: the embedding route named no space")
	}
	out := make([]storedVector, len(texts))
	routeWorks := false
	var embed func(lo, hi int) error
	embed = func(lo, hi int) error {
		vectors, err := c.embedder.Embed(ctx, withTask(taskDocumentPrefix, texts[lo:hi]))
		switch {
		case err == nil:
			for i, vector := range vectors {
				if i < hi-lo && len(vector) == vectorDimensions {
					out[lo+i] = storedVector{vector: vector, space: space.ID}
				}
			}
			return nil
		case !embeddings.RejectsInput(err):
			return err
		case hi-lo > 1:
			mid := lo + (hi-lo)/2
			if err := embed(lo, mid); err != nil {
				return err
			}
			return embed(mid, hi)
		}
		if !routeWorks {
			if _, controlErr := c.embedder.Embed(ctx, withTask(taskDocumentPrefix, []string{controlInput})); controlErr != nil {
				return fmt.Errorf("arcadedb: the embedding route refuses a control input too (%v): %w", controlErr, err)
			}
			routeWorks = true
		}
		out[lo] = storedVector{space: space.ID}
		return nil
	}
	if len(texts) > 0 {
		if err := embed(0, len(texts)); err != nil {
			return nil, err
		}
	}
	return out, nil
}

// embedOne is embedStored for a single-text write, and fail-soft: a route that cannot
// answer leaves the text with neither vector nor stamp, and the write goes ahead. A fact
// that was not stored is lost; one stored without a vector is found lexically today and
// embedded by the pass later.
func (c *Client) embedOne(ctx context.Context, text string) storedVector {
	if c == nil || c.embedder == nil || strings.TrimSpace(text) == "" {
		return storedVector{}
	}
	vectors, err := c.embedStored(ctx, []string{text})
	if err != nil {
		return storedVector{}
	}
	return vectors[0]
}

// embedDistinct embeds each distinct non-blank statement once, in one request, keyed by
// statement: a batch may restate a fact, and the vector is a pure function of the text.
// Fail-soft like embedOne: a statement the route did not answer has no entry.
//
// A memory batch calls it BEFORE its ArcadeDB transaction opens, deliberately: holding a
// write transaction across an HTTP call to a second service would put that service's
// latency inside the lock the batch holds on the identity.
func (c *Client) embedDistinct(ctx context.Context, statements []string) map[string]storedVector {
	out := make(map[string]storedVector, len(statements))
	seen := make(map[string]bool, len(statements))
	unique := make([]string, 0, len(statements))
	for _, statement := range statements {
		if seen[statement] || strings.TrimSpace(statement) == "" {
			continue
		}
		seen[statement] = true
		unique = append(unique, statement)
	}
	if len(unique) == 0 || c == nil || c.embedder == nil {
		return out
	}
	vectors, err := c.embedStored(ctx, unique)
	if err != nil {
		return out
	}
	for i, statement := range unique {
		if vectors[i].vector != nil || vectors[i].space != "" {
			out[statement] = vectors[i]
		}
	}
	return out
}
