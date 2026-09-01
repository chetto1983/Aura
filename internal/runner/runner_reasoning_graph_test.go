package runner

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

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
