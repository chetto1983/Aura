package agentcore

import (
	"errors"
	"log/slog"

	"github.com/aura/aura/internal/agent"
	"github.com/aura/aura/internal/llm"
)

// Builder owns channel-neutral agent.Invocation assembly. Channel adapters
// prepare transport-specific clients/hooks, then hand the common runtime pieces
// here so the final Invocation shape has one owner.
type Builder struct{}

// InvocationInput is the channel-neutral contract for one agent turn.
type InvocationInput struct {
	Client              agent.ChatClient
	Executor            agent.ToolExecutor
	State               agent.State
	PromptVersion       string
	PromptHash          string
	PromptModules       []string
	Toolset             string
	ToolsetSelectReason string
	Tools               []llm.ToolDefinition
	ToolsProvider       func() []llm.ToolDefinition
	Options             agent.Options
	OnEvent             func(agent.Event)
	PostTurn            agent.PostTurnConfig
	Logger              *slog.Logger
}

// Build validates required runtime pieces and returns a complete
// agent.Invocation. Slices are copied so callers can keep mutating local
// planning data without changing the already-built invocation.
func (Builder) Build(input InvocationInput) (agent.Invocation, error) {
	if input.Client == nil {
		return agent.Invocation{}, errors.New("agentcore: chat client is required")
	}
	if input.Executor == nil {
		return agent.Invocation{}, errors.New("agentcore: tool executor is required")
	}
	if input.State == nil {
		return agent.Invocation{}, errors.New("agentcore: state is required")
	}
	return agent.Invocation{
		Client:              input.Client,
		Executor:            input.Executor,
		State:               input.State,
		PromptVersion:       input.PromptVersion,
		PromptHash:          input.PromptHash,
		PromptModules:       append([]string(nil), input.PromptModules...),
		Toolset:             input.Toolset,
		ToolsetSelectReason: input.ToolsetSelectReason,
		Tools:               append([]llm.ToolDefinition(nil), input.Tools...),
		ToolsProvider:       input.ToolsProvider,
		Options:             input.Options,
		OnEvent:             input.OnEvent,
		PostTurn:            input.PostTurn,
		Logger:              input.Logger,
	}, nil
}
