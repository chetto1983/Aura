package arcadedb

import (
	"context"
	"time"

	"github.com/chetto1983/aura/internal/config"
	"github.com/chetto1983/aura/internal/embeddings"
)

// DenseEmbedder is the memory dense leg: it embeds, and it names the space its vectors are
// in, so every stored vector carries that space (spec §2) and every dense read can check
// the corpus is in it (spec §3). Optional: with none, memory retrieval is the lexical leg
// alone, which is the behaviour that shipped.
type DenseEmbedder interface {
	Embed(ctx context.Context, texts []string) ([][]float64, error)
	Space(ctx context.Context) (embeddings.Space, error)
}

// EmbeddingGemma's query and stored-text prefixes are asymmetric. Memory facts
// are stored retrieval documents; natural-language searches are queries.
const (
	taskQueryPrefix    = embeddings.QueryPrefix
	taskDocumentPrefix = embeddings.UntitledDocumentPrefix
)

func withTask(prefix string, texts []string) []string {
	return embeddings.Prefix(prefix, texts)
}

// NewMemoryEmbedder resolves the memory family's route at the index width, or returns nil
// when dense embedding is switched off. The result is an interface on purpose: a nil
// *embeddings.Route stored in one is non-nil, and every "no embedder" branch would call
// through it. credential is read on every request of a cloud route.
func NewMemoryEmbedder(embed config.EmbedConfig, credential func() string) DenseEmbedder {
	route := embeddings.NewRoute(embed, credential, vectorDimensions, DefaultTimeout)
	if route == nil {
		return nil
	}
	return route
}

// SpaceWithin names the space e embeds memory in, for a boot log, giving up after timeout:
// the listener starts after it, and the embeddings client's own timeout is a minute. The
// daemon and arcadedb-mcp both log it, since the two must name the same space or each
// re-stamps the other's writes.
func SpaceWithin(e DenseEmbedder, timeout time.Duration) (embeddings.Space, error) {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	return e.Space(ctx)
}
