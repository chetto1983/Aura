package swarm

import (
	"context"
	"errors"
	"testing"

	"github.com/chetto1983/aura/internal/llm"
)

var errNotScripted = errors.New("fakeIdentityResolver: no snapshot scripted for this identity")

// fakeRuntime is a minimal runtimeSnapshotter a test can point at a client
// distinguishable from whatever RunConfig.Client holds — proving Runtime wins
// over Client, the priority resolveWorkerLLM documents.
type fakeRuntime struct {
	snapshot llm.RuntimeSnapshot
}

func (f fakeRuntime) Snapshot() llm.RuntimeSnapshot { return f.snapshot }

// fakeIdentityResolver scripts one snapshot (or an error) per identity id, for
// resolveWorkerLLM's Resolver branch.
type fakeIdentityResolver struct {
	snapshots map[string]llm.RuntimeSnapshot
	err       error
}

func (f *fakeIdentityResolver) SnapshotFor(_ context.Context, identityID string) (llm.RuntimeSnapshot, error) {
	if f.err != nil {
		return llm.RuntimeSnapshot{}, f.err
	}
	snap, ok := f.snapshots[identityID]
	if !ok {
		return llm.RuntimeSnapshot{}, errNotScripted
	}
	return snap, nil
}

// TestSwarmWorkerInheritsParentSnapshot: a worker built with a Runtime port
// returning a distinct client uses THAT client, even though RunConfig.Client
// also carries a (different, stale) one — Runtime wins, matching
// resolveWorkerLLM's documented priority.
func TestSwarmWorkerInheritsParentSnapshot(t *testing.T) {
	runtimeClient := newRouter().route("goal", outcome{kind: "ok", text: "from-runtime"})
	staleClient := newRouter().route("goal", outcome{kind: "ok", text: "from-stale-client-field"})

	rc := testRunConfig(t, staleClient, 25)
	rc.Runtime = fakeRuntime{snapshot: llm.RuntimeSnapshot{
		Client: runtimeClient,
		Config: llm.Config{Model: "m", Provider: "openrouter", TotalTimeoutSec: 30},
	}}
	budget := rc.ParentBudget.Child(1)

	report, _ := runChild(context.Background(), rc, budget, 0, "goal")
	if report.Status != StatusOK {
		t.Fatalf("report.Status = %q, want %q; report=%+v", report.Status, StatusOK, report)
	}
	if report.Summary != "from-runtime" {
		t.Fatalf("report.Summary = %q, want the Runtime-sourced client's answer (Runtime must win over the stale rc.Client)", report.Summary)
	}
}

// TestSwarmWorkerResolvesFromResolverOverRuntime: when BOTH Resolver+IdentityID
// and Runtime are set, the identity resolver wins — a swarm worker must use the
// job's OWN credential, never the process-wide one, when both are available.
func TestSwarmWorkerResolvesFromResolverOverRuntime(t *testing.T) {
	resolverClient := newRouter().route("goal", outcome{kind: "ok", text: "from-resolver"})
	runtimeClient := newRouter().route("goal", outcome{kind: "ok", text: "from-runtime"})

	rc := testRunConfig(t, nil, 25)
	rc.IdentityID = "identity-b"
	rc.Resolver = &fakeIdentityResolver{snapshots: map[string]llm.RuntimeSnapshot{
		"identity-b": {Client: resolverClient, Config: llm.Config{Model: "m", Provider: "openrouter", TotalTimeoutSec: 30}},
	}}
	rc.Runtime = fakeRuntime{snapshot: llm.RuntimeSnapshot{
		Client: runtimeClient,
		Config: llm.Config{Model: "m", Provider: "openrouter", TotalTimeoutSec: 30},
	}}
	budget := rc.ParentBudget.Child(1)

	report, _ := runChild(context.Background(), rc, budget, 0, "goal")
	if report.Status != StatusOK {
		t.Fatalf("report.Status = %q, want %q; report=%+v", report.Status, StatusOK, report)
	}
	if report.Summary != "from-resolver" {
		t.Fatalf("report.Summary = %q, want the resolver-sourced (identity-b's own) client's answer", report.Summary)
	}
}

// TestNilRuntimePortFailsClosed: a RunConfig with NEITHER a Resolver, a
// Runtime, nor an already-resolved Client produces a refusal — never a nil
// llm.Client silently reaching agent.NewLlmAgent, and never a fabricated
// deployment client (T-02-08b).
func TestNilRuntimePortFailsClosed(t *testing.T) {
	rc := testRunConfig(t, nil, 25)
	rc.Client = nil
	rc.Runtime = nil
	rc.Resolver = nil
	budget := rc.ParentBudget.Child(1)

	report, _ := runChild(context.Background(), rc, budget, 0, "goal")
	if report.Status != StatusFailed {
		t.Fatalf("report.Status = %q, want %q — a worker with no credential source must refuse, not proceed with a nil client", report.Status, StatusFailed)
	}
	if report.Error == "" {
		t.Fatal("report.Error is empty on a failed (no-credential) report")
	}
}
