package webadapter

import (
	"context"
	"errors"
	"testing"

	"github.com/aura/aura/internal/chathub"
)

// fakeHub satisfies HubReceiver. It drives a fixed set of events into the
// router under the run's ID, then returns a Run record with that ID so
// ChatService can read the buffer back.
type fakeHub struct {
	router *Router
	events []chathub.OutboundEvent
	runID  string
	err    error
}

func (h *fakeHub) ReceiveMessage(ctx context.Context, msg chathub.InboundMessage) (*chathub.Run, error) {
	for _, ev := range h.events {
		ev.RunID = h.runID
		_ = h.router.Deliver(ctx, ev)
	}
	if h.err != nil {
		return &chathub.Run{ID: h.runID, Status: chathub.RunStatusFailed}, h.err
	}
	return &chathub.Run{ID: h.runID, Status: chathub.RunStatusCompleted}, nil
}

func TestChatService_HappyPath(t *testing.T) {
	router := NewRouter()
	hub := &fakeHub{
		router: router,
		runID:  "run-happy",
		events: []chathub.OutboundEvent{
			{Type: chathub.EventMessageDone, Content: "result"},
			{Type: chathub.EventUsage, Payload: map[string]any{"llm_calls": 1, "tool_calls": 0, "tokens_total": 42}},
			{Type: chathub.EventDone, Payload: map[string]any{"status": "completed"}},
		},
	}
	svc := NewChatService(hub, router)
	if svc == nil {
		t.Fatal("NewChatService nil")
	}
	reply, err := svc.Chat(context.Background(), "user-1", "hi")
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
	// Buffer should be dropped after Chat returns.
	if _, ok := router.buffers["run-happy"]; ok {
		t.Fatalf("router still holds buffer for completed run")
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
		events: []chathub.OutboundEvent{
			{Type: chathub.EventError, Payload: map[string]any{"error": "agent boom"}},
			{Type: chathub.EventDone, Payload: map[string]any{"status": "failed"}},
		},
	}
	svc := NewChatService(hub, router)
	reply, err := svc.Chat(context.Background(), "u", "m")
	if err == nil {
		t.Fatal("expected error")
	}
	// Partial reply (empty FinalContent here) still travels back.
	if reply.Reply != "" {
		t.Fatalf("Reply = %q", reply.Reply)
	}
}
