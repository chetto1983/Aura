package agent_test

import (
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/chetto1983/aura/internal/agent"
	"github.com/chetto1983/aura/internal/agent/agenttest"
	"github.com/chetto1983/aura/internal/llm"
)

func TestLlmAgent_AppendsClockHintFromBudget(t *testing.T) {
	recordingProvider(t)
	fc := agenttest.NewFakeClient(agenttest.ToolCallTurn(textResponseCall("c1", "ok")))
	a := newAgent(t, fc, llm.Config{})
	loc := time.FixedZone("CEST", 2*60*60)
	now := func() time.Time {
		return time.Date(2026, 6, 9, 12, 34, 56, 0, loc)
	}
	ic := newIC(t, agent.BudgetOptions{MaxSteps: new(25), Now: now})
	if _, err := collect(a.Run(ic)); err != nil {
		t.Fatalf("Run: %v", err)
	}
	tail := fc.LastRequest().Messages[len(fc.LastRequest().Messages)-1]
	// The instant and its offset come from the budget clock unchanged; the weekday before
	// it and the zone name after it are the 2026-08-16 fixes. A bare offset left the model
	// to infer the season and it inferred wrong; a bare date left it to compute the weekday
	// and it computed wrong (it called Sunday 16 August a Monday). Both are now stated.
	if !strings.Contains(tail.Content, "<current_time>Tuesday, 2026-06-09T12:34:56+02:00 (CEST)</current_time>") {
		t.Fatalf("clock hint = %q, want injected current_time from budget clock", tail.Content)
	}
	if !strings.Contains(tail.Content, "<today>2026-06-09</today>") {
		t.Fatalf("clock hint = %q, want injected today from budget clock", tail.Content)
	}
}

func TestLlmAgent_RetriesTransientStreamOpenError(t *testing.T) {
	recordingProvider(t)
	fc := agenttest.NewFakeClient(
		agenttest.FakeTurn{Err: errors.New("wsarecv: A connection attempt failed because the connected party did not properly respond")},
		agenttest.ToolCallTurn(textResponseCall("c1", "recovered")),
	)
	a := newAgent(t, fc, llm.Config{})
	ic := newIC(t, agent.BudgetOptions{MaxSteps: new(25)})
	evs, err := collect(a.Run(ic))
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if fc.CallCount() != 2 {
		t.Fatalf("Stream calls = %d, want retry success on second open", fc.CallCount())
	}
	last := evs[len(evs)-1]
	if last.LLMResponse == nil || last.LLMResponse.Content != "recovered" {
		t.Fatalf("final response = %+v, want recovered", last.LLMResponse)
	}
}
