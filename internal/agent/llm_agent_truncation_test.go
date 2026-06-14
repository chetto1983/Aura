package agent_test

import (
	"testing"

	"github.com/chetto1983/aura/internal/agent"
	"github.com/chetto1983/aura/internal/agent/agenttest"
	"github.com/chetto1983/aura/internal/llm"
)

// truncatedToolTurn scripts an assistant turn that emits a tool call and then ends
// with finish_reason="length": the provider cut the JSON arguments mid-string because
// the output budget was exhausted (the 203-turn truncation disaster, 2026-06-14). The
// arguments are intentionally unterminated — dispatching them yields invalid JSON.
func truncatedToolTurn() agenttest.FakeTurn {
	c := agenttest.MakeToolCall("c1", "fs_write", `{"path":"/tmp/chart.py","content":"import matplotlib`)
	return agenttest.FakeTurn{Chunks: []llm.Chunk{{ToolCall: &c}, {FinishReason: "length"}}}
}

// TestToolArgsTruncated_NudgesNotDispatched: a length-truncated tool call must NOT be
// dispatched (its args are invalid JSON). The loop injects a single user-role nudge and
// runs another turn instead — the truncated call never reaches a tool, so no RoleTool
// error message threads into the next request.
func TestToolArgsTruncated_NudgesNotDispatched(t *testing.T) {
	recordingProvider(t)
	fc := agenttest.NewFakeClient(
		truncatedToolTurn(),                          // length-truncated tool call → nudge, NOT dispatch
		agenttest.TextChunks("stop", "recovered ok"), // the nudged retry answers cleanly
	)
	a := newAgent(t, fc, llm.Config{})
	ic := newIC(t, agent.BudgetOptions{MaxSteps: ptr(10)})

	evs, err := collect(a.Run(ic))
	if err != nil {
		t.Fatalf("truncated tool call surfaced an error slot (must recover): %v", err)
	}
	last := evs[len(evs)-1]
	if last.LLMResponse == nil || last.LLMResponse.Content != "recovered ok" {
		t.Fatalf("terminal content = %+v, want the recovered answer", last.LLMResponse)
	}
	if len(fc.Requests) < 2 {
		t.Fatalf("expected a second (nudged) request, got %d", len(fc.Requests))
	}
	for _, m := range fc.Requests[1].Messages {
		if m.Role == llm.RoleTool {
			t.Fatalf("truncated tool call was DISPATCHED (RoleTool threaded into the retry): %q", m.Content)
		}
	}
}

// TestToolArgsTruncated_TwiceFinalizes: two consecutive length-truncated tool calls
// exhaust the nudge and route to finalize — the terminal Event is non-empty and carries
// limit_hit=tool_args_truncated, so the loop can never thrash on truncation.
func TestToolArgsTruncated_TwiceFinalizes(t *testing.T) {
	recordingProvider(t)
	fc := agenttest.NewFakeClient(
		truncatedToolTurn(),                          // 1st → nudge
		truncatedToolTurn(),                          // 2nd → finalize
		agenttest.TextChunks("stop", finalizeAnswer), // finalize synthesis turn
	)
	a := newAgent(t, fc, llm.Config{})
	ic := newIC(t, agent.BudgetOptions{MaxSteps: ptr(10)})

	evs, err := collect(a.Run(ic))
	if err != nil {
		t.Fatalf("double truncation surfaced an error slot (must finalize): %v", err)
	}
	last := evs[len(evs)-1]
	if last.LLMResponse == nil || last.LLMResponse.Content != finalizeAnswer {
		t.Fatalf("terminal content = %+v, want the synthesized answer", last.LLMResponse)
	}
	if got := last.Actions.StateDelta["limit_hit"]; got != "tool_args_truncated" {
		t.Errorf("limit_hit = %v, want tool_args_truncated", got)
	}
}
