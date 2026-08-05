package documents

import "github.com/chetto1983/aura/internal/embeddings"

// EmbeddingGenerator is the document pipeline's narrow embedding dependency.
type EmbeddingGenerator = embeddings.Embedder

// EmbeddingClient is retained for source compatibility. The only transport
// implementation lives in internal/embeddings and is shared with memory.
type EmbeddingClient = embeddings.Client
