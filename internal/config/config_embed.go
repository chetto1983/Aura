package config

// DefaultEmbedDimensions is the NATIVE width of the repo's default embedding sidecar
// (EmbeddingGemma-300M). The width is deliberately not restated in prose elsewhere: it
// moved 384 -> 768 -> 1024 -> 768 while comments went on asserting the old number. A
// deployment may set a smaller AURA_EMBED_DIMENSIONS, but only together with vector
// indexes created at that same width.
const DefaultEmbedDimensions = 768

// EmbedConfig is the embedding-sidecar wiring: an OpenAI-compatible endpoint, the
// contract width every stored vector is narrowed to, and the model that decides
// whether the endpoint is the local sidecar or a hosted one (see EmbedRoute).
//
// It lives here rather than in an owning subsystem package because there is no
// longer one: the embedder is read by the reasoning classifier, the doctor, the
// adaptive controls and memory alike.
type EmbedConfig struct {
	BaseURL    string // AURA_EMBED_BASE_URL — OpenAI-compat embeddings base URL
	Dimensions int    // AURA_EMBED_DIMENSIONS — contract width; a narrower response is an error
	Model      string // AURA_EMBED_MODEL — set to a hosted model to swap embeddings to OpenRouter; empty = local sidecar
}
