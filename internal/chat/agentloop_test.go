package chat

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"

	"github.com/aura/aura/internal/agent"
	"github.com/aura/aura/internal/llm"
)

// --- Fakes for driving agent.Run end-to-end through the adapter ----

// scriptedChatClient returns a canned LLM response on the first call, then a
// final no-tool-call answer on the second. The first response always has tool
// calls so the adapter sees a Tool lifecycle pair.
type scriptedChatClient struct {
	mu             sync.Mutex
	calls          int
	contentA       string
	contentB       string
	deliveredFinal bool
}

func (c *scriptedChatClient) Chat(_ context.Context, _ []llm.Message, _ []llm.ToolDefinition) (agent.ChatResponse, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.calls++
	if c.calls == 1 {
		return agent.ChatResponse{
			Response: llm.Response{
				Content:      c.contentA,
				HasToolCalls: true,
				ToolCalls: []llm.ToolCall{
					{ID: "call-1", Name: "search_memory", Arguments: map[string]any{"query": "secret value"}},
				},
				Usage: llm.TokenUsage{PromptTokens: 10, CompletionTokens: 5, TotalTokens: 15},
			},
		}, nil
	}
	return agent.ChatResponse{
		Response: llm.Response{
			Content: c.contentB,
			Usage:   llm.TokenUsage{PromptTokens: 12, CompletionTokens: 7, TotalTokens: 19},
		},
		Delivered: c.deliveredFinal,
	}, nil
}

type scriptedExecutor struct{}

func (scriptedExecutor) ExecuteToolCalls(_ context.Context, calls []llm.ToolCall) agent.ExecutionSummary {
	results := map[string]string{}
	for _, c := range calls {
		results[c.ID] = "ok-result-for-" + c.Name
	}
	return agent.ExecutionSummary{
		LastResult: "ok-result-for-search_memory",
		Results:    results,
	}
}

type stubState struct {
	mu       sync.Mutex
	messages []llm.Message
}

func (s *stubState) Messages() []llm.Message {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]llm.Message(nil), s.messages...)
}
func (s *stubState) TrackTokens(_ llm.TokenUsage) {}
func (s *stubState) AddAssistantMessage(content string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.messages = append(s.messages, llm.Message{Role: "assistant", Content: content})
}
func (s *stubState) AddAssistantToolCallMessage(content string, calls []llm.ToolCall) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.messages = append(s.messages, llm.Message{Role: "assistant", Content: content, ToolCalls: calls})
}
func (s *stubState) AddToolResultMessage(id, content string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.messages = append(s.messages, llm.Message{Role: "tool", Content: content, ToolCallID: id})
}

// --- Tests ----------------------------------------------------------------

func TestAgentLoopAdapter_BuildRequired(t *testing.T) {
	if _, err := NewAgentLoopAdapter(nil); err == nil {
		t.Fatal("expected error for nil builder")
	}
}

func TestAgentLoopAdapter_TranslatesRuntimeEventsToOutbound(t *testing.T) {
	chat := &scriptedChatClient{contentA: "thinking", contentB: "final answer here"}
	state := &stubState{}
	tools := []llm.ToolDefinition{{Name: "search_memory", Description: "test"}}

	builder := InvocationBuilder(func(_ context.Context, _ *Run, _ InboundMessage) (agent.Invocation, error) {
		return agent.Invocation{
			Client:        chat,
			Executor:      scriptedExecutor{},
			State:         state,
			PromptVersion: "test-v1",
			PromptHash:    "abcd1234",
			PromptModules: []string{"base"},
			Toolset:       "test",
			Tools:         tools,
			Options: agent.Options{
				MaxIterations:           2,
				AllowNoToolFinalization: true,
			},
		}, nil
	})

	adapter, err := NewAgentLoopAdapter(builder)
	if err != nil {
		t.Fatalf("NewAgentLoopAdapter: %v", err)
	}

	run := &Run{ID: "run-test", Channel: ChannelWeb, Status: RunStatusRunning, Metadata: map[string]any{}}
	msg := InboundMessage{Channel: ChannelWeb, PrincipalID: "p1", Text: "hi", Mode: DeliveryModeDeferred}

	var events []OutboundEvent
	emit := func(ev OutboundEvent) error {
		events = append(events, ev)
		return nil
	}

	if err := adapter.Run(context.Background(), run, msg, emit); err != nil {
		t.Fatalf("Run: %v", err)
	}

	// Collect event types in order.
	types := make([]EventType, 0, len(events))
	for _, ev := range events {
		types = append(types, ev.Type)
	}

	// Required ordered subset: at minimum a ToolStart -> ToolEnd pair, then
	// at least one MessageDelta from the assistant content, then MessageDone
	// + Usage at the end.
	if !containsInOrder(types, []EventType{EventToolStart, EventToolEnd, EventMessageDelta, EventMessageDone, EventUsage}) {
		t.Fatalf("event order missing required types, got %v", types)
	}

	// Verify ToolStart payload — arg KEYS only, never values.
	var toolStart OutboundEvent
	for _, ev := range events {
		if ev.Type == EventToolStart {
			toolStart = ev
			break
		}
	}
	if toolStart.Payload["tool"] != "search_memory" {
		t.Fatalf("ToolStart.tool = %v, want search_memory", toolStart.Payload["tool"])
	}
	if toolStart.Payload["tool_call_id"] != "call-1" {
		t.Fatalf("ToolStart.tool_call_id = %v, want call-1", toolStart.Payload["tool_call_id"])
	}
	argKeys, _ := toolStart.Payload["arg_keys"].([]string)
	if len(argKeys) != 1 || argKeys[0] != "query" {
		t.Fatalf("ToolStart.arg_keys = %v, want [query]", argKeys)
	}
	// CRITICAL: no value leakage. The fake passed `Arguments: {"query": "secret value"}`.
	// Adapter MUST NOT propagate "secret value" anywhere in the OutboundEvent.
	for i, ev := range events {
		raw := marshalEvent(ev)
		if strings.Contains(raw, "secret value") {
			t.Fatalf("event[%d] (%s) leaked arg VALUE: %s", i, ev.Type, raw)
		}
	}

	// Verify ToolEnd has success + elapsed_ms + preview.
	var toolEnd OutboundEvent
	for _, ev := range events {
		if ev.Type == EventToolEnd {
			toolEnd = ev
			break
		}
	}
	if toolEnd.Payload["tool"] != "search_memory" {
		t.Fatalf("ToolEnd.tool = %v", toolEnd.Payload["tool"])
	}
	if _, ok := toolEnd.Payload["success"]; !ok {
		t.Fatalf("ToolEnd missing success")
	}
	if _, ok := toolEnd.Payload["elapsed_ms"]; !ok {
		t.Fatalf("ToolEnd missing elapsed_ms")
	}

	// Verify MessageDone carries the final assistant text.
	var done OutboundEvent
	for _, ev := range events {
		if ev.Type == EventMessageDone {
			done = ev
		}
	}
	if done.Content != "final answer here" {
		t.Fatalf("MessageDone.Content = %q, want %q", done.Content, "final answer here")
	}

	// Verify Run.Metadata captured tools_exposed + stats from the runtime.
	if run.Metadata["tools_exposed"] == nil {
		t.Fatalf("Run.Metadata missing tools_exposed: %+v", run.Metadata)
	}
	if run.Metadata["stats"] == nil {
		t.Fatalf("Run.Metadata missing stats: %+v", run.Metadata)
	}
	if run.Metadata["final_text"] != "final answer here" {
		t.Fatalf("Run.Metadata.final_text = %v", run.Metadata["final_text"])
	}
}

func TestAgentLoopAdapter_BuilderError_Propagates(t *testing.T) {
	builder := InvocationBuilder(func(_ context.Context, _ *Run, _ InboundMessage) (agent.Invocation, error) {
		return agent.Invocation{}, errors.New("builder broke")
	})
	adapter, _ := NewAgentLoopAdapter(builder)
	run := &Run{ID: "x", Channel: ChannelWeb, Status: RunStatusRunning}
	msg := InboundMessage{Channel: ChannelWeb, Mode: DeliveryModeDeferred}
	err := adapter.Run(context.Background(), run, msg, func(OutboundEvent) error { return nil })
	if err == nil || !strings.Contains(err.Error(), "builder broke") {
		t.Fatalf("expected propagated builder error, got %v", err)
	}
}

func TestAgentLoopAdapter_DeliveredFinalKeepsLifecyclePreview(t *testing.T) {
	chat := &scriptedChatClient{
		contentA:       "thinking",
		contentB:       "streamed final answer",
		deliveredFinal: true,
	}
	state := &stubState{}
	builder := InvocationBuilder(func(_ context.Context, _ *Run, _ InboundMessage) (agent.Invocation, error) {
		return agent.Invocation{
			Client:   chat,
			Executor: scriptedExecutor{},
			State:    state,
			Tools:    []llm.ToolDefinition{{Name: "search_memory", Description: "test"}},
			Options: agent.Options{
				MaxIterations:           2,
				AllowNoToolFinalization: true,
			},
		}, nil
	})
	adapter, err := NewAgentLoopAdapter(builder)
	if err != nil {
		t.Fatalf("NewAgentLoopAdapter: %v", err)
	}

	run := &Run{ID: "run-telegram", Channel: ChannelTelegram, Status: RunStatusRunning, Metadata: map[string]any{}}
	msg := InboundMessage{Channel: ChannelTelegram, PrincipalID: "p1", Text: "hi", Mode: DeliveryModeStreaming}
	var done OutboundEvent
	if err := adapter.Run(context.Background(), run, msg, func(ev OutboundEvent) error {
		if ev.Type == EventMessageDone {
			done = ev
		}
		return nil
	}); err != nil {
		t.Fatalf("Run: %v", err)
	}

	if done.Content != "streamed final answer" {
		t.Fatalf("MessageDone.Content = %q, want streamed final answer", done.Content)
	}
	if delivered, _ := done.Payload["delivered"].(bool); !delivered {
		t.Fatalf("MessageDone.delivered = %v, want true", done.Payload["delivered"])
	}
	if run.Metadata["final_text"] != "streamed final answer" {
		t.Fatalf("Run.Metadata.final_text = %v", run.Metadata["final_text"])
	}
	if run.Metadata["delivered"] != true {
		t.Fatalf("Run.Metadata.delivered = %v, want true", run.Metadata["delivered"])
	}
}

func TestAgentLoopAdapter_EmptyDeltasSkipped(t *testing.T) {
	chat := &scriptedChatClient{contentA: "", contentB: "done"}
	state := &stubState{}
	builder := InvocationBuilder(func(_ context.Context, _ *Run, _ InboundMessage) (agent.Invocation, error) {
		return agent.Invocation{
			Client:   chat,
			Executor: scriptedExecutor{},
			State:    state,
			Tools:    []llm.ToolDefinition{{Name: "search_memory"}},
			Options: agent.Options{
				MaxIterations:           2,
				AllowNoToolFinalization: true,
			},
		}, nil
	})
	adapter, _ := NewAgentLoopAdapter(builder)
	run := &Run{ID: "x", Channel: ChannelWeb, Status: RunStatusRunning, Metadata: map[string]any{}}
	msg := InboundMessage{Channel: ChannelWeb, Mode: DeliveryModeDeferred}
	var events []OutboundEvent
	emit := func(ev OutboundEvent) error {
		events = append(events, ev)
		return nil
	}
	if err := adapter.Run(context.Background(), run, msg, emit); err != nil {
		t.Fatalf("Run: %v", err)
	}
	// The first LLM response has empty content, so we expect ZERO
	// MessageDelta events with empty Content — the second response has
	// content "done" and produces exactly one delta.
	deltaCount := 0
	for _, ev := range events {
		if ev.Type == EventMessageDelta {
			if ev.Content == "" {
				t.Fatalf("empty delta should have been skipped: %+v", ev)
			}
			deltaCount++
		}
	}
	if deltaCount != 1 {
		t.Fatalf("expected 1 non-empty delta, got %d (events=%v)", deltaCount, events)
	}
}

// --- Helpers --------------------------------------------------------------

// containsInOrder checks haystack contains needle elements in the given order
// (not necessarily contiguous). Mirrors test patterns in agentruntime tests.
func containsInOrder(haystack, needle []EventType) bool {
	i := 0
	for _, h := range haystack {
		if i >= len(needle) {
			return true
		}
		if h == needle[i] {
			i++
		}
	}
	return i >= len(needle)
}

// marshalEvent renders an OutboundEvent's content+payload into a string for
// substring-leakage checks. We don't import encoding/json here to avoid
// pulling extra dependencies into the test — a sloppy Sprintf is enough for
// the leak detector.
func marshalEvent(ev OutboundEvent) string {
	parts := []string{string(ev.Type), ev.Content}
	for k, v := range ev.Payload {
		parts = append(parts, k)
		switch vv := v.(type) {
		case string:
			parts = append(parts, vv)
		case []string:
			parts = append(parts, strings.Join(vv, ","))
		}
	}
	return strings.Join(parts, "|")
}
