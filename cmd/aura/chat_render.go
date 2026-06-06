// chat_render.go holds the cmd/aura chat rendering helpers split out of chat.go
// (refactor-on-touch, ≤600 LOC): the streamed-prose renderer, the per-turn cost
// footer (D-11), and the dim tool-activity one-liner (D-12).
//
// The LlmAgent decodes the terminal text_response itself (D-13) and surfaces the
// decoded answer as clean prose — on chunk Events for the content-stop fallback,
// or on the final Event (Content+FinishReason) for the text_response path — so the
// REPL never has to parse raw tool-call JSON. renderTurn therefore streams whatever
// prose the agent yields and never shows JSON (Req#11). The incremental JSON-string
// extractor AI-SPEC §4b sketched is unnecessary here because the agent owns that
// decode; were the agent to ever stream raw tool-call args instead, the extractor
// would move here.
package main

import (
	"fmt"
	"io"
	"strings"

	"github.com/chetto1983/aura/internal/agent"
	"github.com/chetto1983/aura/internal/config"
	"github.com/chetto1983/aura/internal/llm"
)

// renderRunnerTurn drives one runner.Turn iterator, streaming clean prose to w as it
// arrives and returning the final answer + finish_reason + per-turn usage (read off
// the final Event's StateDelta, D-11) plus whether the round ended in a pause (an
// Actions.AwaitingInput Event was seen). It prints a dim one-liner for any
// non-terminal tool call (D-12) and surfaces a real infra error from the error slot.
func renderRunnerTurn(w io.Writer, seq iterSeq2) (answer, finish string, usage llm.Usage, paused bool, err error) {
	var prose strings.Builder
	emit := func(s string) {
		prose.WriteString(s)
		_, _ = io.WriteString(w, s)
	}
	var reasoningStarted bool
	for ev, runErr := range seq {
		if runErr != nil {
			return prose.String(), finish, usage, paused, runErr
		}
		if ev == nil {
			continue
		}
		if ev.Actions.AwaitingInput != nil {
			// HITL pause: stop rendering prose; the caller renders the prompt inline.
			paused = true
			continue
		}
		if ev.LLMResponse == nil {
			continue
		}
		resp := ev.LLMResponse
		switch {
		case len(resp.ToolCalls) > 0 && !isTerminalToolCall(resp.ToolCalls):
			renderToolActivity(w, resp.ToolCalls)
		case resp.FinishReason != "":
			// Final Event: flush the not-yet-streamed remainder of the answer and
			// read the per-turn usage off the StateDelta the LlmAgent stamped (D-11).
			// Do NOT return here — under the iter.Seq2 contract an early return makes
			// yield() report consumer-stop, so the Runner's post-round bookkeeping
			// (flushPause + the auto-title worker) is skipped. Record the terminal
			// answer/usage and keep draining; the producer ends the round right after
			// this event, so the range terminates naturally and the worker fires.
			finish = resp.FinishReason
			flushRemainder(&prose, resp.Content, emit)
			usage = usageFromStateDelta(ev.Actions.StateDelta)
		case isToolResultPreview(ev):
			// A tool-result Event carries the raw tool output (e.g. an RFC3339
			// timestamp from current_time) in Content for AG-UI forward-compat — it
			// is NOT assistant prose. Skip it so the raw preview never streams into
			// the REPL and never pollutes the prose buffer (which would make the
			// final-Event flush diverge and double-print the answer).
			continue
		case resp.Reasoning != "":
			// Live CoT (amendment #57e): stream the reasoning to the operator to mask
			// the reasoning-phase latency. It is stream-only — written straight to w,
			// NEVER to prose, so it never enters the returned/persisted answer.
			renderReasoning(w, resp.Reasoning, &reasoningStarted)
		case resp.Content != "":
			// Streamed chunk (raw assistant content, content-stop fallback path).
			emit(resp.Content)
		}
	}
	return prose.String(), finish, usage, paused, nil
}

// costFooterFromFinish renders the per-turn cost footer from the conversation
// config + usage (the finish_reason is accepted for future surfacing; the footer
// itself is the token+USD summary). It wraps costFooter with the config's price
// table + model so the REPL stays a one-liner.
func costFooterFromFinish(cfg *config.Config, usage llm.Usage, _ string) string {
	return costFooter(cfg.LLM.Prices, cfg.LLM.Model, usage, 0)
}

// flushRemainder emits the tail of the final answer that streaming did not already
// show. On the text_response path the agent surfaces the full decoded answer only
// on the final Event, so nothing was streamed yet — emit the whole answer. On the
// content-stop path the chunks already streamed, so only a divergent tail (e.g.
// the truncation notice D-21) is emitted.
func flushRemainder(prose *strings.Builder, finalAnswer string, emit func(string)) {
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

// isToolResultPreview reports whether ev is a tool-result Event (the raw tool
// output the LlmAgent threads back into history and surfaces for AG-UI fan-out),
// identified by the tool_call_id the agent stamps into Actions.StateDelta. Such
// events carry their text in LLMResponse.Content with no FinishReason — the same
// shape as a streamed assistant chunk — so this marker is the only way the prose
// renderer can tell them apart and skip them.
func isToolResultPreview(ev *agent.Event) bool {
	_, ok := ev.Actions.StateDelta["tool_call_id"]
	return ok
}

// isTerminalToolCall reports whether the calls are the loop-terminating
// text_response (whose `text` is the prose, not an activity line).
func isTerminalToolCall(calls []llm.ToolCall) bool {
	for i := range calls {
		if calls[i].Function.Name == "text_response" {
			return true
		}
	}
	return false
}

// renderToolActivity prints a dim one-liner per non-terminal tool call (D-12) so
// the operator sees activity without the streamed prose being invaded by args.
func renderToolActivity(w io.Writer, calls []llm.ToolCall) {
	for i := range calls {
		name := calls[i].Function.Name
		if name == "text_response" {
			continue
		}
		_, _ = fmt.Fprintf(w, "\x1b[2m· %s\x1b[0m\n", name)
	}
}

// renderReasoning streams a live chain-of-thought delta to w in dim style with a
// one-time 💭 prefix (amendment #57e), mirroring renderToolActivity's dim ANSI.
// started tracks whether the prefix has been emitted so the prefix shows once and
// subsequent deltas continue the same dim line. Reasoning is stream-only — the
// caller writes it to w only, never to the prose/answer buffer.
func renderReasoning(w io.Writer, delta string, started *bool) {
	if !*started {
		_, _ = io.WriteString(w, "\x1b[2m💭 ")
		*started = true
	}
	_, _ = io.WriteString(w, delta+"\x1b[0m")
}

// costFooter renders the per-turn footer (D-11): `· {tok} tok ({in} in / {out}
// out) · ${usd} · {lat}s`. USD comes from llm.CostUSD (provider cost preferred,
// price-table fallback, honest "n/a" for an unknown model — never $0).
func costFooter(prices map[string]llm.Price, model string, usage llm.Usage, latencySec float64) string {
	total := usage.PromptTokens + usage.CompletionTokens
	usd, _ := llm.CostUSD(prices, model, usage.PromptTokens, usage.CompletionTokens, usage.Cost)
	return fmt.Sprintf("\x1b[2m· %d tok (%d in / %d out) · %s · %.1fs\x1b[0m",
		total, usage.PromptTokens, usage.CompletionTokens, usd, latencySec)
}

// usageFromStateDelta reconstructs the per-turn llm.Usage the LlmAgent stamped into
// the final Event's Actions.StateDelta (D-11). The values are plain in-process
// ints/floats here (no JSON round-trip on this path), but anyInt tolerates the
// numeric widening JSON would introduce so the footer is robust either way.
func usageFromStateDelta(d map[string]any) llm.Usage {
	if d == nil {
		return llm.Usage{}
	}
	u := llm.Usage{
		PromptTokens:     anyInt(d["prompt_tokens"]),
		CompletionTokens: anyInt(d["completion_tokens"]),
		CachedTokens:     anyInt(d["cache_hit_tokens"]),
	}
	if c, ok := anyFloat(d["cost_usd"]); ok {
		u.Cost = &c
	}
	return u
}

func anyInt(v any) int {
	switch n := v.(type) {
	case int:
		return n
	case int64:
		return int(n)
	case float64:
		return int(n)
	default:
		return 0
	}
}

func anyFloat(v any) (float64, bool) {
	switch f := v.(type) {
	case float64:
		return f, true
	case int:
		return float64(f), true
	default:
		return 0, false
	}
}

// iterSeq2 is the agent.Run result type, aliased so renderTurn's signature stays
// readable.
type iterSeq2 = func(yield func(*agent.Event, error) bool)
