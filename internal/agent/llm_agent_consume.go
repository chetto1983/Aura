// consume is the stream-drain half of the run loop, split out of llm_agent.go to
// keep that file under the no-god-class cap (D-07). It turns one provider stream
// into the (text, calls, finish, usage) the loop acts on, re-emitting chunk/
// tool-call/reasoning Events as they arrive and honoring the iter.Seq2
// yield-after-false contract (a consumer break drains-to-close, then reports
// stopped so Run never yields again).
package agent

import (
	"strings"

	"github.com/chetto1983/aura/internal/llm"
	"github.com/chetto1983/aura/internal/reasoningtrace"
)

// consume drains one stream: it re-emits each text delta as a chunk Event and each
// reasoning delta as a reasoning chunk Event (stream-only — never added to the
// accumulated text the caller persists, amendment #57), gathers finalized tool
// calls, the finish_reason, and the trailing usage. Tool-call Events are emitted as
// they finalize so the Event order is chunk -> tool_call (Req#9).
func (a *LlmAgent) consume(ch <-chan llm.Chunk, ic InvocationContext, spanID [8]byte, parentSpanID *[8]byte,
	requestID string, yield func(*Event, error) bool,
) (text string, calls []llm.ToolCall, finish string, usage llm.Usage, stopped bool, streamErr error) {
	var b strings.Builder
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
			return b.String(), calls, finish, usage, false, c.Err
		case c.Usage != nil:
			usage = *c.Usage
			reasoningtrace.Record("agent_consume_usage_chunk", map[string]any{
				"request_id":        requestID,
				"thread_id":         a.sessionID,
				"prompt_tokens":     c.Usage.PromptTokens,
				"completion_tokens": c.Usage.CompletionTokens,
				"cached_tokens":     c.Usage.CachedTokens,
			})
		case c.ToolCall != nil:
			calls = append(calls, *c.ToolCall)
			reasoningtrace.Record("agent_consume_tool_call_chunk", map[string]any{
				"request_id": requestID,
				"thread_id":  a.sessionID,
				"tool_call":  *c.ToolCall,
			})
			if !yield(a.toolCallEvent(ic, spanID, parentSpanID, *c.ToolCall), nil) {
				// Consumer stopped: drain the rest to let the client close cleanly,
				// then report stopped so Run never yields again (iter.Seq2 contract).
				for range ch { //nolint:revive // drain-to-close keeps the stream goroutine from leaking
				}
				return b.String(), calls, finish, usage, true, nil
			}
		case c.Text != "":
			b.WriteString(c.Text)
			reasoningtrace.Record("agent_consume_text_chunk", map[string]any{
				"request_id": requestID,
				"thread_id":  a.sessionID,
				"chars":      reasoningtrace.RuneLen(c.Text),
				"delta":      c.Text,
			})
			if !yield(a.chunkEvent(ic, spanID, parentSpanID, c.Text), nil) {
				for range ch { //nolint:revive // drain-to-close
				}
				return b.String(), calls, finish, usage, true, nil
			}
		case c.Reasoning != "":
			// Stream-only CoT: yield the reasoning Event but NEVER write to b — the
			// returned text (what persistence reads) must stay reasoning-free (#57).
			reasoningtrace.Record("agent_consume_reasoning_chunk", map[string]any{
				"request_id": requestID,
				"thread_id":  a.sessionID,
				"chars":      reasoningtrace.RuneLen(c.Reasoning),
				"redacted":   true,
			})
			if !yield(a.reasoningChunkEvent(ic, spanID, parentSpanID, c.Reasoning), nil) {
				for range ch { //nolint:revive // drain-to-close
				}
				return b.String(), calls, finish, usage, true, nil
			}
		case c.FinishReason != "":
			finish = c.FinishReason
			reasoningtrace.Record("agent_consume_finish_reason", map[string]any{
				"request_id":    requestID,
				"thread_id":     a.sessionID,
				"finish_reason": c.FinishReason,
			})
		}
	}
	return b.String(), calls, finish, usage, false, nil
}
