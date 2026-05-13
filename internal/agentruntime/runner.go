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
	// EventLLMStart fires immediately before each LLM round-trip. Carries
	// iteration number, message count handed to the model, and tool count
	// exposed. Wave 3.0 Step 2 — mirrors the chathub run_started / round
	// lifecycle so a future chathub adapter can emit a streaming message
	// envelope around each round.
	EventLLMStart EventType = "llm_start"
	// EventLLMDelta fires after each LLM round returns. Today the
	// underlying ChatClient is one-shot so this fires ONCE per LLM call
	// with the full assistant content. When a streaming ChatClient lands
	// it will fire N times per call without breaking consumers. String
	// value matches the public chathub.EventMessageDelta wire name.
	EventLLMDelta EventType = "message_delta"
	// EventToolStart fires before a fresh tool call dispatches. ToolArgKeys
	// carries the sorted set of top-level argument key names — NEVER
	// values, per the CLAUDE.md value-leakage policy.
	EventToolStart EventType = "tool_start"
	// EventToolEnd fires after a fresh tool call completes. ToolSuccess is
	// false when the result starts with the "Error:" sentinel. ToolElapsedMs
	// reports the WHOLE batch duration (calls in the same batch share a
	// timestamp because ExecuteToolCalls is batch-atomic).
	EventToolEnd EventType = "tool_end"
	// EventQuestionRequested is declared for forward compatibility with
	// chathub's question state machine (PRD §9.2). Slice 6 will fire this
	// when a tool result indicates an interactive question is needed;
	// today no tool produces such a result so the constant stays unfired.
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
	Stats               agentloop.Stats
	Text                string
	Delivered           bool
	// LLMStart fields. Iteration is 0-based; MessagesIn is the count after
	// governance trimming; ToolsIn is the size of the per-turn tool pool.
	Iteration  int
	MessagesIn int
	ToolsIn    int
	// Tool lifecycle fields. ToolName is set on ToolStart/ToolEnd/
	// QuestionRequested; ToolCallID only on the tool events. ToolArgKeys
	// is the sorted argument key-name set on ToolStart (NEVER values).
	ToolName    string
	ToolCallID  string
	ToolArgKeys []string
	// ToolSuccess / ToolElapsedMs / ToolResultPreview ride EventToolEnd.
	// ToolResultPreview is capped at agentloop.MaxToolResultPreviewChars
	// (~200 runes) — just enough to tell success from "Error: ...".
	ToolSuccess       bool
	ToolElapsedMs     int64
	ToolResultPreview string
	// Delta is the partial (or full, today) assistant text on
	// EventLLMDelta. Kept distinct from Text so consumers don't confuse
	// streaming chunks with the EventFinal answer.
	Delta string
	// QuestionPayload rides EventQuestionRequested. Schema lands in Slice 6;
	// today the field is declared so the Event struct shape is stable.
	QuestionPayload map[string]any
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
	// Wave 3.0 Step 2: install proxy callbacks for the streaming/tool
	// lifecycle events. Same pattern as OnStats above — preserve any
	// user-supplied callback by calling it first, then emit the matching
	// agentruntime.Event. Adapters that don't care about a kind get a
	// no-op switch case in their OnEvent handler.
	previousOnLLMStart := opts.OnLLMStart
	opts.OnLLMStart = func(iteration, messagesIn, toolsIn int) {
		if previousOnLLMStart != nil {
			previousOnLLMStart(iteration, messagesIn, toolsIn)
		}
		emit(in, Event{
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
		emit(in, Event{Type: EventLLMDelta, RunID: runID, Delta: deltaText})
	}
	previousOnToolStart := opts.OnToolStart
	opts.OnToolStart = func(call llm.ToolCall, argKeys []string) {
		if previousOnToolStart != nil {
			previousOnToolStart(call, argKeys)
		}
		emit(in, Event{
			Type:        EventToolStart,
			RunID:       runID,
			ToolName:    call.Name,
			ToolCallID:  call.ID,
			ToolArgKeys: append([]string(nil), argKeys...),
		})
	}
	previousOnToolEnd := opts.OnToolEnd
	opts.OnToolEnd = func(callID, toolName string, success bool, elapsed time.Duration, preview string) {
		if previousOnToolEnd != nil {
			previousOnToolEnd(callID, toolName, success, elapsed, preview)
		}
		emit(in, Event{
			Type:              EventToolEnd,
			RunID:             runID,
			ToolName:          toolName,
			ToolCallID:        callID,
			ToolSuccess:       success,
			ToolElapsedMs:     elapsed.Milliseconds(),
			ToolResultPreview: preview,
		})
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
