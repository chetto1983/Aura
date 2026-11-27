// consume is the stream-drain half of the run loop, split out of llm_agent.go to
// keep that file under the no-god-class cap (D-07). It turns one provider stream
// into the roundOutput the loop acts on, re-emitting chunk/tool-call/reasoning
// Events as they arrive and honoring the iter.Seq2 yield-after-false contract (a
// consumer break drains-to-close, then reports stopped so Run never yields again).
package agent

import (
	"encoding/json"
	"log/slog"
	"strings"
	"time"

	"github.com/chetto1983/aura/internal/llm"
	"github.com/chetto1983/aura/internal/reasoningtrace"
)

// roundOutput is everything one drained stream produced. providerState is the
// opaque record the provider client asked to get back on this round's assistant
// message; nil on every route that sends none. firstChunkAt is when the first text,
// reasoning or tool call arrived: zero when none did.
type roundOutput struct {
	text          string
	calls         []llm.ToolCall
	finish        string
	usage         llm.Usage
	providerState json.RawMessage
	firstChunkAt  time.Time
	stopped       bool
	streamErr     error
}

func (out *roundOutput) markFirstChunk() {
	if out.firstChunkAt.IsZero() {
		out.firstChunkAt = time.Now()
	}
}

// consume drains one stream: it re-emits each text delta as a chunk Event and each
// reasoning delta as a reasoning chunk Event (stream-only — never added to the
// accumulated text the caller persists, amendment #57), gathers finalized tool
// calls, the finish_reason, the provider state, and the trailing usage. Tool-call
// Events are emitted as they finalize so the Event order is chunk -> tool_call (Req#9).
func (a *LlmAgent) consume(ch <-chan llm.Chunk, ic InvocationContext, spanID [8]byte,
	requestID string, yield func(*Event, error) bool,
) roundOutput {
	var out roundOutput
	var b strings.Builder
	end := func(stopped bool, streamErr error) roundOutput {
		out.text, out.stopped, out.streamErr = b.String(), stopped, streamErr
		return out
	}
	// stop drains the rest so the client can close cleanly, then reports stopped so
	// Run never yields again (iter.Seq2 contract).
	stop := func() roundOutput {
		for range ch { //nolint:revive // drain-to-close keeps the stream goroutine from leaking
		}
		return end(true, nil)
	}
	for c := range ch {
		switch {
		case c.Err != nil:
			// H9 stream contract: a terminal provider error means any accumulated
			// text is incomplete and must never be accepted as the final answer.
			reasoningtrace.Record("agent_consume_stream_error", map[string]any{
				"request_id": requestID,
				"thread_id":  a.sessionID,
				"error":      c.Err.Error(),
			})
			return end(false, c.Err)
		case c.Usage != nil:
			out.usage = *c.Usage
			recordUsage(out.usage)
			reasoningtrace.Record("agent_consume_usage_chunk", map[string]any{
				"request_id":        requestID,
				"thread_id":         a.sessionID,
				"prompt_tokens":     c.Usage.PromptTokens,
				"completion_tokens": c.Usage.CompletionTokens,
				"cached_tokens":     c.Usage.CachedTokens,
			})
		case c.ToolCall != nil:
			out.markFirstChunk()
			out.calls = append(out.calls, *c.ToolCall)
			reasoningtrace.Record("agent_consume_tool_call_chunk", map[string]any{
				"request_id": requestID,
				"thread_id":  a.sessionID,
				"tool_call":  *c.ToolCall,
			})
			if !yield(a.toolCallEvent(ic, spanID, *c.ToolCall), nil) {
				return stop()
			}
		case c.ProviderState != nil:
			out.providerState = c.ProviderState
			// The state can carry thinking blocks: the trace gets its size, never its bytes.
			reasoningtrace.Record("agent_consume_provider_state_chunk", map[string]any{
				"request_id": requestID,
				"thread_id":  a.sessionID,
				"bytes":      len(c.ProviderState),
			})
		case c.Text != "":
			out.markFirstChunk()
			b.WriteString(c.Text)
			reasoningtrace.Record("agent_consume_text_chunk", map[string]any{
				"request_id": requestID,
				"thread_id":  a.sessionID,
				"chars":      reasoningtrace.RuneLen(c.Text),
				"delta":      c.Text,
			})
			if !yield(a.chunkEvent(ic, spanID, c.Text), nil) {
				return stop()
			}
		case c.Reasoning != "":
			out.markFirstChunk()
			// Stream-only CoT: yield the reasoning Event but NEVER write to b — the
			// returned text (what persistence reads) must stay reasoning-free (#57).
			reasoningtrace.Record("agent_consume_reasoning_chunk", map[string]any{
				"request_id": requestID,
				"thread_id":  a.sessionID,
				"chars":      reasoningtrace.RuneLen(c.Reasoning),
				"redacted":   true,
			})
			if !yield(a.reasoningChunkEvent(ic, spanID, c.Reasoning), nil) {
				return stop()
			}
		case c.FinishReason != "":
			out.finish = c.FinishReason
			reasoningtrace.Record("agent_consume_finish_reason", map[string]any{
				"request_id":    requestID,
				"thread_id":     a.sessionID,
				"finish_reason": c.FinishReason,
			})
		}
	}
	return end(false, nil)
}

// logCallEnd records one completed model call: how long it took from opening the stream,
// when the first content arrived (the provider's prompt read, absent when none came), how
// much of the prompt the provider served from its cache, and the response id and upstream
// provider when the stream names them. Without these a slow turn could not be split from the
// logs (lab VM, 2026-11-27).
func (a *LlmAgent) logCallEnd(requestID string, started time.Time, out roundOutput, calls int) {
	attrs := []any{
		"request_id", requestID, "thread_id", a.sessionID, "model", a.cfg.Model,
		"finish_reason", out.finish, "tool_calls", calls,
		"duration_ms", time.Since(started).Milliseconds(),
		"prompt_tokens", out.usage.PromptTokens, "cached_tokens", out.usage.CachedTokens,
		"completion_tokens", out.usage.CompletionTokens,
	}
	if !out.firstChunkAt.IsZero() {
		attrs = append(attrs, "first_token_ms", out.firstChunkAt.Sub(started).Milliseconds())
	}
	if out.usage.ResponseID != "" {
		attrs = append(attrs, "response_id", out.usage.ResponseID)
	}
	if out.usage.Provider != "" {
		attrs = append(attrs, "provider", out.usage.Provider)
	}
	slog.Info("agent llm call end", attrs...)
}
