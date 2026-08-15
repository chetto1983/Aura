package documents

import "github.com/chetto1983/aura/internal/embeddings"

// EmbeddingClient is the name the rest of the tree calls the embedding transport by:
// cmd/aura builds one from the resolved embed route, and the reasoning classifier and
// semantic index both take it. The implementation lives in internal/embeddings and is
// shared with memory, so this is an alias, not a second type.
type EmbeddingClient = embeddings.Client
