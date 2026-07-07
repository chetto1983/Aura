// shell_bg_sandbox_test.go unit-proves the 37-09 box-routing invariants WITHOUT a live dockerd:
// a routed-but-box-failed background START denies fail-CLOSED (no host process spawned,
// T-37-09-FAILOPEN), and the owner/authority layer (random ids + (identity, session) binding +
// owner-only authorizeCaller) is IDENTICAL after the change (T-37-09-AUTHREG). The live
// runs-in-the-box + poll/kill proof is the docker_integration tier (shell_bg_sandbox_docker_test.go).

package tools

import (
	"context"
	"testing"

	"github.com/chetto1983/aura/internal/config"
	"github.com/chetto1983/aura/internal/sandbox/usersandbox"
)

// stubBackend implements the 5-verb usersandbox.Backend but NOT the background streaming verb, so a
// router built over it makes ExecStream fail — the exact routed-but-box-failed START condition.
type stubBackend struct{}

func (stubBackend) Resolve(context.Context, usersandbox.SandboxSpec) (usersandbox.BoxHandle, error) {
	return usersandbox.BoxHandle{ContainerID: "c", IdentityID: "id"}, nil
}

func (stubBackend) Exec(context.Context, usersandbox.BoxHandle, usersandbox.ExecRequest) (usersandbox.ExecResult, error) {
	return usersandbox.ExecResult{}, nil
}

func (stubBackend) Suspend(context.Context, usersandbox.BoxHandle) error { return nil }
func (stubBackend) Resume(context.Context, usersandbox.BoxHandle) error  { return nil }
func (stubBackend) Stop(context.Context, usersandbox.BoxHandle) error    { return nil }

func strictStubRouter() *usersandbox.SandboxRouter {
	return usersandbox.NewSandboxRouter(stubBackend{}, config.ProfileSingleUserHardened, config.SandboxConfig{
		Image: "x", CPULimit: 1, MemoryLimit: 1 << 30, PidsLimit: 128, IdleTTLSec: 1800,
	})
}

// TestShellBg_FailClosedStart: a routed background START whose box exec-stream is unavailable DENIES
// (returns an error the tool maps to sandbox_unavailable) and spawns NO host process — the
// fail-CLOSED containment invariant for background jobs (D-09 / GATE-01).
func TestShellBg_FailClosedStart(t *testing.T) {
	bg := NewBackgroundShells(strictStubRouter())
	h := usersandbox.BoxHandle{ContainerID: "c", IdentityID: "id"}

	id, err := bg.startBox(context.Background(), h, "sleep 60", "", nil)
	if err == nil {
		t.Fatal("startBox must DENY when the box exec-stream is unavailable (fail-CLOSED); got no error")
	}
	if id != "" {
		t.Fatalf("startBox returned id %q on a failed box start, want empty (no job)", id)
	}
	// No half-registered / leaked job survives the failed start, and no host process was spawned.
	bg.mu.Lock()
	n := len(bg.shells)
	bg.mu.Unlock()
	if n != 0 {
		t.Fatalf("registry holds %d shells after a failed box start, want 0 (no host fallback)", n)
	}
}

// TestShellBg_OwnerModelUnchanged: the owner/authority layer is IDENTICAL after the box-routing
// change — startBox and start share newShell, so a routed job binds its (identity, session) owner
// exactly as a host job does, IDs stay unguessable, and owner-only authorizeCaller still gates
// poll/kill (MUSR-03/04 regression guard).
func TestShellBg_OwnerModelUnchanged(t *testing.T) {
	bg := NewBackgroundShells(nil)
	ownerCtx := ctxWithIdentity(t, "sess-A", "call-A", "owner-1")

	sh := bg.newShell(ownerCtx, "id-1")
	if sh.ownerID != "owner-1" {
		t.Fatalf("ownerID = %q, want owner-1 (identity binding at start)", sh.ownerID)
	}
	if sh.sessionID != shellSessionKey(ownerCtx) {
		t.Fatalf("sessionID = %q, want %q (session binding at start)", sh.sessionID, shellSessionKey(ownerCtx))
	}
	if sh.ttl != bg.ttl {
		t.Fatalf("ttl = %v, want %v (per-job TTL captured at start)", sh.ttl, bg.ttl)
	}

	// The owner is authorized; a foreign (identity, session) is denied with nil caps (owner-only,
	// fail-closed) — the poll/kill authority is unchanged.
	if !authorizeCaller(ownerCtx, nil, sh) {
		t.Fatal("owner must be authorized to poll/kill its own job")
	}
	foreign := ctxWithIdentity(t, "sess-B", "call-B", "other-2")
	if authorizeCaller(foreign, nil, sh) {
		t.Fatal("a foreign caller must be denied (owner-only, fail-closed) — authority model regressed")
	}

	// IDs stay unguessable 128-bit crypto-random hex (never sequential, never colliding).
	a, err := newBackgroundShellID()
	if err != nil {
		t.Fatalf("newBackgroundShellID: %v", err)
	}
	b2, err := newBackgroundShellID()
	if err != nil {
		t.Fatalf("newBackgroundShellID: %v", err)
	}
	if a == b2 || len(a) != 32 {
		t.Fatalf("background ids not unguessable/unique: %q, %q", a, b2)
	}
}
