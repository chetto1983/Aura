package runner

import (
	"strings"
	"testing"
	"time"

	"github.com/chetto1983/aura/internal/agent"
	"github.com/chetto1983/aura/internal/arcadedb"
	"github.com/chetto1983/aura/internal/identityctx"
	"github.com/google/uuid"
)

// A turn that exposed no reasoning still ran its tools, and turn recall reads those tools
// back (spec 2026-10-06, "Tool calls"). Measured that day: the graph reached 9 of the lab
// VM's 16 tool turns, because the others had no reasoning to open a trace.
func TestReasoningGraphRecordsATurnThatOnlyRanTools(t *testing.T) {
	r, _ := newReasoningTestRunner(t, 65536, true)
	order := []string{}
	sink := &recordingReasoningGraphSink{order: &order}
	r.reasoningGraphSink = sink
	ctx := identityctx.WithIdentityID(t.Context(), uuid.NewString())
	tr := &turnTracker{convID: newConvID(t), llmRuntime: r.llmSnapshot(ctx)}
	runID := uuid.Must(uuid.NewV7())
	t0 := time.Date(2026, 10, 6, 9, 0, 0, 0, time.UTC)

	events := reasoningGraphToolEvents(runID, t0, "call-task", "task", `{"action":"create"}`, "ok", "scheduled", nil)
	events = append(events, reasoningGraphFinalEvent(runID, t0.Add(time.Second), "Fatto."))
	persistReasoningGraphEvents(t, r, ctx, tr, events...)

	if len(sink.traces) != 1 {
		t.Fatalf("traces = %#v, want the tool-only turn recorded", sink.traces)
	}
	trace := sink.traces[0]
	if trace.TraceID != runID.String() || trace.ProviderSummary != "" {
		t.Fatalf("trace = %#v, want run %s and no summary", trace, runID)
	}
	if len(trace.Steps) != 1 || trace.Steps[0].ProviderSummary != "" || trace.Steps[0].CreatedAt.IsZero() {
		t.Fatalf("steps = %#v, want one dated summary-less step", trace.Steps)
	}
	calls := trace.Steps[0].ToolCalls
	if len(calls) != 1 || calls[0].ToolName != "task" || calls[0].Status != "succeeded" || calls[0].SourceRef != trace.SourceRef {
		t.Fatalf("tool calls = %#v", calls)
	}
}

// Reasoning that arrives after the first tool joins the trace the tool opened, as the
// step after that tool.
func TestReasoningGraphToolOpensTheTraceAndReasoningJoinsIt(t *testing.T) {
	r, _ := newReasoningTestRunner(t, 65536, true)
	order := []string{}
	sink := &recordingReasoningGraphSink{order: &order}
	r.reasoningGraphSink = sink
	ctx := identityctx.WithIdentityID(t.Context(), uuid.NewString())
	tr := &turnTracker{convID: newConvID(t), llmRuntime: r.llmSnapshot(ctx)}
	runID := uuid.Must(uuid.NewV7())
	t0 := time.Date(2026, 10, 6, 9, 0, 0, 0, time.UTC)

	events := reasoningGraphToolEvents(runID, t0, "call-a", "task", `{"action":"list"}`, "ok", "two tasks", nil)
	events = append(events, reasoningGraphEvent(runID, t0.Add(time.Second), "Delete the older one."))
	events = append(events, reasoningGraphToolEvents(runID, t0.Add(2*time.Second), "call-b", "task",
		`{"action":"delete"}`, "ok", "deleted", nil)...)
	events = append(events, reasoningGraphFinalEvent(runID, t0.Add(3*time.Second), "Fatto."))
	persistReasoningGraphEvents(t, r, ctx, tr, events...)

	if len(sink.traces) != 1 {
		t.Fatalf("traces = %#v", sink.traces)
	}
	trace := sink.traces[0]
	if trace.ProviderSummary != "Delete the older one." || len(trace.Steps) != 2 {
		t.Fatalf("trace = %#v, want the reasoning as summary and two steps", trace)
	}
	first, second := trace.Steps[0], trace.Steps[1]
	if first.ProviderSummary != "" || len(first.ToolCalls) != 1 || first.ToolCalls[0].CallID != "call-a" {
		t.Fatalf("first step = %#v, want the summary-less step call-a opened", first)
	}
	if second.ProviderSummary != "Delete the older one." || len(second.ToolCalls) != 1 || second.ToolCalls[0].CallID != "call-b" {
		t.Fatalf("second step = %#v", second)
	}
}

// A failed call is the one fact the next turn must learn from, so a turn whose only tool
// failed is recorded with the failure's redacted reason.
func TestReasoningGraphRecordsAFailedToolOnlyTurn(t *testing.T) {
	r, _ := newReasoningTestRunner(t, 65536, true)
	order := []string{}
	sink := &recordingReasoningGraphSink{order: &order}
	r.reasoningGraphSink = sink
	ctx := identityctx.WithIdentityID(t.Context(), uuid.NewString())
	tr := &turnTracker{convID: newConvID(t), llmRuntime: r.llmSnapshot(ctx)}
	runID := uuid.Must(uuid.NewV7())
	t0 := time.Date(2026, 10, 6, 9, 0, 0, 0, time.UTC)

	events := reasoningGraphToolEvents(runID, t0, "call-bad", "whatsapp__send_message",
		`{"to":"Marco"}`, "error", "contact not found (token=sk-secret)", nil)
	events = append(events, reasoningGraphFinalEvent(runID, t0.Add(time.Second), "Non trovo Marco."))
	persistReasoningGraphEvents(t, r, ctx, tr, events...)

	if len(sink.traces) != 1 || len(sink.traces[0].Steps) != 1 {
		t.Fatalf("traces = %#v, want the failed tool-only turn recorded", sink.traces)
	}
	call := sink.traces[0].Steps[0].ToolCalls[0]
	if call.Status != "failed" || !strings.Contains(call.Observation, "contact not found") {
		t.Fatalf("failed call = %#v, want its reason", call)
	}
	if strings.Contains(call.Observation, "sk-secret") {
		t.Fatalf("failure observation skipped redaction: %q", call.Observation)
	}
}

// With ShowReasoning off the stream is redacted at the source and never observed, but the
// tools the turn ran are runtime facts, not reasoning: they are recorded, and the trace
// carries no reasoning text at all.
func TestReasoningGraphRecordsAHiddenReasoningTurnByItsTools(t *testing.T) {
	r, _ := newReasoningTestRunner(t, 65536, false)
	order := []string{}
	sink := &recordingReasoningGraphSink{order: &order}
	r.reasoningGraphSink = sink
	ctx := identityctx.WithIdentityID(t.Context(), uuid.NewString())
	tr := &turnTracker{convID: newConvID(t), llmRuntime: r.llmSnapshot(ctx)}
	runID := uuid.Must(uuid.NewV7())
	t0 := time.Date(2026, 10, 6, 9, 0, 0, 0, time.UTC)

	events := []*agent.Event{reasoningGraphEvent(runID, t0, "hidden thoughts about the reminder")}
	events = append(events, reasoningGraphToolEvents(runID, t0.Add(time.Second), "call-task", "task",
		`{"action":"create"}`, "ok", "scheduled", nil)...)
	events = append(events, reasoningGraphFinalEvent(runID, t0.Add(2*time.Second), "Fatto."))
	persistReasoningGraphEvents(t, r, ctx, tr, events...)

	if len(sink.traces) != 1 {
		t.Fatalf("traces = %#v, want the tools of the hidden turn recorded", sink.traces)
	}
	trace := sink.traces[0]
	if trace.ProviderSummary != "" || len(trace.Steps) != 1 || trace.Steps[0].ProviderSummary != "" {
		t.Fatalf("trace = %#v, want no reasoning text", trace)
	}
	if calls := trace.Steps[0].ToolCalls; len(calls) != 1 || calls[0].ToolName != "task" {
		t.Fatalf("tool calls = %#v", calls)
	}
}

// A discarded provider attempt takes its tools with it, even when no reasoning opened the
// trace.
func TestReasoningGraphDiscardDropsAToolOnlyAttempt(t *testing.T) {
	r, _ := newReasoningTestRunner(t, 65536, true)
	order := []string{}
	sink := &recordingReasoningGraphSink{order: &order}
	r.reasoningGraphSink = sink
	ctx := identityctx.WithIdentityID(t.Context(), uuid.NewString())
	tr := &turnTracker{convID: newConvID(t), llmRuntime: r.llmSnapshot(ctx)}
	runID := uuid.Must(uuid.NewV7())
	t0 := time.Date(2026, 10, 6, 9, 0, 0, 0, time.UTC)

	events := reasoningGraphToolEvents(runID, t0, "call-old", "task", `{"action":"create"}`, "ok", "scheduled", nil)
	discard := &agent.Event{RequestID: runID, Timestamp: t0.Add(time.Second)}
	discard.Actions.DiscardStreamed = true
	events = append(events, discard)
	events = append(events, reasoningGraphToolEvents(runID, t0.Add(2*time.Second), "call-new", "task",
		`{"action":"create"}`, "ok", "scheduled", nil)...)
	events = append(events, reasoningGraphFinalEvent(runID, t0.Add(3*time.Second), "Fatto."))
	persistReasoningGraphEvents(t, r, ctx, tr, events...)

	if len(sink.traces) != 1 || len(sink.traces[0].Steps) != 1 {
		t.Fatalf("traces = %#v", sink.traces)
	}
	if calls := sink.traces[0].Steps[0].ToolCalls; len(calls) != 1 || calls[0].CallID != "call-new" {
		t.Fatalf("tool calls = %#v, want only the accepted attempt's call", calls)
	}
}

// Once a tool event opened the trace, another attempt's events are foreign and a repeated
// call ID is the same call: neither adds a tool.
func TestReasoningTraceBuilderRejectsForeignRunsAndRepeatedCalls(t *testing.T) {
	var b ReasoningTraceBuilder
	runID, foreign := uuid.Must(uuid.NewV7()), uuid.Must(uuid.NewV7())
	t0 := time.Date(2026, 10, 6, 9, 0, 0, 0, time.UTC)
	end := func(run uuid.UUID, at time.Time, callID string) *agent.Event {
		return reasoningGraphToolEvents(run, at, callID, "task", `{"action":"create"}`, "ok", "scheduled", nil)[1]
	}

	b.ObserveToolInvocation(end(runID, t0, "call-1"))
	b.ObserveToolInvocation(end(foreign, t0.Add(time.Second), "call-2"))
	b.ObserveToolInvocation(end(runID, t0.Add(2*time.Second), "call-1"))
	b.ObserveReasoning(reasoningGraphEvent(foreign, t0.Add(3*time.Second), "foreign reasoning"))

	trace, ok := b.CommitSourceTurn(uuid.NewString(), uuid.NewString(), 2, t0.Add(4*time.Second))
	if !ok {
		t.Fatal("the tool-only trace was not committed")
	}
	var calls []arcadedb.ReasoningToolCall
	for _, step := range trace.Steps {
		calls = append(calls, step.ToolCalls...)
	}
	if trace.TraceID != runID.String() || trace.ProviderSummary != "" || len(calls) != 1 || calls[0].CallID != "call-1" {
		t.Fatalf("trace = %#v, want run %s with call-1 once and no foreign text", trace, runID)
	}
}
