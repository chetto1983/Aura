package webadapter

import (
	"context"
	"testing"
	"time"

	"github.com/aura/aura/internal/chat"
)

func TestRouter_ChannelAndMode(t *testing.T) {
	r := NewRouter()
	if r.Channel() != chat.ChannelWeb {
		t.Fatalf("Channel = %s", r.Channel())
	}
	if r.Mode() != chat.DeliveryModeDeferred {
		t.Fatalf("Mode = %s", r.Mode())
	}
}

func TestRouter_BuffersDeltasAndFinalisesOnDone(t *testing.T) {
	r := NewRouter()
	ctx := context.Background()
	runID := "run-1"

	events := []chat.OutboundEvent{
		{RunID: runID, Type: chat.EventRunStarted},
		{RunID: runID, Type: chat.EventMessageDelta, Content: "Hello, "},
		{RunID: runID, Type: chat.EventMessageDelta, Content: "world!"},
		{RunID: runID, Type: chat.EventMessageDone, Content: "Hello, world!", Payload: map[string]any{"delivered": false}},
		{RunID: runID, Type: chat.EventUsage, Payload: map[string]any{
			"llm_calls":         2,
			"tool_calls":        1,
			"tokens_total":      150,
			"tokens_prompt":     100,
			"tokens_completion": 50,
			"cost_usd":          0.01,
			"terminal_tool":     "",
		}},
		{RunID: runID, Type: chat.EventDone, Payload: map[string]any{"status": "completed"}},
	}
	for _, ev := range events {
		if err := r.Deliver(ctx, ev); err != nil {
			t.Fatalf("Deliver: %v", err)
		}
	}

	buf := r.Reserve(runID)
	defer r.Drop(runID)
	res, err := buf.Wait(ctx)
	if err != nil {
		t.Fatalf("Wait: %v", err)
	}
	if res.FinalContent != "Hello, world!" {
		t.Fatalf("FinalContent = %q", res.FinalContent)
	}
	if res.LLMCalls != 2 || res.ToolCalls != 1 || res.TokensTotal != 150 {
		t.Fatalf("stats = %+v", res)
	}
	if res.Status != "completed" {
		t.Fatalf("Status = %s", res.Status)
	}
}

func TestRouter_RunIsolation(t *testing.T) {
	// Two concurrent runs should not cross-contaminate their buffers.
	r := NewRouter()
	ctx := context.Background()

	_ = r.Deliver(ctx, chat.OutboundEvent{RunID: "A", Type: chat.EventMessageDelta, Content: "A1"})
	_ = r.Deliver(ctx, chat.OutboundEvent{RunID: "B", Type: chat.EventMessageDelta, Content: "B1"})
	_ = r.Deliver(ctx, chat.OutboundEvent{RunID: "A", Type: chat.EventMessageDone, Content: "alpha"})
	_ = r.Deliver(ctx, chat.OutboundEvent{RunID: "B", Type: chat.EventMessageDone, Content: "beta"})
	_ = r.Deliver(ctx, chat.OutboundEvent{RunID: "A", Type: chat.EventDone})
	_ = r.Deliver(ctx, chat.OutboundEvent{RunID: "B", Type: chat.EventDone})

	resA, _ := r.Reserve("A").Wait(ctx)
	resB, _ := r.Reserve("B").Wait(ctx)
	if resA.FinalContent != "alpha" {
		t.Fatalf("A.FinalContent = %q", resA.FinalContent)
	}
	if resB.FinalContent != "beta" {
		t.Fatalf("B.FinalContent = %q", resB.FinalContent)
	}
	r.Drop("A")
	r.Drop("B")
}

func TestRouter_WaitRespectsContextTimeout(t *testing.T) {
	r := NewRouter()
	buf := r.Reserve("never")
	defer r.Drop("never")
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	if _, err := buf.Wait(ctx); err == nil {
		t.Fatal("expected ctx error, got nil")
	}
}

func TestRouter_ErrorCaptured(t *testing.T) {
	r := NewRouter()
	ctx := context.Background()
	_ = r.Deliver(ctx, chat.OutboundEvent{RunID: "x", Type: chat.EventError, Payload: map[string]any{"error": "boom"}})
	_ = r.Deliver(ctx, chat.OutboundEvent{RunID: "x", Type: chat.EventDone, Payload: map[string]any{"status": "failed"}})
	res, _ := r.Reserve("x").Wait(ctx)
	if res.Error != "boom" || res.Status != "failed" {
		t.Fatalf("res = %+v", res)
	}
	r.Drop("x")
}

func TestRouter_MessageDoneOverridesDeltas(t *testing.T) {
	r := NewRouter()
	ctx := context.Background()
	_ = r.Deliver(ctx, chat.OutboundEvent{RunID: "y", Type: chat.EventMessageDelta, Content: "partial..."})
	_ = r.Deliver(ctx, chat.OutboundEvent{RunID: "y", Type: chat.EventMessageDone, Content: "final"})
	_ = r.Deliver(ctx, chat.OutboundEvent{RunID: "y", Type: chat.EventDone})
	res, _ := r.Reserve("y").Wait(ctx)
	if res.FinalContent != "final" {
		t.Fatalf("FinalContent = %q", res.FinalContent)
	}
	r.Drop("y")
}

func TestRouter_DropClearsBuffer(t *testing.T) {
	r := NewRouter()
	ctx := context.Background()
	_ = r.Deliver(ctx, chat.OutboundEvent{RunID: "z", Type: chat.EventDone})
	r.Drop("z")
	if _, ok := r.buffers["z"]; ok {
		t.Fatalf("buffer not dropped")
	}
}

func TestRouter_IgnoresEventsWithoutRunID(t *testing.T) {
	r := NewRouter()
	if err := r.Deliver(context.Background(), chat.OutboundEvent{Type: chat.EventMessageDone, Content: "x"}); err != nil {
		t.Fatalf("Deliver: %v", err)
	}
	if len(r.buffers) != 0 {
		t.Fatalf("RunID-less event created buffer: %+v", r.buffers)
	}
}
