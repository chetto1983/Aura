package handlers

import (
	"context"
	"errors"
	"testing"

	"github.com/chetto1983/aura/internal/agent/agenttest"
	"github.com/chetto1983/aura/internal/identityctx"
	"github.com/chetto1983/aura/internal/llm"
)

var errNotScriptedForIdentity = errors.New("fakeAgentJobResolver: no snapshot scripted for this identity")

// fakeAgentJobResolver scripts one RuntimeSnapshot per identity id, for
// AgentDeps.Resolver.
type fakeAgentJobResolver struct {
	snapshots map[string]llm.RuntimeSnapshot
}

func (f *fakeAgentJobResolver) SnapshotFor(_ context.Context, identityID string) (llm.RuntimeSnapshot, error) {
	snap, ok := f.snapshots[identityID]
	if !ok {
		return llm.RuntimeSnapshot{}, errNotScriptedForIdentity
	}
	return snap, nil
}

// TestCronResolvesFromJobOwningIdentity: a scheduled run has no HTTP principal,
// so the credential comes from identityctx.IdentityID(ctx) — set by cron's own
// scheduledOperationContext from the task row before Run is called — not from
// any per-request principal. The identity's OWN client wins over the
// process-wide Runtime, exactly as the swarm worker's Resolver-over-Runtime
// priority does.
func TestCronResolvesFromJobOwningIdentity(t *testing.T) {
	identityClient := agenttest.NewFakeClient(agenttest.TextChunks("stop", "from-identity-b"))
	runtimeClient := agenttest.NewFakeClient(agenttest.TextChunks("stop", "from-runtime"))
	runtime := llm.NewRuntime(runtimeClient, llm.Config{Model: "process-model"})

	h := AgentJobHandler{Deps: AgentDeps{
		Runtime:  runtime,
		Registry: jobRegistry(),
		Resolver: &fakeAgentJobResolver{snapshots: map[string]llm.RuntimeSnapshot{
			"identity-b": {Client: identityClient, Config: llm.Config{Model: "identity-b-model"}},
		}},
	}}

	ctx := identityctx.WithIdentityID(context.Background(), "identity-b")
	if _, err := h.Run(ctx, Job{Payload: []byte(`{"goal":"use my own identity"}`), RunID: "run-identity"}); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if identityClient.CallCount() == 0 {
		t.Fatal("identity-b's own client was never called")
	}
	if runtimeClient.CallCount() != 0 {
		t.Fatalf("the process-wide runtime client was called %d time(s), want 0 — the job's own identity credential must win", runtimeClient.CallCount())
	}
}

// TestCronFallsBackToRuntimeWithNoIdentityOnContext: a headless system task with
// no owning identity on ctx (e.g. identityctx.IdentityID returns "") degrades to
// the process-wide Runtime rather than refusing outright — Resolver alone is not
// sufficient without an identity to resolve.
func TestCronFallsBackToRuntimeWithNoIdentityOnContext(t *testing.T) {
	runtimeClient := agenttest.NewFakeClient(agenttest.TextChunks("stop", "from-runtime"))
	runtime := llm.NewRuntime(runtimeClient, llm.Config{Model: "process-model"})

	h := AgentJobHandler{Deps: AgentDeps{
		Runtime:  runtime,
		Registry: jobRegistry(),
		Resolver: &fakeAgentJobResolver{snapshots: map[string]llm.RuntimeSnapshot{}},
	}}

	if _, err := h.Run(context.Background(), Job{Payload: []byte(`{"goal":"no identity on ctx"}`), RunID: "run-no-identity"}); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if runtimeClient.CallCount() == 0 {
		t.Fatal("the process-wide runtime client was never called — should have been the fallback")
	}
}

// TestNilRuntimePortFailsClosed: an AgentDeps with NEITHER a Resolver, a
// Runtime, nor an already-set Client refuses the run rather than proceeding
// with a nil client (T-02-08b) — the same fail-closed contract
// internal/swarm's own TestNilRuntimePortFailsClosed asserts for RunConfig.
func TestNilRuntimePortFailsClosed(t *testing.T) {
	h := AgentJobHandler{Deps: AgentDeps{Registry: jobRegistry()}}

	_, err := h.Run(context.Background(), Job{Payload: []byte(`{"goal":"no credential source"}`), RunID: "run-refused"})
	if err == nil {
		t.Fatal("Run with no Resolver/Runtime/Client: want an error, got nil")
	}
	if !errors.Is(err, errAgentJobNoCredential) {
		t.Fatalf("Run err = %v, want errAgentJobNoCredential", err)
	}
}
