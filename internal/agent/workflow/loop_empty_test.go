package workflow_test

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/chetto1983/aura/internal/agent"
	"github.com/chetto1983/aura/internal/agent/workflow"
)

func TestLoopAgent_EmptySubsUnlimitedReturns(t *testing.T) {
	b, err := agent.NewBudgetFromEnv()
	if err != nil {
		t.Fatalf("NewBudgetFromEnv: %v", err)
	}
	ic := agent.InvocationContext{Ctx: context.Background(), Branch: "root", Budget: b}
	lp := workflow.NewLoop("loop", 0)

	done := make(chan error, 1)
	go func() {
		for ev, err := range lp.Run(ic) {
			if err != nil {
				done <- err
				return
			}
			done <- fmt.Errorf("unexpected event from empty loop: %+v", ev)
			return
		}
		done <- nil
	}()

	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(100 * time.Millisecond):
		t.Fatal("empty unlimited LoopAgent did not return")
	}
}
