package agentruntime

import (
	"context"
	"strings"
	"testing"

	"github.com/aura/aura/internal/agentloop"
	"github.com/aura/aura/internal/llm"
)

func TestRunEmitsToolsStatsAndFinalEvents(t *testing.T) {
	state := &fakeState{}
	client := &fakeClient{response: agentloop.ChatResponse{Response: llm.Response{Content: "ok"}}}
	var events []Event

	result, err := Run(context.Background(), Invocation{
		Client: client,
		Executor: agentloop.ToolExecutorFunc(func(context.Context, []llm.ToolCall) agentloop.ExecutionSummary {
			return agentloop.ExecutionSummary{}
		}),
		State:                   state,
		PromptVersion:           "test",
		PromptHash:              "hash",
		PromptModules:           []string{"base"},
		Toolset:                 "registered",
		ToolsetSelectReason:     "test",
		Tools:                   []llm.ToolDefinition{{Name: "search_memory"}},
		RetrievalCapsulePresent: true,
		Options:                 agentloop.Options{MaxIterations: 1},
		OnEvent: func(event Event) {
			events = append(events, event)
		},
	})
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if result.Text != "ok" {
		t.Fatalf("Text = %q, want ok", result.Text)
	}
	if result.PromptVersion != "test" || result.PromptHash != "hash" {
		t.Fatalf("result prompt metadata = %q/%q, want test/hash", result.PromptVersion, result.PromptHash)
	}
	if len(result.ToolsExposed) != 1 || result.ToolsExposed[0] != "search_memory" {
		t.Fatalf("result tools exposed = %+v, want search_memory", result.ToolsExposed)
	}
	if result.Toolset != "registered" || result.ToolsetSelectReason != "test" {
		t.Fatalf("result toolset = %q/%q, want registered/test", result.Toolset, result.ToolsetSelectReason)
	}
	if !result.RetrievalCapsulePresent {
		t.Fatal("RetrievalCapsulePresent = false, want true")
	}
	if len(events) < 3 {
		t.Fatalf("events = %+v, want tools/stats/final", events)
	}
	if events[0].Type != EventToolsExposed || len(events[0].ToolsExposed) != 1 || events[0].ToolsExposed[0] != "search_memory" {
		t.Fatalf("first event = %+v, want tools_exposed search_memory", events[0])
	}
	if events[len(events)-1].Type != EventFinal || events[len(events)-1].Text != "ok" {
		t.Fatalf("last event = %+v, want final ok", events[len(events)-1])
	}
}

// TestRunEmitsStreamingLifecycleEvents drives a Run through one tool-call
// round + a final-answer round and asserts the full streaming/tool lifecycle
// event sequence. Slice 0's chathub expects this exact vocabulary
// (llm_start, message_delta, tool_start, tool_end) so the future adapter
// can translate 1:1 without inventing types.
func TestRunEmitsStreamingLifecycleEvents(t *testing.T) {
	state := &fakeState{}
	client := &scriptedClient{responses: []agentloop.ChatResponse{
		{Response: llm.Response{
			Content:      "",
			HasToolCalls: true,
			ToolCalls: []llm.ToolCall{
				{ID: "call-a", Name: "search_memory", Arguments: map[string]any{"query": "test", "limit": float64(5)}},
				{ID: "call-b", Name: "wiki_page", Arguments: map[string]any{"slug": "x"}},
			},
		}},
		{Response: llm.Response{Content: "all good"}},
	}}
	var events []Event
	executed := 0

	_, err := Run(context.Background(), Invocation{
		Client: client,
		Executor: agentloop.ToolExecutorFunc(func(_ context.Context, calls []llm.ToolCall) agentloop.ExecutionSummary {
			executed += len(calls)
			results := map[string]string{}
			for _, call := range calls {
				state.AddToolResultMessage(call.ID, "result for "+call.Name)
				results[call.ID] = "result for " + call.Name
			}
			return agentloop.ExecutionSummary{LastResult: "result for wiki_page", Results: results}
		}),
		State:               state,
		PromptVersion:       "v",
		Toolset:             "registered",
		ToolsetSelectReason: "test",
		Tools: []llm.ToolDefinition{
			{Name: "search_memory"},
			{Name: "wiki_page"},
		},
		Options: agentloop.Options{MaxIterations: 3},
		OnEvent: func(event Event) {
			events = append(events, event)
		},
	})
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if executed != 2 {
		t.Fatalf("executor ran %d calls, want 2", executed)
	}

	// Collapse stats events (they fire repeatedly via emitStats) before
	// asserting order — the lifecycle types are what we care about.
	var kinds []EventType
	for _, ev := range events {
		switch ev.Type {
		case EventToolsExposed, EventLLMStart, EventLLMDelta,
			EventToolStart, EventToolEnd, EventFinal:
			kinds = append(kinds, ev.Type)
		}
	}
	// Expected order: tools_exposed → llm_start → tool_start,tool_start →
	// tool_end,tool_end → llm_start → message_delta → final. The first LLM
	// round had empty Content (only tool calls), so no message_delta fires
	// there — only the second round produces a delta. Tool starts and
	// ends are batched: all starts before any ends, because they pair with
	// a single executor.ExecuteToolCalls call.
	want := []EventType{
		EventToolsExposed,
		EventLLMStart,
		EventToolStart,
		EventToolStart,
		EventToolEnd,
		EventToolEnd,
		EventLLMStart,
		EventLLMDelta,
		EventFinal,
	}
	if len(kinds) != len(want) {
		t.Fatalf("event kinds = %v, want %v", kinds, want)
	}
	for i := range want {
		if kinds[i] != want[i] {
			t.Fatalf("event[%d] = %q, want %q (full sequence: %v)", i, kinds[i], want[i], kinds)
		}
	}
}

// TestRunToolStartCarriesArgKeysNotValues asserts the CLAUDE.md value-leakage
// policy at the event-type level: a secret-shaped value in tool args must
// never appear in any emitted Event, and ToolArgKeys must contain only the
// key NAME.
func TestRunToolStartCarriesArgKeysNotValues(t *testing.T) {
	state := &fakeState{}
	client := &scriptedClient{responses: []agentloop.ChatResponse{
		{Response: llm.Response{HasToolCalls: true, ToolCalls: []llm.ToolCall{
			{ID: "call-1", Name: "store_source", Arguments: map[string]any{"api_key": "secret123", "name": "doc"}},
		}}},
		{Response: llm.Response{Content: "stored"}},
	}}
	var events []Event

	_, err := Run(context.Background(), Invocation{
		Client: client,
		Executor: agentloop.ToolExecutorFunc(func(_ context.Context, calls []llm.ToolCall) agentloop.ExecutionSummary {
			state.AddToolResultMessage(calls[0].ID, "ok")
			return agentloop.ExecutionSummary{LastResult: "ok", Results: map[string]string{calls[0].ID: "ok"}}
		}),
		State:   state,
		Options: agentloop.Options{MaxIterations: 2},
		OnEvent: func(event Event) {
			events = append(events, event)
		},
	})
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}

	var toolStart *Event
	for i, ev := range events {
		if ev.Type == EventToolStart {
			toolStart = &events[i]
			break
		}
	}
	if toolStart == nil {
		t.Fatal("no EventToolStart fired")
	}
	if got, want := toolStart.ToolArgKeys, []string{"api_key", "name"}; !equalStringSlice(got, want) {
		t.Fatalf("ToolArgKeys = %v, want %v", got, want)
	}
	// Belt-and-braces: scan every emitted event for the literal secret.
	for _, ev := range events {
		if ev.Delta == "secret123" || strings.Contains(ev.Delta, "secret123") {
			t.Fatalf("event leaked arg value in Delta: %+v", ev)
		}
		if strings.Contains(ev.ToolResultPreview, "secret123") {
			t.Fatalf("event leaked arg value in ToolResultPreview: %+v", ev)
		}
		for _, k := range ev.ToolArgKeys {
			if strings.Contains(k, "secret123") {
				t.Fatalf("event leaked arg value in ToolArgKeys: %+v", ev)
			}
		}
	}
}

// TestRunToolEndCapsResultPreview asserts the preview emitted on
// EventToolEnd is bounded at MaxToolResultPreviewChars so a chathub
// adapter can stream it without flooding the wire.
func TestRunToolEndCapsResultPreview(t *testing.T) {
	state := &fakeState{}
	// 1000-char result; far above the 200-rune cap.
	hugeResult := strings.Repeat("x", 1000)
	client := &scriptedClient{responses: []agentloop.ChatResponse{
		{Response: llm.Response{HasToolCalls: true, ToolCalls: []llm.ToolCall{
			{ID: "call-1", Name: "search_memory", Arguments: map[string]any{"query": "x"}},
		}}},
		{Response: llm.Response{Content: "done"}},
	}}
	var events []Event

	_, err := Run(context.Background(), Invocation{
		Client: client,
		Executor: agentloop.ToolExecutorFunc(func(_ context.Context, calls []llm.ToolCall) agentloop.ExecutionSummary {
			state.AddToolResultMessage(calls[0].ID, hugeResult)
			return agentloop.ExecutionSummary{LastResult: hugeResult, Results: map[string]string{calls[0].ID: hugeResult}}
		}),
		State:   state,
		Options: agentloop.Options{MaxIterations: 2},
		OnEvent: func(event Event) {
			events = append(events, event)
		},
	})
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	var toolEnd *Event
	for i, ev := range events {
		if ev.Type == EventToolEnd {
			toolEnd = &events[i]
			break
		}
	}
	if toolEnd == nil {
		t.Fatal("no EventToolEnd fired")
	}
	if len([]rune(toolEnd.ToolResultPreview)) != agentloop.MaxToolResultPreviewChars {
		t.Fatalf("ToolResultPreview length = %d runes, want %d",
			len([]rune(toolEnd.ToolResultPreview)), agentloop.MaxToolResultPreviewChars)
	}
	if !toolEnd.ToolSuccess {
		t.Fatal("ToolSuccess = false, want true (result has no Error: prefix)")
	}
}

func equalStringSlice(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

type fakeClient struct {
	response agentloop.ChatResponse
}

func (f *fakeClient) Chat(context.Context, []llm.Message, []llm.ToolDefinition) (agentloop.ChatResponse, error) {
	return f.response, nil
}

// scriptedClient returns a queued series of ChatResponses, one per call.
// Mirrors fakeLoopClient in internal/agentloop but the agentruntime test
// scope doesn't import that test type.
type scriptedClient struct {
	responses []agentloop.ChatResponse
	calls     int
}

func (s *scriptedClient) Chat(context.Context, []llm.Message, []llm.ToolDefinition) (agentloop.ChatResponse, error) {
	if s.calls >= len(s.responses) {
		return agentloop.ChatResponse{}, nil
	}
	resp := s.responses[s.calls]
	s.calls++
	return resp, nil
}

type fakeState struct {
	messages []llm.Message
}

func (f *fakeState) Messages() []llm.Message    { return f.messages }
func (f *fakeState) TrackTokens(llm.TokenUsage) {}
func (f *fakeState) AddAssistantMessage(content string) {
	f.messages = append(f.messages, llm.Message{Role: "assistant", Content: content})
}
func (f *fakeState) AddAssistantToolCallMessage(content string, calls []llm.ToolCall) {
	f.messages = append(f.messages, llm.Message{Role: "assistant", Content: content, ToolCalls: calls})
}
func (f *fakeState) AddToolResultMessage(id, content string) {
	f.messages = append(f.messages, llm.Message{Role: "tool", ToolCallID: id, Content: content})
}
