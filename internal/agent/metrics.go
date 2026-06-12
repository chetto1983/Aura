package agent

import (
	"expvar"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

var (
	metricBudgetConsumeStepTotal = expvar.NewInt("aura_agent_budget_consume_step_total")
	metricToolDispatchTotal      = expvar.NewInt("aura_agent_tool_dispatch_total")
	metricLLMStreamOpenTotal     = expvar.NewInt("aura_agent_llm_stream_open_total")
	metricLLMStreamRetryTotal    = expvar.NewInt("aura_agent_llm_stream_retry_total")

	promBudgetConsumeStepTotal = promauto.NewCounter(prometheus.CounterOpts{
		Name: "aura_agent_budget_consume_step_total",
		Help: "Total agent budget steps consumed.",
	})
	promToolDispatchTotal = promauto.NewCounter(prometheus.CounterOpts{
		Name: "aura_agent_tool_dispatch_total",
		Help: "Total tool calls dispatched by the agent.",
	})
	promLLMStreamOpenTotal = promauto.NewCounter(prometheus.CounterOpts{
		Name: "aura_agent_llm_stream_open_total",
		Help: "Total LLM stream open attempts.",
	})
	promLLMStreamRetryTotal = promauto.NewCounter(prometheus.CounterOpts{
		Name: "aura_agent_llm_stream_retry_total",
		Help: "Total LLM stream open retries.",
	})
	promToolDuration = promauto.NewHistogram(prometheus.HistogramOpts{
		Name:    "aura_agent_tool_duration_seconds",
		Help:    "Tool execution duration in seconds.",
		Buckets: prometheus.DefBuckets,
	})
)

func recordBudgetConsumeStep() {
	metricBudgetConsumeStepTotal.Add(1)
	promBudgetConsumeStepTotal.Inc()
}

func recordToolDispatch(n int) {
	if n <= 0 {
		return
	}
	metricToolDispatchTotal.Add(int64(n))
	promToolDispatchTotal.Add(float64(n))
}

func recordLLMStreamOpen() {
	metricLLMStreamOpenTotal.Add(1)
	promLLMStreamOpenTotal.Inc()
}

func recordLLMStreamRetry() {
	metricLLMStreamRetryTotal.Add(1)
	promLLMStreamRetryTotal.Inc()
}

func recordToolDuration(d time.Duration) {
	if d < 0 {
		return
	}
	promToolDuration.Observe(d.Seconds())
}
