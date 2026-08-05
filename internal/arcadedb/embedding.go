package arcadedb

import (
	"net/http"
	"strings"
	"time"

	"github.com/chetto1983/aura/internal/embeddings"
)

// Embedder remains optional so memory retrieval can degrade to its lexical leg
// when no embedding route is configured.
type Embedder = embeddings.Embedder

// EmbeddingGemma's query and stored-text prefixes are asymmetric. Memory facts
// are stored retrieval documents; natural-language searches are queries.
const (
	taskQueryPrefix    = embeddings.QueryPrefix
	taskDocumentPrefix = embeddings.UntitledDocumentPrefix
)

func withTask(prefix string, texts []string) []string {
	return embeddings.Prefix(prefix, texts)
}

// SidecarEmbedder is retained as the memory package's public name while the
// transport itself has one implementation shared with documents.
type SidecarEmbedder = embeddings.Client

// NewSidecarEmbedder builds the optional memory embedder. The fixed dimension
// matches the existing memory vector index; changing it requires rebuilding the
// index rather than accepting mixed vector widths.
func NewSidecarEmbedder(baseURL, model, apiKey string, timeout time.Duration) *SidecarEmbedder {
	baseURL = strings.TrimRight(strings.TrimSpace(baseURL), "/")
	if baseURL == "" {
		return nil
	}
	if timeout <= 0 {
		timeout = DefaultTimeout
	}
	return &embeddings.Client{
		BaseURL:    baseURL,
		Model:      strings.TrimSpace(model),
		APIKey:     strings.TrimSpace(apiKey),
		Client:     &http.Client{Timeout: timeout},
		Dimensions: vectorDimensions,
		Timeout:    timeout,
	}
}
