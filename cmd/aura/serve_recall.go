// serve_recall.go: the L4 archival-memory recall adapter (PRD amendment #21), split
// out of serve_adapters.go (refactor-on-touch, 600-LOC cap). It bridges the runner's
// ArchivalRecaller seam onto the live memory MCP: each turn it calls get_context scoped
// to the owner, LONG-TERM ONLY, and returns a block for messages[1].
package main

import (
	"context"
	"encoding/json"
	"log/slog"
	"strings"

	"github.com/chetto1983/aura/internal/config"
	"github.com/chetto1983/aura/internal/runner"
)

// archivalRecallProvider builds the L4 archival-memory recall seam. It returns nil when
// AURA_CONTEXT_MEMORY_RECALL is off, so the Runner receives nil and the recall leg is a
// silent skip — the default-off state is produced HERE, not in the Runner (mirrors
// profileContextProvider's disabled-closure pattern). When on, each turn it calls the
// memory MCP get_context scoped to the owner id and pulls LONG-TERM ONLY (entities /
// preferences / facts): short-term history is already carried by the conversation
// ladder, and reasoning stays with Aura's own learner (no memory-bucket duplication).
// Recall is fail-soft — a dead sidecar or decode error yields an empty block, never a
// turn-blocking error.
func archivalRecallProvider(cfg *config.Config) runner.ArchivalRecaller {
	if cfg == nil || !cfg.MemoryRecall {
		return nil
	}
	maxItems := cfg.MemoryRecallMaxItems
	if maxItems <= 0 {
		maxItems = 8
	}
	return func(ctx context.Context, userIdentifier, query string) (string, error) {
		if strings.TrimSpace(userIdentifier) == "" {
			return "", nil // never hit the memory server's fail-open global scope
		}
		args := map[string]any{
			"user_identifier":    userIdentifier,
			"query":              query,
			"include_short_term": false,
			"include_long_term":  true,
			"include_reasoning":  false,
			"max_items":          maxItems,
		}
		text, err := callMemoryToolText(ctx, "memory_get_context", args)
		if err != nil {
			slog.Warn("archival recall: memory_get_context", "id", userIdentifier, "err", err)
			return "", nil
		}
		var res struct {
			Context    string `json:"context"`
			HasContext bool   `json:"has_context"`
			Error      string `json:"error"`
		}
		if err := json.Unmarshal([]byte(text), &res); err != nil {
			slog.Warn("archival recall: decode get_context", "id", userIdentifier, "err", err)
			return "", nil
		}
		if res.Error != "" || !res.HasContext || strings.TrimSpace(res.Context) == "" {
			return "", nil
		}
		return "## Relevant long-term memory (retrieved; current user instructions take precedence)\n" + strings.TrimSpace(res.Context), nil
	}
}
