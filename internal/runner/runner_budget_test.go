package runner

import (
	"context"
	"testing"
	"time"

	"github.com/chetto1983/aura/internal/agent/agenttest"
	"github.com/chetto1983/aura/internal/llm"
	"github.com/google/uuid"
)

// A hot loop profile (amendment #188) reaches the NEXT turn's Budget without a
// restart: the runner builds it from the runtime snapshot it already holds.
func TestRunnerBudgetFollowsRuntimeLoopProfile(t *testing.T) {
	t.Setenv("AURA_LOOP_MAX_STEPS", "")
	t.Setenv("AURA_LOOP_MAX_WALLCLOCK_SEC", "")
	client := agenttest.NewFakeClient(agenttest.TextChunks("stop", "ok"))
	r, _, _ := newTestRunner(t, client)
	r.runtime.Replace(client, llm.Config{
		Model: "test-model", ContextWindow: 1_000_000, MaxOutputTokens: 32768,
		LoopMaxSteps: 3, LoopMaxWallclockSec: 7,
	})

	_, ic, cancel, err := r.buildAgent(context.Background(), newConvID(t), uuid.New(), nil)
	if err != nil {
		t.Fatal(err)
	}
	defer cancel()
	if got := ic.Budget.Remaining(); got != 3 {
		t.Fatalf("remaining steps = %d, want the profile's 3", got)
	}
	deadline, ok := ic.Ctx.Deadline()
	if !ok {
		t.Fatal("turn ctx carries no wallclock deadline")
	}
	if left := time.Until(deadline); left > 7*time.Second || left < 5*time.Second {
		t.Fatalf("wallclock deadline in %v, want ~7s from the profile", left)
	}
}
