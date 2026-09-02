package runner

import (
	"context"
	"encoding/hex"
	"errors"
	"strings"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/chetto1983/aura/internal/agent"
	"github.com/chetto1983/aura/internal/arcadedb"
	"github.com/chetto1983/aura/internal/conversations"
	"github.com/chetto1983/aura/internal/db/sqlc"
	"github.com/chetto1983/aura/internal/identityctx"
	"github.com/google/uuid"
)

type orderedReasoningConvStore struct {
	*fakeConvStore
	order *[]string
}

func (s *orderedReasoningConvStore) AppendAssistantTurnWithCacheMetric(
	ctx context.Context,
	p conversations.AppendTurnParams,
	metric sqlc.InsertCacheMetricParams,
) error {
	err := s.fakeConvStore.AppendAssistantTurnWithCacheMetric(ctx, p, metric)
	if err == nil {
		*s.order = append(*s.order, "source-committed")
	}
	return err
}

type recordingReasoningGraphSink struct {
	order  *[]string
	traces []arcadedb.ReasoningTrace
	err    error
}

func (s *recordingReasoningGraphSink) UpsertReasoningTrace(
	_ context.Context,
	trace arcadedb.ReasoningTrace,
) error {
	*s.order = append(*s.order, "graph-offered")
	s.traces = append(s.traces, trace)
	return s.err
}

func reasoningGraphEvent(runID uuid.UUID, ts time.Time, delta string) *agent.Event {
	ev := reasoningEvent(ts, delta)
	ev.RequestID = runID
	return ev
}

func reasoningGraphFinalEvent(runID uuid.UUID, ts time.Time, content string) *agent.Event {
	ev := finalAnswerEvent(content)
	ev.RequestID = runID
	ev.Timestamp = ts
	return ev
}

func reasoningGraphToolEvents(
	runID uuid.UUID,
	ts time.Time,
	callID, toolName, arguments, status, preview string,
	meta map[string]any,
) []*agent.Event {
	startedAt, endedAt := ts.UTC(), ts.Add(250*time.Millisecond).UTC()
	start := &agent.Event{RequestID: runID, Timestamp: startedAt}
	start.Actions.ToolInvocation = &agent.ToolInvocation{
		Event: agent.ToolInvocationStart, ToolCallID: callID, ToolName: toolName,
		Arguments: arguments, ArgsBytes: len(arguments), BatchIndex: 0, BatchSize: 1,
		StartedAt: &startedAt,
	}
	end := &agent.Event{RequestID: runID, Timestamp: endedAt}
	end.Actions.ToolInvocation = &agent.ToolInvocation{
		Event: agent.ToolInvocationEnd, ToolCallID: callID, ToolName: toolName,
		Arguments: arguments, ArgsBytes: len(arguments), StartedAt: &startedAt, EndedAt: &endedAt,
		DurationMS: 250, Status: status, ResultPreview: preview, PreviewBytes: len(preview),
		ResultBytes: len(preview), Meta: meta,
	}
	return []*agent.Event{start, end}
}

func persistReasoningGraphEvents(
	t *testing.T,
	r *Runner,
	ctx context.Context,
	tr *turnTracker,
	events ...*agent.Event,
) {
	t.Helper()
	for _, ev := range events {
		if err := r.persistEvent(ctx, tr, ev); err != nil {
			t.Fatalf("persistEvent: %v", err)
		}
	}
}

func TestReasoningGraphTracer(t *testing.T) {
	t.Run("authorized provider reasoning is offered after its exact source turn", func(t *testing.T) {
		r, base := newReasoningTestRunner(t, 65536, true)
		order := []string{}
		r.Conv = &orderedReasoningConvStore{fakeConvStore: base, order: &order}
		sink := &recordingReasoningGraphSink{order: &order}
		r.reasoningGraphSink = sink

		convID := newConvID(t)
		identityID := uuid.NewString()
		ctx := identityctx.WithIdentityID(t.Context(), identityID)
		tr := &turnTracker{convID: convID, llmRuntime: r.llmSnapshot(ctx)}
		runID := uuid.Must(uuid.NewV7())
		t0 := time.Date(2026, 9, 1, 8, 0, 0, 0, time.UTC)
		for _, ev := range []*agent.Event{
			reasoningGraphEvent(runID, t0, "Inspect the deployment; "),
			reasoningGraphEvent(runID, t0.Add(time.Second), "compare its health."),
			reasoningGraphFinalEvent(runID, t0.Add(2*time.Second), "The deployment is healthy."),
		} {
			if err := r.persistEvent(ctx, tr, ev); err != nil {
				t.Fatalf("persistEvent: %v", err)
			}
		}

		if strings.Join(order, ",") != "source-committed,graph-offered" {
			t.Fatalf("persistence order = %v, want source commit before graph offer", order)
		}
		if len(sink.traces) != 1 {
			t.Fatalf("graph offers = %d, want 1", len(sink.traces))
		}
		got := sink.traces[0]
		if got.IdentityID != identityID || got.ConversationID != convID || got.TurnSeq != 1 {
			t.Fatalf("source identity/turn = %q/%q/%d", got.IdentityID, got.ConversationID, got.TurnSeq)
		}
		wantSource := "postgres://aura/conversations/" + convID + "/turns/1"
		if got.SourceRef != wantSource || got.TraceID == "" {
			t.Fatalf("trace source/id = %q/%q, want %q/non-empty", got.SourceRef, got.TraceID, wantSource)
		}
		if got.ProviderSummary != "Inspect the deployment; compare its health." ||
			strings.Contains(got.ProviderSummary, "deployment is healthy") {
			t.Fatalf("provider summary = %q", got.ProviderSummary)
		}
		if len(got.Steps) != 1 || got.Steps[0].ProviderSummary != got.ProviderSummary {
			t.Fatalf("steps = %#v, want one provider-visible ordered step", got.Steps)
		}
	})

	t.Run("failed source append produces no graph offer", func(t *testing.T) {
		r, base := newReasoningTestRunner(t, 65536, true)
		base.appendEr = errFake
		order := []string{}
		sink := &recordingReasoningGraphSink{order: &order}
		r.reasoningGraphSink = sink
		ctx := identityctx.WithIdentityID(t.Context(), uuid.NewString())
		tr := &turnTracker{convID: newConvID(t), llmRuntime: r.llmSnapshot(ctx)}
		runID := uuid.Must(uuid.NewV7())
		_ = r.persistEvent(ctx, tr, reasoningGraphEvent(runID, time.Now().UTC(), "visible"))
		if err := r.persistEvent(ctx, tr, reasoningGraphFinalEvent(runID, time.Now().UTC(), "answer")); err == nil {
			t.Fatal("failed source append returned nil")
		}
		if len(sink.traces) != 0 {
			t.Fatalf("failed source append offered %d traces", len(sink.traces))
		}
	})

	t.Run("graph failure cannot invalidate the committed answer", func(t *testing.T) {
		r, base := newReasoningTestRunner(t, 65536, true)
		order := []string{}
		r.Conv = &orderedReasoningConvStore{fakeConvStore: base, order: &order}
		sink := &recordingReasoningGraphSink{order: &order, err: errors.New("graph unavailable")}
		r.reasoningGraphSink = sink
		ctx := identityctx.WithIdentityID(t.Context(), uuid.NewString())
		convID := newConvID(t)
		tr := &turnTracker{convID: convID, llmRuntime: r.llmSnapshot(ctx)}
		runID := uuid.Must(uuid.NewV7())
		_ = r.persistEvent(ctx, tr, reasoningGraphEvent(runID, time.Now().UTC(), "visible"))
		if err := r.persistEvent(ctx, tr, reasoningGraphFinalEvent(runID, time.Now().UTC(), "durable answer")); err != nil {
			t.Fatalf("graph failure changed source success: %v", err)
		}
		if got := lastTurn(t, base, convID).Content; got != "durable answer" {
			t.Fatalf("committed answer = %q", got)
		}
	})

	t.Run("hidden and synthetic sources have no producer", func(t *testing.T) {
		for _, tc := range []struct {
			name          string
			showReasoning bool
			withReasoning bool
		}{
			{name: "hidden provider reasoning", showReasoning: false, withReasoning: true},
			{name: "final answer only", showReasoning: true, withReasoning: false},
		} {
			t.Run(tc.name, func(t *testing.T) {
				r, _ := newReasoningTestRunner(t, 65536, tc.showReasoning)
				order := []string{}
				sink := &recordingReasoningGraphSink{order: &order}
				r.reasoningGraphSink = sink
				ctx := identityctx.WithIdentityID(t.Context(), uuid.NewString())
				tr := &turnTracker{convID: newConvID(t), llmRuntime: r.llmSnapshot(ctx)}
				runID := uuid.Must(uuid.NewV7())
				if tc.withReasoning {
					_ = r.persistEvent(ctx, tr, reasoningGraphEvent(runID, time.Now().UTC(), "not authorized"))
				}
				if err := r.persistEvent(ctx, tr, reasoningGraphFinalEvent(runID, time.Now().UTC(), "synthetic summary candidate")); err != nil {
					t.Fatalf("persist final answer: %v", err)
				}
				if len(sink.traces) != 0 {
					t.Fatalf("unauthorized source offered %d traces", len(sink.traces))
				}
			})
		}
	})
}

func TestReasoningGraphToolMetadata(t *testing.T) {
	r, _ := newReasoningTestRunner(t, 65536, true)
	order := []string{}
	sink := &recordingReasoningGraphSink{order: &order}
	r.reasoningGraphSink = sink
	ctx := identityctx.WithIdentityID(t.Context(), uuid.NewString())
	tr := &turnTracker{convID: newConvID(t), llmRuntime: r.llmSnapshot(ctx)}
	runID := uuid.Must(uuid.NewV7())
	t0 := time.Date(2026, 9, 1, 9, 0, 0, 0, time.UTC)

	secretPreview := "password=supersecret " + strings.Repeat("x", 5000)
	allowed := reasoningGraphToolEvents(runID, t0.Add(time.Second), "call-safe", "send_file",
		`{"path":"report.txt","token":"sk-secret"}`, "ok", secretPreview,
		map[string]any{"artifact": map[string]any{"asset_id": "asset-123"}})
	blob := reasoningGraphToolEvents(runID, t0.Add(2*time.Second), "call-blob", "send_file",
		`{"path":"blob.bin"}`, "ok", "data:application/octet-stream;base64,AAAA", nil)
	disallowed := reasoningGraphToolEvents(runID, t0.Add(3*time.Second), "call-raw", "shell_exec",
		`{"command":"cat /secret"}`, "ok", "raw private output", nil)
	events := []*agent.Event{reasoningGraphEvent(runID, t0, "Prepare an operator artifact.")}
	events = append(events, allowed...)
	events = append(events, reasoningGraphEvent(runID, t0.Add(1500*time.Millisecond), "Record the artifact result."))
	events = append(events, blob...)
	events = append(events, disallowed...)
	events = append(events, reasoningGraphFinalEvent(runID, t0.Add(4*time.Second), "done"))
	persistReasoningGraphEvents(t, r, ctx, tr, events...)

	if len(sink.traces) != 1 || len(sink.traces[0].Steps) != 2 {
		t.Fatalf("trace/steps = %#v", sink.traces)
	}
	if sink.traces[0].Steps[0].ProviderSummary != "Prepare an operator artifact." ||
		sink.traces[0].Steps[1].ProviderSummary != "Record the artifact result." {
		t.Fatalf("stable tool-boundary segmentation = %#v", sink.traces[0].Steps)
	}
	tools := append([]arcadedb.ReasoningToolCall(nil), sink.traces[0].Steps[0].ToolCalls...)
	tools = append(tools, sink.traces[0].Steps[1].ToolCalls...)
	if len(tools) != 2 {
		t.Fatalf("allowed tool calls = %#v, want two send_file calls only", tools)
	}
	if tools[0].CallID != "call-safe" || tools[1].CallID != "call-blob" {
		t.Fatalf("tool order = %q/%q", tools[0].CallID, tools[1].CallID)
	}
	if tools[0].Status != "succeeded" || tools[0].DurationMillis != 250 {
		t.Fatalf("tool status/duration = %q/%d", tools[0].Status, tools[0].DurationMillis)
	}
	if len(tools[0].ArgumentDigest) != 64 {
		t.Fatalf("argument digest = %q", tools[0].ArgumentDigest)
	}
	if _, err := hex.DecodeString(tools[0].ArgumentDigest); err != nil {
		t.Fatalf("argument digest is not hex: %v", err)
	}
	if strings.Contains(tools[0].Observation, "supersecret") || !strings.Contains(tools[0].Observation, "[REDACTED]") {
		t.Fatalf("observation redaction = %q", tools[0].Observation)
	}
	if utf8.RuneCountInString(tools[0].Observation) > 1024 {
		t.Fatalf("observation runes = %d", utf8.RuneCountInString(tools[0].Observation))
	}
	if tools[1].Observation != "" {
		t.Fatalf("embedded blob persisted as observation: %q", tools[1].Observation)
	}
	if len(tools[0].ArtifactRefs) != 1 || tools[0].ArtifactRefs[0] != "artifact://assets/asset-123" {
		t.Fatalf("artifact refs = %#v", tools[0].ArtifactRefs)
	}
	for _, tool := range tools {
		if tool.SourceRef != sink.traces[0].SourceRef {
			t.Fatalf("tool source %q != trace source %q", tool.SourceRef, sink.traces[0].SourceRef)
		}
	}
}

func TestReasoningGraphRetryDiscard(t *testing.T) {
	r, _ := newReasoningTestRunner(t, 65536, true)
	order := []string{}
	sink := &recordingReasoningGraphSink{order: &order}
	r.reasoningGraphSink = sink
	ctx := identityctx.WithIdentityID(t.Context(), uuid.NewString())
	tr := &turnTracker{convID: newConvID(t), llmRuntime: r.llmSnapshot(ctx)}
	runID := uuid.Must(uuid.NewV7())
	t0 := time.Date(2026, 9, 1, 10, 0, 0, 0, time.UTC)

	oldTool := reasoningGraphToolEvents(runID, t0.Add(time.Second), "call-old", "memory_upsert_fact",
		`{"subject":"Repudiated","object":"Old"}`, "ok", "stored", nil)
	newTool := reasoningGraphToolEvents(runID, t0.Add(4*time.Second), "call-new", "memory_upsert_fact",
		`{"subject":"Accepted","object":"Final"}`, "ok", "stored", nil)
	discard := &agent.Event{RequestID: runID, Timestamp: t0.Add(2 * time.Second)}
	discard.Actions.DiscardStreamed = true
	events := []*agent.Event{reasoningGraphEvent(runID, t0, "repudiated attempt")}
	events = append(events, oldTool...)
	events = append(events, discard, reasoningGraphEvent(runID, t0.Add(3*time.Second), "accepted attempt"))
	events = append(events, newTool...)
	events = append(events, reasoningGraphFinalEvent(runID, t0.Add(5*time.Second), "answer"))
	persistReasoningGraphEvents(t, r, ctx, tr, events...)

	if len(sink.traces) != 1 {
		t.Fatalf("trace count = %d", len(sink.traces))
	}
	got := sink.traces[0]
	if got.ProviderSummary != "accepted attempt" || len(got.Steps) != 1 || len(got.Steps[0].ToolCalls) != 1 {
		t.Fatalf("post-retry trace = %#v", got)
	}
	tool := got.Steps[0].ToolCalls[0]
	if tool.CallID != "call-new" || strings.Contains(strings.Join(tool.EntityRefs, ","), "Repudiated") {
		t.Fatalf("post-retry tool = %#v", tool)
	}
}

func TestReasoningGraphTouchedEntities(t *testing.T) {
	r, _ := newReasoningTestRunner(t, 65536, true)
	order := []string{}
	sink := &recordingReasoningGraphSink{order: &order}
	r.reasoningGraphSink = sink
	ctx := identityctx.WithIdentityID(t.Context(), uuid.NewString())
	tr := &turnTracker{convID: newConvID(t), llmRuntime: r.llmSnapshot(ctx)}
	runID := uuid.Must(uuid.NewV7())
	t0 := time.Date(2026, 9, 1, 11, 0, 0, 0, time.UTC)

	trusted := reasoningGraphToolEvents(runID, t0.Add(time.Second), "call-memory", "memory_upsert_fact",
		`{"subject":"Davide","predicate":"lives_in","object":"Caraglio"}`, "ok", "stored", nil)
	untrusted := reasoningGraphToolEvents(runID, t0.Add(2*time.Second), "call-shell", "shell_exec",
		`{"command":"echo"}`, "ok", "done", map[string]any{"entity_refs": []string{"Forged"}})
	events := []*agent.Event{reasoningGraphEvent(runID, t0, "Inspect ImaginedEntity, then store the observed fact.")}
	events = append(events, trusted...)
	events = append(events, untrusted...)
	events = append(events, reasoningGraphFinalEvent(runID, t0.Add(3*time.Second), "done"))
	persistReasoningGraphEvents(t, r, ctx, tr, events...)

	if len(sink.traces) != 1 || len(sink.traces[0].Steps) != 1 || len(sink.traces[0].Steps[0].ToolCalls) != 1 {
		t.Fatalf("trusted tool trace = %#v", sink.traces)
	}
	refs := sink.traces[0].Steps[0].ToolCalls[0].EntityRefs
	if strings.Join(refs, ",") != "Caraglio,Davide" {
		t.Fatalf("entity refs = %#v, want only structured memory-tool entities", refs)
	}
}

// The runtime names MCP-served memory tools with their server namespace
// ("memory__memory_upsert_fact"), never the bare name. Keying the reasoning
// policy on bare names silently drops every memory tool from the graph, so no
// TOUCHED edge is ever written; measured live on 2026-09-02 with
// ReasoningTrace=7 but TOUCHED=0. See internal/runner/runner_memory_capture.go,
// which already pins the same model-facing name.
func TestReasoningGraphTouchedEntitiesForNamespacedMemoryTool(t *testing.T) {
	r, _ := newReasoningTestRunner(t, 65536, true)
	order := []string{}
	sink := &recordingReasoningGraphSink{order: &order}
	r.reasoningGraphSink = sink
	ctx := identityctx.WithIdentityID(t.Context(), uuid.NewString())
	tr := &turnTracker{convID: newConvID(t), llmRuntime: r.llmSnapshot(ctx)}
	runID := uuid.Must(uuid.NewV7())
	t0 := time.Date(2026, 9, 1, 11, 0, 0, 0, time.UTC)

	namespaced := reasoningGraphToolEvents(runID, t0.Add(time.Second), "call-memory", memoryUpsertFactModelName,
		`{"subject":"Davide","predicate":"lives_in","object":"Caraglio"}`, "ok", "stored", nil)
	events := []*agent.Event{reasoningGraphEvent(runID, t0, "Store the observed fact.")}
	events = append(events, namespaced...)
	events = append(events, reasoningGraphFinalEvent(runID, t0.Add(3*time.Second), "done"))
	persistReasoningGraphEvents(t, r, ctx, tr, events...)

	if len(sink.traces) != 1 || len(sink.traces[0].Steps) != 1 || len(sink.traces[0].Steps[0].ToolCalls) != 1 {
		t.Fatalf("namespaced memory tool was dropped from the reasoning graph: %#v", sink.traces)
	}
	refs := sink.traces[0].Steps[0].ToolCalls[0].EntityRefs
	if strings.Join(refs, ",") != "Caraglio,Davide" {
		t.Fatalf("entity refs = %#v, want the structured memory-tool entities that TOUCHED is built from", refs)
	}
}
