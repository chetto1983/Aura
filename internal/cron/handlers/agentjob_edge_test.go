package handlers

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/chetto1983/aura/internal/agent"
	"github.com/chetto1983/aura/internal/agent/agenttest"
)

// TestAgentJobRunErrorPropagates covers Run's terminal-error branch (and drain's
// error return): a REAL infra failure from the LLM client (the iter.Seq2 error slot,
// D-15) aborts the job with a wrapped "agent_job run" error rather than a quiet
// success. The partial summary collected before the failure is still returned so the
// audit row records what happened up to the break.
func TestAgentJobRunErrorPropagates(t *testing.T) {
	t.Parallel()
	boom := errors.New("upstream 503")
	fc := agenttest.NewFakeClient(agenttest.FakeTurn{Err: boom})
	h := AgentJobHandler{Deps: AgentDeps{Client: fc, Registry: jobRegistry()}}

	_, err := h.Run(context.Background(), Job{
		Payload:    []byte(`{"goal":"fetch the report"}`),
		StepBudget: 5,
		RunID:      "run-err",
	})
	if err == nil {
		t.Fatal("a client Stream error must propagate as a terminal agent_job error (D-15)")
	}
	if !strings.Contains(err.Error(), "agent_job run") {
		t.Fatalf("error should be wrapped as an agent_job run failure, got %v", err)
	}
	if !errors.Is(err, boom) {
		t.Fatalf("the underlying client error must be unwrappable via %%w, got %v", err)
	}
}

// TestAgentJobBudgetError covers Run's budget-construction error branch: a malformed
// budget env knob makes newJobBudget (agent.NewBudget) fail, so Run returns a wrapped
// "budget" error after extracting a valid goal but BEFORE constructing any worker. The
// wallclock knob is always read from env even when StepBudget overrides MaxSteps, so a
// malformed value fails the construction deterministically and environment-free.
func TestAgentJobBudgetError(t *testing.T) {
	t.Setenv("AURA_LOOP_MAX_WALLCLOCK_SEC", "not-a-number")
	fc := agenttest.NewFakeClient()
	h := AgentJobHandler{Deps: AgentDeps{Client: fc, Registry: jobRegistry()}}

	_, err := h.Run(context.Background(), Job{
		Payload:    []byte(`{"goal":"do something"}`),
		StepBudget: 5,
		RunID:      "run-badbudget",
	})
	if err == nil {
		t.Fatal("a malformed budget knob must fail Run with a terminal budget error")
	}
	if !strings.Contains(err.Error(), "budget") {
		t.Fatalf("error should be wrapped as an agent_job budget failure, got %v", err)
	}
	if fc.CallCount() != 0 {
		t.Fatalf("the LLM must NOT be called when the budget fails to construct, got %d calls", fc.CallCount())
	}
}

// TestAgentJobBoundedOutOnRepeatedAskUser covers Run's bounded-out tail: when the model
// re-asks ask_user on EVERY attempt, the inject-and-continue loop must stop after
// maxAutoRejects and finalize with the marker trail (never block, D-25). The summary
// must carry one auto-reject marker per attempt and the run must terminate quickly.
func TestAgentJobBoundedOutOnRepeatedAskUser(t *testing.T) {
	t.Parallel()
	// Script more ask_user turns than the loop tolerates so the bound — not the script —
	// stops it. Each attempt re-runs a fresh worker that immediately asks again.
	turns := make([]agenttest.FakeTurn, 0, maxAutoRejects+2)
	for i := 0; i <= maxAutoRejects+1; i++ {
		turns = append(turns, agenttest.ToolCallTurn(
			agenttest.MakeToolCall("ask", "ask_user", `{"question":"are you sure?","kind":"approval"}`),
		))
	}
	fc := agenttest.NewFakeClient(turns...)
	h := AgentJobHandler{Deps: AgentDeps{Client: fc, Registry: jobRegistry()}}

	done := make(chan struct{})
	var summary string
	var runErr error
	go func() {
		summary, runErr = h.Run(context.Background(), Job{
			Payload:    []byte(`{"goal":"keep asking"}`),
			StepBudget: 30,
			RunID:      "run-bounded",
		})
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(30 * time.Second):
		t.Fatal("a model that re-asks forever must be bounded out, not blocked (D-25)")
	}
	if runErr != nil {
		t.Fatalf("bounded-out finalize must not error: %v", runErr)
	}
	got := strings.Count(summary, autoRejectMarker)
	if got != maxAutoRejects+1 {
		t.Fatalf("bounded-out summary should carry one marker per attempt (%d), got %d in %q",
			maxAutoRejects+1, got, summary)
	}
}

// TestAgentJobGoalMalformedJSON covers agentJobGoal's unmarshal-error branch (distinct
// from the empty-object case): non-JSON payload yields an empty goal, which Run treats
// as a "no goal" terminal error.
func TestAgentJobGoalMalformedJSON(t *testing.T) {
	t.Parallel()
	if got := agentJobGoal([]byte(`{not valid json`)); got != "" {
		t.Fatalf("malformed payload must yield an empty goal, got %q", got)
	}
	fc := agenttest.NewFakeClient()
	h := AgentJobHandler{Deps: AgentDeps{Client: fc, Registry: jobRegistry()}}
	if _, err := h.Run(context.Background(), Job{Payload: []byte(`garbage`), RunID: "r"}); err == nil {
		t.Fatal("a malformed payload (no goal) must be a terminal error")
	}
}

// TestAskUserKindDefaultsClarification covers askUserKind's empty-kind branch: a pause
// with a blank kind reconstructs the assistant ask_user call as a "clarification" so the
// injected resume stays schema-valid on the OpenAI wire.
func TestAskUserKindDefaultsClarification(t *testing.T) {
	t.Parallel()
	if got := askUserKind(""); got != "clarification" {
		t.Fatalf("empty kind must default to clarification, got %q", got)
	}
	if got := askUserKind("   "); got != "clarification" {
		t.Fatalf("blank kind must default to clarification, got %q", got)
	}
	if got := askUserKind("approval"); got != "approval" {
		t.Fatalf("a real kind must pass through, got %q", got)
	}
	// The reconstructed assistant turn for a blank-kind pause must thread the tool_call
	// id and carry the defaulted kind in its arguments.
	turn := assistantAskUserTurn(&agent.AwaitingInput{Question: "go?", ToolCallID: "tc-9"})
	if len(turn.ToolCalls) != 1 || turn.ToolCalls[0].ID != "tc-9" {
		t.Fatalf("reconstructed assistant turn must thread tool_call id tc-9, got %+v", turn.ToolCalls)
	}
	if !strings.Contains(turn.ToolCalls[0].Function.Arguments, "clarification") {
		t.Fatalf("blank-kind reconstruction must default kind to clarification, args=%q",
			turn.ToolCalls[0].Function.Arguments)
	}
}
