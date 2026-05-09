package agentruntime

import (
	"context"

	"github.com/aura/aura/internal/agentloop"
	"github.com/aura/aura/internal/llm"
)

type EventType string

const (
	EventToolsExposed EventType = "tools_exposed"
	EventStats        EventType = "stats"
	EventFinal        EventType = "final"
)

type Event struct {
	Type                EventType
	PromptVersion       string
	PromptHash          string
	PromptModules       []string
	Toolset             string
	ToolsetSelectReason string
	ToolsExposed        []string
	Stats               agentloop.Stats
	Text                string
	Delivered           bool
}

type Invocation struct {
	Client                  agentloop.ChatClient
	Executor                agentloop.ToolExecutor
	State                   agentloop.State
	PromptVersion           string
	PromptHash              string
	PromptModules           []string
	Toolset                 string
	ToolsetSelectReason     string
	Tools                   []llm.ToolDefinition
	RetrievalCapsulePresent bool
	Options                 agentloop.Options
	OnEvent                 func(Event)
}

type Result struct {
	Text                    string
	Delivered               bool
	Stats                   agentloop.Stats
	PromptVersion           string
	PromptHash              string
	PromptModules           []string
	Toolset                 string
	ToolsetSelectReason     string
	ToolsExposed            []string
	RetrievalCapsulePresent bool
}

func Run(ctx context.Context, in Invocation) (Result, error) {
	opts := in.Options
	opts.Tools = in.Tools
	toolsExposed := toolDefinitionNames(in.Tools)
	previousOnStats := opts.OnStats
	opts.OnStats = func(stats agentloop.Stats) {
		if previousOnStats != nil {
			previousOnStats(stats)
		}
		emit(in, Event{Type: EventStats, Stats: stats})
	}

	emit(in, Event{
		Type:                EventToolsExposed,
		ToolsExposed:        toolsExposed,
		PromptVersion:       in.PromptVersion,
		PromptHash:          in.PromptHash,
		PromptModules:       append([]string(nil), in.PromptModules...),
		Toolset:             in.Toolset,
		ToolsetSelectReason: in.ToolsetSelectReason,
	})

	result, err := agentloop.Run(ctx, in.Client, in.Executor, in.State, opts)
	out := Result{
		Text:                    result.Text,
		Delivered:               result.Delivered,
		Stats:                   result.Stats,
		PromptVersion:           in.PromptVersion,
		PromptHash:              in.PromptHash,
		PromptModules:           append([]string(nil), in.PromptModules...),
		Toolset:                 in.Toolset,
		ToolsetSelectReason:     in.ToolsetSelectReason,
		ToolsExposed:            append([]string(nil), toolsExposed...),
		RetrievalCapsulePresent: in.RetrievalCapsulePresent,
	}
	emit(in, Event{Type: EventFinal, Text: out.Text, Delivered: out.Delivered, Stats: out.Stats})
	return out, err
}

func emit(in Invocation, event Event) {
	if in.OnEvent != nil {
		in.OnEvent(event)
	}
}

func toolDefinitionNames(defs []llm.ToolDefinition) []string {
	out := make([]string, 0, len(defs))
	for _, def := range defs {
		out = append(out, def.Name)
	}
	return out
}
