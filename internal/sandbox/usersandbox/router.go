// router.go interposes the per-identity sandbox ROUTING SEAM (SBX-01/GATE-01): Route is the
// single get-or-create entry point the strict-profile tools call. It mirrors gateway.Decide's
// `if g == nil || !Strict()` short-circuit — under dev/local_trusted it is a host-direct no-op
// (routed=false) so tools keep their host os/exec path unchanged (SC-4) — and under a strict
// profile it get-or-creates the identity's box keyed on identityctx.IdentityID (the seeded
// `local` fallback for the CLI / no-principal caller). A routed-but-box-failed call returns
// (_, true, err): the fail-CLOSED containment invariant (D-09/GATE-01) — the tool MUST deny,
// never fall back to host. specFor sources the box image, cgroup caps, and the egress FQDN
// allowlist from the AURA_SANDBOX_* config surface (cfg.Sandbox, 37-01), so a CONFIGURED
// allowlist reaches EgressPolicy, not just a test fixture. The live idle-suspend reaper impl
// (SuspendIdle, the handlers.SandboxReaper seam) lives in reap.go over the lastUsed map bumped
// here on each Route.

package usersandbox

import (
	"context"
	"errors"
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

// errBackendUnavailable is returned by a strict router whose provider could not be composed.
// Keeping the router present preserves the routed=true denial signal and prevents host fallback.
var errBackendUnavailable = errors.New("strict sandbox backend unavailable")

// SandboxRouter is the per-identity routing seam every strict-profile tool call passes through
// (SBX-01). It holds the box Backend, the runtime profile that decides contain-vs-host-direct,
// the AURA_SANDBOX_* config specFor sources the spec from, and a mutex-guarded lastUsed map the
// idle reaper reads. A nil *SandboxRouter is a host-direct no-op reserved for lenient profiles;
// strict composition failures wire a non-nil router with a nil backend so Route fails closed.
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

// NewSandboxRouter builds a router over a box Backend, the runtime profile (its Strict() gates
// contain-vs-host-direct), and the AURA_SANDBOX_* config (37-01) specFor maps into each spec.
func NewSandboxRouter(backend Backend, profile config.RuntimeProfile, cfg config.SandboxConfig) *SandboxRouter {
	return &SandboxRouter{
		backend:  backend,
		profile:  profile,
		cfg:      cfg,
		lastUsed: make(map[string]time.Time),
		handles:  make(map[string]BoxHandle),
	}
}

// Strict reports whether the router is enforcing a strict profile (single_user_hardened /
// server_production). The skill tool (37-07) consults it to select the box-side snippet path
// vs the host path. A nil router is never strict.
func (r *SandboxRouter) Strict() bool {
	return r != nil && r.profile.Strict()
}

// Route is the single get-or-create seam the tools call. Under a nil router or a non-strict
// profile it is a host-direct no-op — (zero handle, routed=false, nil) — the exact gateway.
// Decide short-circuit that preserves the operator's full-host experience (SC-4). Under a
// strict profile it resolves the caller's box keyed on identityctx.IdentityID (the seeded
// `local` fallback for the CLI / no-principal caller) and bumps lastUsed on success. A box
// failure returns (zero handle, routed=TRUE, err): the fail-CLOSED containment invariant
// (D-09/GATE-01) — routed=true tells the tool to DENY, never fall back to host.
func (r *SandboxRouter) Route(ctx context.Context) (BoxHandle, bool, error) {
	if r == nil || !r.profile.Strict() {
		return BoxHandle{}, false, nil
	}
	if r.backend == nil {
		return BoxHandle{}, true, errBackendUnavailable
	}
	id := r.identityID(ctx)
	h, err := r.backend.Resolve(ctx, r.specFor(id))
	if err != nil {
		return BoxHandle{}, true, err // fail-CLOSED (D-09/GATE-01) — routed, so the tool denies
	}
	r.mu.Lock()
	r.lastUsed[id] = r.clock()
	r.handles[id] = h
	r.mu.Unlock()
	return h, true, nil
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

// clock returns the current time via the injectable now (tests) or time.Now().UTC().
func (r *SandboxRouter) clock() time.Time {
	if r.now != nil {
		return r.now()
	}
	return time.Now().UTC()
}
