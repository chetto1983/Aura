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
	"github.com/chetto1983/aura/internal/agentrender"
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
	reason := newCLIReasoning()
	for ev, runErr := range seq {
		if runErr != nil {
			return prose.String(), finish, usage, paused, runErr
		}
		if ev == nil {
			continue
		}
		if ev.Actions.AwaitingInput != nil {
			// HITL pause: stop rendering prose; the caller renders the prompt inline.
			reason.clear(w)
			paused = true
			continue
		}
		if ev.Actions.DiscardStreamed {
			// Mid-stream retry repudiation (B-12): the partial chunks already streamed
			// are stale. Erase the shown partial from the terminal and reset the prose
			// buffer so the retry renders over a blank slate — no partial+answer. This
			// fires ONLY on a rare retry; the common no-retry path never sees it, so
			// live token-by-token streaming is untouched.
			reason.clear(w)
			discardStreamed(&prose, w)
			continue
		}
		if ev.LLMResponse == nil {
			continue
		}
		resp := ev.LLMResponse
		switch {
		case len(resp.ToolCalls) > 0 && !agentrender.IsTerminalToolCall(resp.ToolCalls):
			reason.clear(w)
			renderToolActivity(w, resp.ToolCalls)
		case resp.FinishReason != "":
			// Final Event: flush the not-yet-streamed remainder of the answer and
			// read the per-turn usage off the StateDelta the LlmAgent stamped (D-11).
			// Do NOT return here — under the iter.Seq2 contract an early return makes
			// yield() report consumer-stop, so the Runner's post-round bookkeeping
			// (flushPause + the auto-title worker) is skipped. Record the terminal
			// answer/usage and keep draining; the producer ends the round right after
			// this event, so the range terminates naturally and the worker fires.
			reason.clear(w)
			finish = resp.FinishReason
			agentrender.FlushRemainder(&prose, resp.Content, emit)
			usage = agentrender.UsageFromStateDelta(ev.Actions.StateDelta)
		case agentrender.IsToolResultPreview(ev):
			// A tool-result Event carries the raw tool output (e.g. an RFC3339
			// timestamp from current_time) in Content for AG-UI forward-compat — it
			// is NOT assistant prose. Skip it so the raw preview never streams into
			// the REPL and never pollutes the prose buffer (which would make the
			// final-Event flush diverge and double-print the answer).
			continue
		case resp.Reasoning != "":
			// Live CoT: render a bounded in-place rolling reasoning line to mask the
			// reasoning-phase latency. Stream-only — written to w via the rolling
			// window, NEVER to prose, so it never enters the returned/persisted answer.
			// Only fires when AURA_SHOW_REASONING made the policy stream reasoning.
			reason.push(w, resp.Reasoning)
		case resp.Content != "":
			// Streamed chunk (raw assistant content, content-stop fallback path).
			reason.clear(w)
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

// discardStreamed erases the partial prose a failed stream attempt already showed
// (B-12 mid-stream retry) and resets the buffer so the retry renders clean. It moves
// the cursor to column 0, up over each newline the partial spanned, then clears to
// the end of the screen (\x1b[J) — covering a multi-line partial. A no-op when
// nothing was streamed yet (an empty partial leaves the cursor where it is).
func discardStreamed(prose *strings.Builder, w io.Writer) {
	partial := prose.String()
	if partial == "" {
		return
	}
	lines := strings.Count(partial, "\n")
	var b strings.Builder
	b.WriteString("\r")
	for range lines {
		b.WriteString("\x1b[A")
	}
	b.WriteString("\x1b[J")
	_, _ = io.WriteString(w, b.String())
	prose.Reset()
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

// costFooter renders the per-turn footer (D-11): `· {tok} tok ({in} in / {out}
// out) · ${usd} · {lat}s`. USD comes from llm.CostUSD (provider cost preferred,
// price-table fallback, honest "n/a" for an unknown model — never $0).
func costFooter(prices map[string]llm.Price, model string, usage llm.Usage, latencySec float64) string {
	total := usage.PromptTokens + usage.CompletionTokens
	usd, _ := llm.CostUSD(prices, model, usage.PromptTokens, usage.CompletionTokens, usage.Cost)
	return fmt.Sprintf("\x1b[2m· %d tok (%d in / %d out) · %s · %.1fs\x1b[0m",
		total, usage.PromptTokens, usage.CompletionTokens, usd, latencySec)
}

// iterSeq2 is the agent.Run result type, aliased so renderTurn's signature stays
// readable.
type iterSeq2 = func(yield func(*agent.Event, error) bool)
