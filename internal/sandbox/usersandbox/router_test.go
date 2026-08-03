package usersandbox

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/chetto1983/aura/internal/config"
	"github.com/chetto1983/aura/internal/identityctx"
)

// fakeBackend is a Backend double for the router unit tests. It records the specs passed to
// Resolve and the handles passed to Suspend, and can be told to fail Resolve (the fail-CLOSED
// path).
type fakeBackend struct {
	t          *testing.T
	resolveErr error

	resolved  []SandboxSpec
	suspended []BoxHandle
}

func (f *fakeBackend) Resolve(_ context.Context, spec SandboxSpec) (BoxHandle, error) {
	f.resolved = append(f.resolved, spec)
	if f.resolveErr != nil {
		return BoxHandle{}, f.resolveErr
	}
	return BoxHandle{ContainerID: "c-" + spec.IdentityID, IdentityID: spec.IdentityID}, nil
}

func (f *fakeBackend) Exec(context.Context, BoxHandle, ExecRequest) (ExecResult, error) {
	return ExecResult{}, nil
}

func (f *fakeBackend) Suspend(_ context.Context, h BoxHandle) error {
	f.suspended = append(f.suspended, h)
	return nil
}

func (f *fakeBackend) Resume(context.Context, BoxHandle) error { return nil }
func (f *fakeBackend) Stop(context.Context, BoxHandle) error   { return nil }

// unitSandboxConfig is a sane, non-degenerate config for the router unit tests.
func unitSandboxConfig() config.SandboxConfig {
	return config.SandboxConfig{
		IdleTTLSec:  1800,
		CPULimit:    2,
		MemoryLimit: 2 << 30,
		PidsLimit:   512,
		Image:       "aura-sandbox:latest",
	}
}

// TestRoute_EveryProfileRoutes replaces the former TestRoute_DevNoOp, which pinned the opposite
// behaviour: dev and local_trusted used to short-circuit Route into a host-direct no-op. That arm
// was deliberately removed — the box is the only filesystem and the only shell now — so the test
// is inverted rather than adapted: under EVERY profile Route must resolve a real box.
func TestRoute_EveryProfileRoutes(t *testing.T) {
	for _, p := range []config.RuntimeProfile{
		config.ProfileDev,
		config.ProfileLocalTrusted,
		config.ProfileSingleUserHardened,
		config.ProfileServerProduction,
	} {
		t.Run(string(p), func(t *testing.T) {
			be := &fakeBackend{t: t}
			r := NewSandboxRouter(be, p, unitSandboxConfig())
			h, err := r.Route(context.Background())
			if err != nil {
				t.Fatalf("Route err = %v, want nil", err)
			}
			if len(be.resolved) != 1 {
				t.Fatalf("resolved %d specs under %s, want 1 — a profile must never bypass the box", len(be.resolved), p)
			}
			if h.ContainerID == "" {
				t.Fatalf("handle = %+v, want a live box handle", h)
			}
		})
	}
}

// TestRoute_FailClosed proves a box-create failure returns (zero handle, err): the fail-CLOSED
// containment invariant (D-09/GATE-01). The caller DENIES on that error — there is no host arm
// for it to fall back to.
func TestRoute_FailClosed(t *testing.T) {
	sentinel := errors.New("box create boom")
	be := &fakeBackend{t: t, resolveErr: sentinel}
	r := NewSandboxRouter(be, config.ProfileSingleUserHardened, unitSandboxConfig())

	h, err := r.Route(context.Background())
	if !errors.Is(err, sentinel) {
		t.Fatalf("err = %v, want %v", err, sentinel)
	}
	if h != (BoxHandle{}) {
		t.Fatalf("handle = %+v, want zero on failure (no host fallback)", h)
	}
}

// TestRoute_MissingBackendFailsClosed covers a composition failure before a concrete backend
// exists. Such a router stays a containment boundary: an error, never a usable handle.
func TestRoute_MissingBackendFailsClosed(t *testing.T) {
	r := NewSandboxRouter(nil, config.ProfileServerProduction, unitSandboxConfig())

	h, err := r.Route(context.Background())
	if err == nil {
		t.Fatal("Route error = nil with a missing backend, want containment error")
	}
	if h != (BoxHandle{}) {
		t.Fatalf("handle = %+v, want zero on missing backend", h)
	}
}

// TestRoute_ZeroValueRouterDeniesWithoutPanicking guards a hazard the collapse created. Only
// NewSandboxRouter allocates lastUsed/handles; the deleted non-strict short-circuit used to
// return before Route ever wrote them, so a zero-value &SandboxRouter{} survived by accident.
// Without the merged nil-receiver/nil-backend guard it would now nil-map-panic instead of denying.
func TestRoute_ZeroValueRouterDeniesWithoutPanicking(t *testing.T) {
	h, err := (&SandboxRouter{}).Route(context.Background())
	if !errors.Is(err, errBackendUnavailable) {
		t.Fatalf("err = %v, want errBackendUnavailable", err)
	}
	if h != (BoxHandle{}) {
		t.Fatalf("handle = %+v, want zero", h)
	}

	var nilRouter *SandboxRouter
	if _, err := nilRouter.Route(context.Background()); !errors.Is(err, errBackendUnavailable) {
		t.Fatalf("nil receiver err = %v, want errBackendUnavailable", err)
	}
	if n, err := (&SandboxRouter{}).SuspendIdle(context.Background(), time.Now()); n != 0 || err != nil {
		t.Fatalf("SuspendIdle on a backend-less router = (%d, %v), want (0, nil)", n, err)
	}
}

// TestRoute_LocalFallback proves the local / no-principal caller keys a `local`-id box (never
// host), and an authenticated principal keys strictly on its own identity (T-37-05-CROSSID).
func TestRoute_LocalFallback(t *testing.T) {
	be := &fakeBackend{t: t}
	r := NewSandboxRouter(be, config.ProfileSingleUserHardened, unitSandboxConfig())

	h, err := r.Route(context.Background()) // empty identityctx → local fallback
	if err != nil {
		t.Fatalf("Route = (%+v, %v), want a live handle and no error", h, err)
	}
	if len(be.resolved) != 1 {
		t.Fatalf("resolved %d specs, want 1", len(be.resolved))
	}
	if got := be.resolved[0].IdentityID; got != localIdentityID {
		t.Fatalf("spec.IdentityID = %q, want seeded local %q", got, localIdentityID)
	}
	if h.IdentityID != localIdentityID {
		t.Fatalf("handle.IdentityID = %q, want %q", h.IdentityID, localIdentityID)
	}

	const authID = "11111111-1111-1111-1111-111111111111"
	be2 := &fakeBackend{t: t}
	r2 := NewSandboxRouter(be2, config.ProfileSingleUserHardened, unitSandboxConfig())
	ctx := identityctx.WithIdentityID(context.Background(), authID)
	if _, err := r2.Route(ctx); err != nil {
		t.Fatalf("Route(auth) err = %v", err)
	}
	if got := be2.resolved[0].IdentityID; got != authID {
		t.Fatalf("spec.IdentityID = %q, want the authenticated id %q (never local, never cross-identity)", got, authID)
	}
}

// TestSuspendIdle_NonPositiveTTLKeepsBoxesWarm pins the single-user appliance posture: a
// non-positive AURA_SANDBOX_IDLE_TTL_SEC means NEVER suspend, so the box stays warm and no turn
// pays a container resume.
//
// This is a regression guard for an inverted knob, not a preference. `ttl := IdleTTLSec *
// time.Second` with IdleTTLSec=0 makes the sweep predicate `now.Sub(last) >= 0` true for EVERY
// tracked box, so the value an operator reaches for to disable the reaper used to suspend
// everything on every tick — the exact opposite, and silent. A negative value (the other way
// people spell "off") did the same.
func TestSuspendIdle_NonPositiveTTLKeepsBoxesWarm(t *testing.T) {
	base := time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)
	for _, ttl := range []int{0, -1} {
		be := &fakeBackend{t: t}
		cfg := unitSandboxConfig()
		cfg.IdleTTLSec = ttl
		r := NewSandboxRouter(be, config.ProfileSingleUserHardened, cfg)
		r.now = func() time.Time { return base }
		if _, err := r.Route(context.Background()); err != nil {
			t.Fatalf("ttl=%d: Route err = %v", ttl, err)
		}
		// Far past any plausible TTL: still nothing, because the sweep is off.
		n, err := r.SuspendIdle(context.Background(), base.Add(24*time.Hour))
		if err != nil {
			t.Fatalf("ttl=%d: SuspendIdle err = %v", ttl, err)
		}
		if n != 0 || len(be.suspended) != 0 {
			t.Errorf("ttl=%d: sweep suspended %d (recorded %d), want 0 — the reaper must be OFF",
				ttl, n, len(be.suspended))
		}
	}
}

// TestRoute_IdleBump proves Route bumps lastUsed and SuspendIdle sweeps only boxes idle PAST the
// TTL, Suspends each, and clears its entry (so a second sweep is a no-op) — the D-08 lifecycle.
func TestRoute_IdleBump(t *testing.T) {
	base := time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)
	be := &fakeBackend{t: t}
	cfg := unitSandboxConfig()
	cfg.IdleTTLSec = 60
	r := NewSandboxRouter(be, config.ProfileSingleUserHardened, cfg)
	r.now = func() time.Time { return base }

	if _, err := r.Route(context.Background()); err != nil {
		t.Fatalf("Route err = %v", err)
	}

	// Before the TTL elapses the sweep suspends nothing.
	n, err := r.SuspendIdle(context.Background(), base.Add(30*time.Second))
	if err != nil {
		t.Fatalf("SuspendIdle err = %v", err)
	}
	if n != 0 || len(be.suspended) != 0 {
		t.Fatalf("premature sweep suspended %d (recorded %d), want 0", n, len(be.suspended))
	}

	// Past the TTL the box is swept.
	n, err = r.SuspendIdle(context.Background(), base.Add(61*time.Second))
	if err != nil {
		t.Fatalf("SuspendIdle err = %v", err)
	}
	if n != 1 || len(be.suspended) != 1 {
		t.Fatalf("post-TTL sweep suspended %d (recorded %d), want 1", n, len(be.suspended))
	}
	if be.suspended[0].IdentityID != localIdentityID {
		t.Fatalf("suspended handle identity = %q, want %q", be.suspended[0].IdentityID, localIdentityID)
	}

	// The suspended box's tracking entry is cleared — a later sweep is a no-op (until the next
	// Route re-resolves and re-tracks it).
	n, err = r.SuspendIdle(context.Background(), base.Add(600*time.Second))
	if err != nil {
		t.Fatalf("SuspendIdle err = %v", err)
	}
	if n != 0 {
		t.Fatalf("second sweep suspended %d, want 0 (entry cleared on suspend)", n)
	}
}

// TestSpecFor_UsesConfiguredKnobs proves specFor sources every knob from cfg.Sandbox (image,
// caps, egress allowlist) into the spec — a CONFIGURED allowlist reaches EgressPolicy, not just
// a test fixture (T-37-05-CONFIGDROP) — and selects Runsc ONLY under server_production (D-12).
func TestSpecFor_UsesConfiguredKnobs(t *testing.T) {
	cfg := config.SandboxConfig{
		IdleTTLSec:      900,
		CPULimit:        7,
		MemoryLimit:     3 << 30,
		PidsLimit:       333,
		EgressAllowlist: []string{"api.example.com", "pypi.org"},
		Image:           "registry.example.com/aura-sandbox@sha256:abc",
	}

	r := NewSandboxRouter(&fakeBackend{t: t}, config.ProfileSingleUserHardened, cfg)
	spec := r.specFor("id-1")

	if spec.Image != cfg.Image {
		t.Fatalf("Image = %q, want %q", spec.Image, cfg.Image)
	}
	if spec.WorkspaceVol != "aura-box-id-1" {
		t.Fatalf("WorkspaceVol = %q, want aura-box-id-1", spec.WorkspaceVol)
	}
	if want := int64(cfg.CPULimit) * nanoCPUsPerCPU; spec.Limits.NanoCPUs != want {
		t.Fatalf("NanoCPUs = %d, want %d", spec.Limits.NanoCPUs, want)
	}
	if spec.Limits.MemoryBytes != cfg.MemoryLimit {
		t.Fatalf("MemoryBytes = %d, want %d", spec.Limits.MemoryBytes, cfg.MemoryLimit)
	}
	if spec.Limits.PidsLimit != cfg.PidsLimit {
		t.Fatalf("PidsLimit = %d, want %d", spec.Limits.PidsLimit, cfg.PidsLimit)
	}
	if !spec.Egress.Floor {
		t.Fatalf("Egress.Floor = false, want true (always-on tenancy floor)")
	}
	if len(spec.Egress.FQDNAllowlist) != len(cfg.EgressAllowlist) {
		t.Fatalf("FQDNAllowlist = %v, want %v", spec.Egress.FQDNAllowlist, cfg.EgressAllowlist)
	}
	for i, fqdn := range cfg.EgressAllowlist {
		if spec.Egress.FQDNAllowlist[i] != fqdn {
			t.Fatalf("FQDNAllowlist[%d] = %q, want %q", i, spec.Egress.FQDNAllowlist[i], fqdn)
		}
	}
	if spec.Runtime != Runc {
		t.Fatalf("Runtime = %v under single_user_hardened, want Runc (D-12: runsc is server_production only)", spec.Runtime)
	}

	rp := NewSandboxRouter(&fakeBackend{t: t}, config.ProfileServerProduction, cfg)
	prodSpec := rp.specFor("id-1")
	if prodSpec.Runtime != Runsc {
		t.Fatalf("Runtime = %v under server_production, want Runsc", prodSpec.Runtime)
	}
	// The Runsc spec is accepted by the containment validator under server_production.
	if _, err := NewSandboxSpec(prodSpec, config.ProfileServerProduction); err != nil {
		t.Fatalf("NewSandboxSpec(server_production) rejected the specFor spec: %v", err)
	}
}
