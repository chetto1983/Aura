package main

import (
	"log/slog"
	"time"

	"github.com/chetto1983/aura/internal/arcadedb"
	"github.com/chetto1983/aura/internal/config"
	"github.com/chetto1983/aura/internal/llm"
)

// memoryEmbedder is the daemon's one memory route. Its key is read from the running LLM
// profile on every request: a key rotated in the cockpit replaces that profile in place
// (serve_settings.go primaryLLMRouteReloader) without a restart, and a boot copy would keep
// embedding with the revoked key (spec §5).
func memoryEmbedder(cfg *config.Config, runtime *llm.Runtime) arcadedb.DenseEmbedder {
	return arcadedb.NewMemoryEmbedder(cfg.Embed, func() string { return runtime.Snapshot().Config.APIKey })
}

// memorySpaceBootTimeout bounds the one boot call that is only logged.
const memorySpaceBootTimeout = 5 * time.Second

// logMemorySpace names the daemon's memory space at boot. arcadedb-mcp logs its own, and the
// two must match: otherwise each re-stamps the other's writes and memory stays lexical.
func logMemorySpace(logger *slog.Logger, embedder arcadedb.DenseEmbedder, timeout time.Duration) {
	if embedder == nil {
		logger.Info("aura serve: dense memory disabled: no embedding route; memory retrieval is lexical")
		return
	}
	space, err := arcadedb.SpaceWithin(embedder, timeout)
	logger.Info("aura serve: memory embedding space", "space", space.ID, "space_label", space.Label, "space_error", err)
}
