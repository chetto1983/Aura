package webadapter

import (
	"context"
	"errors"
	"testing"

	"github.com/aura/aura/internal/chat"
)

// fakeHub satisfies HubReceiver. It drives a fixed set of events into the
// router under the run's ID, then returns a Run record with that ID so
// ChatService can read the buffer back.
type fakeHub struct {
	router  *Router
	events  []chat.OutboundEvent
	runID   string
	err     error
	lastMsg chat.InboundMessage
}

func (h *fakeHub) Receive(ctx context.Context, inboundChannel chat.Channel, raw any) (*chat.Run, error) {
	if inboundChannel != chat.ChannelWeb {
		return nil, errors.New("unexpected channel")
	}
	inbound := New()
	msg, err := inbound.Normalize(ctx, raw)
	if err != nil {
		return nil, err
	}
	h.lastMsg = msg
	for _, ev := range h.events {
		ev.RunID = h.runID
		_ = h.router.Deliver(ctx, ev)
	}
	if h.err != nil {
		return &chat.Run{ID: h.runID, Status: chat.RunStatusFailed}, h.err
	}
	return &chat.Run{ID: h.runID, Status: chat.RunStatusCompleted}, nil
}

func TestChatService_HappyPath(t *testing.T) {
	router := NewRouter()
	hub := &fakeHub{
		router: router,
		runID:  "run-happy",
		events: []chat.OutboundEvent{
			{Type: chat.EventMessageDone, Content: "result"},
			{Type: chat.EventUsage, Payload: map[string]any{"llm_calls": 1, "tool_calls": 0, "tokens_total": 42, "budget_warning": "soft budget hit"}},
			{Type: chat.EventDone, Payload: map[string]any{"status": "completed"}},
		},
	}
	svc := NewChatService(hub, router)
	if svc == nil {
		t.Fatal("NewChatService nil")
	}
	reply, err := svc.Chat(context.Background(), "user-1", "default", "hi")
	if err != nil {
		t.Fatalf("Chat: %v", err)
	}
	if reply.Reply != "result" {
		t.Fatalf("Reply = %q", reply.Reply)
	}
	if reply.LLMCalls != 1 {
		t.Fatalf("LLMCalls = %d", reply.LLMCalls)
	}
	if reply.Tokens != 42 {
		t.Fatalf("Tokens = %d", reply.Tokens)
	}
	if reply.BudgetWarning != "soft budget hit" {
		t.Fatalf("BudgetWarning = %q", reply.BudgetWarning)
	}
	if hub.lastMsg.ThreadID != "web:user-1:default" {
		t.Fatalf("ThreadID = %q, want web:user-1:default", hub.lastMsg.ThreadID)
	}
	// Buffer should be dropped after Chat returns.
	if _, ok := router.buffers["run-happy"]; ok {
		t.Fatalf("router still holds buffer for completed run")
	}
}

func TestChatService_ReturnsPendingQuestion(t *testing.T) {
	router := NewRouter()
	hub := &fakeHub{
		router: router,
		runID:  "run-question",
		events: []chat.OutboundEvent{
			{
				ID:      "question-1",
				Type:    chat.EventQuestionRequested,
				Payload: map[string]any{"question": "Pick one", "options": []string{"alpha", "beta"}, "kind": "selection"},
			},
			{Type: chat.EventDone, Payload: map[string]any{"status": string(chat.RunStatusWaitingForUser)}},
		},
	}
	svc := NewChatService(hub, router)
	reply, err := svc.Chat(context.Background(), "user-1", "default", "ask")
	if err != nil {
		t.Fatalf("Chat: %v", err)
	}
	if reply.PendingQuestion == nil || reply.PendingQuestion.ID != "question-1" || reply.PendingQuestion.Question != "Pick one" {
		t.Fatalf("PendingQuestion = %+v", reply.PendingQuestion)
	}
	if got := reply.PendingQuestion.Options; len(got) != 2 || got[1] != "beta" {
		t.Fatalf("options = %v", got)
	}
}

func TestChatService_AnswerNormalizesQuestionPayload(t *testing.T) {
	router := NewRouter()
	hub := &fakeHub{
		router: router,
		runID:  "run-answer",
		events: []chat.OutboundEvent{
			{Type: chat.EventMessageDone, Content: "resumed"},
			{Type: chat.EventDone, Payload: map[string]any{"status": string(chat.RunStatusCompleted)}},
		},
	}
	svc := NewChatService(hub, router)
	reply, err := svc.Answer(context.Background(), "user-1", "default", "question-1", "2", []string{"2"})
	if err != nil {
		t.Fatalf("Answer: %v", err)
	}
	if reply.Reply != "resumed" {
		t.Fatalf("Reply = %q", reply.Reply)
	}
	if hub.lastMsg.Question == nil || hub.lastMsg.Question.QuestionID != "question-1" || hub.lastMsg.Question.FreeText != "2" {
		t.Fatalf("Question = %+v", hub.lastMsg.Question)
	}
	if got := hub.lastMsg.Question.SelectedOptionIDs; len(got) != 1 || got[0] != "2" {
		t.Fatalf("SelectedOptionIDs = %v", got)
	}
}

func TestChatService_ThreadIDFallback(t *testing.T) {
	router := NewRouter()
	hub := &fakeHub{
		router: router,
		runID:  "run-anon",
		events: []chat.OutboundEvent{
			{Type: chat.EventDone, Payload: map[string]any{"status": "completed"}},
		},
	}
	svc := NewChatService(hub, router)
	if _, err := svc.Chat(context.Background(), "  ", "default", "hi"); err != nil {
		t.Fatalf("Chat: %v", err)
	}
	if hub.lastMsg.ThreadID != "web:anonymous:default" {
		t.Fatalf("ThreadID = %q, want web:anonymous:default", hub.lastMsg.ThreadID)
	}
}

func TestChatService_NilHubReturnsNil(t *testing.T) {
	if NewChatService(nil, NewRouter()) != nil {
		t.Fatal("NewChatService(nil,...) should return nil")
	}
	if NewChatService(&fakeHub{}, nil) != nil {
		t.Fatal("NewChatService(_, nil) should return nil")
	}
}

func TestChatService_PropagatesRunError(t *testing.T) {
	router := NewRouter()
	hub := &fakeHub{
		router: router,
		runID:  "run-err",
		err:    errors.New("agent boom"),
		events: []chat.OutboundEvent{
			{Type: chat.EventError, Payload: map[string]any{"error": "agent boom"}},
			{Type: chat.EventDone, Payload: map[string]any{"status": "failed"}},
		},
	}
	svc := NewChatService(hub, router)
	reply, err := svc.Chat(context.Background(), "u", "default", "m")
	if err == nil {
		t.Fatal("expected error")
	}
	// Partial reply (empty FinalContent here) still travels back.
	if reply.Reply != "" {
		t.Fatalf("Reply = %q", reply.Reply)
	}
}
