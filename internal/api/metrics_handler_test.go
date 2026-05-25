package api

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/aura/aura/internal/ctxmetrics"
)

func TestMetricsHandler_PrometheusTextSnapshot(t *testing.T) {
	oldCounters := ctxmetrics.Global
	oldGauges := ctxmetrics.GlobalGauges
	ctxmetrics.Global = &ctxmetrics.Counters{}
	ctxmetrics.GlobalGauges = &ctxmetrics.Gauges{
		ModelContextWindow:        200000,
		CompactionThresholdTokens: 100000,
	}
	t.Cleanup(func() {
		ctxmetrics.Global = oldCounters
		ctxmetrics.GlobalGauges = oldGauges
	})

	ctxmetrics.Global.CTXCompactionsTotal.Add(2)
	ctxmetrics.Global.PayloadSummarizationsTotal.Add(3)
	ctxmetrics.Global.PayloadBreakerTripsTotal.Add(1)
	ctxmetrics.Global.MicrocompactRunsTotal.Add(4)

	router := NewRouter(Deps{})
	rr := httptest.NewRecorder()
	router.ServeHTTP(rr, httptest.NewRequest(http.MethodGet, "/metrics", nil))

	if rr.Code != http.StatusOK {
		t.Fatalf("status %d, body %s", rr.Code, rr.Body.String())
	}
	if got := rr.Header().Get("Content-Type"); !strings.HasPrefix(got, "text/plain; version=0.0.4") {
		t.Fatalf("Content-Type = %q", got)
	}

	body := rr.Body.String()
	for _, want := range []string{
		"# HELP aura_model_context_window LLM input context window size resolved at boot (tokens).\n",
		"# TYPE aura_model_context_window gauge\n",
		"aura_model_context_window 200000\n",
		"# TYPE aura_compaction_threshold_tokens gauge\n",
		"aura_compaction_threshold_tokens 100000\n",
		"# TYPE aura_ctx_compactions_total counter\n",
		"aura_ctx_compactions_total 2\n",
		"# TYPE aura_payload_summarizations_total counter\n",
		"aura_payload_summarizations_total 3\n",
		"# TYPE aura_payload_breaker_trips_total counter\n",
		"aura_payload_breaker_trips_total 1\n",
		"# TYPE aura_microcompact_runs_total counter\n",
		"aura_microcompact_runs_total 4\n",
	} {
		if !strings.Contains(body, want) {
			t.Fatalf("metrics body missing %q\nbody:\n%s", want, body)
		}
	}
	if strings.Contains(body, "aura_ctx_compactions_skipped_total") {
		t.Fatalf("metrics body exposes an unimplemented skipped counter:\n%s", body)
	}
	if !strings.HasSuffix(body, "\n") {
		t.Fatalf("metrics body must end with newline, got %q", body)
	}
}
