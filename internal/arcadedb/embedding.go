package arcadedb

import (
	"context"

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
