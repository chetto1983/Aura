package agent

import (
	"errors"
	"expvar"
	"log/slog"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/chetto1983/aura/internal/agent/panicobs"
	"github.com/chetto1983/aura/internal/llm"
	"github.com/prometheus/client_golang/prometheus"
)

var metrics = newAgentMetrics(prometheus.DefaultRegisterer, true)

type agentMetrics struct {
	budgetConsumeStepTotal        *expvar.Int
	toolDispatchTotal             *expvar.Int
	llmStreamOpenTotal            *expvar.Int
	llmStreamRetryTotal           *expvar.Int
	turnTotal                     *expvar.Map
	llmErrorsTotal                *expvar.Map
	toolErrorsTotal               *expvar.Map
	hookTotal                     *expvar.Map
	promptTokensTotal             *expvar.Int
	completionTokensTotal         *expvar.Int
	cachedTokensTotal             *expvar.Int
	costUSDTotal                  *expvar.Float
	spanExportFailuresTotal       *expvar.Int
	spanIDEntropyFailuresTotal    *expvar.Int
	prefixDriftTotal              *expvar.Int
	promBudgetConsumeStepTotal    prometheus.Counter
	promToolDispatchTotal         prometheus.Counter
	promLLMStreamOpenTotal        prometheus.Counter
	promLLMStreamRetryTotal       prometheus.Counter
	promToolDuration              prometheus.Histogram
	promTurnTotal                 *prometheus.CounterVec
	promLLMCallDuration           prometheus.Histogram
	promLLMErrorsTotal            *prometheus.CounterVec
	promToolErrorsTotal           *prometheus.CounterVec
	promHookTotal                 *prometheus.CounterVec
	promPromptTokensTotal         prometheus.Counter
	promCompletionTokensTotal     prometheus.Counter
	promCachedTokensTotal         prometheus.Counter
	promCostUSDTotal              prometheus.Counter
	promSpanExportFailuresTotal   prometheus.Counter
	promSpanIDEntropyFailureTotal prometheus.Counter
	promPrefixDriftTotal          prometheus.Counter
}

func newAgentMetrics(reg prometheus.Registerer, publishExpvar bool) *agentMetrics {
	if reg == nil {
		reg = prometheus.DefaultRegisterer
	}
	return &agentMetrics{
		budgetConsumeStepTotal:     newExpvarInt("aura_agent_budget_consume_step_total", publishExpvar),
		toolDispatchTotal:          newExpvarInt("aura_agent_tool_dispatch_total", publishExpvar),
		llmStreamOpenTotal:         newExpvarInt("aura_agent_llm_stream_open_total", publishExpvar),
		llmStreamRetryTotal:        newExpvarInt("aura_agent_llm_stream_retry_total", publishExpvar),
		turnTotal:                  newExpvarMap("aura_agent_turn_total", publishExpvar),
		llmErrorsTotal:             newExpvarMap("aura_agent_llm_errors_total", publishExpvar),
		toolErrorsTotal:            newExpvarMap("aura_agent_tool_errors_total", publishExpvar),
		hookTotal:                  newExpvarMap("aura_agent_hook_total", publishExpvar),
		promptTokensTotal:          newExpvarInt("aura_agent_prompt_tokens_total", publishExpvar),
		completionTokensTotal:      newExpvarInt("aura_agent_completion_tokens_total", publishExpvar),
		cachedTokensTotal:          newExpvarInt("aura_agent_cached_tokens_total", publishExpvar),
		costUSDTotal:               newExpvarFloat("aura_agent_cost_usd_total", publishExpvar),
		spanExportFailuresTotal:    newExpvarInt("aura_agent_span_export_failures_total", publishExpvar),
		spanIDEntropyFailuresTotal: newExpvarInt("aura_agent_span_id_entropy_failures_total", publishExpvar),
		prefixDriftTotal:           newExpvarInt("aura_agent_prefix_drift_total", publishExpvar),

		promBudgetConsumeStepTotal: registerCounter(reg, prometheus.CounterOpts{
			Name: "aura_agent_budget_consume_step_total",
			Help: "Total agent budget steps consumed.",
		}),
		promToolDispatchTotal: registerCounter(reg, prometheus.CounterOpts{
			Name: "aura_agent_tool_dispatch_total",
			Help: "Total tool calls dispatched by the agent.",
		}),
		promLLMStreamOpenTotal: registerCounter(reg, prometheus.CounterOpts{
			Name: "aura_agent_llm_stream_open_total",
			Help: "Total LLM stream open attempts.",
		}),
		promLLMStreamRetryTotal: registerCounter(reg, prometheus.CounterOpts{
			Name: "aura_agent_llm_stream_retry_total",
			Help: "Total LLM stream open retries.",
		}),
		promToolDuration: registerHistogram(reg, prometheus.HistogramOpts{
			Name:    "aura_agent_tool_duration_seconds",
			Help:    "Tool execution duration in seconds.",
			Buckets: prometheus.DefBuckets,
		}),
		promTurnTotal: registerCounterVec(reg, prometheus.CounterOpts{
			Name: "aura_agent_turn_total",
			Help: "Total agent turns by terminal outcome.",
		}, []string{"outcome"}),
		promLLMCallDuration: registerHistogram(reg, prometheus.HistogramOpts{
			Name:    "aura_agent_llm_call_duration_seconds",
			Help:    "LLM call duration from stream open through full drain.",
			Buckets: prometheus.DefBuckets,
		}),
		promLLMErrorsTotal: registerCounterVec(reg, prometheus.CounterOpts{
			Name: "aura_agent_llm_errors_total",
			Help: "Total LLM errors by bounded kind.",
		}, []string{"kind"}),
		promToolErrorsTotal: registerCounterVec(reg, prometheus.CounterOpts{
			Name: "aura_agent_tool_errors_total",
			Help: "Total tool errors by tool name.",
		}, []string{"tool"}),
		promHookTotal: registerCounterVec(reg, prometheus.CounterOpts{
			Name: "aura_agent_hook_total",
			Help: "Total hook outcomes by hook point and outcome.",
		}, []string{"point", "outcome"}),
		promPromptTokensTotal: registerCounter(reg, prometheus.CounterOpts{
			Name: "aura_agent_prompt_tokens_total",
			Help: "Total prompt tokens reported by LLM usage chunks.",
		}),
		promCompletionTokensTotal: registerCounter(reg, prometheus.CounterOpts{
			Name: "aura_agent_completion_tokens_total",
			Help: "Total completion tokens reported by LLM usage chunks.",
		}),
		promCachedTokensTotal: registerCounter(reg, prometheus.CounterOpts{
			Name: "aura_agent_cached_tokens_total",
			Help: "Total cached prompt tokens reported by LLM usage chunks.",
		}),
		promCostUSDTotal: registerCounter(reg, prometheus.CounterOpts{
			Name: "aura_agent_cost_usd_total",
			Help: "Total reported LLM cost in USD.",
		}),
		promSpanExportFailuresTotal: registerCounter(reg, prometheus.CounterOpts{
			Name: "aura_agent_span_export_failures_total",
			Help: "Total OpenTelemetry span export failures observed by the agent exporter wrapper.",
		}),
		promSpanIDEntropyFailureTotal: registerCounter(reg, prometheus.CounterOpts{
			Name: "aura_agent_span_id_entropy_failures_total",
			Help: "Total span-id entropy failures recovered with a zero-id fallback.",
		}),
		promPrefixDriftTotal: registerCounter(reg, prometheus.CounterOpts{
			Name: "aura_agent_prefix_drift_total",
			Help: "Total times a BeforeModel hook rewrite changed the cache-stable messages prefix (AG-031).",
		}),
	}
}

func recordPrefixDrift() {
	metrics.prefixDriftTotal.Add(1)
	metrics.promPrefixDriftTotal.Inc()
}

func recordBudgetConsumeStep() {
	metrics.budgetConsumeStepTotal.Add(1)
	metrics.promBudgetConsumeStepTotal.Inc()
}

func recordToolDispatch(n int) {
	if n <= 0 {
		return
	}
	metrics.toolDispatchTotal.Add(int64(n))
	metrics.promToolDispatchTotal.Add(float64(n))
}

func recordLLMStreamOpen() {
	metrics.llmStreamOpenTotal.Add(1)
	metrics.promLLMStreamOpenTotal.Inc()
}

func recordLLMStreamRetry() {
	metrics.llmStreamRetryTotal.Add(1)
	metrics.promLLMStreamRetryTotal.Inc()
}

func recordToolDuration(d time.Duration) {
	if d < 0 {
		return
	}
	metrics.promToolDuration.Observe(d.Seconds())
}

func recordTurnOutcome(outcome string) {
	metrics.recordTurnOutcome(outcome)
}

func recordLLMDuration(d time.Duration) {
	metrics.recordLLMDuration(d)
}

func recordLLMError(kind string) {
	metrics.recordLLMError(kind)
}

func recordToolError(tool string) {
	metrics.recordToolError(tool)
}

func recordHookOutcome(point, outcome string) {
	metrics.recordHookOutcome(point, outcome)
}

func recordUsage(usage llm.Usage) {
	metrics.recordUsage(usage)
}

func recordSpanExportFailure() {
	metrics.spanExportFailuresTotal.Add(1)
	metrics.promSpanExportFailuresTotal.Inc()
}

func recordSpanIDEntropyFailure() {
	metrics.spanIDEntropyFailuresTotal.Add(1)
	metrics.promSpanIDEntropyFailureTotal.Inc()
}

func recordRecoveredPanic(site string) {
	panicobs.Record(site)
	slog.Error("agent recovered panic", "site", metricLabel(site))
}

func (m *agentMetrics) recordTurnOutcome(outcome string) {
	label := metricLabel(outcome)
	m.turnTotal.Add(label, 1)
	m.promTurnTotal.WithLabelValues(label).Inc()
}

func (m *agentMetrics) recordLLMDuration(d time.Duration) {
	if d < 0 {
		return
	}
	m.promLLMCallDuration.Observe(d.Seconds())
}

func (m *agentMetrics) recordLLMError(kind string) {
	label := metricLabel(kind)
	m.llmErrorsTotal.Add(label, 1)
	m.promLLMErrorsTotal.WithLabelValues(label).Inc()
}

func (m *agentMetrics) recordToolError(tool string) {
	label := metricLabel(tool)
	m.toolErrorsTotal.Add(label, 1)
	m.promToolErrorsTotal.WithLabelValues(label).Inc()
}

func (m *agentMetrics) recordHookOutcome(point, outcome string) {
	pointLabel := metricLabel(point)
	outcomeLabel := metricLabel(outcome)
	m.hookTotal.Add(pointLabel+":"+outcomeLabel, 1)
	m.promHookTotal.WithLabelValues(pointLabel, outcomeLabel).Inc()
}

func (m *agentMetrics) recordUsage(usage llm.Usage) {
	if usage.PromptTokens > 0 {
		m.promptTokensTotal.Add(int64(usage.PromptTokens))
		m.promPromptTokensTotal.Add(float64(usage.PromptTokens))
	}
	if usage.CompletionTokens > 0 {
		m.completionTokensTotal.Add(int64(usage.CompletionTokens))
		m.promCompletionTokensTotal.Add(float64(usage.CompletionTokens))
	}
	if usage.CachedTokens > 0 {
		m.cachedTokensTotal.Add(int64(usage.CachedTokens))
		m.promCachedTokensTotal.Add(float64(usage.CachedTokens))
	}
	if usage.Cost != nil && *usage.Cost > 0 {
		m.costUSDTotal.Add(*usage.Cost)
		m.promCostUSDTotal.Add(*usage.Cost)
	}
}

func newExpvarInt(name string, publish bool) *expvar.Int {
	if publish {
		return expvar.NewInt(name)
	}
	return new(expvar.Int)
}

func newExpvarFloat(name string, publish bool) *expvar.Float {
	if publish {
		return expvar.NewFloat(name)
	}
	return new(expvar.Float)
}

func newExpvarMap(name string, publish bool) *expvar.Map {
	if publish {
		return expvar.NewMap(name)
	}
	m := new(expvar.Map)
	m.Init()
	return m
}

func registerCounter(reg prometheus.Registerer, opts prometheus.CounterOpts) prometheus.Counter {
	c := prometheus.NewCounter(opts)
	if err := reg.Register(c); err != nil {
		var already prometheus.AlreadyRegisteredError
		if errors.As(err, &already) {
			if existing, ok := already.ExistingCollector.(prometheus.Counter); ok {
				return existing
			}
		}
		panic(err)
	}
	return c
}

func registerHistogram(reg prometheus.Registerer, opts prometheus.HistogramOpts) prometheus.Histogram {
	h := prometheus.NewHistogram(opts)
	if err := reg.Register(h); err != nil {
		var already prometheus.AlreadyRegisteredError
		if errors.As(err, &already) {
			if existing, ok := already.ExistingCollector.(prometheus.Histogram); ok {
				return existing
			}
		}
		panic(err)
	}
	return h
}

func registerCounterVec(reg prometheus.Registerer, opts prometheus.CounterOpts, labels []string) *prometheus.CounterVec {
	cv := prometheus.NewCounterVec(opts, labels)
	if err := reg.Register(cv); err != nil {
		var already prometheus.AlreadyRegisteredError
		if errors.As(err, &already) {
			if existing, ok := already.ExistingCollector.(*prometheus.CounterVec); ok {
				return existing
			}
		}
		panic(err)
	}
	return cv
}

func metricLabel(s string) string {
	s = strings.TrimSpace(s)
	if s == "" {
		return "unknown"
	}
	var b strings.Builder
	for _, r := range s {
		if b.Len() >= 96 {
			break
		}
		if unicode.IsLetter(r) || unicode.IsDigit(r) || r == '_' || r == '-' || r == '.' {
			b.WriteRune(r)
			continue
		}
		b.WriteByte('_')
	}
	out := b.String()
	if out == "" || !utf8.ValidString(out) {
		return "unknown"
	}
	return out
}
