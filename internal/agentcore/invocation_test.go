package agentcore

import (
	"context"
	"io"
	"log/slog"
	"testing"

	"github.com/aura/aura/internal/agent"
	"github.com/aura/aura/internal/agent/agentdef"
	"github.com/aura/aura/internal/llm"
)

type stubChatClient struct{}

func (stubChatClient) Chat(context.Context, []llm.Message, []llm.ToolDefinition) (agent.ChatResponse, error) {
	return agent.ChatResponse{}, nil
}

type stubToolExecutor struct{}

func (stubToolExecutor) ExecuteToolCalls(context.Context, []llm.ToolCall) agent.ExecutionSummary {
	return agent.ExecutionSummary{}
}

type stubState struct{}

func (stubState) Messages() []llm.Message                            { return nil }
func (stubState) TrackTokens(llm.TokenUsage)                         {}
func (stubState) AddAssistantMessage(string)                         {}
func (stubState) AddAssistantToolCallMessage(string, []llm.ToolCall) {}
func (stubState) AddToolResultMessage(string, string)                {}

func TestBuilderBuildsInvocation(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	toolDefs := []llm.ToolDefinition{{Name: "search"}}
	promptModules := []string{"base", "tools"}
	agentDefs, err := agentdef.NewRegistry(context.Background(), nil, nil)
	if err != nil {
		t.Fatalf("NewRegistry: %v", err)
	}

	inv, err := (Builder{}).Build(InvocationInput{
		Client:              stubChatClient{},
		Executor:            stubToolExecutor{},
		State:               stubState{},
		PromptVersion:       "prompt-v1",
		PromptHash:          "hash",
		PromptModules:       promptModules,
		Toolset:             "registered",
		ToolsetSelectReason: "core",
		Tools:               toolDefs,
		ToolsProvider:       func() []llm.ToolDefinition { return toolDefs },
		Options:             agent.Options{MaxIterations: 7},
		AgentDefs:           agentDefs,
		Logger:              logger,
	})
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	promptModules[0] = "mutated"
	toolDefs[0].Name = "mutated"

	if inv.Client == nil || inv.Executor == nil || inv.State == nil {
		t.Fatalf("missing runtime fields: %+v", inv)
	}
	if inv.PromptVersion != "prompt-v1" || inv.PromptHash != "hash" {
		t.Fatalf("prompt metadata = %q/%q", inv.PromptVersion, inv.PromptHash)
	}
	if inv.PromptModules[0] != "base" {
		t.Fatalf("PromptModules aliased caller slice: %+v", inv.PromptModules)
	}
	if inv.Tools[0].Name != "search" {
		t.Fatalf("Tools aliased caller slice: %+v", inv.Tools)
	}
	if inv.ToolsProvider == nil || inv.Options.MaxIterations != 7 || inv.Logger != logger || inv.AgentDefs != agentDefs {
		t.Fatalf("invocation fields not preserved: %+v", inv)
	}
}

func TestBuilderRequiresRuntimeFields(t *testing.T) {
	tests := []struct {
		name  string
		input InvocationInput
		want  string
	}{
		{
			name: "missing client",
			input: InvocationInput{
				Executor: stubToolExecutor{},
				State:    stubState{},
			},
			want: "agentcore: chat client is required",
		},
		{
			name: "missing executor",
			input: InvocationInput{
				Client: stubChatClient{},
				State:  stubState{},
			},
			want: "agentcore: tool executor is required",
		},
		{
			name: "missing state",
			input: InvocationInput{
				Client:   stubChatClient{},
				Executor: stubToolExecutor{},
			},
			want: "agentcore: state is required",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := (Builder{}).Build(tt.input)
			if err == nil || err.Error() != tt.want {
				t.Fatalf("err = %v, want %q", err, tt.want)
			}
		})
	}
}
