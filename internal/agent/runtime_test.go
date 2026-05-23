package agent

import (
	"context"
	"strings"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/aura/aura/internal/llm"
)

func TestRunEmitsToolsStatsAndFinalEvents(t *testing.T) {
	state := &fakeRTState{}
	client := &fakeRTClient{response: ChatResponse{Response: llm.Response{Content: "ok"}}}
	var events []Event

	result, err := Run(context.Background(), Invocation{
		Client: client,
		Executor: ToolExecutorFunc(func(context.Context, []llm.ToolCall) ExecutionSummary {
			return ExecutionSummary{}
		}),
		State:               state,
		PromptVersion:       "test",
		PromptHash:          "hash",
		PromptModules:       []string{"base"},
		Toolset:             "registered",
		ToolsetSelectReason: "test",
		Tools:               []llm.ToolDefinition{{Name: "search_memory"}},
		Options:             Options{MaxIterations: 1},
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
// event sequence.
func TestRunEmitsStreamingLifecycleEvents(t *testing.T) {
	state := &fakeRTState{}
	client := &scriptedRTClient{responses: []ChatResponse{
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
	var executed atomic.Int64

	_, err := Run(context.Background(), Invocation{
		Client: client,
		Executor: ToolExecutorFunc(func(_ context.Context, calls []llm.ToolCall) ExecutionSummary {
			executed.Add(int64(len(calls)))
			results := map[string]string{}
			for _, call := range calls {
				state.AddToolResultMessage(call.ID, "result for "+call.Name)
				results[call.ID] = "result for " + call.Name
			}
			return ExecutionSummary{LastResult: "result for wiki_page", Results: results}
		}),
		State:               state,
		PromptVersion:       "v",
		Toolset:             "registered",
		ToolsetSelectReason: "test",
		Tools: []llm.ToolDefinition{
			{Name: "search_memory"},
			{Name: "wiki_page"},
		},
		Options: Options{MaxIterations: 3},
		OnEvent: func(event Event) {
			events = append(events, event)
		},
	})
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if got := executed.Load(); got != 2 {
		t.Fatalf("executor ran %d calls, want 2", got)
	}

	var kinds []EventType
	for _, ev := range events {
		switch ev.Type {
		case EventToolsExposed, EventLLMStart, EventLLMDelta,
			EventToolStart, EventToolEnd, EventFinal:
			kinds = append(kinds, ev.Type)
		}
	}
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
// policy at the event-type level.
func TestRunToolStartCarriesArgKeysNotValues(t *testing.T) {
	state := &fakeRTState{}
	client := &scriptedRTClient{responses: []ChatResponse{
		{Response: llm.Response{HasToolCalls: true, ToolCalls: []llm.ToolCall{
			{ID: "call-1", Name: "store_source", Arguments: map[string]any{"api_key": "secret123", "name": "doc"}},
		}}},
		{Response: llm.Response{Content: "stored"}},
	}}
	var events []Event

	_, err := Run(context.Background(), Invocation{
		Client: client,
		Executor: ToolExecutorFunc(func(_ context.Context, calls []llm.ToolCall) ExecutionSummary {
			state.AddToolResultMessage(calls[0].ID, "ok")
			return ExecutionSummary{LastResult: "ok", Results: map[string]string{calls[0].ID: "ok"}}
		}),
		State:   state,
		Options: Options{MaxIterations: 2},
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
	if got, want := toolStart.ToolArgKeys, []string{"api_key", "name"}; !equalStringSlices(got, want) {
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

// TestRunToolEndCapsResultPreview asserts the preview is bounded at MaxToolResultPreviewChars.
func TestRunToolEndCapsResultPreview(t *testing.T) {
	state := &fakeRTState{}
	hugeResult := strings.Repeat("x", 1000)
	client := &scriptedRTClient{responses: []ChatResponse{
		{Response: llm.Response{HasToolCalls: true, ToolCalls: []llm.ToolCall{
			{ID: "call-1", Name: "search_memory", Arguments: map[string]any{"query": "x"}},
		}}},
		{Response: llm.Response{Content: "done"}},
	}}
	var events []Event

	_, err := Run(context.Background(), Invocation{
		Client: client,
		Executor: ToolExecutorFunc(func(_ context.Context, calls []llm.ToolCall) ExecutionSummary {
			state.AddToolResultMessage(calls[0].ID, hugeResult)
			return ExecutionSummary{LastResult: hugeResult, Results: map[string]string{calls[0].ID: hugeResult}}
		}),
		State:   state,
		Options: Options{MaxIterations: 2},
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
	if len([]rune(toolEnd.ToolResultPreview)) != MaxToolResultPreviewChars {
		t.Fatalf("ToolResultPreview length = %d runes, want %d",
			len([]rune(toolEnd.ToolResultPreview)), MaxToolResultPreviewChars)
	}
	if !toolEnd.ToolSuccess {
		t.Fatal("ToolSuccess = false, want true (result has no Error: prefix)")
	}
}

func equalStringSlices(a, b []string) bool {
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

type fakeRTClient struct {
	response ChatResponse
}

func (f *fakeRTClient) Chat(context.Context, []llm.Message, []llm.ToolDefinition) (ChatResponse, error) {
	return f.response, nil
}

// scriptedRTClient returns a queued series of ChatResponses, one per call.
type scriptedRTClient struct {
	responses []ChatResponse
	calls     int
}

func (s *scriptedRTClient) Chat(context.Context, []llm.Message, []llm.ToolDefinition) (ChatResponse, error) {
	if s.calls >= len(s.responses) {
		return ChatResponse{}, nil
	}
	resp := s.responses[s.calls]
	s.calls++
	return resp, nil
}

type fakeRTState struct {
	mu       sync.Mutex
	messages []llm.Message
}

func (f *fakeRTState) Messages() []llm.Message {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]llm.Message(nil), f.messages...)
}
func (f *fakeRTState) TrackTokens(llm.TokenUsage) {}
func (f *fakeRTState) AddAssistantMessage(content string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.messages = append(f.messages, llm.Message{Role: "assistant", Content: content})
}
func (f *fakeRTState) AddAssistantToolCallMessage(content string, calls []llm.ToolCall) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.messages = append(f.messages, llm.Message{Role: "assistant", Content: content, ToolCalls: calls})
}
func (f *fakeRTState) AddToolResultMessage(id, content string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.messages = append(f.messages, llm.Message{Role: "tool", ToolCallID: id, Content: content})
}
