package main

import (
	"log/slog"
	"time"

	"github.com/chetto1983/aura/internal/arcadedb"
	"github.com/chetto1983/aura/internal/config"
	"github.com/chetto1983/aura/internal/llm"
)

// liveKey reads the running LLM profile's key on every request: a key rotated in the cockpit
// replaces that profile in place (serve_settings.go primaryLLMRouteReloader) without a restart,
// and a boot copy would keep embedding with the revoked key (spec §5).
func liveKey(runtime *llm.Runtime) func() string {
	return func() string { return runtime.Snapshot().Config.APIKey }
}

// memoryEmbedder is the daemon's one memory route.
func memoryEmbedder(cfg *config.Config, runtime *llm.Runtime) arcadedb.DenseEmbedder {
	return arcadedb.NewMemoryEmbedder(cfg.Embed, liveKey(runtime))
}

// spaceBootTimeout bounds the one boot call that is only logged.
const spaceBootTimeout = 5 * time.Second

// logEmbeddingSpace names a family's embedding space at boot. The daemon's memory space must
// match arcadedb-mcp's, and its documents space the ingest supervisor's stamp; otherwise that
// family stays lexical, and this line is where an operator sees why.
func logEmbeddingSpace(logger *slog.Logger, family string, embedder arcadedb.DenseEmbedder, timeout time.Duration) {
	if embedder == nil {
		logger.Info("aura serve: dense retrieval disabled: no embedding route", "family", family)
		return
	}
	space, err := arcadedb.SpaceWithin(embedder, timeout)
	logger.Info("aura serve: embedding space", "family", family, "space", space.ID,
		"space_label", space.Label, "space_error", err)
}
