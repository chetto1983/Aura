package telegram

import (
	"context"
	"errors"
	"testing"

	"github.com/chetto1983/aura/internal/identityctx"
	"github.com/chetto1983/aura/internal/llm"
)

var errNoSnapshotScripted = errors.New("fakeCommandsResolver: no snapshot scripted for this identity")

// fakeCommandsResolver scripts one RuntimeSnapshot per identity id, for
// commandDeps.Resolver.
type fakeCommandsResolver struct {
	snapshots map[string]llm.RuntimeSnapshot
}

func (f *fakeCommandsResolver) SnapshotFor(_ context.Context, identityID string) (llm.RuntimeSnapshot, error) {
	snap, ok := f.snapshots[identityID]
	if !ok {
		return llm.RuntimeSnapshot{}, errNoSnapshotScripted
	}
	return snap, nil
}

// TestTelegramTurnResolvesFromLinkedIdentity: the chat's linked identity's OWN
// model profile is used — never the process-wide Runtime's — when a Resolver is
// wired and the identity is present on ctx (CRED-07/D-11 extended to this
// channel's /cost render, T-02-08b).
func TestTelegramTurnResolvesFromLinkedIdentity(t *testing.T) {
	t.Parallel()
	identityCfg := llm.Config{Model: "identity-b-model", Provider: "openrouter"}
	runtime := llm.NewRuntime(nil, llm.Config{Model: "runtime-model", Provider: "openrouter"})
	resolver := &fakeCommandsResolver{snapshots: map[string]llm.RuntimeSnapshot{
		"identity-b": {Config: identityCfg},
	}}

	cmds := newTestCommands(commandDeps{Runtime: runtime, Resolver: resolver})

	ctx := identityctx.WithIdentityID(context.Background(), "identity-b")
	_, model, _, _ := cmds.activeModelProfile(ctx)
	if model != "identity-b-model" {
		t.Fatalf("model = %q, want the linked identity's own model (identity-b-model), not the process-wide runtime's", model)
	}
}

// TestTelegramFallsBackToRuntimeWithNoLinkedIdentity: with no identity on ctx
// (dispatchRich's daemonCtx today, per bot_dispatch.go — a documented open
// wiring item, see the SUMMARY), or an identity the resolver has no snapshot
// for, activeModelProfile degrades to the process-wide Runtime rather than
// failing the command.
func TestTelegramFallsBackToRuntimeWithNoLinkedIdentity(t *testing.T) {
	t.Parallel()
	runtime := llm.NewRuntime(nil, llm.Config{Model: "runtime-model", Provider: "openrouter"})
	resolver := &fakeCommandsResolver{snapshots: map[string]llm.RuntimeSnapshot{}}

	cmds := newTestCommands(commandDeps{Runtime: runtime, Resolver: resolver})

	_, model, _, _ := cmds.activeModelProfile(context.Background())
	if model != "runtime-model" {
		t.Fatalf("model = %q, want the process-wide runtime's model when no identity is linked on ctx", model)
	}
}
