package swarm

import (
	"context"
	"testing"

	"github.com/chetto1983/aura/internal/agent"
	"github.com/chetto1983/aura/internal/agent/tools"
	"go.uber.org/goleak"
)

// withToolCtx layers the tool-call spillover context the production runTool injects
// alongside the swarm context, so tools.NewResult inside the adapter can place its
// sidecar (the adapter always runs under a runTool-built ctx in production).
func withToolCtx(ctx context.Context, t *testing.T) context.Context {
	return tools.WithToolCallContext(ctx, "sess-adapter", "call-adapter", t.TempDir(), 2048)
}

// TestRunnerAdapterMissingContext pins Amendment #103: a dispatch outside the
// agent loop is a typed execution error, never an OK result containing error text.
func TestRunnerAdapterMissingContext(t *testing.T) {
	defer goleak.VerifyNone(t)
	a := NewRunnerAdapter(testRunConfig(t, newRouter(), 25).Cfg)
	_, err := a.Run(withToolCtx(context.Background(), t), []string{"x"})
	if err == nil {
		t.Fatal("missing context must return an execution error")
	}
}

// TestRunnerAdapterDrivesEngine drives the full adapter path through the ctx seam:
// WithSwarmContext carries the parent budget/registry/client/cfg/convID, the adapter
// reads them off the ctx, and the engine fans the goals out. The worker registry the
// engine builds excludes swarm_spawn (D-08/D-10 — proven by the worker being able to
// run with swarm_spawn dropped; the Without exclusion is re-asserted directly below).
func TestRunnerAdapterDrivesEngine(t *testing.T) {
	defer goleak.VerifyNone(t)
	r := newRouter().
		route("alpha", outcome{kind: "ok", text: "A done"}).
		route("bravo", outcome{kind: "ok", text: "B done"})
	rc := testRunConfig(t, r, 25)

	ctx := agent.WithSwarmContext(withToolCtx(context.Background(), t),
		rc.ParentBudget, rc.ParentRegistry, rc.Client, rc.LLM, rc.ConvID, rc.Gateway)
	a := NewRunnerAdapter(rc.Cfg)

	res, err := a.Run(ctx, []string{"alpha task", "bravo task"})
	if err != nil {
		t.Fatalf("adapter Run: %v", err)
	}
	reports := parseReports(t, res.Preview)
	if len(reports) != 2 {
		t.Fatalf("want 2 reports, got %d (%q)", len(reports), res.Preview)
	}
	if reports[0].Status != StatusOK || reports[1].Status != StatusOK {
		t.Fatalf("both workers should finish ok: %+v", reports)
	}
	if res.Provenance == nil {
		t.Fatal("swarm reports must carry untrusted provenance")
	}
	if res.Provenance.Source != "swarm" || res.Provenance.Trust != tools.TrustUntrusted {
		t.Fatalf("swarm provenance = %+v, want source=swarm trust=untrusted", res.Provenance)
	}
}

// TestRunnerAdapterWorkerRegistryExcludesSwarmSpawn re-asserts the flat-v1 invariant
// the adapter relies on: the worker registry the engine derives via
// Without(parentRegistry, "swarm_spawn") never contains swarm_spawn (D-08/D-10), so a
// worker cannot recursively fan out (T-09-12 mitigation).
func TestRunnerAdapterWorkerRegistryExcludesSwarmSpawn(t *testing.T) {
	parent := workerRegistry() // includes a stub swarm_spawn entry
	if _, ok := parent.Get(swarmSpawnTool); !ok {
		t.Fatal("precondition: the parent registry must carry swarm_spawn")
	}
	worker := tools.Without(parent, swarmSpawnTool)
	if _, ok := worker.Get(swarmSpawnTool); ok {
		t.Fatal("the worker registry must NOT contain swarm_spawn (flat v1, D-08/D-10)")
	}
}
