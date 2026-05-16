package chat

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	tools "github.com/aura/aura/internal/agent/tools/registry"
	"github.com/aura/aura/internal/db/migrations"
	"github.com/aura/aura/internal/identity"
	runstore "github.com/aura/aura/internal/storage/runs"
	"github.com/aura/aura/internal/testutil"
)

// --- Mocks ------------------------------------------------------------------

type recordingLoop struct {
	emits       []OutboundEvent
	err         error
	mu          sync.Mutex
	calls       int
	finalStatus RunStatus
}

type registryAuthorizationLoop struct {
	reg  *tools.Registry
	args map[string]any
}

func (r *registryAuthorizationLoop) Run(ctx context.Context, _ *Run, _ InboundMessage, _ EmitFn) error {
	_, err := r.reg.Execute(ctx, "fake", r.args)
	return err
}

func (r *recordingLoop) Run(_ context.Context, run *Run, _ InboundMessage, emit EmitFn) error {
	r.mu.Lock()
	r.calls++
	emits := append([]OutboundEvent(nil), r.emits...)
	err := r.err
	finalStatus := r.finalStatus
	r.mu.Unlock()

	if err != nil {
		return err
	}
	for _, ev := range emits {
		_ = emit(ev)
	}
	if finalStatus != "" {
		run.Status = finalStatus
	}
	return nil
}

type fakeInbound struct {
	channel Channel
	out     InboundMessage
	err     error
}

func (f *fakeInbound) Channel() Channel { return f.channel }
func (f *fakeInbound) Normalize(_ context.Context, _ any) (InboundMessage, error) {
	return f.out, f.err
}

type fakeOutbound struct {
	channel Channel
	mode    DeliveryMode
	mu      sync.Mutex
	got     []OutboundEvent
	err     error
}

type chatRegistryTool struct {
	executed *atomic.Bool
}

func (t chatRegistryTool) Name() string        { return "fake" }
func (t chatRegistryTool) Description() string { return "fake" }
func (t chatRegistryTool) Parameters() map[string]any {
	return map[string]any{"type": "object"}
}
func (t chatRegistryTool) Execute(context.Context, map[string]any) (string, error) {
	t.executed.Store(true)
	return "ok", nil
}

func (f *fakeOutbound) Channel() Channel   { return f.channel }
func (f *fakeOutbound) Mode() DeliveryMode { return f.mode }
func (f *fakeOutbound) Deliver(_ context.Context, ev OutboundEvent) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.got = append(f.got, ev)
	return f.err
}

// --- Tests ------------------------------------------------------------------

func newHub(t *testing.T, loop AgentLoop) *Hub {
	t.Helper()
	h, err := New(Config{Loop: loop})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	return h
}

func TestNew_RejectsMissingLoop(t *testing.T) {
	if _, err := New(Config{}); err == nil {
		t.Fatal("expected error when AgentLoop nil")
	}
}

func TestReceive_HappyPath_FanoutsToMatchingOutbound(t *testing.T) {
	loop := &recordingLoop{
		emits: []OutboundEvent{
			{Type: EventMessageCreated},
			{Type: EventMessageDelta, Content: "hello"},
			{Type: EventMessageDone, Content: "hello"},
			{Type: EventUsage, Payload: map[string]any{"tokens": 12}},
		},
	}
	h := newHub(t, loop)

	in := &fakeInbound{channel: ChannelWeb, out: InboundMessage{
		ID: "msg-1", Channel: ChannelWeb, PrincipalID: "p1", Text: "hi",
		Mode: DeliveryModeDeferred, CreatedAt: time.Now(),
	}}
	out := &fakeOutbound{channel: ChannelWeb, mode: DeliveryModeDeferred}
	h.RegisterInbound(in)
	h.RegisterOutbound(out)

	run, err := h.Receive(context.Background(), ChannelWeb, nil)
	if err != nil {
		t.Fatalf("Receive: %v", err)
	}
	if run.Status != RunStatusCompleted {
		t.Fatalf("Status = %s, want completed", run.Status)
	}
	// Expected events: run_started + loop's 4 + done = 6
	if got := len(out.got); got != 6 {
		t.Fatalf("delivered %d events, want 6: %+v", got, out.got)
	}
	// First event is run_started (injected by Hub), last is done.
	if out.got[0].Type != EventRunStarted {
		t.Fatalf("first event = %s, want run_started", out.got[0].Type)
	}
	if out.got[len(out.got)-1].Type != EventDone {
		t.Fatalf("last event = %s, want done", out.got[len(out.got)-1].Type)
	}
	// Seq is monotonic + non-zero.
	for i, ev := range out.got {
		if ev.Seq == 0 {
			t.Fatalf("event[%d] has zero Seq", i)
		}
		if i > 0 && ev.Seq <= out.got[i-1].Seq {
			t.Fatalf("seq not monotonic at i=%d: %d <= %d", i, ev.Seq, out.got[i-1].Seq)
		}
	}
	// run_id, channel, created_at populated on every event.
	for i, ev := range out.got {
		if ev.RunID == "" {
			t.Fatalf("event[%d] missing RunID", i)
		}
		if ev.Channel != ChannelWeb {
			t.Fatalf("event[%d] channel=%s, want web", i, ev.Channel)
		}
		if ev.CreatedAt.IsZero() {
			t.Fatalf("event[%d] missing CreatedAt", i)
		}
	}
}

func TestReceive_MissingInboundAdapter(t *testing.T) {
	h := newHub(t, &recordingLoop{})
	_, err := h.Receive(context.Background(), ChannelTelegram, nil)
	if err == nil {
		t.Fatal("expected error for unregistered channel")
	}
}

func TestReceive_InboundNormalizeError(t *testing.T) {
	h := newHub(t, &recordingLoop{})
	h.RegisterInbound(&fakeInbound{channel: ChannelWeb, err: errors.New("bad payload")})
	_, err := h.Receive(context.Background(), ChannelWeb, nil)
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestReceive_LoopError_PropagatesAndEmitsErrorThenDone(t *testing.T) {
	loop := &recordingLoop{err: errors.New("boom")}
	h := newHub(t, loop)
	h.RegisterInbound(&fakeInbound{channel: ChannelWeb, out: InboundMessage{Channel: ChannelWeb, Mode: DeliveryModeDeferred}})
	out := &fakeOutbound{channel: ChannelWeb, mode: DeliveryModeDeferred}
	h.RegisterOutbound(out)

	run, err := h.Receive(context.Background(), ChannelWeb, nil)
	if err == nil || err.Error() != "boom" {
		t.Fatalf("expected boom, got %v", err)
	}
	if run.Status != RunStatusFailed {
		t.Fatalf("status = %s, want failed", run.Status)
	}
	if run.LastError != "boom" {
		t.Fatalf("LastError = %q", run.LastError)
	}
	// Expect: run_started + error + done.
	if len(out.got) != 3 {
		t.Fatalf("delivered %d events, want 3", len(out.got))
	}
	if out.got[0].Type != EventRunStarted {
		t.Fatalf("evt[0] = %s", out.got[0].Type)
	}
	if out.got[1].Type != EventError {
		t.Fatalf("evt[1] = %s", out.got[1].Type)
	}
	if out.got[2].Type != EventDone {
		t.Fatalf("evt[2] = %s", out.got[2].Type)
	}
}

func TestReceive_ContextCanceled_RunStatusCancelled(t *testing.T) {
	canceledLoop := &recordingLoop{err: context.Canceled}
	h := newHub(t, canceledLoop)
	h.RegisterInbound(&fakeInbound{channel: ChannelWeb, out: InboundMessage{Channel: ChannelWeb, Mode: DeliveryModeDeferred}})
	out := &fakeOutbound{channel: ChannelWeb, mode: DeliveryModeDeferred}
	h.RegisterOutbound(out)

	run, err := h.Receive(context.Background(), ChannelWeb, nil)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("expected context.Canceled, got %v", err)
	}
	if run.Status != RunStatusCancelled {
		t.Fatalf("status = %s, want cancelled", run.Status)
	}
}

func TestStop_BestEffortAndIdempotent(t *testing.T) {
	// Loop that blocks until ctx is done, so Stop can interrupt it.
	type blockingLoop struct{}
	bl := blockingLoopFn(func(ctx context.Context, run *Run, _ InboundMessage, _ EmitFn) error {
		<-ctx.Done()
		return ctx.Err()
	})
	h := newHub(t, bl)
	h.RegisterInbound(&fakeInbound{channel: ChannelWeb, out: InboundMessage{Channel: ChannelWeb, Mode: DeliveryModeDeferred}})
	h.RegisterOutbound(&fakeOutbound{channel: ChannelWeb, mode: DeliveryModeDeferred})

	// Capture the run_id by intercepting the run_started event from a
	// closure-based outbound.
	var runID atomic.Value
	out := outboundFn(ChannelWeb, DeliveryModeDeferred, func(ev OutboundEvent) error {
		if ev.Type == EventRunStarted {
			runID.Store(ev.RunID)
		}
		return nil
	})
	h.RegisterOutbound(out)

	done := make(chan struct{})
	go func() {
		_, _ = h.Receive(context.Background(), ChannelWeb, nil)
		close(done)
	}()

	// Wait for run_id to be populated.
	for i := 0; i < 200; i++ {
		if runID.Load() != nil {
			break
		}
		time.Sleep(5 * time.Millisecond)
	}
	id, _ := runID.Load().(string)
	if id == "" {
		t.Fatal("never observed run_started")
	}

	if !h.Stop(id) {
		t.Fatal("Stop(known id) returned false")
	}
	// Idempotent: stopping a finished run returns false (already cleaned up).
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("loop did not exit after Stop")
	}
	if h.Stop(id) {
		t.Fatal("second Stop should be false (run already cleaned up)")
	}
	if h.Stop("unknown") {
		t.Fatal("Stop(unknown) should be false")
	}
}

func TestReceiveMessage_BypassesInboundAdapter(t *testing.T) {
	loop := &recordingLoop{emits: []OutboundEvent{{Type: EventMessageDone}}}
	h := newHub(t, loop)
	out := &fakeOutbound{channel: ChannelCron, mode: DeliveryModeSilent}
	h.RegisterOutbound(out)
	// No inbound adapter registered for cron.

	msg := InboundMessage{Channel: ChannelCron, PrincipalID: "system", Mode: DeliveryModeSilent, Text: "tick"}
	_, err := h.ReceiveMessage(context.Background(), msg)
	if err != nil {
		t.Fatalf("ReceiveMessage: %v", err)
	}
	if len(out.got) < 2 {
		t.Fatalf("expected at least run_started + done events, got %d", len(out.got))
	}
}

func TestReceiveMessage_WithLifecycleStorePersistsRunStartedAndDone(t *testing.T) {
	db, store := newLifecycleStore(t)
	loop := &recordingLoop{}
	h := newPersistentHub(t, loop, store)

	msg := InboundMessage{
		ID:          "inbound-1",
		Channel:     ChannelWeb,
		PrincipalID: "principal-1",
		ThreadID:    "thread-1",
		Text:        "secret user prompt",
		Mode:        DeliveryModeDeferred,
		Locale:      "it-IT",
		TimeZone:    "Europe/Rome",
		ParentRunID: "parent-run-1",
		ChannelData: map[string]any{"chat_id": "do-not-store"},
	}
	run, err := h.ReceiveMessage(context.Background(), msg)
	if err != nil {
		t.Fatalf("ReceiveMessage: %v", err)
	}

	stored, err := store.GetRun(context.Background(), run.ID)
	if err != nil {
		t.Fatalf("GetRun: %v", err)
	}
	if stored.Status != string(RunStatusCompleted) {
		t.Fatalf("stored status = %s, want completed", stored.Status)
	}
	if stored.ParentRunID != msg.ParentRunID {
		t.Fatalf("stored parent_run_id = %q, want %q", stored.ParentRunID, msg.ParentRunID)
	}
	if stored.CurrentSeq != 2 {
		t.Fatalf("stored current_seq = %d, want 2", stored.CurrentSeq)
	}

	events, err := store.Events(context.Background(), run.ID)
	if err != nil {
		t.Fatalf("Events: %v", err)
	}
	if got, want := eventTypes(events), []string{string(EventRunStarted), string(EventDone)}; !sameStrings(got, want) {
		t.Fatalf("event types = %v, want %v", got, want)
	}
	assertNoStoredText(t, db, "secret user prompt")
	assertNoStoredText(t, db, "do-not-store")
}

func TestReceiveMessage_WithLifecycleStoreDedupesInboundMessage(t *testing.T) {
	_, store := newLifecycleStore(t)
	loop := &recordingLoop{}
	h := newPersistentHub(t, loop, store)
	msg := InboundMessage{
		ID:          "same-inbound-id",
		Channel:     ChannelWeb,
		PrincipalID: "principal-1",
		ThreadID:    "thread-1",
		Text:        "dedupe text",
		Mode:        DeliveryModeDeferred,
	}

	first, err := h.ReceiveMessage(context.Background(), msg)
	if err != nil {
		t.Fatalf("first ReceiveMessage: %v", err)
	}
	second, err := h.ReceiveMessage(context.Background(), msg)
	if err != nil {
		t.Fatalf("second ReceiveMessage: %v", err)
	}
	if second.ID != first.ID {
		t.Fatalf("second run ID = %s, want %s", second.ID, first.ID)
	}
	loop.mu.Lock()
	calls := loop.calls
	loop.mu.Unlock()
	if calls != 1 {
		t.Fatalf("loop calls = %d, want 1", calls)
	}

	events, err := store.Events(context.Background(), first.ID)
	if err != nil {
		t.Fatalf("Events: %v", err)
	}
	if len(events) != 2 {
		t.Fatalf("events = %d, want 2", len(events))
	}
}

func TestReceiveMessage_WithLifecycleStorePersistsActorOnRunAndEvents(t *testing.T) {
	db, store := newLifecycleStore(t)
	h := newPersistentHub(t, &recordingLoop{}, store)
	ctx := identity.WithActorID(context.Background(), "actor:web:session:1")
	msg := InboundMessage{
		ID:          "actor-inbound-id",
		Channel:     ChannelWeb,
		PrincipalID: "principal-1",
		ThreadID:    "thread-1",
		Text:        "hello",
		Mode:        DeliveryModeDeferred,
	}

	run, err := h.ReceiveMessage(ctx, msg)
	if err != nil {
		t.Fatalf("ReceiveMessage: %v", err)
	}
	if run.ActorID != "actor:web:session:1" {
		t.Fatalf("run ActorID = %q", run.ActorID)
	}
	stored, err := store.GetRun(context.Background(), run.ID)
	if err != nil {
		t.Fatalf("GetRun: %v", err)
	}
	if stored.ActorID != "actor:web:session:1" {
		t.Fatalf("stored ActorID = %q", stored.ActorID)
	}
	assertChatScalar(t, db, `
SELECT COUNT(*)
FROM run_events
WHERE run_id = ? AND actor_id = ?
`, 2, run.ID, "actor:web:session:1")
}

func TestReceiveMessage_LoopErrorPersistsRedactedErrorLifecycle(t *testing.T) {
	db, store := newLifecycleStore(t)
	rawErr := "boom secret raw error"
	h := newPersistentHub(t, &recordingLoop{err: errors.New(rawErr)}, store)
	msg := InboundMessage{
		ID:          "error-inbound-id",
		Channel:     ChannelWeb,
		PrincipalID: "principal-1",
		ThreadID:    "thread-1",
		Text:        "secret failing prompt",
		Mode:        DeliveryModeDeferred,
	}

	run, err := h.ReceiveMessage(context.Background(), msg)
	if err == nil || err.Error() != rawErr {
		t.Fatalf("expected raw loop error for caller, got %v", err)
	}
	if run.Status != RunStatusFailed {
		t.Fatalf("run status = %s, want failed", run.Status)
	}

	stored, err := store.GetRun(context.Background(), run.ID)
	if err != nil {
		t.Fatalf("GetRun: %v", err)
	}
	if stored.Status != string(RunStatusFailed) {
		t.Fatalf("stored status = %s, want failed", stored.Status)
	}
	if stored.LastError != "agent_loop_error" {
		t.Fatalf("stored last_error = %q, want redacted class", stored.LastError)
	}

	events, err := store.Events(context.Background(), run.ID)
	if err != nil {
		t.Fatalf("Events: %v", err)
	}
	if got, want := eventTypes(events), []string{string(EventRunStarted), string(EventError), string(EventDone)}; !sameStrings(got, want) {
		t.Fatalf("event types = %v, want %v", got, want)
	}
	assertNoStoredText(t, db, rawErr)
	assertNoStoredText(t, db, "secret failing prompt")
}

func TestReceiveMessage_WithLifecycleStorePersistsRedactedToolUsageAndFinalEvents(t *testing.T) {
	db, store := newLifecycleStore(t)
	loop := &recordingLoop{emits: []OutboundEvent{
		{
			Type: EventToolStart,
			Payload: map[string]any{
				"tool":           "search_memory",
				"tool_call_id":   "call-1",
				"arg_keys":       []string{"query"},
				"raw_arg_values": "secret tool arg value",
			},
		},
		{
			Type: EventToolEnd,
			Payload: map[string]any{
				"tool":         "search_memory",
				"tool_call_id": "call-1",
				"success":      false,
				"elapsed_ms":   int64(12),
				"preview":      "secret tool result preview",
			},
		},
		{
			Type: EventUsage,
			Payload: map[string]any{
				"llm_calls":         2,
				"tool_calls":        1,
				"loop_steps":        2,
				"tokens_prompt":     10,
				"tokens_completion": 5,
				"tokens_total":      15,
				"cost_usd":          0.01,
				"terminal_tool":     "",
				"raw_usage_note":    "secret usage note",
			},
		},
		{
			Type:    EventMessageDone,
			Content: "final answer for the user",
			Payload: map[string]any{
				"delivered":    true,
				"raw_delivery": "secret delivery value",
			},
		},
	}}
	h := newPersistentHub(t, loop, store)
	msg := InboundMessage{
		ID:          "tool-inbound-id",
		Channel:     ChannelWeb,
		PrincipalID: "principal-1",
		ThreadID:    "thread-1",
		Text:        "secret tool prompt",
		Mode:        DeliveryModeDeferred,
	}

	run, err := h.ReceiveMessage(context.Background(), msg)
	if err != nil {
		t.Fatalf("ReceiveMessage: %v", err)
	}
	events, err := store.Events(context.Background(), run.ID)
	if err != nil {
		t.Fatalf("Events: %v", err)
	}
	if got, want := eventTypes(events), []string{
		string(EventRunStarted),
		string(EventToolStart),
		string(EventToolEnd),
		string(EventUsage),
		string(EventMessageDone),
		string(EventDone),
	}; !sameStrings(got, want) {
		t.Fatalf("event types = %v, want %v", got, want)
	}

	stored, err := store.GetRun(context.Background(), run.ID)
	if err != nil {
		t.Fatalf("GetRun: %v", err)
	}
	if stored.FinalTextPreview != "final answer for the user" {
		t.Fatalf("final_text_preview = %q", stored.FinalTextPreview)
	}
	if !strings.Contains(stored.StatsJSON, `"tokens_total":15`) {
		t.Fatalf("stats_json missing tokens_total: %s", stored.StatsJSON)
	}
	assertNoStoredText(t, db, "secret tool prompt")
	assertNoStoredText(t, db, "secret tool arg value")
	assertNoStoredText(t, db, "secret tool result preview")
	assertNoStoredText(t, db, "secret usage note")
	assertNoStoredText(t, db, "secret delivery value")
}

func TestReceiveMessage_WithLifecycleStorePersistsQuestionState(t *testing.T) {
	db, store := newLifecycleStore(t)
	loop := &recordingLoop{
		emits: []OutboundEvent{{
			Type: EventQuestionRequested,
			Payload: map[string]any{
				"question":     "Approve durable memory write?",
				"kind":         "approval",
				"options":      []string{"approve_once", "deny", "cancel"},
				"tool":         "ask_user",
				"tool_call_id": "ask-1",
			},
		}},
		finalStatus: RunStatusWaitingForUser,
	}
	h := newPersistentHub(t, loop, store)
	ctx := identity.WithActorID(context.Background(), "actor:web:session:1")
	run, err := h.ReceiveMessage(ctx, InboundMessage{
		ID:          "question-inbound-id",
		Channel:     ChannelWeb,
		PrincipalID: "principal-1",
		ThreadID:    "thread-question",
		Text:        "secret prompt before question",
		Mode:        DeliveryModeDeferred,
	})
	if err != nil {
		t.Fatalf("ReceiveMessage: %v", err)
	}
	if run.Status != RunStatusWaitingForUser {
		t.Fatalf("run status = %s, want waiting_for_user", run.Status)
	}
	events, err := store.Events(context.Background(), run.ID)
	if err != nil {
		t.Fatalf("Events: %v", err)
	}
	if got, want := eventTypes(events), []string{string(EventRunStarted), string(EventQuestionRequested), string(EventDone)}; !sameStrings(got, want) {
		t.Fatalf("event types = %v, want %v", got, want)
	}
	pending, ok, err := store.LatestPendingQuestion(context.Background(), "thread-question", string(ChannelWeb))
	if err != nil {
		t.Fatalf("LatestPendingQuestion: %v", err)
	}
	if !ok {
		t.Fatal("expected pending question")
	}
	if pending.RunID != run.ID || pending.EventID != events[1].ID {
		t.Fatalf("pending linkage = run:%s event:%s", pending.RunID, pending.EventID)
	}
	if pending.Kind != "approval" || pending.Status != runstore.QuestionStatusWaiting {
		t.Fatalf("pending kind/status = %s/%s", pending.Kind, pending.Status)
	}
	assertChatScalar(t, db, `SELECT COUNT(*) FROM run_events WHERE run_id = ? AND type = 'question_requested' AND actor_id = ?`, 1, run.ID, "actor:web:session:1")
	assertNoStoredText(t, db, "secret prompt before question")
}

func TestRecordQuestionAnswerPersistsAnswerEventAndClosesPendingQuestion(t *testing.T) {
	_, store := newLifecycleStore(t)
	h := newPersistentHub(t, &recordingLoop{emits: []OutboundEvent{{
		Type: EventQuestionRequested,
		Payload: map[string]any{
			"question": "Which project?",
			"kind":     "clarification",
			"options":  []string{"Aura", "Gamma"},
		},
	}}, finalStatus: RunStatusWaitingForUser}, store)

	questionRun, err := h.ReceiveMessage(context.Background(), InboundMessage{
		ID:          "question-inbound",
		Channel:     ChannelTelegram,
		PrincipalID: "principal-1",
		ThreadID:    "thread-answer",
		Text:        "need a report",
		Mode:        DeliveryModeDeferred,
	})
	if err != nil {
		t.Fatalf("question ReceiveMessage: %v", err)
	}
	pending, ok, err := h.PendingQuestion(context.Background(), "thread-answer", ChannelTelegram)
	if err != nil {
		t.Fatalf("PendingQuestion: %v", err)
	}
	if !ok || pending.RunID != questionRun.ID {
		t.Fatalf("pending = %+v ok=%v", pending, ok)
	}

	storedAnswerRun, created, err := store.CreateOrGetRun(context.Background(), runstore.CreateRunParams{
		ID:          "run-answer",
		ThreadID:    "thread-answer",
		PrincipalID: "principal-1",
		Channel:     string(ChannelTelegram),
		Status:      string(RunStatusRunning),
		StartedAt:   time.Date(2026, 5, 16, 8, 30, 0, 0, time.UTC),
	})
	if err != nil {
		t.Fatalf("CreateOrGetRun answer: %v", err)
	}
	if !created {
		t.Fatal("answer run created=false")
	}
	answerRun := &Run{
		ID:          storedAnswerRun.ID,
		ThreadID:    storedAnswerRun.ThreadID,
		PrincipalID: storedAnswerRun.PrincipalID,
		Channel:     Channel(storedAnswerRun.Channel),
		Status:      RunStatus(storedAnswerRun.Status),
	}
	if err := h.RecordQuestionAnswer(context.Background(), answerRun, InboundMessage{
		ID:       "answer-inbound",
		Channel:  ChannelTelegram,
		ThreadID: "thread-answer",
	}, QuestionAnswer{
		SelectedOptionIDs: []string{"1"},
		FreeText:          "Aura",
		AnsweredMessageID: "answer-inbound",
	}); err != nil {
		t.Fatalf("RecordQuestionAnswer: %v", err)
	}

	question, err := store.GetQuestion(context.Background(), pending.ID)
	if err != nil {
		t.Fatalf("GetQuestion: %v", err)
	}
	if question.Status != runstore.QuestionStatusAnswered {
		t.Fatalf("question status = %s, want answered", question.Status)
	}
	if question.AnswerRunID != answerRun.ID || question.AnswerEventID == "" {
		t.Fatalf("answer linkage = run:%s event:%s", question.AnswerRunID, question.AnswerEventID)
	}
	answerEvents, err := store.Events(context.Background(), answerRun.ID)
	if err != nil {
		t.Fatalf("answer Events: %v", err)
	}
	found := false
	for _, event := range answerEvents {
		if event.Type == string(EventQuestionAnswered) {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("question_answered event missing from answer run: %v", eventTypes(answerEvents))
	}
}

func TestReceiveMessage_WithLifecycleStoreRecordsAuthorizationDenial(t *testing.T) {
	db, store := newLifecycleStore(t)
	identityStore, err := identity.NewStore(db)
	if err != nil {
		t.Fatalf("identity.NewStore: %v", err)
	}
	actorID := seedHubIdentityActor(t, identityStore)
	var executed atomic.Bool
	reg := tools.NewRegistry(nil)
	reg.Register(chatRegistryTool{executed: &executed})
	h := newPersistentHub(t, &registryAuthorizationLoop{
		reg:  reg,
		args: map[string]any{"query": "secret tool arg value"},
	}, store)
	ctx := identity.WithActorID(identity.WithAuthorizer(context.Background(), identityStore), actorID)

	run, err := h.ReceiveMessage(ctx, InboundMessage{
		ID:          "authz-denial-inbound",
		Channel:     ChannelWeb,
		PrincipalID: "principal-1",
		ThreadID:    "thread-1",
		Text:        "secret prompt",
		Mode:        DeliveryModeDeferred,
	})
	if err == nil {
		t.Fatal("ReceiveMessage error = nil, want registry authorization denial")
	}
	if executed.Load() {
		t.Fatal("tool executed despite missing grant")
	}

	assertChatScalar(t, db, `
SELECT COUNT(*)
FROM authz_decisions
WHERE actor_id = ? AND run_id = ? AND capability = 'tool.execute' AND decision = 'deny'
`, 1, actorID, run.ID)
	assertChatScalar(t, db, `
SELECT COUNT(*)
FROM run_events
WHERE run_id = ? AND type = 'authorization_denied' AND actor_id = ?
`, 1, run.ID, actorID)
	assertChatScalar(t, db, `
SELECT COUNT(*)
FROM audit_events
WHERE run_id = ? AND type = 'authorization_denied' AND actor_id = ?
`, 1, run.ID, actorID)
	assertNoStoredText(t, db, "secret prompt")
	assertNoStoredText(t, db, "secret tool arg value")
}

func TestRegisterOutbound_MultipleAdaptersFanout(t *testing.T) {
	loop := &recordingLoop{emits: []OutboundEvent{{Type: EventMessageDone}}}
	h := newHub(t, loop)
	h.RegisterInbound(&fakeInbound{channel: ChannelTelegram, out: InboundMessage{Channel: ChannelTelegram, Mode: DeliveryModeStreaming}})
	a := &fakeOutbound{channel: ChannelTelegram, mode: DeliveryModeStreaming}
	b := &fakeOutbound{channel: ChannelTelegram, mode: DeliveryModeStreaming}
	h.RegisterOutbound(a)
	h.RegisterOutbound(b)
	if _, err := h.Receive(context.Background(), ChannelTelegram, nil); err != nil {
		t.Fatal(err)
	}
	// Both adapters should have received the same events.
	if len(a.got) != len(b.got) || len(a.got) < 3 {
		t.Fatalf("fanout mismatch: a=%d b=%d", len(a.got), len(b.got))
	}
}

// --- ThreadRunStatus -------------------------------------------------------

func TestThreadRunStatus_UnknownThreadReturnsFalse(t *testing.T) {
	h := newHub(t, &recordingLoop{})
	if _, ok := h.ThreadRunStatus("unknown"); ok {
		t.Fatal("expected ok=false for unknown thread")
	}
}

func TestThreadRunStatus_AfterCompletedRun(t *testing.T) {
	h := newHub(t, &recordingLoop{})
	h.RegisterInbound(&fakeInbound{channel: ChannelWeb, out: InboundMessage{Channel: ChannelWeb, ThreadID: "t1", Mode: DeliveryModeDeferred}})
	h.RegisterOutbound(&fakeOutbound{channel: ChannelWeb, mode: DeliveryModeDeferred})

	if _, err := h.Receive(context.Background(), ChannelWeb, nil); err != nil {
		t.Fatalf("Receive: %v", err)
	}
	status, ok := h.ThreadRunStatus("t1")
	if !ok {
		t.Fatal("expected ok=true after completed run")
	}
	if status != RunStatusCompleted {
		t.Fatalf("status=%s want completed", status)
	}
}

func TestThreadRunStatus_WaitingForUserPause(t *testing.T) {
	loop := &recordingLoop{finalStatus: RunStatusWaitingForUser}
	h := newHub(t, loop)
	h.RegisterInbound(&fakeInbound{channel: ChannelWeb, out: InboundMessage{Channel: ChannelWeb, ThreadID: "t2", Mode: DeliveryModeDeferred}})
	h.RegisterOutbound(&fakeOutbound{channel: ChannelWeb, mode: DeliveryModeDeferred})

	if _, err := h.Receive(context.Background(), ChannelWeb, nil); err != nil {
		t.Fatalf("Receive: %v", err)
	}
	status, ok := h.ThreadRunStatus("t2")
	if !ok {
		t.Fatal("expected ok=true after waiting_for_user pause")
	}
	if status != RunStatusWaitingForUser {
		t.Fatalf("status=%s want waiting_for_user", status)
	}
}

func TestThreadRunStatus_OutOfRangePreservesWaitingForUser(t *testing.T) {
	// Simulates out-of-range reply: loop sets WaitingForUser then returns error.
	// Hub must suppress the error and preserve WaitingForUser in threadStatus.
	loop := blockingLoopFn(func(_ context.Context, run *Run, _ InboundMessage, _ EmitFn) error {
		run.Status = RunStatusWaitingForUser
		return errors.New("out-of-range reply")
	})
	h := newHub(t, loop)
	h.RegisterInbound(&fakeInbound{channel: ChannelWeb, out: InboundMessage{Channel: ChannelWeb, ThreadID: "t3", Mode: DeliveryModeDeferred}})
	h.RegisterOutbound(&fakeOutbound{channel: ChannelWeb, mode: DeliveryModeDeferred})

	run, err := h.Receive(context.Background(), ChannelWeb, nil)
	if err != nil {
		t.Fatalf("expected nil error for WaitingForUser abort, got %v", err)
	}
	if run.Status != RunStatusWaitingForUser {
		t.Fatalf("run status=%s want waiting_for_user", run.Status)
	}
	status, ok := h.ThreadRunStatus("t3")
	if !ok {
		t.Fatal("expected ok=true after WaitingForUser abort")
	}
	if status != RunStatusWaitingForUser {
		t.Fatalf("thread status=%s want waiting_for_user", status)
	}
}

// --- Helpers ---------------------------------------------------------------

func newPersistentHub(t *testing.T, loop AgentLoop, store LifecycleStore) *Hub {
	t.Helper()
	h, err := New(Config{Loop: loop, LifecycleStore: store})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	return h
}

func newLifecycleStore(t *testing.T) (*sql.DB, *runstore.Store) {
	t.Helper()
	db := testutil.OpenTestDB(t, migrations.Run)
	store, err := runstore.NewStore(db)
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}
	return db, store
}

func eventTypes(events []runstore.Event) []string {
	types := make([]string, 0, len(events))
	for _, event := range events {
		types = append(types, event.Type)
	}
	return types
}

func seedHubIdentityActor(t *testing.T, store *identity.Store) string {
	t.Helper()
	ctx := context.Background()
	if _, _, err := store.CreateOrResolvePrincipal(ctx, identity.PrincipalParams{
		ID:          "principal-1",
		Kind:        identity.PrincipalKindHuman,
		DisplayName: "Owner",
	}); err != nil {
		t.Fatalf("CreateOrResolvePrincipal: %v", err)
	}
	account, _, err := store.CreateOrResolveChannelAccount(ctx, identity.ChannelAccountParams{
		ID:          "acct-1",
		PrincipalID: "principal-1",
		Provider:    "telegram",
		ExternalID:  "123",
		DisplayName: "Owner TG",
	})
	if err != nil {
		t.Fatalf("CreateOrResolveChannelAccount: %v", err)
	}
	actor, err := store.CreateActor(ctx, identity.ActorParams{
		ID:               "actor-1",
		PrincipalID:      "principal-1",
		ActorType:        identity.ActorTypeSession,
		ChannelAccountID: account.ID,
	})
	if err != nil {
		t.Fatalf("CreateActor: %v", err)
	}
	return actor.ID
}

func sameStrings(got, want []string) bool {
	if len(got) != len(want) {
		return false
	}
	for i := range got {
		if got[i] != want[i] {
			return false
		}
	}
	return true
}

func assertChatScalar(t *testing.T, db *sql.DB, query string, want int, args ...any) {
	t.Helper()
	var got int
	if err := db.QueryRow(query, args...).Scan(&got); err != nil {
		t.Fatalf("query scalar %q: %v", query, err)
	}
	if got != want {
		t.Fatalf("scalar %q = %d, want %d", query, got, want)
	}
}

func assertNoStoredText(t *testing.T, db *sql.DB, text string) {
	t.Helper()
	pattern := "%" + text + "%"
	queries := []struct {
		name  string
		query string
		args  []any
	}{
		{
			name:  "runs metadata",
			query: `SELECT COUNT(*) FROM runs WHERE metadata_json LIKE ?`,
			args:  []any{pattern},
		},
		{
			name:  "runs final text",
			query: `SELECT COUNT(*) FROM runs WHERE final_text_preview LIKE ?`,
			args:  []any{pattern},
		},
		{
			name:  "runs last error",
			query: `SELECT COUNT(*) FROM runs WHERE last_error LIKE ?`,
			args:  []any{pattern},
		},
		{
			name:  "run events payload",
			query: `SELECT COUNT(*) FROM run_events WHERE payload_json LIKE ?`,
			args:  []any{pattern},
		},
		{
			name:  "audit events payload",
			query: `SELECT COUNT(*) FROM audit_events WHERE payload_json LIKE ?`,
			args:  []any{pattern},
		},
	}
	for _, q := range queries {
		var count int
		if err := db.QueryRow(q.query, q.args...).Scan(&count); err != nil {
			t.Fatalf("%s query failed: %v", q.name, err)
		}
		if count != 0 {
			t.Fatalf("%s contains raw text %q", q.name, text)
		}
	}
	if strings.Contains(text, "%") || strings.Contains(text, "_") {
		t.Fatalf("test text %q contains LIKE wildcard", text)
	}
}

type blockingLoopFn func(context.Context, *Run, InboundMessage, EmitFn) error

func (f blockingLoopFn) Run(ctx context.Context, run *Run, msg InboundMessage, emit EmitFn) error {
	return f(ctx, run, msg, emit)
}

type outboundFnAdapter struct {
	ch Channel
	md DeliveryMode
	fn func(OutboundEvent) error
}

func (a outboundFnAdapter) Channel() Channel                                  { return a.ch }
func (a outboundFnAdapter) Mode() DeliveryMode                                { return a.md }
func (a outboundFnAdapter) Deliver(_ context.Context, ev OutboundEvent) error { return a.fn(ev) }

func outboundFn(ch Channel, md DeliveryMode, fn func(OutboundEvent) error) OutboundAdapter {
	return outboundFnAdapter{ch: ch, md: md, fn: fn}
}
