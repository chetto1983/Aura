package usersandbox

import (
	"context"
	"errors"
	"testing"

	"github.com/chetto1983/aura/internal/config"
)

// router_provision_test.go covers EnsureBox (D-09): the explicit-identity get-or-create seam
// the provisioning saga's sandbox leg calls, as distinct from Route's context-derived,
// local-fallback seam every tool call uses. These are daemon-free unit tests against a fake
// Backend — no Docker daemon required (CLAUDE.md: daemon-gated runtime code needs daemon-free
// unit tests for its pure logic too).

// TestEnsureBoxRefusesEmptyIdentity proves EnsureBox fail-closes on a blank identityID rather
// than falling back to the seeded `local` identity the way Route's context path does — a
// dropped identity stamp must never provision one identity's box under another's key
// (T-01-02).
func TestEnsureBoxRefusesEmptyIdentity(t *testing.T) {
	be := &fakeBackend{t: t}
	r := NewSandboxRouter(be, config.ProfileSingleUserHardened, unitSandboxConfig())

	h, err := r.EnsureBox(context.Background(), "")
	if err == nil {
		t.Fatal("EnsureBox(\"\") err = nil, want a refusal error")
	}
	if h != (BoxHandle{}) {
		t.Fatalf("EnsureBox(\"\") handle = %+v, want zero", h)
	}
	if len(be.resolved) != 0 {
		t.Fatalf("EnsureBox(\"\") called the backend %d times, want 0 (never falls back to local)", len(be.resolved))
	}
}

// TestEnsureBoxIsIdempotent proves two EnsureBox calls for the same identityID resolve the
// same box key and both reach the backend get-or-create seam (the backend itself, not the
// router, owns idempotent creation — Resolve is documented as "idempotent get-or-create").
func TestEnsureBoxIsIdempotent(t *testing.T) {
	be := &fakeBackend{t: t}
	r := NewSandboxRouter(be, config.ProfileSingleUserHardened, unitSandboxConfig())
	const id = "22222222-2222-2222-2222-222222222222"

	h1, err := r.EnsureBox(context.Background(), id)
	if err != nil {
		t.Fatalf("first EnsureBox err = %v, want nil", err)
	}
	h2, err := r.EnsureBox(context.Background(), id)
	if err != nil {
		t.Fatalf("second EnsureBox err = %v, want nil", err)
	}
	if h1 != h2 {
		t.Fatalf("EnsureBox handles differ across calls: %+v vs %+v, want the same box key", h1, h2)
	}
	if len(be.resolved) != 2 {
		t.Fatalf("backend Resolve called %d times, want 2 (idempotent create-or-get, not router-side caching)", len(be.resolved))
	}
	for _, spec := range be.resolved {
		if spec.IdentityID != id {
			t.Fatalf("resolved spec.IdentityID = %q, want %q", spec.IdentityID, id)
		}
	}
}

// TestEnsureBoxNilReceiverAndBackendDenies mirrors Route's nil-receiver/nil-backend shared
// denial point: a zero-value/nil router must deny rather than nil-map-panic.
func TestEnsureBoxNilReceiverAndBackendDenies(t *testing.T) {
	h, err := (&SandboxRouter{}).EnsureBox(context.Background(), "id-1")
	if !errors.Is(err, errBackendUnavailable) {
		t.Fatalf("zero-value router err = %v, want errBackendUnavailable", err)
	}
	if h != (BoxHandle{}) {
		t.Fatalf("zero-value router handle = %+v, want zero", h)
	}

	var nilRouter *SandboxRouter
	if _, err := nilRouter.EnsureBox(context.Background(), "id-1"); !errors.Is(err, errBackendUnavailable) {
		t.Fatalf("nil receiver err = %v, want errBackendUnavailable", err)
	}
}

// TestEnsureBoxPropagatesBackendFailure proves an unreachable backend fails the caller rather
// than returning a zero-value handle silently — the sandbox provisioning leg journals a
// FAILURE, never a success it cannot back up.
func TestEnsureBoxPropagatesBackendFailure(t *testing.T) {
	sentinel := errors.New("box create boom")
	be := &fakeBackend{t: t, resolveErr: sentinel}
	r := NewSandboxRouter(be, config.ProfileSingleUserHardened, unitSandboxConfig())

	h, err := r.EnsureBox(context.Background(), "id-2")
	if !errors.Is(err, sentinel) {
		t.Fatalf("err = %v, want %v", err, sentinel)
	}
	if h != (BoxHandle{}) {
		t.Fatalf("handle = %+v, want zero on backend failure", h)
	}
}
