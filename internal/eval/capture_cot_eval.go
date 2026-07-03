//go:build cot_eval || live_e2e

// capture_cot_eval.go drives one LlmAgent.Run the way the production REPL renderer
// (cmd/aura/chat_render.go) does, so the live CoT harness observes everything a real
// `aura chat` turn would render WITHOUT importing the production main package. The
// render primitives it shares with the REPL — FlushRemainder, IsToolResultPreview,
// IsTerminalToolCall, UsageFromStateDelta — now live in internal/agentrender (Phase
// 32-07); the ~80 LOC formerly duplicated here are gone, and this path picks up the
// agentrender AnyInt superset that counts the json.Number token fields the old eval
// copy silently zeroed (QA-C-04 / T-32-07-FIX).
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
	"github.com/chetto1983/aura/internal/agentrender"
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
	firstEventMS   float64     // ms to first observable LLM/tool event (tool call, chunk, final, pause)
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
	firstEventSeen := false
	firstByteSeen := false
	markFirstEvent := func() {
		if firstEventSeen {
			return
		}
		c.firstEventMS = float64(time.Since(start).Microseconds()) / 1000.0
		firstEventSeen = true
	}

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
			markFirstEvent()
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
		case len(resp.ToolCalls) > 0 && !agentrender.IsTerminalToolCall(resp.ToolCalls):
			markFirstEvent()
			c.eventKinds = append(c.eventKinds, kindToolCall)
			for i := range resp.ToolCalls {
				c.toolNames = append(c.toolNames, resp.ToolCalls[i].Function.Name)
				c.toolArgs = append(c.toolArgs, resp.ToolCalls[i].Function.Arguments)
				c.toolCallMS = append(c.toolCallMS, float64(time.Since(start).Microseconds())/1000.0)
			}
		case resp.FinishReason != "":
			markFirstEvent()
			c.eventKinds = append(c.eventKinds, kindFinal)
			c.finish = resp.FinishReason
			agentrender.FlushRemainder(&prose, resp.Content, func(s string) {
				prose.WriteString(s)
				raw.WriteString(s)
			})
			c.usage = agentrender.UsageFromStateDelta(ev.Actions.StateDelta)
			c.prose = prose.String()
			c.rawProse = raw.String()
			c.totalMS = float64(time.Since(start).Microseconds()) / 1000.0
			return c
		case agentrender.IsToolResultPreview(ev):
			markFirstEvent()
			c.eventKinds = append(c.eventKinds, kindToolResult)
			c.toolResults = append(c.toolResults, resp.Content)
			c.toolResultMS = append(c.toolResultMS, float64(time.Since(start).Microseconds())/1000.0)
			raw.WriteString(resp.Content) // tool-result previews are part of the raw leak surface
		case resp.Content != "":
			markFirstEvent()
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
