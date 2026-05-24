package chat

import (
	"context"
	"errors"
	"fmt"
	"maps"

	"github.com/aura/aura/internal/agent"
	"github.com/aura/aura/internal/conversation"
)

// InvocationBuilder is the dependency-injection seam between chat Hub and the
// concrete agent.Invocation that drives a single agent turn. The
// production wiring (telegram/setup.go) installs a builder that closes over
// the LLM client, tool registry, per-channel executor (the Telegram bot's
// streaming consumer, the web buffered consumer, …) and per-turn prompt
// composition. Tests inject a builder that fabricates a canned Invocation.
//
// The builder receives the channel-neutral InboundMessage AND the immutable
// Run record (already in RunStatusRunning) so the implementation can mint a
// per-turn logger, attach the run_id correlation, and resolve the tool pool
// for the principal. ChannelData on the InboundMessage is intentionally
// available to the builder — it carries the channel-specific handles
// (tele.Context, HTTP request id, scheduler task) the executor needs to
// deliver output. Once the Invocation returns the builder MUST NOT install
// its own OnEvent; the chat AgentLoop overwrites it.
type InvocationBuilder func(ctx context.Context, run *Run, msg InboundMessage) (agent.Invocation, error)

// AgentLoopAdapter is the production chat.AgentLoop implementation. It
// wraps internal/agent.Run, translating the runtime's typed Event
// callback stream into the chat.OutboundEvent vocabulary the Hub fan-out
// expects. Per PRD §11.2 streaming events, the adapter:
//
//   - emits EventMessageDelta for each non-empty assistant delta
//   - emits EventToolStart / EventToolEnd with arg KEYS only — values
//     never cross this boundary (CLAUDE.md value-leakage rule)
//   - emits EventQuestionRequested opaquely for channel adapters to render
//   - emits EventMessageDone + EventUsage at the end of the run
//   - skips no-op upstream events (tools_exposed, llm_start, stats) and
//     keeps them on Run.Metadata for adapters that want to log them
//
// Hub itself owns the lifecycle markers (RunStarted / Done / Error /
// Cancelled) — the adapter does NOT emit those.
type AgentLoopAdapter struct {
	build InvocationBuilder
}

// NewAgentLoopAdapter wires an AgentLoopAdapter around an InvocationBuilder.
// Returns an error if build is nil so production wiring fails loud at boot
// instead of panicking on the first Telegram message.
func NewAgentLoopAdapter(build InvocationBuilder) (*AgentLoopAdapter, error) {
	if build == nil {
		return nil, errors.New("chat: InvocationBuilder is required")
	}
	return &AgentLoopAdapter{build: build}, nil
}

// Run satisfies chat.AgentLoop. Sequence per call:
//  1. Builder produces an agent.Invocation. ChannelData carries any
//     adapter-private handles the builder needs.
//  2. We override Invocation.OnEvent with a translator that fans out into
//     chat.OutboundEvent via the supplied emit closure. A pre-existing
//     OnEvent on the Invocation is preserved (called first), matching the
//     agent runtime convention so telemetry attached by builders survives.
//  3. We call agent.Run; on return we emit a MessageDone (with the
//     final text) followed by an optional EventUsage carrying captured
//     stats. Hub emits the terminal Done event after Run returns.
//
// Errors from emit are logged at the Hub layer (see Hub.makeEmit) and do
// not abort the run. Errors from agent.Run propagate; the Hub maps
// them to RunStatusFailed / RunStatusCancelled in dispatch().
func (a *AgentLoopAdapter) Run(ctx context.Context, run *Run, msg InboundMessage, emit EmitFn) error {
	if a == nil || a.build == nil {
		return errors.New("chat: AgentLoopAdapter not wired")
	}
	inv, err := a.build(ctx, run, msg)
	if err != nil {
		return fmt.Errorf("chat: build invocation: %w", err)
	}

	// US-CTX-01: ensure a ContextEngine is always set so the loop can compress
	// long conversations. The builder may override this with a smarter engine
	// (AutoCompactEngine in US-CTX-04); the default cap mirrors MaxHistoryMessages.
	if inv.Options.ContextEngine == nil {
		inv.Options.ContextEngine = conversation.NewDefaultContextEngine(0)
	}

	previousOnEvent := inv.OnEvent
	statsCaptured := false
	var lastStats agent.Event
	inv.OnEvent = func(event agent.Event) {
		if previousOnEvent != nil {
			previousOnEvent(event)
		}
		switch event.Type {
		case agent.EventToolsExposed:
			// Setup chatter — adapters get this via Run.Metadata for logging.
			if run != nil {
				if run.Metadata == nil {
					run.Metadata = map[string]any{}
				}
				run.Metadata["tools_exposed"] = append([]string(nil), event.ToolsExposed...)
				if event.PromptVersion != "" {
					run.Metadata["prompt_version"] = event.PromptVersion
				}
				if event.PromptHash != "" {
					run.Metadata["prompt_hash"] = event.PromptHash
				}
				if event.Toolset != "" {
					run.Metadata["toolset"] = event.Toolset
				}
				if run.Model == "" && event.PromptVersion != "" {
					run.Model = event.PromptVersion
				}
			}
		case agent.EventLLMStart:
			// Hub already emitted RunStarted; per-iteration LLM start is
			// telemetry-only in v1. SSE adapters can subscribe to a future
			// EventMessageCreated frame if they want it.
		case agent.EventLLMDelta:
			if event.Delta == "" {
				return
			}
			_ = emit(OutboundEvent{
				Type:    EventMessageDelta,
				Content: event.Delta,
			})
		case agent.EventToolStart:
			payload := map[string]any{
				"tool":         event.ToolName,
				"tool_call_id": event.ToolCallID,
			}
			if len(event.ToolArgKeys) > 0 {
				payload["arg_keys"] = append([]string(nil), event.ToolArgKeys...)
			}
			_ = emit(OutboundEvent{
				Type:    EventToolStart,
				Payload: payload,
			})
		case agent.EventToolEnd:
			payload := map[string]any{
				"tool":         event.ToolName,
				"tool_call_id": event.ToolCallID,
				"success":      event.ToolSuccess,
				"elapsed_ms":   event.ToolElapsedMs,
			}
			if event.ToolResultPreview != "" {
				payload["preview"] = event.ToolResultPreview
			}
			_ = emit(OutboundEvent{
				Type:    EventToolEnd,
				Payload: payload,
			})
		case agent.EventQuestionRequested:
			// Pass the payload through opaquely so a forward-compatible
			// adapter can choose to render it.
			payload := map[string]any{}
			maps.Copy(payload, event.QuestionPayload)
			if event.ToolName != "" {
				payload["tool"] = event.ToolName
			}
			_ = emit(OutboundEvent{
				Type:    EventQuestionRequested,
				Payload: payload,
			})
		case agent.EventStats:
			// Coalesce into a single EventUsage after the final answer.
			statsCaptured = true
			lastStats = event
		case agent.EventFinal:
			// Handled below — we want the final emit AFTER agent.Run
			// returns so the order is deterministic with the Done/Error
			// markers Hub appends.
		}
	}

	result, runErr := agent.Run(ctx, inv)

	// ask_user pause: flip run to waiting_for_user so the hub preserves the
	// status instead of overwriting it with completed after this returns.
	if runErr == nil && result.Stats.StopReason == "waiting_for_user" && run != nil {
		run.Status = RunStatusWaitingForUser
	}

	// Surface the final assistant text + usage even on error: a partial
	// transcript can still be useful (the LLM might have produced text
	// before a tool failure). The Hub appends EventDone after this returns.
	if runErr == nil || result.Text != "" {
		_ = emit(OutboundEvent{
			Type:    EventMessageDone,
			Content: result.Text,
			Payload: map[string]any{
				"delivered": result.Delivered,
			},
		})
	}
	if statsCaptured {
		stats := lastStats.Stats
		_ = emit(OutboundEvent{
			Type: EventUsage,
			Payload: map[string]any{
				"llm_calls":          stats.LLMCalls,
				"tool_calls":         stats.ToolCalls,
				"loop_steps":         stats.LoopSteps,
				"tokens_prompt":      stats.TokensPrompt,
				"tokens_completion":  stats.TokensCompletion,
				"tokens_total":       stats.TokensTotal,
				"cache_read_tokens":  stats.CacheReadTokens,
				"cost_usd":           stats.CostUSD,
				"terminal_tool":      stats.TerminalTool,
				"stop_reason":        stats.StopReason,
			},
		})
		if run != nil {
			if run.Metadata == nil {
				run.Metadata = map[string]any{}
			}
			run.Metadata["stats"] = map[string]any{
				"llm_calls":         stats.LLMCalls,
				"tool_calls":        stats.ToolCalls,
				"tokens_total":      stats.TokensTotal,
				"tokens_prompt":     stats.TokensPrompt,
				"tokens_completion": stats.TokensCompletion,
				"cost_usd":          stats.CostUSD,
				"terminal_tool":     stats.TerminalTool,
				"stop_reason":       stats.StopReason,
			}
			run.Metadata["final_text"] = result.Text
			run.Metadata["delivered"] = result.Delivered
		}
	}

	return runErr
}
