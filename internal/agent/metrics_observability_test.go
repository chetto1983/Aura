package agent

import (
	"context"
	"encoding/json"
	"errors"
	"expvar"
	"iter"
	"strconv"
	"testing"
	"time"

	"github.com/chetto1983/aura/internal/agent/tools"
	"github.com/chetto1983/aura/internal/llm"
	"github.com/google/uuid"
	"github.com/prometheus/client_golang/prometheus"
	"go.opentelemetry.io/otel/attribute"
	otelprometheus "go.opentelemetry.io/otel/exporters/prometheus"
	"go.opentelemetry.io/otel/metric/noop"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"
)

type observabilityClient struct {
	turns []llm.Chunk
	err   error
}

func (c observabilityClient) Stream(context.Context, llm.Request) (<-chan llm.Chunk, error) {
	if c.err != nil {
		return nil, c.err
	}
	ch := make(chan llm.Chunk, len(c.turns))
	for _, chunk := range c.turns {
		ch <- chunk
	}
	close(ch)
	return ch, nil
}

type errReader struct{}

func (errReader) Read([]byte) (int, error) {
	return 0, errors.New("entropy unavailable")
}

type hookMetricErrorHook struct{}

func (hookMetricErrorHook) OnTurnStart(context.Context, HookTurn) error { return nil }
func (hookMetricErrorHook) BeforeModel(context.Context, *llm.Request) (*ModelHookResult, error) {
	return nil, errors.New("hook failed")
}
func (hookMetricErrorHook) BeforeTool(context.Context, llm.ToolCall) (*ToolHookResult, error) {
	return nil, nil
}
func (hookMetricErrorHook) AfterTool(context.Context, llm.ToolCall, tools.ToolResult) (*ToolResultHookResult, error) {
	return nil, nil
}
func (hookMetricErrorHook) OnTurnEnd(context.Context, HookTurn) error { return nil }

func TestMetrics_CustomRegistryReregisterSafe(t *testing.T) {
	m1, reader1 := newTestAgentMetrics(t)
	m1.recordTurnOutcome("content_stop")

	m2, reader2 := newTestAgentMetrics(t)
	m2.recordLLMError("stream_open")

	if got := metricMapInt(m1.legacy.turnTotal, "content_stop"); got != 1 {
		t.Fatalf("custom turn counter = %d, want 1", got)
	}
	turnMetric := findOTelMetric(t, reader1, "aura.agent.turn")
	turns, ok := turnMetric.Data.(metricdata.Sum[int64])
	if !ok || len(turns.DataPoints) != 1 || turns.DataPoints[0].Value != 1 {
		t.Fatalf("turn metric = %#v, want exactly one value-1 point", turnMetric.Data)
	}
	findOTelMetric(t, reader2, "aura.agent.llm.errors")
}

func TestMetrics_CardinalityFoldsRawToolNamesToOther(t *testing.T) {
	m, reader := newTestAgentMetrics(t)
	m.recordToolError("tenant-secret-dynamic-tool-123")

	metric := findOTelMetric(t, reader, "aura.agent.tool.errors")
	sum, ok := metric.Data.(metricdata.Sum[int64])
	if !ok || len(sum.DataPoints) != 1 {
		t.Fatalf("tool error data = %#v, want one int64 sum point", metric.Data)
	}
	labels := sum.DataPoints[0].Attributes.ToSlice()
	if !hasOTelLabel(labels, "tool_class", "other") || !hasOTelLabel(labels, "error_class", "other") {
		t.Fatalf("tool error labels = %v, want bounded other classes", labels)
	}
}

func TestMetrics_CompatibilityDoesNotRegisterDirectCollectors(t *testing.T) {
	reg := prometheus.NewRegistry()
	_ = newAgentMetrics(noop.NewMeterProvider().Meter(agentMeterName), false)
	families, err := reg.Gather()
	if err != nil {
		t.Fatalf("Gather: %v", err)
	}
	if len(families) != 0 {
		t.Fatalf("agent compatibility registered %d direct client_golang families; want 0", len(families))
	}
}

func TestMetrics_DescriptorCompatibilityEmitsEachPrometheusFamilyOnce(t *testing.T) {
	reg := prometheus.NewRegistry()
	exporter, err := otelprometheus.New(otelprometheus.WithRegisterer(reg))
	if err != nil {
		t.Fatalf("prometheus exporter: %v", err)
	}
	provider := sdkmetric.NewMeterProvider(sdkmetric.WithReader(exporter))
	t.Cleanup(func() { _ = provider.Shutdown(context.Background()) })
	m := newAgentMetrics(provider.Meter(agentMeterName), false)
	m.recordTurnOutcome("content_stop")
	m.recordToolError("web_fetch")
	m.recordLLMDuration(10 * time.Millisecond)

	families, err := reg.Gather()
	if err != nil {
		t.Fatalf("Gather: %v", err)
	}
	counts := map[string]int{}
	for _, family := range families {
		counts[family.GetName()]++
	}
	for _, name := range []string{
		"aura_agent_turn_total",
		"aura_agent_tool_errors_total",
		"aura_agent_llm_call_duration_seconds",
	} {
		if counts[name] != 1 {
			t.Errorf("Prometheus family %q count = %d, want 1", name, counts[name])
		}
	}
}

func TestTurnOutcomeCounter_RunRecordsExactlyOnce(t *testing.T) {
	before := metricMapInt(metrics.legacy.turnTotal, "content_stop")
	beforePrompt := metricInt(metrics.legacy.promptTokensTotal)
	cost := 0.25
	client := observabilityClient{turns: []llm.Chunk{
		{Text: "ciao"},
		{FinishReason: "stop"},
		{Usage: &llm.Usage{PromptTokens: 7, CompletionTokens: 3, CachedTokens: 2, Cost: &cost}},
	}}
	a := NewLlmAgent(LlmAgentConfig{
		Client:      client,
		LLM:         llm.Config{Model: "m", Provider: "p", TotalTimeoutSec: 30},
		Registry:    tools.NewRegistry(),
		PreviewCap:  1024,
		RunDir:      t.TempDir(),
		SessionID:   "metrics-turn",
		UserTurns:   []llm.Message{{Role: llm.RoleUser, Content: "ciao"}},
		HookManager: NewHookManager(),
	})
	b, err := NewBudget(BudgetOptions{MaxSteps: new(3)})
	if err != nil {
		t.Fatalf("NewBudget: %v", err)
	}
	_, err = drainRun(a.Run(InvocationContext{
		Ctx:       context.Background(),
		RequestID: uuid.Must(uuid.NewV7()),
		Budget:    b,
	}))
	if err != nil {
		t.Fatalf("Run: %v", err)
	}

	if got := metricMapInt(metrics.legacy.turnTotal, "content_stop") - before; got != 1 {
		t.Fatalf("content_stop turn counter delta = %d, want 1", got)
	}
	if got := metricInt(metrics.legacy.promptTokensTotal) - beforePrompt; got != 7 {
		t.Fatalf("prompt token counter delta = %d, want 7", got)
	}
}

func TestLLMCallDurationMetric_RecordsHistogram(t *testing.T) {
	m, reader := newTestAgentMetrics(t)
	m.recordLLMDuration(25 * time.Millisecond)

	metric := findOTelMetric(t, reader, "aura.agent.llm.call.duration")
	histogram, ok := metric.Data.(metricdata.Histogram[float64])
	if !ok || len(histogram.DataPoints) == 0 {
		t.Fatalf("metric = %#v, want histogram with samples", metric)
	}
	if got := histogram.DataPoints[0].Count; got != 1 {
		t.Fatalf("llm duration histogram count = %d, want 1", got)
	}
}

func TestToolErrorMetric_UnknownTool(t *testing.T) {
	before := metricMapInt(metrics.legacy.toolErrorsTotal, "other")
	a := NewLlmAgent(LlmAgentConfig{
		Client:     observabilityClient{},
		LLM:        llm.Config{Model: "m", Provider: "p", TotalTimeoutSec: 30},
		Registry:   tools.NewRegistry(),
		PreviewCap: 1024,
		RunDir:     t.TempDir(),
		SessionID:  "metrics-tool",
		UserTurns:  []llm.Message{{Role: llm.RoleUser, Content: "ciao"}},
	})
	b, err := NewBudget(BudgetOptions{MaxSteps: new(3)})
	if err != nil {
		t.Fatalf("NewBudget: %v", err)
	}
	call := llm.ToolCall{ID: "c1", Type: "function"}
	call.Function.Name = "missing_tool"
	call.Function.Arguments = string(json.RawMessage(`{}`))
	run := a.runTool(context.Background(), b, call, time.Now())
	if run.Err == "" {
		t.Fatal("runTool unknown tool returned no error")
	}
	if got := metricMapInt(metrics.legacy.toolErrorsTotal, "other") - before; got != 1 {
		t.Fatalf("tool error metric delta = %d, want 1", got)
	}
}

func TestHookMetric_ErrorOutcome(t *testing.T) {
	before := metricMapInt(metrics.legacy.hookTotal, "before_model:error")
	_, err := NewHookManager(hookMetricErrorHook{}).BeforeModel(context.Background(), &llm.Request{})
	if err == nil {
		t.Fatal("BeforeModel error = nil, want hook failure")
	}
	if got := metricMapInt(metrics.legacy.hookTotal, "before_model:error") - before; got != 1 {
		t.Fatalf("hook metric delta = %d, want 1", got)
	}
}

func TestMintSpanIDEntropyFailureFallbackRecordsMetric(t *testing.T) {
	before := metricInt(metrics.legacy.spanIDEntropyFailuresTotal)
	old := spanIDReader
	spanIDReader = errReader{}
	t.Cleanup(func() { spanIDReader = old })

	id := mintSpanID()
	if id != ([8]byte{}) {
		t.Fatalf("mintSpanID entropy failure = %x, want zero fallback", id)
	}
	if got := metricInt(metrics.legacy.spanIDEntropyFailuresTotal) - before; got != 1 {
		t.Fatalf("entropy failure metric delta = %d, want 1", got)
	}
}

func drainRun(seq iter.Seq2[*Event, error]) ([]*Event, error) {
	var out []*Event
	for ev, err := range seq {
		if err != nil {
			return out, err
		}
		if ev != nil {
			out = append(out, ev)
		}
	}
	return out, nil
}

func metricMapInt(m *expvar.Map, key string) int64 {
	v := m.Get(key)
	if v == nil {
		return 0
	}
	n, _ := strconv.ParseInt(v.String(), 10, 64)
	return n
}

func metricInt(v *expvar.Int) int64 {
	n, _ := strconv.ParseInt(v.String(), 10, 64)
	return n
}

func newTestAgentMetrics(t *testing.T) (*agentMetrics, *sdkmetric.ManualReader) {
	t.Helper()
	reader := sdkmetric.NewManualReader()
	provider := sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader))
	t.Cleanup(func() { _ = provider.Shutdown(context.Background()) })
	return newAgentMetrics(provider.Meter(agentMeterName), false), reader
}

func findOTelMetric(t *testing.T, reader *sdkmetric.ManualReader, name string) metricdata.Metrics {
	t.Helper()
	var rm metricdata.ResourceMetrics
	if err := reader.Collect(context.Background(), &rm); err != nil {
		t.Fatalf("Collect: %v", err)
	}
	for _, scope := range rm.ScopeMetrics {
		for _, metric := range scope.Metrics {
			if metric.Name == name {
				return metric
			}
		}
	}
	t.Fatalf("OTel metric %q not found", name)
	return metricdata.Metrics{}
}

func hasOTelLabel(labels []attribute.KeyValue, key, value string) bool {
	for _, label := range labels {
		if string(label.Key) == key && label.Value.AsString() == value {
			return true
		}
	}
	return false
}
