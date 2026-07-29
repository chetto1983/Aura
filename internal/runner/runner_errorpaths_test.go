package runner

import (
	"context"
	"testing"
	"time"

	"github.com/chetto1983/aura/internal/agent"
	"github.com/chetto1983/aura/internal/agent/agenttest"
	"github.com/chetto1983/aura/internal/askuser"
)

// errPauseStore wraps fakePauseStore to inject errors on the read/resolve paths the
// shared fake does not expose, so the runner's resume/cancel/Stop error branches are
// reachable without touching the shared fakes_test.go.
type errPauseStore struct {
	*fakePauseStore
	listErr        error
	autoResolveErr error
	batchErr       error
}

func (e *errPauseStore) ListPending(ctx context.Context, convID string) ([]askuser.Pending, error) {
	if e.listErr != nil {
		return nil, e.listErr
	}
	return e.fakePauseStore.ListPending(ctx, convID)
}

func (e *errPauseStore) AutoResolveForConversation(ctx context.Context, convID string) error {
	if e.autoResolveErr != nil {
		return e.autoResolveErr
	}
	return e.fakePauseStore.AutoResolveForConversation(ctx, convID)
}

func (e *errPauseStore) MarkResumedBatch(ctx context.Context, answers map[string]askuser.ResumeAnswer) error {
	if e.batchErr != nil {
		return e.batchErr
	}
	return e.fakePauseStore.MarkResumedBatch(ctx, answers)
}

// seedPending inserts a pending row directly through the store so the resolve paths
// have something to read.
func seedPending(t *testing.T, p PauseStore, token, convID string) {
	t.Helper()
	if err := p.Insert(context.Background(), askuser.InsertParams{
		Token: token, ConversationID: convID, Kind: "clarification",
		Question: "q?", ToolCallID: "call-" + token,
	}); err != nil {
		t.Fatalf("seed pending: %v", err)
	}
}

func TestPendingFor_ListError(t *testing.T) {
	r, _, _ := newTestRunner(t, agenttest.NewFakeClient())
	r.pause = &errPauseStore{fakePauseStore: newFakePauseStore(), listErr: errFake}
	if _, err := r.PendingFor(context.Background(), newConvID(t)); err == nil {
		t.Fatal("expected PendingFor to surface the ListPending error")
	}
}

func TestSubmitAnswers_EmptyMapIsNoOp(t *testing.T) {
	r, _, _ := newTestRunner(t, agenttest.NewFakeClient())
	n, err := r.SubmitAnswers(context.Background(), nil)
	if err != nil || n != 0 {
		t.Fatalf("SubmitAnswers(nil) = (%d, %v); want (0, nil)", n, err)
	}
}

func TestSubmitAnswers_MarkResumedBatchError(t *testing.T) {
	convID := newConvID(t)
	ep := &errPauseStore{fakePauseStore: newFakePauseStore(), batchErr: errFake}
	seedPending(t, ep, "tok-1", convID)
	r, _, _ := newTestRunner(t, agenttest.NewFakeClient())
	r.pause = ep
	_, err := r.SubmitAnswers(context.Background(), map[string]ResponseInput{
		"tok-1": {Action: askuser.ActionAccept, Content: "a"},
	})
	if err == nil {
		t.Fatal("expected the MarkResumedBatch error to surface")
	}
}

// TestSubmitAnswer_AutoResolveErrorOnCancel exercises cancelConversation's
// AutoResolveForConversation error branch.
func TestSubmitAnswer_AutoResolveErrorOnCancel(t *testing.T) {
	convID := newConvID(t)
	ep := &errPauseStore{fakePauseStore: newFakePauseStore(), autoResolveErr: errFake}
	seedPending(t, ep, "tok-1", convID)
	r, _, _ := newTestRunner(t, agenttest.NewFakeClient())
	r.pause = ep
	if _, err := r.SubmitAnswer(context.Background(), "tok-1", ResponseInput{Action: askuser.ActionCancel}); err == nil {
		t.Fatal("expected the auto-resolve error to surface on cancel")
	}
}

// TestStop_ListPendingErrorSurfaces exercises Stop's resolveErr (injectCancelledAnswers
// → ListPending) branch with a clean worker drain.
func TestStop_ListPendingErrorSurfaces(t *testing.T) {
	r, _, _ := newTestRunner(t, agenttest.NewFakeClient())
	r.pause = &errPauseStore{fakePauseStore: newFakePauseStore(), listErr: errFake}
	if err := r.Stop(context.Background(), newConvID(t)); err == nil {
		t.Fatal("expected Stop to surface the auto-resolve (ListPending) error")
	}
}

// TestStop_ResolveErrorAndWorkerTimeout exercises the combined branch: an auto-resolve
// error AND a worker that does not drain within StopTimeout (the two-fault message).
func TestStop_ResolveErrorAndWorkerTimeout(t *testing.T) {
	r, _, _ := newTestRunner(t, agenttest.NewFakeClient())
	r.pause = &errPauseStore{fakePauseStore: newFakePauseStore(), listErr: errFake}
	r.stopTimeout = 10 * time.Millisecond
	release := make(chan struct{})
	r.wg.Go(func() {
		<-release
	})
	err := r.Stop(context.Background(), newConvID(t))
	close(release) // let the worker finish so goleak is clean
	if err == nil {
		t.Fatal("expected a combined auto-resolve + drain-timeout error")
	}
}

// TestEvictSessionToolState_NilRegistry covers the nil-registry guard.
func TestEvictSessionToolState_NilRegistry(t *testing.T) {
	r, _, _ := newTestRunner(t, agenttest.NewFakeClient())
	r.registry = nil
	r.evictSessionToolState(newConvID(t)) // must not panic
}

// --- persist error branches ---

func TestPersistEvent_NilEventIsNoOp(t *testing.T) {
	r, _, _ := newTestRunner(t, agenttest.NewFakeClient())
	if err := r.persistEvent(context.Background(), &turnTracker{convID: newConvID(t)}, nil); err != nil {
		t.Fatalf("nil event must be a no-op, got %v", err)
	}
}

func TestPersistToolTurn_NilOrEmptyCallID(t *testing.T) {
	r, _, _ := newTestRunner(t, agenttest.NewFakeClient())
	tr := &turnTracker{convID: newConvID(t)}
	if err := r.persistToolTurn(context.Background(), tr, nil); err != nil {
		t.Fatalf("nil ToolInvocation must be a no-op, got %v", err)
	}
	if err := r.persistToolTurn(context.Background(), tr, &agent.ToolInvocation{Event: agent.ToolInvocationEnd}); err != nil {
		t.Fatalf("empty ToolCallID must be a no-op, got %v", err)
	}
}

// TestPersistToolTurn_UnknownEventIsNoOp covers the default switch arm (an event that
// is neither Start nor End).
func TestPersistToolTurn_UnknownEventIsNoOp(t *testing.T) {
	r, _, _ := newTestRunner(t, agenttest.NewFakeClient())
	tr := &turnTracker{convID: newConvID(t)}
	ti := &agent.ToolInvocation{Event: "progress", ToolCallID: "c1", ToolName: "x"}
	if err := r.persistToolTurn(context.Background(), tr, ti); err != nil {
		t.Fatalf("an unknown tool-invocation event must be a no-op, got %v", err)
	}
}

// TestFlushToolCalls_AppendError surfaces an AppendTurn failure while persisting the
// assistant tool_calls turn.
func TestFlushToolCalls_AppendError(t *testing.T) {
	r, conv, _ := newTestRunner(t, agenttest.NewFakeClient())
	conv.appendEr = errFake
	tr := &turnTracker{convID: newConvID(t)}
	tr.addPendingToolCall(&agent.ToolInvocation{ToolCallID: "c1", ToolName: "shell_exec"})
	if err := r.flushToolCalls(context.Background(), tr); err == nil {
		t.Fatal("expected the AppendTurn error to surface from flushToolCalls")
	}
}

// TestFlushToolCalls_EmptyIsNoOp covers the no-pending-calls early return.
func TestFlushToolCalls_EmptyIsNoOp(t *testing.T) {
	r, _, _ := newTestRunner(t, agenttest.NewFakeClient())
	if err := r.flushToolCalls(context.Background(), &turnTracker{convID: newConvID(t)}); err != nil {
		t.Fatalf("empty flush must be a no-op, got %v", err)
	}
}

// TestPersistAssistantAnswer_AppendError surfaces the AppendAssistantTurnWithCacheMetric
// failure (line 269-271).
func TestPersistAssistantAnswer_AppendError(t *testing.T) {
	r, conv, _ := newTestRunner(t, agenttest.NewFakeClient())
	conv.appendEr = errFake
	ev := &agent.Event{LLMResponse: &agent.LLMResponse{Content: "hi", FinishReason: "stop"}}
	if err := r.persistAssistantAnswer(context.Background(), &turnTracker{convID: newConvID(t)}, ev); err == nil {
		t.Fatal("expected the assistant-turn append error to surface")
	}
}

// TestFlushPause_AppendError surfaces the AppendTurn failure while writing the combined
// ask_user assistant turn through the committer (D-05). tr carries a matching pauseInsert
// so the committer reaches the assistant-turn append (persistPause populates both in
// lock-step).
func TestFlushPause_AppendError(t *testing.T) {
	r, conv, _ := newTestRunner(t, agenttest.NewFakeClient())
	conv.appendEr = errFake
	convID := newConvID(t)
	tr := &turnTracker{convID: convID, pauses: []*agent.AwaitingInput{
		{ToolCallID: "c1", Question: "q?", Kind: "clarification"},
	}, pauseInserts: []askuser.InsertParams{
		{Token: newConvID(t), ConversationID: convID, Kind: "clarification", Question: "q?", ToolCallID: "c1"},
	}}
	if err := r.flushPause(context.Background(), tr); err == nil {
		t.Fatal("expected the flushPause AppendTurn error to surface")
	}
}

// TestFlushPause_EmptyIsNoOp covers the no-pauses early return.
func TestFlushPause_EmptyIsNoOp(t *testing.T) {
	r, _, _ := newTestRunner(t, agenttest.NewFakeClient())
	if err := r.flushPause(context.Background(), &turnTracker{convID: newConvID(t)}); err != nil {
		t.Fatalf("empty flushPause must be a no-op, got %v", err)
	}
}

// TestAssistantAskUserToolCalls_RichArgs covers the options/priority/resume_context
// legs of the args marshaller.
func TestAssistantAskUserToolCalls_RichArgs(t *testing.T) {
	raw, err := assistantAskUserToolCalls([]*agent.AwaitingInput{
		{
			ToolCallID:    "c1",
			Question:      "Pick one",
			Kind:          "choice",
			Priority:      3,
			Options:       []agent.PauseOption{{Label: "A", Value: "a"}, {Label: "B", Value: "b"}},
			ResumeContext: []byte(`{"ctx":1}`),
		},
	})
	if err != nil {
		t.Fatalf("assistantAskUserToolCalls: %v", err)
	}
	for _, want := range []string{"options", "priority", "resume_context", "ask_user", "Pick one"} {
		if !containsFold(string(raw), want) {
			t.Fatalf("tool_calls JSON missing %q: %s", want, raw)
		}
	}
}

// TestAnyFloat covers each numeric case + the default (unconvertible) leg.
func TestAnyFloat(t *testing.T) {
	cases := []struct {
		in   any
		want float64
		ok   bool
	}{
		{float64(1.5), 1.5, true},
		{int(2), 2, true},
		{int64(3), 3, true},
		{"not a number", 0, false},
		{nil, 0, false},
	}
	for _, c := range cases {
		got, ok := anyFloat(c.in)
		if ok != c.ok || (ok && got != c.want) {
			t.Errorf("anyFloat(%v) = (%v, %v); want (%v, %v)", c.in, got, ok, c.want, c.ok)
		}
	}
}

// TestToolInvocationTimestamp covers the start/end/fallback arms.
func TestToolInvocationTimestamp(t *testing.T) {
	start := time.Now().UTC()
	end := start.Add(time.Second)
	fallback := start.Add(2 * time.Second)

	if got := toolInvocationTimestamp(&agent.ToolInvocation{Event: agent.ToolInvocationStart, StartedAt: &start}, fallback); !got.Equal(start) {
		t.Errorf("start arm = %v; want %v", got, start)
	}
	if got := toolInvocationTimestamp(&agent.ToolInvocation{Event: agent.ToolInvocationEnd, EndedAt: &end}, fallback); !got.Equal(end) {
		t.Errorf("end arm = %v; want %v", got, end)
	}
	// A start event without StartedAt falls through to the fallback.
	if got := toolInvocationTimestamp(&agent.ToolInvocation{Event: agent.ToolInvocationStart}, fallback); !got.Equal(fallback) {
		t.Errorf("fallback arm = %v; want %v", got, fallback)
	}
}

// --- applyResumeHook + remainingPending error legs ---

// errHookPauseStore lets a GetByToken return a pending carrying ResumeContext so the
// applyResumeHook leg (which gates on len(ResumeContext) > 0) is reachable, and
// otherwise delegates to the shared fake.
func seedPendingWithContext(t *testing.T, p PauseStore, token, convID string) {
	t.Helper()
	if err := p.Insert(context.Background(), askuser.InsertParams{
		Token: token, ConversationID: convID, Kind: "approval",
		Question: "approve?", ToolCallID: "tc-" + token,
		ResumeContext: []byte(`{"type":"skill_approval"}`),
	}); err != nil {
		t.Fatalf("seed pending: %v", err)
	}
}

func TestSubmitAnswer_ResumeHookError(t *testing.T) {
	convID := newConvID(t)
	r, conv, pause := newTestRunner(t, agenttest.NewFakeClient())
	mustCreate(t, r, convID)
	_ = conv
	seedPendingWithContext(t, pause, "tok-1", convID)
	r.resumeHook = func(context.Context, askuser.Pending, ResponseInput) error { return errFake }
	if _, err := r.SubmitAnswer(context.Background(), "tok-1", ResponseInput{Action: askuser.ActionAccept, Content: "yes"}); err == nil {
		t.Fatal("expected the resume-hook error to surface from SubmitAnswer")
	}
}

func TestSubmitAnswers_ResumeHookError(t *testing.T) {
	convID := newConvID(t)
	r, conv, pause := newTestRunner(t, agenttest.NewFakeClient())
	mustCreate(t, r, convID)
	_ = conv
	seedPendingWithContext(t, pause, "tok-1", convID)
	r.resumeHook = func(context.Context, askuser.Pending, ResponseInput) error { return errFake }
	if _, err := r.SubmitAnswers(context.Background(), map[string]ResponseInput{
		"tok-1": {Action: askuser.ActionAccept, Content: "yes"},
	}); err == nil {
		t.Fatal("expected the resume-hook error to surface from SubmitAnswers")
	}
}

func TestRemainingPending_ListError(t *testing.T) {
	convID := newConvID(t)
	ep := &errPauseStore{fakePauseStore: newFakePauseStore()}
	seedPending(t, ep, "tok-1", convID)
	r, _, _ := newTestRunner(t, agenttest.NewFakeClient())
	r.pause = ep
	// First resolve succeeds; then flip ListPending to error so remainingPending fails.
	ep.listErr = errFake
	if _, err := r.SubmitAnswer(context.Background(), "tok-1", ResponseInput{Action: askuser.ActionAccept, Content: "a"}); err == nil {
		t.Fatal("expected remainingPending's ListPending error to surface")
	}
}

// --- pause Insert error now surfaces at flushPause (D-05) ---

// TestFlushPause_PauseInsertError pins the moved-insert contract: persistPause is a pure
// accumulator (no DB I/O), so the paused_states Insert failure surfaces at flushPause's
// CommitPause — a pause is never exposed without its durable assistant history.
func TestFlushPause_PauseInsertError(t *testing.T) {
	r, _, pause := newTestRunner(t, agenttest.NewFakeClient())
	pause.insertEr = errFake
	tr := &turnTracker{convID: newConvID(t)}
	ai := &agent.AwaitingInput{Question: "q?", Kind: "clarification", ToolCallID: "c1"}
	if err := r.persistPause(context.Background(), tr, ai); err != nil {
		t.Fatalf("persistPause must not do I/O now (D-05 accumulate-only), got %v", err)
	}
	if err := r.flushPause(context.Background(), tr); err == nil {
		t.Fatal("expected the paused_states Insert error to surface at flushPause")
	}
}

// --- persistToolTurn End-path errors ---

// TestPersistToolTurn_EndAppendError surfaces the RoleTool-result AppendTurn failure on
// a ToolInvocationEnd (the assistant tool_calls flush succeeds, the result append fails).
func TestPersistToolTurn_EndAppendError(t *testing.T) {
	r, conv, _ := newTestRunner(t, agenttest.NewFakeClient())
	// Fail only the 2nd append: the 1st is the assistant tool_calls flush, the 2nd is
	// the RoleTool result the End path writes.
	conv.failAppN = 2
	tr := &turnTracker{convID: newConvID(t)}
	start := time.Now().UTC()
	end := start.Add(time.Millisecond)
	ti := &agent.ToolInvocation{
		Event: agent.ToolInvocationEnd, ToolCallID: "c1", ToolName: "shell_exec",
		StartedAt: &start, EndedAt: &end, ResultPreview: "out",
	}
	if err := r.persistToolTurn(context.Background(), tr, ti); err == nil {
		t.Fatal("expected the RoleTool-result AppendTurn error to surface")
	}
}

// TestPersistToolTurn_StartBatchFlushError surfaces the flush error on the last
// batch-index Start event (BatchIndex == BatchSize-1 triggers an early flush).
func TestPersistToolTurn_StartBatchFlushError(t *testing.T) {
	r, conv, _ := newTestRunner(t, agenttest.NewFakeClient())
	conv.appendEr = errFake
	tr := &turnTracker{convID: newConvID(t)}
	ti := &agent.ToolInvocation{
		Event: agent.ToolInvocationStart, ToolCallID: "c1", ToolName: "shell_exec",
		BatchSize: 1, BatchIndex: 0, // last index of a 1-call batch → flush fires
	}
	if err := r.persistToolTurn(context.Background(), tr, ti); err == nil {
		t.Fatal("expected the batch-last-index flush error to surface")
	}
}

// --- persistAssistantAnswer cacheMetricParams error (nil store) routed through the
// persist seam, not the direct unit ---

func TestPersistAssistantAnswer_NilCacheMetricStore(t *testing.T) {
	r, _, _ := newTestRunner(t, agenttest.NewFakeClient())
	r.cacheMetrics = nil
	ev := &agent.Event{LLMResponse: &agent.LLMResponse{Content: "hi", FinishReason: "stop"}}
	if err := r.persistAssistantAnswer(context.Background(), &turnTracker{convID: newConvID(t)}, ev); err == nil {
		t.Fatal("a nil cacheMetrics store must fail loud through persistAssistantAnswer")
	}
}

// --- fast-path persistEvent error + consumer stop-on-chunk + deferred-flush error ---

// TestTurn_FastGreetingPersistError surfaces a persistEvent failure on the fast-reply
// path (the fast assistant turn's AppendAssistantTurnWithCacheMetric fails).
func TestTurn_FastGreetingPersistError(t *testing.T) {
	r, conv, _ := newTestRunner(t, agenttest.NewFakeClient())
	convID := newConvID(t)
	mustCreate(t, r, convID)
	// The fast path appends the user turn (1st append) then persists the fast assistant
	// turn via the cache-metric seam; fail that 2nd append.
	conv.failAppN = 2
	if _, err := drain(r.Turn(context.Background(), convID, new("ciao"))); err == nil {
		t.Fatal("expected the fast-reply persistEvent error to surface")
	}
}

// TestTurn_DeferredFlushOnConsumerStop exercises the deferred flushOnce path: a consumer
// that stops iterating ON the pause Event forces the early-return, so the combined
// ask_user assistant turn is written by the deferred flush, not the post-loop call.
func TestTurn_DeferredFlushOnConsumerStop(t *testing.T) {
	client := agenttest.NewFakeClient(
		agenttest.ToolCallTurn(askUserCall("call-1", "Proceed?", "approval")),
	)
	r, conv, pause := newTestRunner(t, client)
	convID := newConvID(t)
	mustCreate(t, r, convID)

	// Stop on the first pause Event (the AG-UI translator behavior).
	for ev, err := range r.Turn(context.Background(), convID, new("hi")) {
		if err != nil {
			t.Fatalf("turn error: %v", err)
		}
		if ev != nil && ev.Actions.AwaitingInput != nil {
			break // consumer stops here → yield returns false → deferred flush must run
		}
	}
	if pause.inserts != 1 {
		t.Fatalf("want 1 paused_states insert, got %d", pause.inserts)
	}
	// The deferred flush must have written the combined assistant ask_user tool_call turn.
	hist, err := conv.LoadHistory(context.Background(), convID)
	if err != nil {
		t.Fatalf("history: %v", err)
	}
	sawAskUser := false
	for _, m := range hist {
		for _, tc := range m.ToolCalls {
			if tc.Function.Name == "ask_user" {
				sawAskUser = true
			}
		}
	}
	if !sawAskUser {
		t.Fatal("deferred flush did not persist the combined ask_user assistant turn")
	}
}
