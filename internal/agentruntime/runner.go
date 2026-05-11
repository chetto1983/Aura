package agentruntime

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"log/slog"
	"time"

	"github.com/aura/aura/internal/agentloop"
	"github.com/aura/aura/internal/llm"
)

// newRunID returns an 8-byte hex correlation ID per Run invocation (F-024).
// Falls back to a fixed sentinel when crypto/rand is unavailable so structured
// logs never end up with an empty run_id field.
func newRunID() string {
	var buf [8]byte
	if _, err := rand.Read(buf[:]); err != nil {
		return "00000000"
	}
	return hex.EncodeToString(buf[:])
}

type EventType string

const (
	EventToolsExposed EventType = "tools_exposed"
	EventStats        EventType = "stats"
	EventFinal        EventType = "final"
)

type Event struct {
	Type                EventType
	RunID               string
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
	ToolsProvider           func() []llm.ToolDefinition
	RetrievalCapsulePresent bool
	Options                 agentloop.Options
	OnEvent                 func(Event)
	// Logger is the structured logger every Run uses. Nil falls back to
	// slog.Default(). The runner attaches a per-invocation run_id correlation
	// ID so multi-conversation logs can be disentangled (F-011, F-024).
	Logger *slog.Logger
}

type Result struct {
	Text                    string
	Delivered               bool
	Stats                   agentloop.Stats
	RunID                   string
	PromptVersion           string
	PromptHash              string
	PromptModules           []string
	Toolset                 string
	ToolsetSelectReason     string
	ToolsExposed            []string
	RetrievalCapsulePresent bool
}

func Run(ctx context.Context, in Invocation) (Result, error) {
	logger := in.Logger
	if logger == nil {
		logger = slog.Default()
	}
	runID := newRunID()
	logger = logger.With("run_id", runID, "toolset", in.Toolset, "prompt_version", in.PromptVersion)
	opts := in.Options
	opts.Tools = in.Tools
	opts.ToolsProvider = in.ToolsProvider
	if opts.Logger == nil {
		opts.Logger = logger
	}
	tools := in.Tools
	if in.ToolsProvider != nil {
		tools = in.ToolsProvider()
	}
	toolsExposed := toolDefinitionNames(tools)
	previousOnStats := opts.OnStats
	opts.OnStats = func(stats agentloop.Stats) {
		if previousOnStats != nil {
			previousOnStats(stats)
		}
		emit(in, Event{Type: EventStats, RunID: runID, Stats: stats})
	}

	emit(in, Event{
		Type:                EventToolsExposed,
		RunID:               runID,
		ToolsExposed:        toolsExposed,
		PromptVersion:       in.PromptVersion,
		PromptHash:          in.PromptHash,
		PromptModules:       append([]string(nil), in.PromptModules...),
		Toolset:             in.Toolset,
		ToolsetSelectReason: in.ToolsetSelectReason,
	})

	start := time.Now()
	logger.Info("agentruntime: run start", "tools_exposed", len(toolsExposed))
	result, err := agentloop.Run(ctx, in.Client, in.Executor, in.State, opts)
	out := Result{
		Text:                    result.Text,
		Delivered:               result.Delivered,
		Stats:                   result.Stats,
		RunID:                   runID,
		PromptVersion:           in.PromptVersion,
		PromptHash:              in.PromptHash,
		PromptModules:           append([]string(nil), in.PromptModules...),
		Toolset:                 in.Toolset,
		ToolsetSelectReason:     in.ToolsetSelectReason,
		ToolsExposed:            append([]string(nil), toolsExposed...),
		RetrievalCapsulePresent: in.RetrievalCapsulePresent,
	}
	level := slog.LevelInfo
	if err != nil {
		level = slog.LevelWarn
	}
	logger.Log(ctx, level, "agentruntime: run end",
		"elapsed_ms", time.Since(start).Milliseconds(),
		"llm_calls", result.Stats.LLMCalls,
		"tool_calls", result.Stats.ToolCalls,
		"delivered", result.Delivered,
		"max_iterations_hit", result.Stats.MaxIterationsHit,
		"max_elapsed_hit", result.Stats.MaxElapsedHit,
		"error", err,
	)
	emit(in, Event{Type: EventFinal, RunID: runID, Text: out.Text, Delivered: out.Delivered, Stats: out.Stats})
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
