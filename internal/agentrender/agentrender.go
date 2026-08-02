// Package agentrender holds the REPL event-render primitives used by the cmd/aura
// chat renderer. It was extracted when a second copy lived in the (since-deleted)
// eval harness and the two ~80-LOC copies silently diverged on token coercion: one
// AnyInt lacked the json.Number branch and returned 0 for a json.Number token count
// (audit finding QA-C-04). AnyInt keeps the SUPERSET behaviour that fixed it.
//
// Boundary (Shared Pattern C): agentrender imports internal/agent (for *agent.Event)
// and internal/llm (for llm.ToolCall / llm.Usage) ONLY — never internal/agui, so the
// one-directional agent ⇸ agui boundary gains no back-edge.
package agentrender

import (
	"encoding/json"
	"strings"

	"github.com/chetto1983/aura/internal/agent"
	"github.com/chetto1983/aura/internal/llm"
)

// FlushRemainder emits the tail of the final answer that streaming did not already
// show. On the text_response path the agent surfaces the full decoded answer only
// on the final Event, so nothing was streamed yet — emit the whole answer. On the
// content-stop path the chunks already streamed, so only a divergent tail (e.g. the
// truncation notice D-21) is emitted; a final that diverges from the streamed prefix
// resets the buffer and emits in full so the answer is never double-printed.
func FlushRemainder(prose *strings.Builder, finalAnswer string, emit func(string)) {
	already := prose.String()
	if finalAnswer == "" || finalAnswer == already {
		return
	}
	if strings.HasPrefix(finalAnswer, already) {
		emit(finalAnswer[len(already):])
		return
	}
	// Divergent: reset and emit the full answer.
	prose.Reset()
	emit(finalAnswer)
}

// IsToolResultPreview reports whether ev is a tool-result Event (the raw tool output
// the LlmAgent threads back into history and surfaces for AG-UI fan-out), identified
// by the tool_call_id the agent stamps into Actions.StateDelta. Such events carry
// their text in LLMResponse.Content with no FinishReason — the same shape as a
// streamed assistant chunk — so this marker is the only way a prose renderer can tell
// them apart and skip them.
func IsToolResultPreview(ev *agent.Event) bool {
	_, ok := ev.Actions.StateDelta["tool_call_id"]
	return ok
}

// IsTerminalToolCall reports whether the calls are the loop-terminating text_response
// (whose `text` is the prose, not an activity line).
func IsTerminalToolCall(calls []llm.ToolCall) bool {
	for i := range calls {
		if calls[i].Function.Name == "text_response" {
			return true
		}
	}
	return false
}

// UsageFromStateDelta reconstructs the per-turn llm.Usage the LlmAgent stamped into
// the final Event's Actions.StateDelta (D-11). On the REPL path the values are plain
// in-process ints/floats (no JSON round-trip), but AnyInt/AnyFloat tolerate the
// numeric widening a UseNumber decoder or a jsonb round-trip introduces so the usage
// is robust on either path.
func UsageFromStateDelta(d map[string]any) llm.Usage {
	if d == nil {
		return llm.Usage{}
	}
	u := llm.Usage{
		PromptTokens:     AnyInt(d["prompt_tokens"]),
		CompletionTokens: AnyInt(d["completion_tokens"]),
		CachedTokens:     AnyInt(d["cache_hit_tokens"]),
	}
	if c, ok := AnyFloat(d["cost_usd"]); ok {
		u.Cost = &c
	}
	return u
}

// AnyInt coerces a StateDelta numeric to int. It is the SUPERSET of the two former
// copies: the cmd/aura/chat_render copy handled json.Number, the internal/eval copy
// did NOT and silently returned 0 for a json.Number token count (a UseNumber decoder
// or a jsonb round-trip widens token counts to json.Number, M-07). Adopting the
// json.Number branch FIXES that eval token-count bug; an unparseable json.Number
// still falls back to 0 like any other unknown input.
func AnyInt(v any) int {
	switch n := v.(type) {
	case int:
		return n
	case int64:
		return int(n)
	case float64:
		return int(n)
	case json.Number:
		if i, err := n.Int64(); err == nil {
			return int(i)
		}
		return 0
	default:
		return 0
	}
}

// AnyFloat coerces a StateDelta numeric to float64. float64/int widen directly; a
// json.Number (UseNumber decoder / jsonb round-trip, M-07 sibling) is parsed instead
// of silently dropping the wire cost. Any other or unparseable value returns
// (0, false) so the caller fabricates no cost.
func AnyFloat(v any) (float64, bool) {
	switch f := v.(type) {
	case float64:
		return f, true
	case int:
		return float64(f), true
	case json.Number:
		n, err := f.Float64()
		return n, err == nil
	default:
		return 0, false
	}
}
