package api

import (
	"fmt"
	"io"
	"net/http"
	"strings"

	"github.com/aura/aura/internal/ctxmetrics"
)

// handleMetrics returns a Prometheus text-format (version 0.0.4) snapshot of
// Phase-CTX counters and gauges. No authentication requirement beyond the outer
// bearer wrapper already applied by NewRouter; the data is operational metrics
// without user data.
func handleMetrics(_ Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/plain; version=0.0.4; charset=utf-8")
		w.WriteHeader(http.StatusOK)

		g := ctxmetrics.GlobalGauges
		c := ctxmetrics.Global

		lines := []string{
			"# HELP aura_model_context_window LLM input context window size resolved at boot (tokens).",
			"# TYPE aura_model_context_window gauge",
			fmt.Sprintf("aura_model_context_window %d", g.ModelContextWindow),

			"# HELP aura_compaction_threshold_tokens Token count at which AutoCompactEngine triggers compaction.",
			"# TYPE aura_compaction_threshold_tokens gauge",
			fmt.Sprintf("aura_compaction_threshold_tokens %d", g.CompactionThresholdTokens),

			"# HELP aura_ctx_compactions_total Total conversation compactions performed by AutoCompactEngine.",
			"# TYPE aura_ctx_compactions_total counter",
			fmt.Sprintf("aura_ctx_compactions_total %d", c.CTXCompactionsTotal.Load()),

			"# HELP aura_payload_summarizations_total Total tool-output summarizations by SubagentPayloadSummarizer.",
			"# TYPE aura_payload_summarizations_total counter",
			fmt.Sprintf("aura_payload_summarizations_total %d", c.PayloadSummarizationsTotal.Load()),

			"# HELP aura_payload_breaker_trips_total Total circuit-breaker trips in SubagentPayloadSummarizer.",
			"# TYPE aura_payload_breaker_trips_total counter",
			fmt.Sprintf("aura_payload_breaker_trips_total %d", c.PayloadBreakerTripsTotal.Load()),

			"# HELP aura_microcompact_runs_total Total old tool-result microcompaction passes that cleared content.",
			"# TYPE aura_microcompact_runs_total counter",
			fmt.Sprintf("aura_microcompact_runs_total %d", c.MicrocompactRunsTotal.Load()),
		}
		var sb strings.Builder
		for _, line := range lines {
			sb.WriteString(line)
			sb.WriteByte('\n')
		}
		_, _ = io.WriteString(w, sb.String())
	}
}
