package workflow_test

import (
	"context"
	"testing"

	"github.com/chetto1983/aura/internal/agent"
	"github.com/chetto1983/aura/internal/agent/workflow"
)

func TestLoopAgent_DedupVeto_When_ResultChanges(t *testing.T) {
	// Same name+args but a CHANGING result content each time -> progress veto (D-18):
	// dedup never fires, so termination is by the hard step budget, NOT dedup.
	t.Setenv("AURA_LOOP_MAX_STEPS", "10")
	b, err := agent.NewBudgetFromEnv()
	if err != nil {
		t.Fatalf("NewBudgetFromEnv: %v", err)
	}
	ic := agent.InvocationContext{Ctx: context.Background(), Branch: "root", Budget: b}

	sub := &scriptedToolAgent{
		name:     "progresser",
		toolName: "search",
		toolArgs: `{"q":"x"}`,
		contents: []string{"r0", "r1", "r2", "r3", "r4", "r5", "r6", "r7", "r8", "r9", "r10"},
	}
	lp := workflow.NewLoop("loop", 0, sub)

	evs, err := drain(lp.Run(ic))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	final := evs[len(evs)-1]
	if got := final.Actions.StateDelta["limit_hit"]; got != "max_steps" {
		t.Fatalf("changing result must veto dedup -> terminate by max_steps, got limit_hit=%v", got)
	}
	if got := final.Actions.StateDelta["steps_consumed"]; got != 10 {
		t.Errorf("steps_consumed: want 10, got %v", got)
	}
}
