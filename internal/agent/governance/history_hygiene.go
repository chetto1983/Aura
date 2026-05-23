package governance

import (
	"log/slog"

	"github.com/aura/aura/internal/llm"
)

// ScrubOrphanToolCalls repairs two classes of history corruption before the
// history is sent to an LLM provider:
//
//  1. Orphan tool messages — role:tool entries whose tool_call_id was never
//     announced by an assistant message. Providers return HTTP 400 on these.
//  2. Missing tool results — assistant messages that declared one or more tool
//     calls but have no corresponding role:tool result. Providers return HTTP
//     400 on these; a placeholder is backfilled so the conversation stays valid.
//
// Mutations are logged at Warn level via slog.Default() for /api/insights
// visibility. Returns a new slice when changes are made; returns the input
// slice unchanged when the history is already valid.
func ScrubOrphanToolCalls(history []llm.Message) []llm.Message {
	before := len(history)
	history = dropOrphanToolResults(history)
	if dropped := before - len(history); dropped > 0 {
		slog.Default().Warn("governance: orphan_tool_results_dropped", "count", dropped)
	}

	countBefore := len(history)
	history = backfillMissingToolResults(history)
	if backfilled := len(history) - countBefore; backfilled > 0 {
		slog.Default().Warn("governance: missing_tool_results_backfilled", "count", backfilled)
	}

	return history
}
