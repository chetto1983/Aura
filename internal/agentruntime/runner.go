package agentruntime

import (
	"context"

	"github.com/aura/aura/internal/agentloop"
	"github.com/aura/aura/internal/llm"
	"github.com/aura/aura/internal/orchestration"
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
	Toolset             orchestration.Toolset
	ToolsetSelectReason string
	ToolsExposed        []string
	Stats               agentloop.Stats
	Text                string
	Delivered           bool
}

type Invocation struct {
	Client          agentloop.ChatClient
	Executor        agentloop.ToolExecutor
	State           agentloop.State
	PromptPlan      orchestration.PromptPlan
	ToolsetDecision orchestration.ToolsetDecision
	Tools           []llm.ToolDefinition
	Options         agentloop.Options
	OnEvent         func(Event)
}

type Result struct {
	Text      string
	Delivered bool
	Stats     agentloop.Stats
}

func Run(ctx context.Context, in Invocation) (Result, error) {
	opts := in.Options
	opts.Tools = in.Tools
	previousOnStats := opts.OnStats
	opts.OnStats = func(stats agentloop.Stats) {
		if previousOnStats != nil {
			previousOnStats(stats)
		}
		emit(in, Event{Type: EventStats, Stats: stats})
	}

	emit(in, Event{
		Type:                EventToolsExposed,
		ToolsExposed:        toolDefinitionNames(in.Tools),
		PromptVersion:       in.PromptPlan.Version,
		PromptHash:          in.PromptPlan.Hash,
		PromptModules:       append([]string(nil), in.PromptPlan.Modules...),
		Toolset:             in.ToolsetDecision.Toolset,
		ToolsetSelectReason: in.ToolsetDecision.Reason,
	})

	result, err := agentloop.Run(ctx, in.Client, in.Executor, in.State, opts)
	out := Result{Text: result.Text, Delivered: result.Delivered, Stats: result.Stats}
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
