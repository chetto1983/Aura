package runner

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/chetto1983/aura/internal/agent"
	"github.com/chetto1983/aura/internal/agent/tools"
	"github.com/chetto1983/aura/internal/identityctx"
	"github.com/google/uuid"
	"go.uber.org/goleak"
)

type recordingCaptureSink struct {
	mu      sync.Mutex
	seen    []AcceptedCapture
	started chan struct{}
	release chan struct{}
	err     error
	once    sync.Once
}

func (s *recordingCaptureSink) ApplyAcceptedCapture(ctx context.Context, capture AcceptedCapture) error {
	if s.started != nil {
		s.once.Do(func() { close(s.started) })
	}
	if s.release != nil {
		select {
		case <-s.release:
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	if s.err != nil {
		return s.err
	}
	s.mu.Lock()
	s.seen = append(s.seen, capture)
	s.mu.Unlock()
	return nil
}

func (s *recordingCaptureSink) captures() []AcceptedCapture {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]AcceptedCapture(nil), s.seen...)
}

func queueCapture(id string) AcceptedCapture {
	return AcceptedCapture{
		IdempotencyKey: id, IdentityID: "identity-a", ActorRunID: "run-a", ActorRole: "parent",
		SourceKind: CaptureSourceExplicitFact, ConversationID: "conversation-a", ToolCallID: "call-" + id,
		Subject: "operator", Predicate: "uses", Object: id, Statement: "operator uses " + id,
		Confidence: 1, ObservedAt: time.Now().UTC(),
	}
}

func captureEvent(toolName, status, callID string, meta map[string]any) *agent.Event {
	now := time.Date(2026, 9, 1, 2, 3, 4, 0, time.UTC)
	return &agent.Event{
		RequestID: uuid.MustParse("01991f4c-7a00-7000-8000-000000000001"),
		Timestamp: now,
		Actions: agent.Actions{ToolInvocation: &agent.ToolInvocation{
			Event: agent.ToolInvocationEnd, ToolName: toolName, ToolCallID: callID,
			Status: status, Meta: meta,
		}},
	}
}

func TestMemoryUpsertAcceptedCapture(t *testing.T) {
	ctx := identityctx.WithIdentityID(context.Background(), "identity-a")
	evidence := tools.AcceptedFactEvidence{
		Subject: "operator", Predicate: "lives_in", Object: "Torino",
		Statement: "The operator lives in Torino.", SourceMemoryIDs: []string{"message-7"},
		ActorRunID: "01991f4c-7a00-7000-8000-000000000001", ActorRole: "parent",
	}
	ev := captureEvent("memory__memory_upsert_fact", "ok", "call-memory", map[string]any{
		tools.MetaAcceptedFact: evidence,
	})

	got, ok := acceptedCaptureFromEvent(ctx, "conversation-a", ev)
	if !ok {
		t.Fatal("successful memory_upsert_fact evidence emitted no AcceptedCapture")
	}
	if got.SourceKind != CaptureSourceExplicitFact || got.Subject != evidence.Subject ||
		got.Predicate != evidence.Predicate || got.Object != evidence.Object || got.Statement != evidence.Statement {
		t.Fatalf("fact capture = %+v, want canonical evidence %+v", got, evidence)
	}
	if got.IdentityID != "identity-a" || got.ActorRunID != evidence.ActorRunID || got.ActorRole != "parent" ||
		got.ConversationID != "conversation-a" || got.ToolCallID != "call-memory" {
		t.Fatalf("direct provenance changed: %+v", got)
	}
	if got.IdempotencyKey == "" || got.Confidence <= 0 || !got.ObservedAt.Equal(ev.Timestamp) {
		t.Fatalf("capture lacks stable identity/confidence/time: %+v", got)
	}
}

func TestDurableArtifactAcceptedCapture(t *testing.T) {
	ctx := identityctx.WithIdentityID(context.Background(), "identity-a")
	for _, tc := range []struct {
		tool      string
		operation string
	}{
		{tool: "write_file", operation: "write"},
		{tool: "patch", operation: "patch"},
	} {
		t.Run(tc.tool, func(t *testing.T) {
			evidence := tools.DurableArtifactEvidence{
				Path: "/workspace/report.md", Operation: tc.operation,
				ActorRunID: "01991f4c-7a00-7000-8000-000000000001", ActorRole: "worker",
			}
			ev := captureEvent(tc.tool, "ok", "call-"+tc.tool, map[string]any{
				tools.MetaDurableArtifact: evidence,
			})
			got, ok := acceptedCaptureFromEvent(ctx, "conversation-a", ev)
			if !ok {
				t.Fatal("successful durable artifact evidence emitted no AcceptedCapture")
			}
			if got.SourceKind != CaptureSourceDurableArtifact || got.ArtifactRef != evidence.Path ||
				got.ActorRole != "worker" || got.ActorRunID != evidence.ActorRunID {
				t.Fatalf("artifact capture = %+v, want evidence %+v", got, evidence)
			}
			if got.Statement != "" || got.IdempotencyKey == "" {
				t.Fatalf("artifact capture copied prose or lacks idempotency: %+v", got)
			}
		})
	}
}

func TestAcceptedCaptureProducerRejectsExcludedSources(t *testing.T) {
	ctx := identityctx.WithIdentityID(context.Background(), "identity-a")
	validFact := tools.AcceptedFactEvidence{
		Subject: "operator", Predicate: "uses", Object: "Aura", Statement: "The operator uses Aura.",
		ActorRunID: "01991f4c-7a00-7000-8000-000000000001", ActorRole: "parent",
	}
	validArtifact := tools.DurableArtifactEvidence{
		Path: "/workspace/report.md", Operation: "write",
		ActorRunID: "01991f4c-7a00-7000-8000-000000000001", ActorRole: "parent",
	}
	cases := map[string]*agent.Event{
		"failed fact":        captureEvent("memory__memory_upsert_fact", "error", "c1", map[string]any{tools.MetaAcceptedFact: validFact}),
		"shell output":       captureEvent("shell_exec", "success", "c2", map[string]any{tools.MetaDurableArtifact: validArtifact}),
		"read output":        captureEvent("read_file", "success", "c3", map[string]any{tools.MetaDurableArtifact: validArtifact}),
		"document output":    captureEvent("document_search", "success", "c4", map[string]any{tools.MetaAcceptedFact: validFact}),
		"temporary artifact": captureEvent("write_file", "ok", "c5", map[string]any{"temporary_artifact": validArtifact}),
		"assistant prose":    {LLMResponse: &agent.LLMResponse{Content: "I infer a durable fact."}},
		"reasoning":          {LLMResponse: &agent.LLMResponse{Reasoning: "scratch-work conclusion"}},
		"discard":            {Actions: agent.Actions{DiscardStreamed: true}},
	}
	secretFact := validFact
	secretFact.Object = "sk-or-1234567890abcdefghijklmnopqrstuvwxyz"
	secretFact.Statement = "API key is " + secretFact.Object
	cases["secret"] = captureEvent("memory__memory_upsert_fact", "ok", "c6", map[string]any{tools.MetaAcceptedFact: secretFact})

	for name, ev := range cases {
		t.Run(name, func(t *testing.T) {
			if got, ok := acceptedCaptureFromEvent(ctx, "conversation-a", ev); ok {
				t.Fatalf("excluded source emitted capture: %+v", got)
			}
		})
	}
	if _, ok := acceptedCaptureFromEvent(context.Background(), "conversation-a",
		captureEvent("write_file", "ok", "c7", map[string]any{tools.MetaDurableArtifact: validArtifact})); ok {
		t.Fatal("missing authenticated identity emitted capture")
	}
	if strings.TrimSpace(validFact.Statement) == "" {
		t.Fatal("fixture must carry explicit fact text")
	}
}

func TestMemoryCaptureQueueOrder(t *testing.T) {
	sink := &recordingCaptureSink{}
	queue := NewMemoryCaptureQueue(sink, MemoryCaptureQueueConfig{Capacity: 2})
	var last uint64
	for _, id := range []string{"one", "two", "three"} {
		seq, err := queue.Accept(t.Context(), queueCapture(id))
		if err != nil {
			t.Fatalf("Accept(%s): %v", id, err)
		}
		last = seq
	}
	if err := queue.FlushThrough(t.Context(), last); err != nil {
		t.Fatalf("FlushThrough(%d): %v", last, err)
	}
	got := sink.captures()
	if len(got) != 3 {
		t.Fatalf("sink captures = %d, want 3", len(got))
	}
	for i, want := range []string{"one", "two", "three"} {
		if got[i].Sequence != uint64(i+1) || got[i].Object != want {
			t.Fatalf("capture %d = %+v, want sequence %d object %q", i, got[i], i+1, want)
		}
	}

	t.Run("bounded backpressure", func(t *testing.T) {
		sink := &recordingCaptureSink{started: make(chan struct{}), release: make(chan struct{})}
		queue := NewMemoryCaptureQueue(sink, MemoryCaptureQueueConfig{Capacity: 1, WriteTimeout: time.Second})
		if _, err := queue.Accept(t.Context(), queueCapture("first")); err != nil {
			t.Fatalf("Accept first: %v", err)
		}
		<-sink.started
		if _, err := queue.Accept(t.Context(), queueCapture("second")); err != nil {
			t.Fatalf("Accept second: %v", err)
		}
		third := make(chan error, 1)
		go func() {
			_, err := queue.Accept(t.Context(), queueCapture("third"))
			third <- err
		}()
		select {
		case err := <-third:
			t.Fatalf("third Accept bypassed full queue backpressure: %v", err)
		case <-time.After(30 * time.Millisecond):
		}
		close(sink.release)
		if err := <-third; err != nil {
			t.Fatalf("third Accept after capacity freed: %v", err)
		}
		if err := queue.FlushThrough(t.Context(), queue.AcceptedSequence()); err != nil {
			t.Fatalf("FlushThrough after backpressure: %v", err)
		}
	})
}

func TestMemoryCaptureTerminalBarrier(t *testing.T) {
	t.Run("runner completion path", func(t *testing.T) {
		r, _, _ := newTestRunner(t, nil)
		sink := &recordingCaptureSink{started: make(chan struct{}), release: make(chan struct{})}
		defer func() {
			select {
			case <-sink.release:
			default:
				close(sink.release)
			}
		}()
		r.memoryCaptures = NewMemoryCaptureQueue(sink, MemoryCaptureQueueConfig{Capacity: 1, WriteTimeout: time.Second})
		ctx := identityctx.WithIdentityID(t.Context(), "identity-a")
		tr := &turnTracker{convID: newConvID(t), llmRuntime: r.llmSnapshot(ctx)}
		evidence := tools.DurableArtifactEvidence{
			Path: "/workspace/report.md", Operation: "write",
			ActorRunID: "01991f4c-7a00-7000-8000-000000000001", ActorRole: "parent",
		}
		if err := r.persistEvent(ctx, tr, captureEvent("write_file", "ok", "call-write", map[string]any{
			tools.MetaDurableArtifact: evidence,
		})); err != nil {
			t.Fatalf("persist tool result: %v", err)
		}
		if tr.lastAcceptedCapture == 0 {
			t.Fatal("runner did not accept the direct tool evidence")
		}
		terminal := &agent.Event{
			RequestID: uuid.MustParse(evidence.ActorRunID), Timestamp: time.Now().UTC(),
			LLMResponse: &agent.LLMResponse{Content: "done", FinishReason: "stop"},
		}
		done := make(chan error, 1)
		go func() { done <- r.persistEvent(ctx, tr, terminal) }()
		select {
		case err := <-done:
			t.Fatalf("runner returned terminal success before capture durability: %v", err)
		case <-time.After(30 * time.Millisecond):
		}
		close(sink.release)
		if err := <-done; err != nil {
			t.Fatalf("runner terminal completion after durability: %v", err)
		}
	})

	t.Run("resume drains pre-pause global watermark", func(t *testing.T) {
		r, _, _ := newTestRunner(t, nil)
		sink := &recordingCaptureSink{started: make(chan struct{}), release: make(chan struct{})}
		defer func() {
			select {
			case <-sink.release:
			default:
				close(sink.release)
			}
		}()
		r.memoryCaptures = NewMemoryCaptureQueue(sink, MemoryCaptureQueueConfig{Capacity: 1, WriteTimeout: time.Second})
		if _, err := r.memoryCaptures.Accept(t.Context(), queueCapture("before-pause")); err != nil {
			t.Fatalf("Accept pre-pause capture: %v", err)
		}
		<-sink.started
		ctx := identityctx.WithIdentityID(t.Context(), "identity-a")
		tr := &turnTracker{convID: newConvID(t), llmRuntime: r.llmSnapshot(ctx)}
		terminal := &agent.Event{
			RequestID: uuid.MustParse("01991f4c-7a00-7000-8000-000000000001"), Timestamp: time.Now().UTC(),
			LLMResponse: &agent.LLMResponse{Content: "resumed", FinishReason: "stop"},
		}
		done := make(chan error, 1)
		go func() { done <- r.persistEvent(ctx, tr, terminal) }()
		select {
		case err := <-done:
			t.Fatalf("resumed terminal returned before pre-pause capture durability: %v", err)
		case <-time.After(30 * time.Millisecond):
		}
		close(sink.release)
		if err := <-done; err != nil {
			t.Fatalf("resumed terminal after durability: %v", err)
		}
	})

	t.Run("completion waits for durability", func(t *testing.T) {
		sink := &recordingCaptureSink{started: make(chan struct{}), release: make(chan struct{})}
		queue := NewMemoryCaptureQueue(sink, MemoryCaptureQueueConfig{Capacity: 1, WriteTimeout: time.Second})
		seq, err := queue.Accept(t.Context(), queueCapture("barrier"))
		if err != nil {
			t.Fatalf("Accept: %v", err)
		}
		done := make(chan error, 1)
		go func() { done <- queue.FlushThrough(t.Context(), seq) }()
		select {
		case err := <-done:
			t.Fatalf("terminal barrier returned before durability: %v", err)
		case <-time.After(30 * time.Millisecond):
		}
		close(sink.release)
		if err := <-done; err != nil {
			t.Fatalf("terminal barrier after durability: %v", err)
		}
	})

	t.Run("exhausted sink failure rejects success", func(t *testing.T) {
		sinkErr := errors.New("arcadedb unavailable")
		queue := NewMemoryCaptureQueue(&recordingCaptureSink{err: sinkErr}, MemoryCaptureQueueConfig{
			Capacity: 1, MaxAttempts: 2, WriteTimeout: 20 * time.Millisecond,
		})
		seq, err := queue.Accept(t.Context(), queueCapture("failure"))
		if err != nil {
			t.Fatalf("Accept: %v", err)
		}
		if err := queue.FlushThrough(t.Context(), seq); !errors.Is(err, sinkErr) {
			t.Fatalf("FlushThrough error = %v, want %v", err, sinkErr)
		}
	})
}

func TestMemoryCaptureRetryDiscard(t *testing.T) {
	sink := &recordingCaptureSink{}
	queue := NewMemoryCaptureQueue(sink, MemoryCaptureQueueConfig{})
	r := &Runner{memoryCaptures: queue}
	ev := &agent.Event{RequestID: uuid.MustParse("01991f4c-7a00-7000-8000-000000000001"), Timestamp: time.Now().UTC()}
	ev.Actions.DiscardStreamed = true
	tr := &turnTracker{convID: "conversation-a"}
	ctx := identityctx.WithIdentityID(t.Context(), "identity-a")
	if err := r.persistEvent(ctx, tr, ev); err != nil {
		t.Fatalf("persist discarded event: %v", err)
	}
	if got := queue.AcceptedSequence(); got != 0 || tr.lastAcceptedCapture != 0 {
		t.Fatalf("discarded attempt accepted sequence queue=%d turn=%d", got, tr.lastAcceptedCapture)
	}
}

func TestMemoryCaptureStop(t *testing.T) {
	defer goleak.VerifyNone(t, goleak.IgnoreCurrent())
	t.Run("queue close", func(t *testing.T) {
		sink := &recordingCaptureSink{started: make(chan struct{}), release: make(chan struct{})}
		queue := NewMemoryCaptureQueue(sink, MemoryCaptureQueueConfig{WriteTimeout: time.Second})
		if _, err := queue.Accept(t.Context(), queueCapture("stop")); err != nil {
			t.Fatalf("Accept: %v", err)
		}
		select {
		case <-sink.started:
		case <-time.After(time.Second):
			t.Fatal("sink did not start")
		}
		short, cancel := context.WithTimeout(t.Context(), 20*time.Millisecond)
		defer cancel()
		if err := queue.Close(short); !errors.Is(err, context.DeadlineExceeded) {
			t.Fatalf("first Close error = %v, want deadline", err)
		}
		close(sink.release)
		if err := queue.Close(t.Context()); err != nil {
			t.Fatalf("second Close: %v", err)
		}
		if _, err := queue.Accept(t.Context(), queueCapture("late")); err == nil {
			t.Fatal("Accept after Close succeeded")
		}
	})

	t.Run("runner stop drains watermark", func(t *testing.T) {
		r, _, _ := newTestRunner(t, nil)
		sink := &recordingCaptureSink{started: make(chan struct{}), release: make(chan struct{})}
		r.memoryCaptures = NewMemoryCaptureQueue(sink, MemoryCaptureQueueConfig{WriteTimeout: time.Second})
		if _, err := r.memoryCaptures.Accept(t.Context(), queueCapture("runner-stop")); err != nil {
			t.Fatalf("Accept: %v", err)
		}
		select {
		case <-sink.started:
		case <-time.After(time.Second):
			t.Fatal("sink did not start")
		}
		done := make(chan error, 1)
		go func() { done <- r.Stop(t.Context(), "conversation-a") }()
		select {
		case err := <-done:
			t.Fatalf("Runner.Stop returned before capture drain: %v", err)
		case <-time.After(30 * time.Millisecond):
		}
		close(sink.release)
		if err := <-done; err != nil {
			t.Fatalf("Runner.Stop after capture drain: %v", err)
		}
	})
}
