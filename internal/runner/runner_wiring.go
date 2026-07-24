package runner

import (
	"context"
	"log/slog"
	"time"

	"github.com/chetto1983/aura/internal/agent/prompt"
	"github.com/chetto1983/aura/internal/agent/tools"
)

// embedHealthCheckTimeout bounds the best-effort boot probe of the embed sidecar.
// It is short — the probe is a non-fatal liveness log, never a gate on boot.
const embedHealthCheckTimeout = 3 * time.Second

// wireToolSearchEmbedder wires the granite embedder into the registered tool_search
// hook so free-text tool_search ranks deferred tools by embedding cosine (08.2-03).
// The embed sidecar is a HARD dependency for tool_search: with it down, tool_search
// Execute returns an explicit model-visible error (Req-6) — but boot is NOT failed
// (Open-Q #2: an MCP-free `aura chat` must not be coupled to embed availability). A
// non-fatal health-check probes the sidecar once and logs an unreachable :8081 so the
// operator sees the dependency, then continues. nil embedder => tool_search has no
// ranker and its free-text path errors per call; the select: path still works.
func wireToolSearchEmbedder(reg *tools.Registry, embedder prompt.Embedder) {
	if reg == nil {
		return
	}
	t, ok := reg.Get("tool_search")
	if !ok {
		return
	}
	ts, ok := t.(*tools.ToolSearch)
	if !ok {
		return
	}
	ts.Embed = embedder
	if embedder == nil {
		slog.Warn("tool_search semantic ranking disabled: no embedder wired (embed sidecar)")
		return
	}
	// Boot health-check: probe the embed sidecar (granite :8081) once. Log-only —
	// never fatal, never blocks boot.
	ctx, cancel := context.WithTimeout(context.Background(), embedHealthCheckTimeout)
	defer cancel()
	if _, err := embedder.Embed(ctx, []string{"healthcheck"}); err != nil {
		slog.Warn("embed sidecar unreachable at boot: tool_search free-text ranking will error until it recovers (granite :8081)",
			"error", err)
	}
}
