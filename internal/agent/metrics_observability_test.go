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
	dto "github.com/prometheus/client_model/go"
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
	reg := prometheus.NewRegistry()
	m1 := newAgentMetrics(reg, false)
	m1.recordTurnOutcome("content_stop")

	m2 := newAgentMetrics(prometheus.NewRegistry(), false)
	m2.recordLLMError("stream_open")

	if got := metricMapInt(m1.turnTotal, "content_stop"); got != 1 {
		t.Fatalf("custom turn counter = %d, want 1", got)
	}
}

func TestTurnOutcomeCounter_RunRecordsExactlyOnce(t *testing.T) {
	before := metricMapInt(metrics.turnTotal, "content_stop")
	beforePrompt := metricInt(metrics.promptTokensTotal)
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
	b, err := NewBudget(BudgetOptions{MaxSteps: ptrInt(3)})
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

	if got := metricMapInt(metrics.turnTotal, "content_stop") - before; got != 1 {
		t.Fatalf("content_stop turn counter delta = %d, want 1", got)
	}
	if got := metricInt(metrics.promptTokensTotal) - beforePrompt; got != 7 {
		t.Fatalf("prompt token counter delta = %d, want 7", got)
	}
}

func TestLLMCallDurationMetric_RecordsHistogram(t *testing.T) {
	reg := prometheus.NewRegistry()
	m := newAgentMetrics(reg, false)
	m.recordLLMDuration(25 * time.Millisecond)

	fam := findMetricFamily(t, reg, "aura_agent_llm_call_duration_seconds")
	if fam.GetType() != dto.MetricType_HISTOGRAM || len(fam.GetMetric()) == 0 {
		t.Fatalf("metric family = %#v, want histogram with samples", fam)
	}
	if got := fam.GetMetric()[0].GetHistogram().GetSampleCount(); got != 1 {
		t.Fatalf("llm duration histogram count = %d, want 1", got)
	}
}

func TestToolErrorMetric_UnknownTool(t *testing.T) {
	before := metricMapInt(metrics.toolErrorsTotal, "missing_tool")
	a := NewLlmAgent(LlmAgentConfig{
		Client:     observabilityClient{},
		LLM:        llm.Config{Model: "m", Provider: "p", TotalTimeoutSec: 30},
		Registry:   tools.NewRegistry(),
		PreviewCap: 1024,
		RunDir:     t.TempDir(),
		SessionID:  "metrics-tool",
		UserTurns:  []llm.Message{{Role: llm.RoleUser, Content: "ciao"}},
	})
	b, err := NewBudget(BudgetOptions{MaxSteps: ptrInt(3)})
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
	if got := metricMapInt(metrics.toolErrorsTotal, "missing_tool") - before; got != 1 {
		t.Fatalf("tool error metric delta = %d, want 1", got)
	}
}

func TestHookMetric_ErrorOutcome(t *testing.T) {
	before := metricMapInt(metrics.hookTotal, "before_model:error")
	_, err := NewHookManager(hookMetricErrorHook{}).BeforeModel(context.Background(), &llm.Request{})
	if err == nil {
		t.Fatal("BeforeModel error = nil, want hook failure")
	}
	if got := metricMapInt(metrics.hookTotal, "before_model:error") - before; got != 1 {
		t.Fatalf("hook metric delta = %d, want 1", got)
	}
}

func TestMintSpanIDEntropyFailureFallbackRecordsMetric(t *testing.T) {
	before := metricInt(metrics.spanIDEntropyFailuresTotal)
	old := spanIDReader
	spanIDReader = errReader{}
	t.Cleanup(func() { spanIDReader = old })

	id := mintSpanID()
	if id != ([8]byte{}) {
		t.Fatalf("mintSpanID entropy failure = %x, want zero fallback", id)
	}
	if got := metricInt(metrics.spanIDEntropyFailuresTotal) - before; got != 1 {
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

func findMetricFamily(t *testing.T, reg *prometheus.Registry, name string) *dto.MetricFamily {
	t.Helper()
	families, err := reg.Gather()
	if err != nil {
		t.Fatalf("Gather: %v", err)
	}
	for _, fam := range families {
		if fam.GetName() == name {
			return fam
		}
	}
	t.Fatalf("metric family %q not found", name)
	return nil
}

func ptrInt(v int) *int { return &v }
