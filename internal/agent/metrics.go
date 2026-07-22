package agent

import (
	"context"
	"expvar"
	"log/slog"
	"time"

	"github.com/chetto1983/aura/internal/agent/panicobs"
	"github.com/chetto1983/aura/internal/llm"
	"github.com/chetto1983/aura/internal/obs"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
)

const agentMeterName = "github.com/chetto1983/aura/internal/agent"

// legacyExpvarMetrics is the named, time-bounded compatibility adapter for
// pre-OTel /debug/vars consumers. It is projection-only: every semantic event
// enters through one agentMetrics helper, and no direct client_golang collector
// is registered. Remove this adapter after the Phase 40 compatibility window.
type legacyExpvarMetrics struct {
	budgetConsumeStepTotal     *expvar.Int
	toolDispatchTotal          *expvar.Int
	llmStreamOpenTotal         *expvar.Int
	llmStreamRetryTotal        *expvar.Int
	turnTotal                  *expvar.Map
	llmErrorsTotal             *expvar.Map
	toolErrorsTotal            *expvar.Map
	hookTotal                  *expvar.Map
	promptTokensTotal          *expvar.Int
	completionTokensTotal      *expvar.Int
	cachedTokensTotal          *expvar.Int
	costUSDTotal               *expvar.Float
	spanExportFailuresTotal    *expvar.Int
	spanIDEntropyFailuresTotal *expvar.Int
	prefixDriftTotal           *expvar.Int
}

type agentMetrics struct {
	legacy legacyExpvarMetrics

	budgetConsumeStepTotal     metric.Int64Counter
	toolDispatchTotal          metric.Int64Counter
	llmStreamOpenTotal         metric.Int64Counter
	llmStreamRetryTotal        metric.Int64Counter
	toolDuration               metric.Float64Histogram
	turnTotal                  metric.Int64Counter
	llmCallDuration            metric.Float64Histogram
	llmErrorsTotal             metric.Int64Counter
	toolErrorsTotal            metric.Int64Counter
	hookTotal                  metric.Int64Counter
	promptTokensTotal          metric.Int64Counter
	completionTokensTotal      metric.Int64Counter
	cachedTokensTotal          metric.Int64Counter
	costUSDTotal               metric.Float64Counter
	spanExportFailuresTotal    metric.Int64Counter
	spanIDEntropyFailuresTotal metric.Int64Counter
	prefixDriftTotal           metric.Int64Counter
}

var metrics = newAgentMetrics(otel.Meter(agentMeterName), true)

var toolExecutionBoundaries = map[string]*obs.Boundary{
	obs.ToolClassBuiltin:     newToolExecutionBoundary(obs.ToolClassBuiltin),
	obs.ToolClassFilesystem:  newToolExecutionBoundary(obs.ToolClassFilesystem),
	obs.ToolClassShell:       newToolExecutionBoundary(obs.ToolClassShell),
	obs.ToolClassWeb:         newToolExecutionBoundary(obs.ToolClassWeb),
	obs.ToolClassInteraction: newToolExecutionBoundary(obs.ToolClassInteraction),
	obs.ToolClassMCP:         newToolExecutionBoundary(obs.ToolClassMCP),
	obs.ToolClassScheduler:   newToolExecutionBoundary(obs.ToolClassScheduler),
	obs.ToolClassData:        newToolExecutionBoundary(obs.ToolClassData),
	obs.ValueOther:           newToolExecutionBoundary(obs.ValueOther),
}

var llmCallBoundary = obs.NewGlobalBoundary(agentMeterName, obs.BoundaryConfig{
	Operation: "llm_call", Count: obs.AgentLLMCallsID, Duration: obs.AgentLLMDurationID,
})

func newToolExecutionBoundary(toolClass string) *obs.Boundary {
	return obs.NewGlobalBoundary(agentMeterName, obs.BoundaryConfig{
		Operation: "tool_dispatch", ToolClass: toolClass,
		Count: obs.AgentToolDispatchID, Duration: obs.AgentToolDurationID,
	})
}

func newAgentMetrics(meter metric.Meter, publishExpvar bool) *agentMetrics {
	return &agentMetrics{
		legacy: legacyExpvarMetrics{
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
		},
		budgetConsumeStepTotal:     mustInt64Counter(meter, obs.AgentBudgetStepsID),
		toolDispatchTotal:          mustInt64Counter(meter, obs.AgentToolDispatchID),
		llmStreamOpenTotal:         mustInt64Counter(meter, obs.AgentLLMStreamOpenID),
		llmStreamRetryTotal:        mustInt64Counter(meter, obs.AgentLLMStreamRetryID),
		toolDuration:               mustFloat64Histogram(meter, obs.AgentToolDurationID),
		turnTotal:                  mustInt64Counter(meter, obs.AgentTurnsID),
		llmCallDuration:            mustFloat64Histogram(meter, obs.AgentLLMDurationID),
		llmErrorsTotal:             mustInt64Counter(meter, obs.AgentLLMErrorsID),
		toolErrorsTotal:            mustInt64Counter(meter, obs.AgentToolErrorsID),
		hookTotal:                  mustInt64Counter(meter, obs.AgentHooksID),
		promptTokensTotal:          mustInt64Counter(meter, obs.AgentPromptTokensID),
		completionTokensTotal:      mustInt64Counter(meter, obs.AgentCompletionTokensID),
		cachedTokensTotal:          mustInt64Counter(meter, obs.AgentCacheReadTokensID),
		costUSDTotal:               mustFloat64Counter(meter, obs.AgentCostUSDID),
		spanExportFailuresTotal:    mustInt64Counter(meter, obs.AgentSpanExportFailuresID),
		spanIDEntropyFailuresTotal: mustInt64Counter(meter, obs.AgentSpanIDEntropyFailuresID),
		prefixDriftTotal:           mustInt64Counter(meter, obs.AgentPrefixDriftID),
	}
}

func recordPrefixDrift() {
	metrics.legacy.prefixDriftTotal.Add(1)
	metrics.prefixDriftTotal.Add(context.Background(), 1)
}

func recordBudgetConsumeStep() {
	metrics.legacy.budgetConsumeStepTotal.Add(1)
	metrics.budgetConsumeStepTotal.Add(context.Background(), 1)
}

func startToolObservation(ctx context.Context, toolName string) (context.Context, *obs.BoundaryEnd) {
	metrics.legacy.toolDispatchTotal.Add(1)
	return toolExecutionBoundaries[obs.ClassifyTool(toolName)].Start(ctx)
}

func recordLLMStreamOpen() {
	metrics.legacy.llmStreamOpenTotal.Add(1)
	metrics.llmStreamOpenTotal.Add(context.Background(), 1)
}

func recordLLMStreamRetry() {
	metrics.legacy.llmStreamRetryTotal.Add(1)
	metrics.llmStreamRetryTotal.Add(context.Background(), 1)
}

func recordTurnOutcome(outcome string) { metrics.recordTurnOutcome(outcome) }
func recordLLMError(kind string)       { metrics.recordLLMError(kind) }
func recordToolError(tool string)      { metrics.recordToolError(tool) }
func recordHookOutcome(point, outcome string) {
	metrics.recordHookOutcome(point, outcome)
}
func recordUsage(usage llm.Usage) { metrics.recordUsage(usage) }

func recordSpanExportFailure() {
	metrics.legacy.spanExportFailuresTotal.Add(1)
	metrics.spanExportFailuresTotal.Add(context.Background(), 1)
}

func recordSpanIDEntropyFailure() {
	metrics.legacy.spanIDEntropyFailuresTotal.Add(1)
	metrics.spanIDEntropyFailuresTotal.Add(context.Background(), 1)
}

func recordRecoveredPanic(site string) {
	panicobs.Record(site)
	slog.Error("agent recovered panic", "site", obs.NormalizeAttribute(obs.AttributeOperation, site))
}

func (m *agentMetrics) recordTurnOutcome(outcome string) {
	label := obs.NormalizeAttribute(obs.AttributeOutcome, outcome)
	m.legacy.turnTotal.Add(label, 1)
	m.turnTotal.Add(context.Background(), 1, metric.WithAttributes(boundedAttr(obs.AttributeOutcome, label)))
}

func (m *agentMetrics) recordLLMDuration(d time.Duration) {
	if d < 0 {
		return
	}
	m.llmCallDuration.Record(context.Background(), d.Seconds(), metric.WithAttributes(
		boundedAttr(obs.AttributeOutcome, obs.ValueOther),
		boundedAttr(obs.AttributeErrorClass, obs.ValueOther),
	))
}

func (m *agentMetrics) recordLLMError(kind string) {
	label := obs.ClassifyError(kind)
	m.legacy.llmErrorsTotal.Add(label, 1)
	m.llmErrorsTotal.Add(context.Background(), 1, metric.WithAttributes(boundedAttr(obs.AttributeErrorClass, label)))
}

func (m *agentMetrics) recordToolError(tool string) {
	toolClass := obs.ClassifyTool(tool)
	m.legacy.toolErrorsTotal.Add(toolClass, 1)
	m.toolErrorsTotal.Add(context.Background(), 1, metric.WithAttributes(
		boundedAttr(obs.AttributeToolClass, toolClass),
		boundedAttr(obs.AttributeErrorClass, obs.ValueOther),
	))
}

func (m *agentMetrics) recordHookOutcome(point, outcome string) {
	pointLabel := obs.NormalizeAttribute(obs.AttributeOperation, point)
	outcomeLabel := obs.NormalizeAttribute(obs.AttributeOutcome, outcome)
	m.legacy.hookTotal.Add(pointLabel+":"+outcomeLabel, 1)
	m.hookTotal.Add(context.Background(), 1, metric.WithAttributes(
		boundedAttr(obs.AttributeOperation, pointLabel),
		boundedAttr(obs.AttributeOutcome, outcomeLabel),
	))
}

func (m *agentMetrics) recordUsage(usage llm.Usage) {
	ctx := context.Background()
	if usage.PromptTokens > 0 {
		m.legacy.promptTokensTotal.Add(int64(usage.PromptTokens))
		m.promptTokensTotal.Add(ctx, int64(usage.PromptTokens))
	}
	if usage.CompletionTokens > 0 {
		m.legacy.completionTokensTotal.Add(int64(usage.CompletionTokens))
		m.completionTokensTotal.Add(ctx, int64(usage.CompletionTokens))
	}
	if usage.CachedTokens > 0 {
		m.legacy.cachedTokensTotal.Add(int64(usage.CachedTokens))
		m.cachedTokensTotal.Add(ctx, int64(usage.CachedTokens))
	}
	if usage.Cost != nil && *usage.Cost > 0 {
		m.legacy.costUSDTotal.Add(*usage.Cost)
		m.costUSDTotal.Add(ctx, *usage.Cost)
	}
}

func boundedAttr(key obs.AttributeKey, value string) attribute.KeyValue {
	return attribute.String(string(key), obs.NormalizeAttribute(key, value))
}

func mustInt64Counter(meter metric.Meter, id obs.InstrumentID) metric.Int64Counter {
	descriptor := obs.MustDescriptor(id)
	instrument, err := meter.Int64Counter(descriptor.Name,
		metric.WithDescription(descriptor.Description), metric.WithUnit(descriptor.Unit))
	if err != nil {
		panic(err)
	}
	return instrument
}

func mustFloat64Counter(meter metric.Meter, id obs.InstrumentID) metric.Float64Counter {
	descriptor := obs.MustDescriptor(id)
	instrument, err := meter.Float64Counter(descriptor.Name,
		metric.WithDescription(descriptor.Description), metric.WithUnit(descriptor.Unit))
	if err != nil {
		panic(err)
	}
	return instrument
}

func mustFloat64Histogram(meter metric.Meter, id obs.InstrumentID) metric.Float64Histogram {
	descriptor := obs.MustDescriptor(id)
	instrument, err := meter.Float64Histogram(descriptor.Name,
		metric.WithDescription(descriptor.Description), metric.WithUnit(descriptor.Unit))
	if err != nil {
		panic(err)
	}
	return instrument
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
