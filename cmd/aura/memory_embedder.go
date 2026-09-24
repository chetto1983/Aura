package main

import (
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
