package runner

import (
	"context"
	"errors"
	"testing"

	"github.com/chetto1983/aura/internal/identityctx"
	"github.com/chetto1983/aura/internal/llm"
)

// stubIdentitySnapshotter records the identity it was asked about and returns a fixed
// answer, so a test asserts on WHICH credential a turn resolved rather than merely on
// the absence of an error — the distinction CRED-07 turns on.
type stubIdentitySnapshotter struct {
	asked    []string
	snapshot llm.RuntimeSnapshot
	err      error
}

func (s *stubIdentitySnapshotter) SnapshotFor(_ context.Context, identityID string) (llm.RuntimeSnapshot, error) {
	s.asked = append(s.asked, identityID)
	return s.snapshot, s.err
}

func markerConfig(model string) llm.Config { return llm.Config{Model: model} }

// TestTurnLLMSnapshotPrefersTheContextSnapshot proves a caller that already scoped the
// context (ScopeContextToIdentitySnapshot, or a nested resume re-entering the turn) is
// not second-guessed — its decision stands and the resolver is never consulted.
func TestTurnLLMSnapshotPrefersTheContextSnapshot(t *testing.T) {
	t.Parallel()
	stub := &stubIdentitySnapshotter{snapshot: llm.RuntimeSnapshot{Config: markerConfig("resolver")}}
	r := &Runner{identityLLM: stub, runtime: llm.NewRuntime(nil, markerConfig("deployment"))}

	ctx := withLLMRuntimeSnapshot(identityctx.WithIdentityID(context.Background(), "id-a"),
		llm.RuntimeSnapshot{Config: markerConfig("already-scoped")})

	got, err := r.turnLLMSnapshot(ctx)
	if err != nil {
		t.Fatalf("turnLLMSnapshot: %v", err)
	}
	if got.Config.Model != "already-scoped" {
		t.Errorf("model = %q, want the snapshot already on ctx", got.Config.Model)
	}
	if len(stub.asked) != 0 {
		t.Errorf("resolver consulted %v, want not consulted when ctx already carries a snapshot", stub.asked)
	}
}

// TestTurnLLMSnapshotResolvesFromTheTurnIdentity is the invariant this seam exists for:
// with a resolver injected, the turn's credential comes from the identity that owns the
// conversation, never from the process-wide deployment snapshot (CRED-07, D-11).
func TestTurnLLMSnapshotResolvesFromTheTurnIdentity(t *testing.T) {
	t.Parallel()
	stub := &stubIdentitySnapshotter{snapshot: llm.RuntimeSnapshot{Config: markerConfig("identity-b")}}
	r := &Runner{identityLLM: stub, runtime: llm.NewRuntime(nil, markerConfig("deployment"))}

	got, err := r.turnLLMSnapshot(identityctx.WithIdentityID(context.Background(), "id-b"))
	if err != nil {
		t.Fatalf("turnLLMSnapshot: %v", err)
	}
	if got.Config.Model != "identity-b" {
		t.Errorf("model = %q, want the identity's own snapshot — the deployment client is the fail-open path D-11 closes", got.Config.Model)
	}
	if len(stub.asked) != 1 || stub.asked[0] != "id-b" {
		t.Errorf("resolver asked about %v, want exactly [id-b]", stub.asked)
	}
}

// TestTurnLLMSnapshotRefusesAnIdentitylessTurn covers the fail-closed branch: a turn that
// reached this seam with a resolver injected but nothing on ctx cannot be billed to
// anybody, so serving it the deployment client would charge the operator for it.
func TestTurnLLMSnapshotRefusesAnIdentitylessTurn(t *testing.T) {
	t.Parallel()
	stub := &stubIdentitySnapshotter{snapshot: llm.RuntimeSnapshot{Config: markerConfig("identity")}}
	r := &Runner{identityLLM: stub, runtime: llm.NewRuntime(nil, markerConfig("deployment"))}

	got, err := r.turnLLMSnapshot(context.Background())
	if !errors.Is(err, ErrTurnIdentityUnknown) {
		t.Fatalf("err = %v, want ErrTurnIdentityUnknown", err)
	}
	if got.Client != nil || got.Config.Model != "" {
		t.Errorf("snapshot = %+v, want the zero value — a refusal must not hand back a usable client", got)
	}
	if len(stub.asked) != 0 {
		t.Errorf("resolver asked about %v, want not consulted without an identity", stub.asked)
	}
}

// TestTurnLLMSnapshotPropagatesAResolverRefusal proves a refusal from the resolver
// (ErrNoIdentityLLMKey, or a store outage) reaches the caller as an error instead of
// degrading to the deployment client — the substitution CRED-07 forbids is exactly the
// one a swallowed error would perform.
func TestTurnLLMSnapshotPropagatesAResolverRefusal(t *testing.T) {
	t.Parallel()
	refusal := errors.New("no key for this identity")
	stub := &stubIdentitySnapshotter{err: refusal}
	r := &Runner{identityLLM: stub, runtime: llm.NewRuntime(nil, markerConfig("deployment"))}

	got, err := r.turnLLMSnapshot(identityctx.WithIdentityID(context.Background(), "id-c"))
	if !errors.Is(err, refusal) {
		t.Fatalf("err = %v, want the resolver's own refusal", err)
	}
	if got.Config.Model == "deployment" {
		t.Error("a refused identity was served the deployment snapshot — the fail-open path D-11 closes")
	}
}

// TestTurnLLMSnapshotFallsBackWithoutAResolver pins the one deployment shape where the
// process-wide snapshot is still correct: no key store was injected at all, so there is
// nothing to resolve against. This is the CLI REPL and this package's own unit tests, and
// it must keep working unchanged.
func TestTurnLLMSnapshotFallsBackWithoutAResolver(t *testing.T) {
	t.Parallel()
	r := &Runner{runtime: llm.NewRuntime(nil, markerConfig("deployment"))}

	got, err := r.turnLLMSnapshot(identityctx.WithIdentityID(context.Background(), "id-d"))
	if err != nil {
		t.Fatalf("turnLLMSnapshot without a resolver: %v", err)
	}
	if got.Config.Model != "deployment" {
		t.Errorf("model = %q, want the process snapshot when no resolver is wired", got.Config.Model)
	}
}
