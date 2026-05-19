package agent

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"log/slog"
	"sync"
	"time"

	tools "github.com/aura/aura/internal/agent/tools/registry"
	"github.com/aura/aura/internal/identity"
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
	// EventLLMStart fires immediately before each LLM round-trip.
	EventLLMStart EventType = "llm_start"
	// EventLLMDelta fires after each LLM round returns.
	EventLLMDelta EventType = "message_delta"
	// EventToolStart fires before a fresh tool call dispatches.
	EventToolStart EventType = "tool_start"
	// EventToolEnd fires after a fresh tool call completes.
	EventToolEnd EventType = "tool_end"
	// EventQuestionRequested is declared for forward compatibility.
	EventQuestionRequested EventType = "question_requested"
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
	Stats               Stats
	Text                string
	Delivered           bool
	Iteration           int
	MessagesIn          int
	ToolsIn             int
	ToolName            string
	ToolCallID          string
	ToolArgKeys         []string
	ToolSuccess         bool
	ToolElapsedMs       int64
	ToolResultPreview   string
	Delta               string
	QuestionPayload     map[string]any
}

type Invocation struct {
	Client              ChatClient
	Executor            ToolExecutor
	State               State
	PromptVersion       string
	PromptHash          string
	PromptModules       []string
	Toolset             string
	ToolsetSelectReason string
	Tools               []llm.ToolDefinition
	ToolsProvider       func() []llm.ToolDefinition
	Options             Options
	OnEvent             func(Event)
	PostTurn            PostTurnConfig
	// Logger is the structured logger every Run uses. Nil falls back to
	// slog.Default(). The runner attaches a per-invocation run_id correlation
	// ID so multi-conversation logs can be disentangled (F-011, F-024).
	Logger *slog.Logger
}

// InvocationResult is the result of a Run call. Named InvocationResult to
// avoid shadowing agent.Result, the background-task result type.
type InvocationResult struct {
	Text                string
	Delivered           bool
	Stats               Stats
	RunID               string
	PromptVersion       string
	PromptHash          string
	PromptModules       []string
	Toolset             string
	ToolsetSelectReason string
	ToolsExposed        []string
}

func Run(ctx context.Context, in Invocation) (InvocationResult, error) {
	logger := in.Logger
	if logger == nil {
		logger = slog.Default()
	}
	runID := newRunID()
	durableRunID := identity.RunIDFromContext(ctx)
	if durableRunID == "" {
		durableRunID = runID
	}
	logger = logger.With("run_id", runID, "toolset", in.Toolset, "prompt_version", in.PromptVersion)
	opts := in.Options
	opts.Tools = in.Tools
	opts.ToolsProvider = in.ToolsProvider
	if opts.Logger == nil {
		opts.Logger = logger
	}
	toolDefs := in.Tools
	if in.ToolsProvider != nil {
		toolDefs = in.ToolsProvider()
	}
	toolsExposed := toolDefinitionNames(toolDefs)
	previousOnStats := opts.OnStats
	opts.OnStats = func(stats Stats) {
		if previousOnStats != nil {
			previousOnStats(stats)
		}
		emitEvent(in, Event{Type: EventStats, RunID: runID, Stats: stats})
	}
	previousOnLLMStart := opts.OnLLMStart
	opts.OnLLMStart = func(iteration, messagesIn, toolsIn int) {
		if previousOnLLMStart != nil {
			previousOnLLMStart(iteration, messagesIn, toolsIn)
		}
		emitEvent(in, Event{
			Type:       EventLLMStart,
			RunID:      runID,
			Iteration:  iteration,
			MessagesIn: messagesIn,
			ToolsIn:    toolsIn,
		})
	}
	previousOnLLMDelta := opts.OnLLMDelta
	opts.OnLLMDelta = func(deltaText string) {
		if previousOnLLMDelta != nil {
			previousOnLLMDelta(deltaText)
		}
		emitEvent(in, Event{Type: EventLLMDelta, RunID: runID, Delta: deltaText})
	}
	previousOnToolStart := opts.OnToolStart
	opts.OnToolStart = func(call llm.ToolCall, argKeys []string) {
		if previousOnToolStart != nil {
			previousOnToolStart(call, argKeys)
		}
		emitEvent(in, Event{
			Type:        EventToolStart,
			RunID:       runID,
			ToolName:    call.Name,
			ToolCallID:  call.ID,
			ToolArgKeys: append([]string(nil), argKeys...),
		})
	}
	var turnMu sync.Mutex
	var turnToolCalls []TurnToolCall
	previousOnToolEnd := opts.OnToolEnd
	opts.OnToolEnd = func(callID, toolName string, success bool, elapsed time.Duration, preview string) {
		if previousOnToolEnd != nil {
			previousOnToolEnd(callID, toolName, success, elapsed, preview)
		}
		turnMu.Lock()
		turnToolCalls = append(turnToolCalls, TurnToolCall{
			ID:            callID,
			Name:          toolName,
			Success:       success,
			Elapsed:       elapsed,
			ResultPreview: preview,
		})
		turnMu.Unlock()
		emitEvent(in, Event{
			Type:              EventToolEnd,
			RunID:             runID,
			ToolName:          toolName,
			ToolCallID:        callID,
			ToolSuccess:       success,
			ToolElapsedMs:     elapsed.Milliseconds(),
			ToolResultPreview: preview,
		})
	}

	previousOnQuestionRequested := opts.OnQuestionRequested
	opts.OnQuestionRequested = func(e *tools.ErrAwaitingUserInput) {
		if previousOnQuestionRequested != nil {
			previousOnQuestionRequested(e)
		}
		payload := map[string]any{
			"question": e.Question,
			"kind":     e.Kind,
		}
		if e.ToolCallID != "" {
			payload["tool_call_id"] = e.ToolCallID
		}
		if len(e.Options) > 0 {
			payload["options"] = append([]string(nil), e.Options...)
		}
		emitEvent(in, Event{
			Type:            EventQuestionRequested,
			RunID:           runID,
			ToolName:        "ask_user",
			QuestionPayload: payload,
		})
	}

	emitEvent(in, Event{
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
	logger.Info("agent: invocation start", "tools_exposed", len(toolsExposed))
	result, err := runLoop(ctx, in.Client, in.Executor, in.State, opts)
	if len(in.PostTurn.Hooks) > 0 && in.PostTurn.Store != nil {
		turn := postTurnRecordFromState(in.PostTurn.Record, in.State, durableRunID)
		turnMu.Lock()
		turn.ToolCalls = append(turn.ToolCalls, turnToolCalls...)
		turnMu.Unlock()
		postCfg := in.PostTurn
		postCfg.Record = turn
		FirePostTurnHooks(context.WithoutCancel(ctx), postCfg, logger)
	}
	out := InvocationResult{
		Text:                result.Text,
		Delivered:           result.Delivered,
		Stats:               result.Stats,
		RunID:               runID,
		PromptVersion:       in.PromptVersion,
		PromptHash:          in.PromptHash,
		PromptModules:       append([]string(nil), in.PromptModules...),
		Toolset:             in.Toolset,
		ToolsetSelectReason: in.ToolsetSelectReason,
		ToolsExposed:        append([]string(nil), toolsExposed...),
	}
	level := slog.LevelInfo
	if err != nil {
		level = slog.LevelWarn
	}
	logger.Log(ctx, level, "agent: invocation end",
		"elapsed_ms", time.Since(start).Milliseconds(),
		"llm_calls", result.Stats.LLMCalls,
		"tool_calls", result.Stats.ToolCalls,
		"delivered", result.Delivered,
		"max_iterations_hit", result.Stats.MaxIterationsHit,
		"max_elapsed_hit", result.Stats.MaxElapsedHit,
		"error", err,
	)
	emitEvent(in, Event{Type: EventFinal, RunID: runID, Text: out.Text, Delivered: out.Delivered, Stats: out.Stats})
	return out, err
}

func emitEvent(in Invocation, event Event) {
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
