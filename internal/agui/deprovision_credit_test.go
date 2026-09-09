package agui

import (
	"context"
	"errors"
	"sync"
	"testing"
)

// deprovision_credit_test.go proves the plan 02-06 reverse-saga revoke leg: a purge
// revokes the identity's OpenRouter key exactly once (keyed on identity id, not hash —
// the adapter owns the store lookup), the step converges on a repeat call, a nil port
// skips the plane, a revoke that cannot prove the key is gone fails the step rather than
// silently succeeding, and every other leg TestDeprovisionPurgeReversesEveryLeg already
// proves still fires with the new step present.

// fakeOpenRouterRevoker is the OpenRouterKeyRevoker double for this file's tests. err,
// when set, simulates the adapter's own failure path (openrouterprovision.RevokeKey
// reporting the key is still readable) — a real failure, never a silent success.
type fakeOpenRouterRevoker struct {
	mu    sync.Mutex
	calls []string
	err   error
	// order, when non-nil, records this call's position in a shared cross-leg timeline
	// so a test can assert THIS leg ran before another leg's own order-aware fake.
	order *[]string
}

func (f *fakeOpenRouterRevoker) RevokeKey(_ context.Context, identityID string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls = append(f.calls, identityID)
	if f.order != nil {
		*f.order = append(*f.order, "openrouter_key")
	}
	return f.err
}

func (f *fakeOpenRouterRevoker) count() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.calls)
}

// orderedIdentityDeleter wraps fakeIdentityDeleter (deprovision_test.go, same package)
// to additionally record its position in a shared timeline — declared here rather than
// widening the shared fake, since only this file's ordering assertion needs it.
type orderedIdentityDeleter struct {
	inner *fakeIdentityDeleter
	order *[]string
}

func (o orderedIdentityDeleter) DeleteIdentity(ctx context.Context, name string) error {
	*o.order = append(*o.order, "identity_row")
	return o.inner.DeleteIdentity(ctx, name)
}

// TestPurgeRevokesOpenRouterKey proves a purge calls the revoker exactly once with the
// identity's id, journals sagaStepOpenRouterKey, and revokes BEFORE the identity row is
// torn down — the reverse of the provisioning order, where the key is minted last.
func TestPurgeRevokesOpenRouterKey(t *testing.T) {
	deps, f := fullDeprovisionDeps()
	var order []string
	revoker := &fakeOpenRouterRevoker{order: &order}
	deps.OpenRouterKey = revoker
	deps.IdentityDelete = orderedIdentityDeleter{inner: f.iddel, order: &order}
	d := NewDeprovisioner(deps)

	target := targetFor(testIdentityID)
	if err := d.Purge(context.Background(), target); err != nil {
		t.Fatalf("Purge: %v", err)
	}
	if revoker.count() != 1 {
		t.Fatalf("revoke calls = %d, want 1", revoker.count())
	}
	if revoker.calls[0] != testIdentityID {
		t.Fatalf("revoke called with %q, want the target identity id %q", revoker.calls[0], testIdentityID)
	}
	sid := sagaID(sagaKindDeprovision, testIdentityID)
	if !f.journal.stepDone(sid, sagaStepOpenRouterKey) {
		t.Fatal("deprovision journal step for the OpenRouter key not marked done")
	}
	if len(order) != 2 || order[0] != "openrouter_key" || order[1] != "identity_row" {
		t.Fatalf("teardown order = %v, want [openrouter_key identity_row] (revoke before the identity row is torn down)", order)
	}
}

// TestPurgeRevokeIsIdempotent proves a second purge for the same identity — no journal
// wired, so the step genuinely re-runs rather than being skipped by forward-recovery —
// still succeeds: a repeat RevokeKey call on an already-revoked key converges, matching
// the file header's Delete/Deny by-id 404=success contract for every other plane.
func TestPurgeRevokeIsIdempotent(t *testing.T) {
	deps, _ := fullDeprovisionDeps()
	deps.Journal = nil
	revoker := &fakeOpenRouterRevoker{}
	deps.OpenRouterKey = revoker
	d := NewDeprovisioner(deps)
	target := targetFor(testIdentityID)

	if err := d.Purge(context.Background(), target); err != nil {
		t.Fatalf("first Purge: %v", err)
	}
	if err := d.Purge(context.Background(), target); err != nil {
		t.Fatalf("second Purge (key already gone): %v", err)
	}
	if revoker.count() != 2 {
		t.Fatalf("revoke calls = %d, want 2 (both purges must call through, and both must succeed)", revoker.count())
	}
}

// TestPurgeWithNilRevokerSkipsPlane proves a nil OpenRouterKey port skips the plane
// without error, matching the existing nil-port convention every other leg follows.
func TestPurgeWithNilRevokerSkipsPlane(t *testing.T) {
	deps, _ := fullDeprovisionDeps()
	deps.OpenRouterKey = nil
	d := NewDeprovisioner(deps)

	if err := d.Purge(context.Background(), targetFor(testIdentityID)); err != nil {
		t.Fatalf("Purge with a nil OpenRouterKey port: %v", err)
	}
}

// TestPurgeRevokeFailureDoesNotSilentlySucceed proves a revoker that reports the key is
// still readable fails the step — a purge must never claim to have revoked a key that
// is still alive at the provider (CRED-08), and the identity/Authula legs after it must
// not run.
func TestPurgeRevokeFailureDoesNotSilentlySucceed(t *testing.T) {
	deps, f := fullDeprovisionDeps()
	revoker := &fakeOpenRouterRevoker{err: errors.New("openrouterprovision: revoke key: verification failed: key is still readable")}
	deps.OpenRouterKey = revoker
	d := NewDeprovisioner(deps)

	if err := d.Purge(context.Background(), targetFor(testIdentityID)); err == nil {
		t.Fatal("want error when the revoke cannot verify the key is gone")
	}
	if f.iddel.count() != 0 || f.authdel.count() != 0 {
		t.Fatalf("identity/authula torn down despite an unverified revoke: id=%d authula=%d",
			f.iddel.count(), f.authdel.count())
	}
}

// TestPurgeStillReversesEveryOtherLeg re-runs TestDeprovisionPurgeReversesEveryLeg's own
// assertions (deprovision_test.go) with the new step present, proving the leg was ADDED
// to the sequence rather than substituted for one of the others.
func TestPurgeStillReversesEveryOtherLeg(t *testing.T) {
	deps, f := fullDeprovisionDeps()
	revoker := &fakeOpenRouterRevoker{}
	deps.OpenRouterKey = revoker
	d := NewDeprovisioner(deps)

	if err := d.Purge(context.Background(), targetFor(testIdentityID)); err != nil {
		t.Fatalf("Purge: %v", err)
	}
	if f.conv.count() != 1 || f.memory.count() != 1 {
		t.Fatalf("data-plane purge: conversations=%d memory=%d, want 1/1", f.conv.count(), f.memory.count())
	}
	if f.os.deprovCalls[testIdentityID] != 1 || f.fs.deprovCalls[testIdentityID] != 1 {
		t.Fatalf("resource-plane purge: objectstore=%d filesystem=%d, want 1/1",
			f.os.deprovCalls[testIdentityID], f.fs.deprovCalls[testIdentityID])
	}
	if f.iddel.count() != 1 || f.authdel.count() != 1 {
		t.Fatalf("identity/authula delete: id=%d authula=%d, want 1/1", f.iddel.count(), f.authdel.count())
	}
	if revoker.count() != 1 {
		t.Fatalf("openrouter revoke calls = %d, want 1 (added, not substituted)", revoker.count())
	}
	sid := sagaID(sagaKindDeprovision, testIdentityID)
	for _, step := range []string{sagaStepOpenRouterKey, sagaStepConversations, sagaStepMemory, sagaStepObjectStore, sagaStepDirs, sagaStepIdentityRow, sagaStepAuthula} {
		if !f.journal.stepDone(sid, step) {
			t.Errorf("deprovision journal step %q not marked done", step)
		}
	}
}
