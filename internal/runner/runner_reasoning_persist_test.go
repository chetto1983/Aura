package runner

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/chetto1983/aura/internal/agent"
	"github.com/chetto1983/aura/internal/agent/agenttest"
	"github.com/chetto1983/aura/internal/agent/tools"
	"github.com/chetto1983/aura/internal/conversations"
	"github.com/chetto1983/aura/internal/llm"
)

// newReasoningTestRunner mirrors newTestRunnerCfg but wires the amendment #91
// display-only CoT persistence knobs: the rune cap (Deps.ReasoningPersistMaxRunes)
// and the ShowReasoning master switch the accumulation gate reads.
func newReasoningTestRunner(t *testing.T, maxRunes int, showReasoning bool) (*Runner, *fakeConvStore) {
	t.Helper()
	conv := newFakeConvStore()
	reg := tools.NewRegistry()
	reg.Register(tools.TextResponse{})
	r := New(Deps{
		Conv:                     conv,
		Pause:                    newFakePauseStore(),
		Identity:                 newFakeIdentityStore(),
		CacheMetrics:             newFakeCacheMetricStore(),
		ToolInvocations:          newFakeToolInvocationStore(),
		Client:                   agenttest.NewFakeClient(),
		Registry:                 reg,
		LLM:                      llm.Config{Model: "test-model", ShowReasoning: showReasoning, ContextWindow: 1000000, MaxOutputTokens: 32768},
		ReasoningPersistMaxRunes: maxRunes,
		TitleTimeout:             2 * time.Second,
		StopTimeout:              2 * time.Second,
	})
	return r, conv
}

func reasoningEvent(ts time.Time, delta string) *agent.Event {
	return &agent.Event{Timestamp: ts, LLMResponse: &agent.LLMResponse{Reasoning: delta}}
}

func finalAnswerEvent(content string) *agent.Event {
	return &agent.Event{Timestamp: time.Now().UTC(), LLMResponse: &agent.LLMResponse{Content: content, FinishReason: "stop"}}
}

func lastTurn(t *testing.T, conv *fakeConvStore, convID string) conversations.AppendTurnParams {
	t.Helper()
	conv.mu.Lock()
	defer conv.mu.Unlock()
	turns := conv.turns[convID]
	if len(turns) == 0 {
		t.Fatal("no turns persisted")
	}
	return turns[len(turns)-1]
}

// TestPersistEvent_AccumulatesReasoningOntoAnswer is the amendment #91 happy path:
// reasoning deltas observed through the persist seam are joined in arrival order and
// land on the FINAL assistant answer turn with the first→last delta wall time.
func TestPersistEvent_AccumulatesReasoningOntoAnswer(t *testing.T) {
	r, conv := newReasoningTestRunner(t, 65536, true)
	convID := newConvID(t)
	tr := &turnTracker{convID: convID}
	ctx := context.Background()

	t0 := time.Now().UTC()
	for _, ev := range []*agent.Event{
		reasoningEvent(t0, "the user greets; "),
		reasoningEvent(t0.Add(700*time.Millisecond), "reply warmly"),
		finalAnswerEvent("hello!"),
	} {
		if err := r.persistEvent(ctx, tr, ev); err != nil {
			t.Fatalf("persistEvent: %v", err)
		}
	}

	got := lastTurn(t, conv, convID)
	if got.Role != llm.RoleAssistant || got.Content != "hello!" {
		t.Fatalf("last turn = %s/%q, want the assistant answer", got.Role, got.Content)
	}
	if got.Reasoning != "the user greets; reply warmly" {
		t.Errorf("persisted reasoning = %q, want the joined deltas", got.Reasoning)
	}
	if got.ReasoningDurationMS != 700 {
		t.Errorf("persisted duration = %dms, want 700 (first→last delta wall time)", got.ReasoningDurationMS)
	}
}

// TestPersistEvent_RedactedStreamPersistsNothing pins the HARDEN-05 posture the
// amendment preserves: when ShowReasoning is off the agent redacts every delta at
// the source, and the runner's config-seam gate (the SAME llm.Config the agent
// reads) skips accumulation entirely — a hidden stream never becomes a stored
// verbatim trace, not even as the redaction constant.
func TestPersistEvent_RedactedStreamPersistsNothing(t *testing.T) {
	r, conv := newReasoningTestRunner(t, 65536, false)
	convID := newConvID(t)
	tr := &turnTracker{convID: convID}
	ctx := context.Background()

	for _, ev := range []*agent.Event{
		reasoningEvent(time.Now().UTC(), "[reasoning redacted]"),
		finalAnswerEvent("hi"),
	} {
		if err := r.persistEvent(ctx, tr, ev); err != nil {
			t.Fatalf("persistEvent: %v", err)
		}
	}
	got := lastTurn(t, conv, convID)
	if got.Reasoning != "" || got.ReasoningDurationMS != 0 {
		t.Fatalf("redacted stream persisted reasoning %q/%dms, want nothing", got.Reasoning, got.ReasoningDurationMS)
	}
}

// TestPersistEvent_KnobDisabledPersistsNothing: <=0 is the documented off switch
// (also the hand-built-Deps zero value, so legacy runner tests stay byte-identical).
func TestPersistEvent_KnobDisabledPersistsNothing(t *testing.T) {
	r, conv := newReasoningTestRunner(t, 0, true)
	convID := newConvID(t)
	tr := &turnTracker{convID: convID}
	ctx := context.Background()

	for _, ev := range []*agent.Event{
		reasoningEvent(time.Now().UTC(), "genuine CoT"),
		finalAnswerEvent("hi"),
	} {
		if err := r.persistEvent(ctx, tr, ev); err != nil {
			t.Fatalf("persistEvent: %v", err)
		}
	}
	got := lastTurn(t, conv, convID)
	if got.Reasoning != "" || got.ReasoningDurationMS != 0 {
		t.Fatalf("disabled knob persisted reasoning %q/%dms, want nothing", got.Reasoning, got.ReasoningDurationMS)
	}
}

// TestObserveReasoning_OverCapKeepsHeadPlusMarker: the rune cap keeps the HEAD and
// appends the truncation marker ONCE; later deltas stop growing the text but keep
// stamping timestamps so the persisted duration covers the whole reasoning phase.
// The cap counts RUNES (a multibyte delta must never be split mid-rune).
func TestObserveReasoning_OverCapKeepsHeadPlusMarker(t *testing.T) {
	r, conv := newReasoningTestRunner(t, 6, true)
	convID := newConvID(t)
	tr := &turnTracker{convID: convID}
	ctx := context.Background()

	t0 := time.Now().UTC()
	for _, ev := range []*agent.Event{
		reasoningEvent(t0, "héll"),                               // 4 runes (5 bytes)
		reasoningEvent(t0.Add(100*time.Millisecond), "oworld"),   // over cap: keep "ow"
		reasoningEvent(t0.Add(2500*time.Millisecond), "ignored"), // truncated: text frozen, time stamped
		finalAnswerEvent("done"),
	} {
		if err := r.persistEvent(ctx, tr, ev); err != nil {
			t.Fatalf("persistEvent: %v", err)
		}
	}

	got := lastTurn(t, conv, convID)
	want := "héllow" + reasoningTruncationMarker
	if got.Reasoning != want {
		t.Errorf("truncated reasoning = %q, want %q (head + marker)", got.Reasoning, want)
	}
	if strings.Count(got.Reasoning, reasoningTruncationMarker) != 1 {
		t.Errorf("truncation marker must appear exactly once: %q", got.Reasoning)
	}
	if got.ReasoningDurationMS != 2500 {
		t.Errorf("duration = %dms, want 2500 (spans post-truncation deltas too)", got.ReasoningDurationMS)
	}
}

// TestPersistEvent_DiscardStreamedResetsReasoning: the B-12 mid-stream-retry
// repudiation drops the already-accumulated CoT so the persisted trace mirrors what
// the consumer rendered after the blank slate — never failed-attempt deltas joined
// with their replay.
func TestPersistEvent_DiscardStreamedResetsReasoning(t *testing.T) {
	r, conv := newReasoningTestRunner(t, 65536, true)
	convID := newConvID(t)
	tr := &turnTracker{convID: convID}
	ctx := context.Background()

	t0 := time.Now().UTC()
	discard := &agent.Event{Timestamp: t0.Add(time.Second)}
	discard.Actions.DiscardStreamed = true
	for _, ev := range []*agent.Event{
		reasoningEvent(t0, "failed-attempt partial"),
		discard,
		reasoningEvent(t0.Add(2*time.Second), "fresh retry CoT"),
		finalAnswerEvent("answer"),
	} {
		if err := r.persistEvent(ctx, tr, ev); err != nil {
			t.Fatalf("persistEvent: %v", err)
		}
	}

	got := lastTurn(t, conv, convID)
	if got.Reasoning != "fresh retry CoT" {
		t.Errorf("post-discard reasoning = %q, want only the retry's deltas", got.Reasoning)
	}
	if got.ReasoningDurationMS != 0 {
		t.Errorf("duration = %dms, want 0 (single post-discard delta)", got.ReasoningDurationMS)
	}
}
