package chat_test

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	tools "github.com/aura/aura/internal/agent/tools/registry"
	"github.com/aura/aura/internal/chat"
	"github.com/aura/aura/internal/chat/testhelpers"
	"github.com/aura/aura/internal/db/migrations"
	"github.com/aura/aura/internal/identity"
	runstore "github.com/aura/aura/internal/storage/runs"
	"github.com/aura/aura/internal/testutil"
)

// --- Local test doubles (not shared with swarm tests) -----------------------

type registryAuthorizationLoop struct {
	reg  *tools.Registry
	args map[string]any
}

func (r *registryAuthorizationLoop) Run(ctx context.Context, _ *chat.Run, _ chat.InboundMessage, _ chat.EmitFn) error {
	_, err := r.reg.Execute(ctx, "fake", r.args)
	return err
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

// --- Tests ------------------------------------------------------------------

func newHub(t *testing.T, loop chat.AgentLoop) *chat.Hub {
	t.Helper()
	h, err := chat.New(chat.Config{Loop: loop})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	return h
}

func TestNew_RejectsMissingLoop(t *testing.T) {
	if _, err := chat.New(chat.Config{}); err == nil {
		t.Fatal("expected error when AgentLoop nil")
	}
}

func TestReceive_HappyPath_FanoutsToMatchingOutbound(t *testing.T) {
	loop := &testhelpers.RecordingLoop{
		Emits: []chat.OutboundEvent{
			{Type: chat.EventMessageCreated},
			{Type: chat.EventMessageDelta, Content: "hello"},
			{Type: chat.EventMessageDone, Content: "hello"},
			{Type: chat.EventUsage, Payload: map[string]any{"tokens": 12}},
		},
	}
	h := newHub(t, loop)

	in := &testhelpers.FakeInbound{Ch: chat.ChannelWeb, Out: chat.InboundMessage{
		ID: "msg-1", Channel: chat.ChannelWeb, PrincipalID: "p1", Text: "hi",
		Mode: chat.DeliveryModeDeferred, CreatedAt: time.Now(),
	}}
	out := &testhelpers.FakeOutbound{Ch: chat.ChannelWeb, Md: chat.DeliveryModeDeferred}
	h.RegisterInbound(in)
	h.RegisterOutbound(out)

	run, err := h.Receive(context.Background(), chat.ChannelWeb, nil)
	if err != nil {
		t.Fatalf("Receive: %v", err)
	}
	if run.Status != chat.RunStatusCompleted {
		t.Fatalf("Status = %s, want completed", run.Status)
	}
	// Expected events: run_started + loop's 4 + done = 6
	if got := len(out.Got); got != 6 {
		t.Fatalf("delivered %d events, want 6: %+v", got, out.Got)
	}
	// First event is run_started (injected by Hub), last is done.
	if out.Got[0].Type != chat.EventRunStarted {
		t.Fatalf("first event = %s, want run_started", out.Got[0].Type)
	}
	if out.Got[len(out.Got)-1].Type != chat.EventDone {
		t.Fatalf("last event = %s, want done", out.Got[len(out.Got)-1].Type)
	}
	// Seq is monotonic + non-zero.
	for i, ev := range out.Got {
		if ev.Seq == 0 {
			t.Fatalf("event[%d] has zero Seq", i)
		}
		if i > 0 && ev.Seq <= out.Got[i-1].Seq {
			t.Fatalf("seq not monotonic at i=%d: %d <= %d", i, ev.Seq, out.Got[i-1].Seq)
		}
	}
	// run_id, channel, created_at populated on every event.
	for i, ev := range out.Got {
		if ev.RunID == "" {
			t.Fatalf("event[%d] missing RunID", i)
		}
		if ev.Channel != chat.ChannelWeb {
			t.Fatalf("event[%d] channel=%s, want web", i, ev.Channel)
		}
		if ev.CreatedAt.IsZero() {
			t.Fatalf("event[%d] missing CreatedAt", i)
		}
	}
}

func TestReceive_MissingInboundAdapter(t *testing.T) {
	h := newHub(t, &testhelpers.RecordingLoop{})
	_, err := h.Receive(context.Background(), chat.ChannelTelegram, nil)
	if err == nil {
		t.Fatal("expected error for unregistered channel")
	}
}

func TestReceive_InboundNormalizeError(t *testing.T) {
	h := newHub(t, &testhelpers.RecordingLoop{})
	h.RegisterInbound(&testhelpers.FakeInbound{Ch: chat.ChannelWeb, Err: errors.New("bad payload")})
	_, err := h.Receive(context.Background(), chat.ChannelWeb, nil)
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestReceive_LoopError_PropagatesAndEmitsErrorThenDone(t *testing.T) {
	loop := &testhelpers.RecordingLoop{Err: errors.New("boom")}
	h := newHub(t, loop)
	h.RegisterInbound(&testhelpers.FakeInbound{Ch: chat.ChannelWeb, Out: chat.InboundMessage{Channel: chat.ChannelWeb, Mode: chat.DeliveryModeDeferred}})
	out := &testhelpers.FakeOutbound{Ch: chat.ChannelWeb, Md: chat.DeliveryModeDeferred}
	h.RegisterOutbound(out)

	run, err := h.Receive(context.Background(), chat.ChannelWeb, nil)
	if err == nil || err.Error() != "boom" {
		t.Fatalf("expected boom, got %v", err)
	}
	if run.Status != chat.RunStatusFailed {
		t.Fatalf("status = %s, want failed", run.Status)
	}
	if run.LastError != "boom" {
		t.Fatalf("LastError = %q", run.LastError)
	}
	// Expect: run_started + error + done.
	if len(out.Got) != 3 {
		t.Fatalf("delivered %d events, want 3", len(out.Got))
	}
	if out.Got[0].Type != chat.EventRunStarted {
		t.Fatalf("evt[0] = %s", out.Got[0].Type)
	}
	if out.Got[1].Type != chat.EventError {
		t.Fatalf("evt[1] = %s", out.Got[1].Type)
	}
	if out.Got[2].Type != chat.EventDone {
		t.Fatalf("evt[2] = %s", out.Got[2].Type)
	}
}

func TestReceive_ContextCanceled_RunStatusCancelled(t *testing.T) {
	canceledLoop := &testhelpers.RecordingLoop{Err: context.Canceled}
	h := newHub(t, canceledLoop)
	h.RegisterInbound(&testhelpers.FakeInbound{Ch: chat.ChannelWeb, Out: chat.InboundMessage{Channel: chat.ChannelWeb, Mode: chat.DeliveryModeDeferred}})
	out := &testhelpers.FakeOutbound{Ch: chat.ChannelWeb, Md: chat.DeliveryModeDeferred}
	h.RegisterOutbound(out)

	run, err := h.Receive(context.Background(), chat.ChannelWeb, nil)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("expected context.Canceled, got %v", err)
	}
	if run.Status != chat.RunStatusCancelled {
		t.Fatalf("status = %s, want cancelled", run.Status)
	}
}

func TestStop_BestEffortAndIdempotent(t *testing.T) {
	// Loop that blocks until ctx is done, so Stop can interrupt it.
	bl := testhelpers.BlockingLoopFn(func(ctx context.Context, _ *chat.Run, _ chat.InboundMessage, _ chat.EmitFn) error {
		<-ctx.Done()
		return ctx.Err()
	})
	h := newHub(t, bl)
	h.RegisterInbound(&testhelpers.FakeInbound{Ch: chat.ChannelWeb, Out: chat.InboundMessage{Channel: chat.ChannelWeb, Mode: chat.DeliveryModeDeferred}})
	h.RegisterOutbound(&testhelpers.FakeOutbound{Ch: chat.ChannelWeb, Md: chat.DeliveryModeDeferred})

	// Capture the run_id by intercepting the run_started event from a
	// closure-based outbound.
	var runID atomic.Value
	out := outboundFn(chat.ChannelWeb, chat.DeliveryModeDeferred, func(ev chat.OutboundEvent) error {
		if ev.Type == chat.EventRunStarted {
			runID.Store(ev.RunID)
		}
		return nil
	})
	h.RegisterOutbound(out)

	done := make(chan struct{})
	go func() {
		_, _ = h.Receive(context.Background(), chat.ChannelWeb, nil)
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
	loop := &testhelpers.RecordingLoop{Emits: []chat.OutboundEvent{{Type: chat.EventMessageDone}}}
	h := newHub(t, loop)
	out := &testhelpers.FakeOutbound{Ch: chat.ChannelCron, Md: chat.DeliveryModeSilent}
	h.RegisterOutbound(out)
	// No inbound adapter registered for cron.

	msg := chat.InboundMessage{Channel: chat.ChannelCron, PrincipalID: "system", Mode: chat.DeliveryModeSilent, Text: "tick"}
	_, err := h.ReceiveMessage(context.Background(), msg)
	if err != nil {
		t.Fatalf("ReceiveMessage: %v", err)
	}
	if len(out.Got) < 2 {
		t.Fatalf("expected at least run_started + done events, got %d", len(out.Got))
	}
}

func TestReceiveMessage_WithLifecycleStorePersistsRunStartedAndDone(t *testing.T) {
	db, store := newLifecycleStore(t)
	loop := &testhelpers.RecordingLoop{}
	h := newPersistentHub(t, loop, store)

	msg := chat.InboundMessage{
		ID:          "inbound-1",
		Channel:     chat.ChannelWeb,
		PrincipalID: "principal-1",
		ThreadID:    "thread-1",
		Text:        "secret user prompt",
		Mode:        chat.DeliveryModeDeferred,
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
	if stored.Status != string(chat.RunStatusCompleted) {
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
	if got, want := eventTypes(events), []string{string(chat.EventRunStarted), string(chat.EventDone)}; !sameStrings(got, want) {
		t.Fatalf("event types = %v, want %v", got, want)
	}
	assertNoStoredText(t, db, "secret user prompt")
	assertNoStoredText(t, db, "do-not-store")
}

func TestReceiveMessage_WithLifecycleStoreDedupesInboundMessage(t *testing.T) {
	_, store := newLifecycleStore(t)
	loop := &testhelpers.RecordingLoop{}
	h := newPersistentHub(t, loop, store)
	msg := chat.InboundMessage{
		ID:          "same-inbound-id",
		Channel:     chat.ChannelWeb,
		PrincipalID: "principal-1",
		ThreadID:    "thread-1",
		Text:        "dedupe text",
		Mode:        chat.DeliveryModeDeferred,
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
	loop.Mu.Lock()
	calls := loop.Calls
	loop.Mu.Unlock()
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
	h := newPersistentHub(t, &testhelpers.RecordingLoop{}, store)
	ctx := identity.WithActorID(context.Background(), "actor:web:session:1")
	msg := chat.InboundMessage{
		ID:          "actor-inbound-id",
		Channel:     chat.ChannelWeb,
		PrincipalID: "principal-1",
		ThreadID:    "thread-1",
		Text:        "hello",
		Mode:        chat.DeliveryModeDeferred,
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
	h := newPersistentHub(t, &testhelpers.RecordingLoop{Err: errors.New(rawErr)}, store)
	msg := chat.InboundMessage{
		ID:          "error-inbound-id",
		Channel:     chat.ChannelWeb,
		PrincipalID: "principal-1",
		ThreadID:    "thread-1",
		Text:        "secret failing prompt",
		Mode:        chat.DeliveryModeDeferred,
	}

	run, err := h.ReceiveMessage(context.Background(), msg)
	if err == nil || err.Error() != rawErr {
		t.Fatalf("expected raw loop error for caller, got %v", err)
	}
	if run.Status != chat.RunStatusFailed {
		t.Fatalf("run status = %s, want failed", run.Status)
	}

	stored, err := store.GetRun(context.Background(), run.ID)
	if err != nil {
		t.Fatalf("GetRun: %v", err)
	}
	if stored.Status != string(chat.RunStatusFailed) {
		t.Fatalf("stored status = %s, want failed", stored.Status)
	}
	if stored.LastError != "agent_loop_error" {
		t.Fatalf("stored last_error = %q, want redacted class", stored.LastError)
	}

	events, err := store.Events(context.Background(), run.ID)
	if err != nil {
		t.Fatalf("Events: %v", err)
	}
	if got, want := eventTypes(events), []string{string(chat.EventRunStarted), string(chat.EventError), string(chat.EventDone)}; !sameStrings(got, want) {
		t.Fatalf("event types = %v, want %v", got, want)
	}
	assertNoStoredText(t, db, rawErr)
	assertNoStoredText(t, db, "secret failing prompt")
}

func TestReceiveMessage_WithLifecycleStorePersistsRedactedToolUsageAndFinalEvents(t *testing.T) {
	db, store := newLifecycleStore(t)
	loop := &testhelpers.RecordingLoop{Emits: []chat.OutboundEvent{
		{
			Type: chat.EventToolStart,
			Payload: map[string]any{
				"tool":           "search_memory",
				"tool_call_id":   "call-1",
				"arg_keys":       []string{"query"},
				"raw_arg_values": "secret tool arg value",
			},
		},
		{
			Type: chat.EventToolEnd,
			Payload: map[string]any{
				"tool":         "search_memory",
				"tool_call_id": "call-1",
				"success":      false,
				"elapsed_ms":   int64(12),
				"preview":      "secret tool result preview",
			},
		},
		{
			Type: chat.EventUsage,
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
			Type:    chat.EventMessageDone,
			Content: "final answer for the user",
			Payload: map[string]any{
				"delivered":    true,
				"raw_delivery": "secret delivery value",
			},
		},
	}}
	h := newPersistentHub(t, loop, store)
	msg := chat.InboundMessage{
		ID:          "tool-inbound-id",
		Channel:     chat.ChannelWeb,
		PrincipalID: "principal-1",
		ThreadID:    "thread-1",
		Text:        "secret tool prompt",
		Mode:        chat.DeliveryModeDeferred,
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
		string(chat.EventRunStarted),
		string(chat.EventToolStart),
		string(chat.EventToolEnd),
		string(chat.EventUsage),
		string(chat.EventMessageDone),
		string(chat.EventDone),
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
	loop := &testhelpers.RecordingLoop{
		Emits: []chat.OutboundEvent{{
			Type: chat.EventQuestionRequested,
			Payload: map[string]any{
				"question":     "Approve durable memory write?",
				"kind":         "approval",
				"options":      []string{"approve_once", "deny", "cancel"},
				"tool":         "ask_user",
				"tool_call_id": "ask-1",
			},
		}},
		FinalStatus: chat.RunStatusWaitingForUser,
	}
	h := newPersistentHub(t, loop, store)
	ctx := identity.WithActorID(context.Background(), "actor:web:session:1")
	run, err := h.ReceiveMessage(ctx, chat.InboundMessage{
		ID:          "question-inbound-id",
		Channel:     chat.ChannelWeb,
		PrincipalID: "principal-1",
		ThreadID:    "thread-question",
		Text:        "secret prompt before question",
		Mode:        chat.DeliveryModeDeferred,
	})
	if err != nil {
		t.Fatalf("ReceiveMessage: %v", err)
	}
	if run.Status != chat.RunStatusWaitingForUser {
		t.Fatalf("run status = %s, want waiting_for_user", run.Status)
	}
	events, err := store.Events(context.Background(), run.ID)
	if err != nil {
		t.Fatalf("Events: %v", err)
	}
	if got, want := eventTypes(events), []string{string(chat.EventRunStarted), string(chat.EventQuestionRequested), string(chat.EventDone)}; !sameStrings(got, want) {
		t.Fatalf("event types = %v, want %v", got, want)
	}
	pending, ok, err := store.LatestPendingQuestion(context.Background(), "thread-question", string(chat.ChannelWeb))
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
	h := newPersistentHub(t, &testhelpers.RecordingLoop{Emits: []chat.OutboundEvent{{
		Type: chat.EventQuestionRequested,
		Payload: map[string]any{
			"question": "Which project?",
			"kind":     "clarification",
			"options":  []string{"Aura", "Gamma"},
		},
	}}, FinalStatus: chat.RunStatusWaitingForUser}, store)

	questionRun, err := h.ReceiveMessage(context.Background(), chat.InboundMessage{
		ID:          "question-inbound",
		Channel:     chat.ChannelTelegram,
		PrincipalID: "principal-1",
		ThreadID:    "thread-answer",
		Text:        "need a report",
		Mode:        chat.DeliveryModeDeferred,
	})
	if err != nil {
		t.Fatalf("question ReceiveMessage: %v", err)
	}
	pending, ok, err := h.PendingQuestion(context.Background(), "thread-answer", chat.ChannelTelegram)
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
		Channel:     string(chat.ChannelTelegram),
		Status:      string(chat.RunStatusRunning),
		StartedAt:   time.Date(2026, 5, 16, 8, 30, 0, 0, time.UTC),
	})
	if err != nil {
		t.Fatalf("CreateOrGetRun answer: %v", err)
	}
	if !created {
		t.Fatal("answer run created=false")
	}
	answerRun := &chat.Run{
		ID:          storedAnswerRun.ID,
		ThreadID:    storedAnswerRun.ThreadID,
		PrincipalID: storedAnswerRun.PrincipalID,
		Channel:     chat.Channel(storedAnswerRun.Channel),
		Status:      chat.RunStatus(storedAnswerRun.Status),
	}
	if err := h.RecordQuestionAnswer(context.Background(), answerRun, chat.InboundMessage{
		ID:       "answer-inbound",
		Channel:  chat.ChannelTelegram,
		ThreadID: "thread-answer",
	}, chat.QuestionAnswer{
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
		if event.Type == string(chat.EventQuestionAnswered) {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("question_answered event missing from answer run: %v", eventTypes(answerEvents))
	}
}

func TestQuestionStateSurvivesStoreReopenAndNewHub(t *testing.T) {
	db, store := newLifecycleStore(t)
	h := newPersistentHub(t, &testhelpers.RecordingLoop{Emits: []chat.OutboundEvent{{
		Type: chat.EventQuestionRequested,
		Payload: map[string]any{
			"question": "Which project?",
			"kind":     "clarification",
			"options":  []string{"Aura", "Gamma"},
		},
	}}, FinalStatus: chat.RunStatusWaitingForUser}, store)

	questionRun, err := h.ReceiveMessage(context.Background(), chat.InboundMessage{
		ID:          "restart-question-inbound",
		Channel:     chat.ChannelTelegram,
		PrincipalID: "principal-1",
		ThreadID:    "thread-restart",
		Text:        "prepare the report",
		Mode:        chat.DeliveryModeDeferred,
	})
	if err != nil {
		t.Fatalf("question ReceiveMessage: %v", err)
	}

	reopened, err := runstore.NewStore(db)
	if err != nil {
		t.Fatalf("NewStore reopened: %v", err)
	}
	reopenedHub := newPersistentHub(t, &testhelpers.RecordingLoop{}, reopened)
	pending, ok, err := reopenedHub.PendingQuestion(context.Background(), "thread-restart", chat.ChannelTelegram)
	if err != nil {
		t.Fatalf("PendingQuestion reopened: %v", err)
	}
	if !ok {
		t.Fatal("pending question not visible after store reopen")
	}
	if pending.RunID != questionRun.ID || pending.Kind != "clarification" || !sameStrings(pending.Options, []string{"Aura", "Gamma"}) {
		t.Fatalf("pending after reopen = %+v", pending)
	}

	storedQuestionRun, err := reopened.GetRun(context.Background(), questionRun.ID)
	if err != nil {
		t.Fatalf("GetRun question run after reopen: %v", err)
	}
	if storedQuestionRun.Status != string(chat.RunStatusWaitingForUser) {
		t.Fatalf("stored question run status = %s, want waiting_for_user", storedQuestionRun.Status)
	}

	answerRun, created, err := reopened.CreateOrGetRun(context.Background(), runstore.CreateRunParams{
		ID:          "run-restart-answer",
		ThreadID:    "thread-restart",
		PrincipalID: "principal-1",
		Channel:     string(chat.ChannelTelegram),
		Status:      string(chat.RunStatusRunning),
		StartedAt:   time.Date(2026, 5, 16, 9, 0, 0, 0, time.UTC),
	})
	if err != nil {
		t.Fatalf("CreateOrGetRun answer: %v", err)
	}
	if !created {
		t.Fatal("answer run created=false")
	}
	if err := reopenedHub.RecordQuestionAnswer(context.Background(), &chat.Run{
		ID:          answerRun.ID,
		ThreadID:    answerRun.ThreadID,
		PrincipalID: answerRun.PrincipalID,
		Channel:     chat.Channel(answerRun.Channel),
		Status:      chat.RunStatus(answerRun.Status),
	}, chat.InboundMessage{
		ID:       "restart-answer-inbound",
		Channel:  chat.ChannelTelegram,
		ThreadID: "thread-restart",
	}, chat.QuestionAnswer{
		QuestionID:        pending.ID,
		SelectedOptionIDs: []string{"1"},
		FreeText:          "Aura",
		AnsweredMessageID: "restart-answer-inbound",
	}); err != nil {
		t.Fatalf("RecordQuestionAnswer reopened: %v", err)
	}

	question, err := reopened.GetQuestion(context.Background(), pending.ID)
	if err != nil {
		t.Fatalf("GetQuestion after answer: %v", err)
	}
	if question.Status != runstore.QuestionStatusAnswered || question.AnswerRunID != answerRun.ID {
		t.Fatalf("answered question = %+v", question)
	}
	assertChatScalar(t, db, `SELECT COUNT(*) FROM run_events WHERE run_id = ? AND type = 'question_answered' AND causation_id = ?`, 1, answerRun.ID, pending.ID)
}

func TestRecordQuestionAnswerRejectsDuplicateWithoutAppendingEvent(t *testing.T) {
	db, store := newLifecycleStore(t)
	h := newPersistentHub(t, &testhelpers.RecordingLoop{Emits: []chat.OutboundEvent{{
		Type: chat.EventQuestionRequested,
		Payload: map[string]any{
			"question": "Approve this?",
			"kind":     "approval",
		},
	}}, FinalStatus: chat.RunStatusWaitingForUser}, store)

	if _, err := h.ReceiveMessage(context.Background(), chat.InboundMessage{
		ID:          "duplicate-question-inbound",
		Channel:     chat.ChannelTelegram,
		PrincipalID: "principal-1",
		ThreadID:    "thread-duplicate",
		Text:        "delete this",
		Mode:        chat.DeliveryModeDeferred,
	}); err != nil {
		t.Fatalf("question ReceiveMessage: %v", err)
	}
	pending, ok, err := h.PendingQuestion(context.Background(), "thread-duplicate", chat.ChannelTelegram)
	if err != nil {
		t.Fatalf("PendingQuestion: %v", err)
	}
	if !ok {
		t.Fatal("expected pending question")
	}
	answerRun, created, err := store.CreateOrGetRun(context.Background(), runstore.CreateRunParams{
		ID:          "run-duplicate-answer",
		ThreadID:    "thread-duplicate",
		PrincipalID: "principal-1",
		Channel:     string(chat.ChannelTelegram),
		Status:      string(chat.RunStatusRunning),
		StartedAt:   time.Date(2026, 5, 16, 9, 30, 0, 0, time.UTC),
	})
	if err != nil {
		t.Fatalf("CreateOrGetRun answer: %v", err)
	}
	if !created {
		t.Fatal("answer run created=false")
	}
	answer := &chat.Run{
		ID:          answerRun.ID,
		ThreadID:    answerRun.ThreadID,
		PrincipalID: answerRun.PrincipalID,
		Channel:     chat.Channel(answerRun.Channel),
		Status:      chat.RunStatus(answerRun.Status),
	}
	msg := chat.InboundMessage{ID: "duplicate-answer-inbound", Channel: chat.ChannelTelegram, ThreadID: "thread-duplicate"}
	payload := chat.QuestionAnswer{QuestionID: pending.ID, FreeText: "approve_once", AnsweredMessageID: msg.ID}
	if err := h.RecordQuestionAnswer(context.Background(), answer, msg, payload); err != nil {
		t.Fatalf("RecordQuestionAnswer first: %v", err)
	}
	if err := h.RecordQuestionAnswer(context.Background(), answer, chat.InboundMessage{ID: "duplicate-answer-inbound-2", Channel: chat.ChannelTelegram, ThreadID: "thread-duplicate"}, payload); !errors.Is(err, runstore.ErrQuestionNotWaiting) {
		t.Fatalf("duplicate answer error = %v, want ErrQuestionNotWaiting", err)
	}
	assertChatScalar(t, db, `SELECT COUNT(*) FROM run_events WHERE run_id = ? AND type = 'question_answered' AND causation_id = ?`, 1, answerRun.ID, pending.ID)
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

	run, err := h.ReceiveMessage(ctx, chat.InboundMessage{
		ID:          "authz-denial-inbound",
		Channel:     chat.ChannelWeb,
		PrincipalID: "principal-1",
		ThreadID:    "thread-1",
		Text:        "secret prompt",
		Mode:        chat.DeliveryModeDeferred,
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
	loop := &testhelpers.RecordingLoop{Emits: []chat.OutboundEvent{{Type: chat.EventMessageDone}}}
	h := newHub(t, loop)
	h.RegisterInbound(&testhelpers.FakeInbound{Ch: chat.ChannelTelegram, Out: chat.InboundMessage{Channel: chat.ChannelTelegram, Mode: chat.DeliveryModeStreaming}})
	a := &testhelpers.FakeOutbound{Ch: chat.ChannelTelegram, Md: chat.DeliveryModeStreaming}
	b := &testhelpers.FakeOutbound{Ch: chat.ChannelTelegram, Md: chat.DeliveryModeStreaming}
	h.RegisterOutbound(a)
	h.RegisterOutbound(b)
	if _, err := h.Receive(context.Background(), chat.ChannelTelegram, nil); err != nil {
		t.Fatal(err)
	}
	// Both adapters should have received the same events.
	if len(a.Got) != len(b.Got) || len(a.Got) < 3 {
		t.Fatalf("fanout mismatch: a=%d b=%d", len(a.Got), len(b.Got))
	}
}

// --- ThreadRunStatus -------------------------------------------------------

func TestThreadRunStatus_UnknownThreadReturnsFalse(t *testing.T) {
	h := newHub(t, &testhelpers.RecordingLoop{})
	if _, ok := h.ThreadRunStatus("unknown"); ok {
		t.Fatal("expected ok=false for unknown thread")
	}
}

func TestThreadRunStatus_AfterCompletedRun(t *testing.T) {
	h := newHub(t, &testhelpers.RecordingLoop{})
	h.RegisterInbound(&testhelpers.FakeInbound{Ch: chat.ChannelWeb, Out: chat.InboundMessage{Channel: chat.ChannelWeb, ThreadID: "t1", Mode: chat.DeliveryModeDeferred}})
	h.RegisterOutbound(&testhelpers.FakeOutbound{Ch: chat.ChannelWeb, Md: chat.DeliveryModeDeferred})

	if _, err := h.Receive(context.Background(), chat.ChannelWeb, nil); err != nil {
		t.Fatalf("Receive: %v", err)
	}
	status, ok := h.ThreadRunStatus("t1")
	if !ok {
		t.Fatal("expected ok=true after completed run")
	}
	if status != chat.RunStatusCompleted {
		t.Fatalf("status=%s want completed", status)
	}
}

func TestThreadRunStatus_WaitingForUserPause(t *testing.T) {
	loop := &testhelpers.RecordingLoop{FinalStatus: chat.RunStatusWaitingForUser}
	h := newHub(t, loop)
	h.RegisterInbound(&testhelpers.FakeInbound{Ch: chat.ChannelWeb, Out: chat.InboundMessage{Channel: chat.ChannelWeb, ThreadID: "t2", Mode: chat.DeliveryModeDeferred}})
	h.RegisterOutbound(&testhelpers.FakeOutbound{Ch: chat.ChannelWeb, Md: chat.DeliveryModeDeferred})

	if _, err := h.Receive(context.Background(), chat.ChannelWeb, nil); err != nil {
		t.Fatalf("Receive: %v", err)
	}
	status, ok := h.ThreadRunStatus("t2")
	if !ok {
		t.Fatal("expected ok=true after waiting_for_user pause")
	}
	if status != chat.RunStatusWaitingForUser {
		t.Fatalf("status=%s want waiting_for_user", status)
	}
}

func TestThreadRunStatus_OutOfRangePreservesWaitingForUser(t *testing.T) {
	// Simulates out-of-range reply: loop sets WaitingForUser then returns error.
	// Hub must suppress the error and preserve WaitingForUser in threadStatus.
	loop := testhelpers.BlockingLoopFn(func(_ context.Context, run *chat.Run, _ chat.InboundMessage, _ chat.EmitFn) error {
		run.Status = chat.RunStatusWaitingForUser
		return errors.New("out-of-range reply")
	})
	h := newHub(t, loop)
	h.RegisterInbound(&testhelpers.FakeInbound{Ch: chat.ChannelWeb, Out: chat.InboundMessage{Channel: chat.ChannelWeb, ThreadID: "t3", Mode: chat.DeliveryModeDeferred}})
	h.RegisterOutbound(&testhelpers.FakeOutbound{Ch: chat.ChannelWeb, Md: chat.DeliveryModeDeferred})

	run, err := h.Receive(context.Background(), chat.ChannelWeb, nil)
	if err != nil {
		t.Fatalf("expected nil error for WaitingForUser abort, got %v", err)
	}
	if run.Status != chat.RunStatusWaitingForUser {
		t.Fatalf("run status=%s want waiting_for_user", run.Status)
	}
	status, ok := h.ThreadRunStatus("t3")
	if !ok {
		t.Fatal("expected ok=true after WaitingForUser abort")
	}
	if status != chat.RunStatusWaitingForUser {
		t.Fatalf("thread status=%s want waiting_for_user", status)
	}
}

// --- Helpers ---------------------------------------------------------------

func newPersistentHub(t *testing.T, loop chat.AgentLoop, store chat.LifecycleStore) *chat.Hub {
	t.Helper()
	h, err := chat.New(chat.Config{Loop: loop, LifecycleStore: store})
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

type outboundFnAdapter struct {
	ch chat.Channel
	md chat.DeliveryMode
	fn func(chat.OutboundEvent) error
}

func (a outboundFnAdapter) Channel() chat.Channel   { return a.ch }
func (a outboundFnAdapter) Mode() chat.DeliveryMode { return a.md }
func (a outboundFnAdapter) Deliver(_ context.Context, ev chat.OutboundEvent) error {
	return a.fn(ev)
}

func outboundFn(ch chat.Channel, md chat.DeliveryMode, fn func(chat.OutboundEvent) error) chat.OutboundAdapter {
	return outboundFnAdapter{ch: ch, md: md, fn: fn}
}
