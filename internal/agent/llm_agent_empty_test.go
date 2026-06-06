package agent_test

import (
	"strings"
	"testing"

	"github.com/chetto1983/aura/internal/agent"
	"github.com/chetto1983/aura/internal/agent/agenttest"
	"github.com/chetto1983/aura/internal/llm"
)

// TestEmptyResponse_RecoversWithNudge: a completion with NO tool calls and NO
// content is a provider hiccup, not an answer (observed live 2026-06-06: a DeepSeek
// empty turn silently ended the xlsx North-Star run mid-task). The loop must spend
// its ONE recovery nudge and run another turn; here the retry answers normally and
// the final Event carries that answer.
func TestEmptyResponse_RecoversWithNudge(t *testing.T) {
	recordingProvider(t)
	fc := agenttest.NewFakeClient(
		agenttest.TextChunks(""),                     // empty completion: no calls, no content, finish=""
		agenttest.TextChunks("stop", "recovered ok"), // the nudged retry answers
	)
	a := newAgent(t, fc, llm.Config{})
	ic := newIC(t, agent.BudgetOptions{MaxSteps: ptr(10)})

	evs, err := collect(a.Run(ic))
	if err != nil {
		t.Fatalf("empty completion surfaced an error slot (must recover or finalize): %v", err)
	}
	if len(evs) == 0 {
		t.Fatal("no events emitted")
	}
	last := evs[len(evs)-1]
	if last.LLMResponse == nil || last.LLMResponse.Content != "recovered ok" {
		t.Fatalf("terminal content = %+v, want the recovered answer %q", last.LLMResponse, "recovered ok")
	}
}

// TestEmptyResponse_TwiceFinalizesNonEmpty: a SECOND empty completion (the nudge
// counter is spent) routes to finalize, which consumes the scripted synthesis turn —
// the terminal Event is non-empty and carries limit_hit=empty_response. No path ever
// emits an empty terminal (Req#2).
func TestEmptyResponse_TwiceFinalizesNonEmpty(t *testing.T) {
	recordingProvider(t)
	fc := agenttest.NewFakeClient(
		agenttest.TextChunks(""),                     // 1st empty → nudge
		agenttest.TextChunks(""),                     // 2nd empty → finalize
		agenttest.TextChunks("stop", finalizeAnswer), // finalize synthesis turn
	)
	a := newAgent(t, fc, llm.Config{})
	ic := newIC(t, agent.BudgetOptions{MaxSteps: ptr(10)})

	evs, err := collect(a.Run(ic))
	if err != nil {
		t.Fatalf("double-empty surfaced an error slot (must finalize as a terminal Event): %v", err)
	}
	if len(evs) == 0 {
		t.Fatal("no events emitted")
	}
	last := evs[len(evs)-1]
	if last.LLMResponse == nil || strings.TrimSpace(last.LLMResponse.Content) == "" {
		t.Fatalf("terminal event has EMPTY content after double-empty: %+v (the exact live failure this guards)", last.LLMResponse)
	}
	if last.LLMResponse.Content != finalizeAnswer {
		t.Errorf("terminal content = %q, want the synthesized answer %q", last.LLMResponse.Content, finalizeAnswer)
	}
	if got := last.Actions.StateDelta["limit_hit"]; got != "empty_response" {
		t.Errorf("limit_hit = %v, want empty_response", got)
	}
}
