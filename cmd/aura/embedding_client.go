package main

import (
	"net/http"

	"github.com/chetto1983/aura/internal/config"
	"github.com/chetto1983/aura/internal/documents"
)

// embeddingClient builds a documents.EmbeddingClient resolving the ONE-knob
// local↔cloud embed swap (config.EmbedRoute, D-28): AURA_EMBED_MODEL unset → the
// local granite sidecar with no auth; set → the shared OpenRouter endpoint with
// the single OPENROUTER_API_KEY. Dimensions is the single AURA_EMBED_DIMENSIONS
// width the client validates and (on the cloud route) requests. Centralized so the
// four construction sites (chat, docs generator/query, doctor) stay identical.
func embeddingClient(cfg *config.Config, client *http.Client) *documents.EmbeddingClient {
	base, key, model := cfg.EmbedRoute()
	return &documents.EmbeddingClient{
		BaseURL:    base,
		Model:      model,
		APIKey:     key,
		Client:     client,
		Dimensions: cfg.Neo4j.EmbedDimensions,
	}
}
