// router.go interposes the per-identity sandbox ROUTING SEAM (SBX-01/GATE-01): Route is the
// single get-or-create entry point EVERY tool call passes through. There is no host arm behind
// it any more — the per-identity box is the only filesystem and the only shell the agent has, on
// every profile. Route therefore has exactly TWO outcomes: a live BoxHandle, or an error the
// caller turns into a DENY (the fail-CLOSED containment invariant, D-09/GATE-01). specFor sources
// the box image, cgroup caps, and the egress FQDN allowlist from the AURA_SANDBOX_* config surface
// (cfg.Sandbox, 37-01), so a CONFIGURED allowlist reaches EgressPolicy, not just a test fixture.
// The live idle-suspend reaper impl (SuspendIdle, the handlers.SandboxReaper seam) lives in
// reap.go over the lastUsed map bumped here on each Route.
//
// The collapse to one successful outcome closed a live defect: while a host arm existed, a tool
// could write a file into the aura container's own volume and report a path the box — where the
// agent's shell actually looks — could not see (document_open / Clienti.xlsx, 2026-08-03).

package usersandbox

import (
	"context"
	"errors"
	"strings"
	"sync"
	"time"

	"github.com/chetto1983/aura/internal/config"
	"github.com/chetto1983/aura/internal/identityctx"
)

// localIdentityID is the migration-0004 seeded `local` identity — the CLI / no-principal
// fallback owner (the same UUID as tools.localOwnerID and agui's localIdentityID). A tool call
// made without an authenticated principal resolves to, and is contained in, `local`'s box —
// never the host.
const localIdentityID = "00000000-0000-0000-0000-000000000001"

// nanoCPUsPerCPU converts a whole-CPU count (cfg.CPULimit) to moby's NanoCPUs unit (1 CPU =
// 1e9 nano-CPUs), the container.Resources.NanoCPUs cgroup cap (D-14).
const nanoCPUsPerCPU = 1_000_000_000

// errBackendUnavailable is returned by a router whose provider could not be composed. It is a
// DENIAL, not a degraded mode: there is no host arm to fall back to, so the composition root
// deliberately keeps a backend-less router alive rather than returning nil.
var errBackendUnavailable = errors.New("sandbox backend unavailable")

// SandboxRouter is the per-identity routing seam EVERY tool call passes through (SBX-01). It
// holds the box Backend, the runtime profile specFor reads to pick the container runtime, the
// AURA_SANDBOX_* config specFor sources the rest of the spec from, and a mutex-guarded lastUsed
// map the idle reaper reads. A nil *SandboxRouter and a router with a nil backend behave
// identically: every Route denies (see Route).
type SandboxRouter struct {
	backend Backend
	profile config.RuntimeProfile
	cfg     config.SandboxConfig

	mu       sync.Mutex
	lastUsed map[string]time.Time
	handles  map[string]BoxHandle

	// now is the injectable clock for tests; nil → time.Now().UTC() via clock().
	now func() time.Time
}

// NewSandboxRouter builds a router over a box Backend, the runtime profile (specFor reads it to
// pick Runc vs Runsc), and the AURA_SANDBOX_* config (37-01) specFor maps into each spec.
func NewSandboxRouter(backend Backend, profile config.RuntimeProfile, cfg config.SandboxConfig) *SandboxRouter {
	return &SandboxRouter{
		backend:  backend,
		profile:  profile,
		cfg:      cfg,
		lastUsed: make(map[string]time.Time),
		handles:  make(map[string]BoxHandle),
	}
}

// Route is the single get-or-create seam every tool calls, and it ALWAYS routes: it resolves the
// caller's box keyed on identityctx.IdentityID (the seeded `local` fallback for the CLI /
// no-principal caller) and bumps lastUsed on success. A non-nil error means the caller DENIES —
// there is no host arm to fall back to (D-09/GATE-01).
//
// The nil-receiver and nil-backend cases share ONE denial point on purpose. NewSandboxRouter is
// the only constructor that allocates lastUsed/handles, so a zero-value &SandboxRouter{} has to
// be refused before the map write below — otherwise it nil-map-panics instead of denying.
func (r *SandboxRouter) Route(ctx context.Context) (BoxHandle, error) {
	if r == nil || r.backend == nil {
		return BoxHandle{}, errBackendUnavailable
	}
	id := r.identityID(ctx)
	h, err := r.backend.Resolve(ctx, r.specFor(id))
	if err != nil {
		return BoxHandle{}, err // fail-CLOSED (D-09/GATE-01) — the tool denies, never host
	}
	r.mu.Lock()
	r.lastUsed[id] = r.clock()
	r.handles[id] = h
	r.mu.Unlock()
	return h, nil
}

// identityID resolves the caller principal: the authenticated identity from identityctx, or the
// seeded `local` identity when no principal is scoped (CLI / bare ctx). It mirrors
// tools.ownerFromContext so a routed box is keyed on the SAME identity the rest of Aura uses —
// never a per-session or cross-identity key (T-37-05-CROSSID).
func (r *SandboxRouter) identityID(ctx context.Context) string {
	if id := identityctx.IdentityID(ctx); id != "" {
		return id
	}
	return localIdentityID
}

// specFor builds the SandboxSpec for an identity, sourcing every knob from cfg.Sandbox (37-01)
// rather than hardcoding: Image, the per-identity workspace volume name, the cgroup caps
// (CPU→NanoCPUs / memory / pids), and the egress allowlist reaching EgressPolicy.FQDNAllowlist
// on top of the always-on tenancy Floor. Runsc (gVisor) is selected ONLY under server_production
// (D-12); every other strict profile stays Runc — so the spec is always valid for NewSandboxSpec.
func (r *SandboxRouter) specFor(id string) SandboxSpec {
	runtime := Runc
	if r.profile == config.ProfileServerProduction {
		runtime = Runsc
	}
	return SandboxSpec{
		IdentityID:   id,
		Image:        r.cfg.Image,
		WorkspaceVol: boxName(id),
		Runtime:      runtime,
		Egress: EgressPolicy{
			Floor:         true,
			FQDNAllowlist: r.cfg.EgressAllowlist,
		},
		Limits: Resources{
			NanoCPUs:    int64(r.cfg.CPULimit) * nanoCPUsPerCPU,
			MemoryBytes: r.cfg.MemoryLimit,
			PidsLimit:   r.cfg.PidsLimit,
		},
	}
}

// Destroy tears down one identity's box: the egress sidecar, the container, and the
// per-identity workspace volume (Backend.Stop, ShutdownPolicy:Delete). It is the deprovision
// counterpart to Route, and it is the ONLY per-identity teardown the router has -- SuspendIdle
// sweeps by idleness and retains everything, which is the opposite intent.
//
// It is safe on a COLD process, which is the case that actually matters: an identity is
// usually deleted in a later daemon lifetime than the one that started its box, so the
// handles map is empty and there is nothing cached to stop. Synthesizing the handle is sound
// rather than a guess -- boxName derives the container name from the identity id alone, by
// construction, which is the same property Suspend/Resume/Stop already rely on to manage the
// egress sidecar from a BoxHandle without the full spec. Resolving first would be worse than
// useless: it would CREATE the box in order to delete it.
//
// Idempotent by delegation: removing an absent container or volume is not an error the
// backend raises, so a re-run over an already-destroyed identity converges. A nil receiver or
// a backend-less router reports the same denial Route does, because a deprovision that
// silently did nothing would leave an orphan nobody sweeps.
func (r *SandboxRouter) Destroy(ctx context.Context, identityID string) error {
	if r == nil || r.backend == nil {
		return errBackendUnavailable
	}
	identityID = strings.TrimSpace(identityID)
	if identityID == "" {
		return errors.New("sandbox destroy: identity is required")
	}
	r.mu.Lock()
	handle, cached := r.handles[identityID]
	delete(r.handles, identityID)
	delete(r.lastUsed, identityID)
	r.mu.Unlock()
	if !cached {
		handle = BoxHandle{ContainerID: boxName(identityID), IdentityID: identityID}
	}
	return r.backend.Stop(ctx, handle)
}

// clock returns the current time via the injectable now (tests) or time.Now().UTC().
func (r *SandboxRouter) clock() time.Time {
	if r.now != nil {
		return r.now()
	}
	return time.Now().UTC()
}
