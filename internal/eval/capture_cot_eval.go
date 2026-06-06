//go:build cot_eval || live_e2e

// capture_cot_eval.go mirrors cmd/aura/chat_render.go's Event-stream extraction
// (the unexported renderTurn/usageFromStateDelta logic) so the live CoT harness can
// observe everything a real `aura chat` turn would render WITHOUT importing the
// production main package or changing it. ~80 lines duplicated by design (the rule
// in the task: production stays untouched; test code may mirror the extractor).
//
// It captures, per turn: the rendered prose (what the operator would see), the
// ordered Event kinds (chunk -> tool_call -> tool_result -> final / terminal), the
// tool names called, the finish_reason, the per-turn llm.Usage, and any terminal
// budget reason — the raw material every scoring dimension traces against.
package eval

import (
	"strings"
	"time"

	"github.com/chetto1983/aura/internal/agent"
	"github.com/chetto1983/aura/internal/llm"
)

// eventKind labels one captured Event for ordering assertions (tool-loop
// correctness dimension).
type eventKind string

const (
	kindChunk      eventKind = "chunk"
	kindToolCall   eventKind = "tool_call"
	kindToolResult eventKind = "tool_result"
	kindFinal      eventKind = "final"
	kindTerminal   eventKind = "terminal" // budget-trip terminal Event (escalate)
)

// askUserToolNameCapture is the canonical ask_user tool name the capture stamps into
// toolNames when a pause Event fires (the rewritten assistant message is ask_user-only,
// llm_agent_pause.go). It is a literal — the cot_eval harness does not import the tools
// package's unexported pause-detection constant.
const askUserToolNameCapture = "ask_user"

// turnCapture is the full observable record of one LlmAgent.Run, assembled by
// mirroring the production renderTurn logic. Every field is something a scoring
// dimension reads.
type turnCapture struct {
	prose          string      // clean prose the REPL would render (no JSON, no tool previews)
	rawProse       string      // EVERYTHING emitted to the writer incl. tool-result previews — leak surface
	finish         string      // finish_reason on the final Event
	usage          llm.Usage   // per-turn token+cost usage off the final Event StateDelta
	eventKinds     []eventKind // ordered Event kinds
	toolNames      []string    // non-terminal tool calls in call order
	toolArgs       []string    // raw JSON arguments of each tool call, aligned with toolNames (action-aware capture, Phase 11)
	toolCallMS     []float64   // ms-since-start of each tool call, aligned with toolNames
	toolResults    []string    // raw tool-result previews threaded back to the model
	toolResultMS   []float64   // ms-since-start of each tool result, aligned with toolResults
	terminalReason string      // budget-trip "limit_hit" reason, "" when not tripped
	terminated     bool        // a terminal budget Event was emitted
	firstByteMS    float64     // ms to first streamed delta (latency metric)
	totalMS        float64     // ms total turn duration
	runErr         error       // a real infra error off the iter.Seq2 error slot

	// HITL pause (Slice 1.5 / Phase 11 install gate). When ask_user fires the agent
	// suspends and emits an AwaitingInput Event with LLMResponse==nil; the capture
	// records it so a resume-driving harness (TestSkillsE2E) can synthesize the
	// assistant ask_user tool_call + the resume answer and start the next turn.
	paused        bool                 // an AwaitingInput Event was emitted (the turn suspended)
	awaitingInput *agent.AwaitingInput // the pause payload (question + tool_call_id + kind/options)
}

// captureTurn drives one LlmAgent.Run to completion (or to a consumer-stop) and
// returns the full observable record. start anchors the latency clock.
func captureTurn(run func() func(func(*agent.Event, error) bool)) *turnCapture {
	c := &turnCapture{}
	var prose strings.Builder
	var raw strings.Builder
	start := time.Now()
	firstByteSeen := false

	for ev, runErr := range run() {
		if runErr != nil {
			c.runErr = runErr
			break
		}
		if ev == nil {
			continue
		}
		// Terminal budget Event: escalate + budget_exhausted in StateDelta, no LLMResponse.
		if ev.Actions.Escalate {
			c.terminated = true
			c.eventKinds = append(c.eventKinds, kindTerminal)
			if r, ok := ev.Actions.StateDelta["limit_hit"].(string); ok {
				c.terminalReason = r
			}
			break
		}
		// HITL pause Event: ask_user fired, the agent suspended (LLMResponse==nil). Record
		// the pause and stop draining — the harness resumes with the synthesized answer.
		if ev.Actions.AwaitingInput != nil {
			c.paused = true
			c.awaitingInput = ev.Actions.AwaitingInput
			c.eventKinds = append(c.eventKinds, kindToolCall) // the ask_user call
			c.toolNames = append(c.toolNames, askUserToolNameCapture)
			c.toolArgs = append(c.toolArgs, "") // pause payload carried on awaitingInput, not args
			c.toolCallMS = append(c.toolCallMS, float64(time.Since(start).Microseconds())/1000.0)
			break
		}
		if ev.LLMResponse == nil {
			continue
		}
		resp := ev.LLMResponse
		switch {
		case len(resp.ToolCalls) > 0 && !isTerminalToolCall(resp.ToolCalls):
			c.eventKinds = append(c.eventKinds, kindToolCall)
			for i := range resp.ToolCalls {
				c.toolNames = append(c.toolNames, resp.ToolCalls[i].Function.Name)
				c.toolArgs = append(c.toolArgs, resp.ToolCalls[i].Function.Arguments)
				c.toolCallMS = append(c.toolCallMS, float64(time.Since(start).Microseconds())/1000.0)
			}
		case resp.FinishReason != "":
			c.eventKinds = append(c.eventKinds, kindFinal)
			c.finish = resp.FinishReason
			flushRemainder(&prose, resp.Content, func(s string) {
				prose.WriteString(s)
				raw.WriteString(s)
			})
			c.usage = usageFromStateDelta(ev.Actions.StateDelta)
			c.prose = prose.String()
			c.rawProse = raw.String()
			c.totalMS = float64(time.Since(start).Microseconds()) / 1000.0
			return c
		case isToolResultPreview(ev):
			c.eventKinds = append(c.eventKinds, kindToolResult)
			c.toolResults = append(c.toolResults, resp.Content)
			c.toolResultMS = append(c.toolResultMS, float64(time.Since(start).Microseconds())/1000.0)
			raw.WriteString(resp.Content) // tool-result previews are part of the raw leak surface
		case resp.Content != "":
			if !firstByteSeen {
				c.firstByteMS = float64(time.Since(start).Microseconds()) / 1000.0
				firstByteSeen = true
			}
			c.eventKinds = append(c.eventKinds, kindChunk)
			prose.WriteString(resp.Content)
			raw.WriteString(resp.Content)
		}
	}

	c.prose = prose.String()
	c.rawProse = raw.String()
	c.totalMS = float64(time.Since(start).Microseconds()) / 1000.0
	return c
}

// flushRemainder mirrors cmd/aura/chat_render.go flushRemainder: emit the tail of
// the final answer streaming did not already show.
func flushRemainder(prose *strings.Builder, finalAnswer string, emit func(string)) {
	already := prose.String()
	if finalAnswer == "" || finalAnswer == already {
		return
	}
	if strings.HasPrefix(finalAnswer, already) {
		emit(finalAnswer[len(already):])
		return
	}
	prose.Reset()
	emit(finalAnswer)
}

// isToolResultPreview mirrors chat_render.go: a tool-result Event is marked by the
// tool_call_id the LlmAgent stamps into Actions.StateDelta.
func isToolResultPreview(ev *agent.Event) bool {
	_, ok := ev.Actions.StateDelta["tool_call_id"]
	return ok
}

// isTerminalToolCall mirrors chat_render.go: the loop-terminating text_response.
func isTerminalToolCall(calls []llm.ToolCall) bool {
	for i := range calls {
		if calls[i].Function.Name == "text_response" {
			return true
		}
	}
	return false
}

// usageFromStateDelta mirrors chat_render.go: reconstruct llm.Usage from the final
// Event StateDelta the LlmAgent stamped.
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
