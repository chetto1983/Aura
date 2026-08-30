package swarm

import (
	"testing"

	"github.com/chetto1983/aura/internal/llm"
)

// The resident delegation worker builds each claimed job's Budget from the
// runtime snapshot (amendment #188): a hot loop profile reaches the next job, and
// a worker wired without a Runtime keeps the boot config → env → default path.
func TestDelegationBudgetFollowsRuntimeLoopProfile(t *testing.T) {
	t.Setenv("AURA_LOOP_MAX_STEPS", "")
	t.Setenv("AURA_LOOP_MAX_WALLCLOCK_SEC", "")
	client := newRouter().route("goal", outcome{kind: "ok", text: "done"})
	rc := testRunConfig(t, client, 25)

	static, err := delegationBudget(rc)
	if err != nil {
		t.Fatal(err)
	}
	if got := static.Remaining(); got != 25 {
		t.Fatalf("no-runtime remaining = %d, want builtin 25", got)
	}

	rc.Runtime = llm.NewRuntime(client, rc.LLM)
	hot := rc.LLM
	hot.LoopMaxSteps = 4
	rc.Runtime.Replace(client, hot)
	fromProfile, err := delegationBudget(rc)
	if err != nil {
		t.Fatal(err)
	}
	if got := fromProfile.Remaining(); got != 4 {
		t.Fatalf("hot-profile remaining = %d, want 4", got)
	}
}
