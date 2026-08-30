package agent_test

import (
	"strings"
	"testing"

	"github.com/chetto1983/aura/internal/agent"
	"github.com/chetto1983/aura/internal/agent/agenttest"
	"github.com/chetto1983/aura/internal/llm"
)

// A finish_reason="length" completion with reasoning and NO content is the reasoning
// overrun measured 2026-08-30 (amendment #189). The single recovery turn must run with
// reasoning pinned off and carry the overrun nudge; the user gets that retry's answer.
func TestReasoningOverrun_RetriesWithoutReasoning(t *testing.T) {
	recordingProvider(t)
	fc := agenttest.NewFakeClient(
		agenttest.ReasoningChunks("length", "let me think…", "and think some more…"),
		agenttest.TextChunks("stop", "short answer"),
	)
	a := newAgent(t, fc, llm.Config{Provider: "llamacpp", BaseURL: "http://aura-llm:8084/v1"})
	ic := newIC(t, agent.BudgetOptions{MaxSteps: new(10)})

	evs, err := collect(a.Run(ic))
	if err != nil {
		t.Fatalf("reasoning overrun surfaced an error slot (must retry or finalize): %v", err)
	}
	if len(evs) == 0 {
		t.Fatal("no events emitted")
	}
	last := evs[len(evs)-1]
	if last.LLMResponse == nil || last.LLMResponse.Content != "short answer" {
		t.Fatalf("terminal content = %+v, want the retry's answer", last.LLMResponse)
	}
	if len(fc.Requests) != 2 {
		t.Fatalf("LLM calls = %d, want exactly one retry", len(fc.Requests))
	}
	retry := fc.Requests[1]
	if retry.Reasoning.Effort != llm.ReasoningEffortNone {
		t.Fatalf("retry reasoning effort = %q, want none (the retry must not burn the budget the same way)", retry.Reasoning.Effort)
	}
	nudged := false
	for _, m := range retry.Messages {
		if m.Role == llm.RoleUser && strings.Contains(m.Content, "without extended reasoning") {
			nudged = true
		}
	}
	if !nudged {
		t.Fatal("retry request carries no overrun nudge")
	}
}

// A SECOND overrun finds the recovery counter spent and finalizes through the synthesis
// path: the terminal Event is non-empty and keeps limit_hit=empty_response (Req#2).
func TestReasoningOverrun_TwiceFinalizesNonEmpty(t *testing.T) {
	recordingProvider(t)
	fc := agenttest.NewFakeClient(
		agenttest.ReasoningChunks("length", "thinking…"),
		agenttest.ReasoningChunks("length", "thinking again…"),
		agenttest.TextChunks("stop", finalizeAnswer),
	)
	a := newAgent(t, fc, llm.Config{Provider: "llamacpp", BaseURL: "http://aura-llm:8084/v1"})
	ic := newIC(t, agent.BudgetOptions{MaxSteps: new(10)})

	evs, err := collect(a.Run(ic))
	if err != nil {
		t.Fatalf("double overrun surfaced an error slot: %v", err)
	}
	last := evs[len(evs)-1]
	if last.LLMResponse == nil || last.LLMResponse.Content != finalizeAnswer {
		t.Fatalf("terminal content = %+v, want the synthesized answer", last.LLMResponse)
	}
	if got := last.Actions.StateDelta["limit_hit"]; got != "empty_response" {
		t.Errorf("limit_hit = %v, want empty_response", got)
	}
	if len(fc.Requests) != 3 {
		t.Fatalf("LLM calls = %d, want overrun + one retry + one synthesis", len(fc.Requests))
	}
}
