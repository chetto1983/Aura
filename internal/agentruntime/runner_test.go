package agentruntime

import (
	"context"
	"testing"

	"github.com/aura/aura/internal/agentloop"
	"github.com/aura/aura/internal/llm"
	"github.com/aura/aura/internal/orchestration"
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
		State: state,
		PromptPlan: orchestration.PromptPlan{
			Version: "test",
			Hash:    "hash",
			Modules: []string{"base"},
		},
		ToolsetDecision: orchestration.ToolsetDecision{Toolset: orchestration.ToolsetDefault, Reason: "test"},
		Tools:           []llm.ToolDefinition{{Name: "search_memory"}},
		Options:         agentloop.Options{MaxIterations: 1},
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

type fakeClient struct {
	response agentloop.ChatResponse
}

func (f *fakeClient) Chat(context.Context, []llm.Message, []llm.ToolDefinition) (agentloop.ChatResponse, error) {
	return f.response, nil
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
