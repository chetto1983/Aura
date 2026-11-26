// Shell tools and trusted browser adapters have separate per-identity routes.
// Both fail closed: there is no host execution fallback.

package usersandbox

import (
	"context"
	"errors"
	"strings"
	"sync"
	"time"

	"github.com/chetto1983/aura/internal/config"
	"github.com/chetto1983/aura/internal/identityctx"
	"github.com/chetto1983/aura/internal/keylock"
)

// localIdentityID is the migration-0004 seeded `local` identity — the CLI / no-principal
// fallback owner (identityctx.LocalOperatorIdentity, like every other no-principal path). A tool call
// made without an authenticated principal resolves to, and is contained in, `local`'s box —
// never the host.
const localIdentityID = identityctx.LocalOperatorIdentity

// nanoCPUsPerCPU converts a whole-CPU count (cfg.CPULimit) to moby's NanoCPUs unit (1 CPU =
// 1e9 nano-CPUs), the container.Resources.NanoCPUs cgroup cap (D-14).
const nanoCPUsPerCPU = 1_000_000_000

// errBackendUnavailable is returned by a router whose provider could not be composed. It is a
// DENIAL, not a degraded mode: there is no host arm to fall back to, so the composition root
// deliberately keeps a backend-less router alive rather than returning nil.
var errBackendUnavailable = errors.New("sandbox backend unavailable")

// SandboxRouter is the per-identity routing seam EVERY tool call passes through (SBX-01). It
// holds the box Backend, the AURA_SANDBOX_* config specFor sources each spec from, and a
// mutex-guarded lastUsed map the idle reaper reads. A nil *SandboxRouter and a router with a
// nil backend behave identically: every Route denies (see Route).
type SandboxRouter struct {
	backend Backend
	cfg     config.SandboxConfig

	// identities serializes one identity's resolve, reap and teardown and leaves the others free;
	// mu guards only the two maps.
	identities keylock.Striped
	mu         sync.Mutex
	lastUsed   map[string]time.Time
	handles    map[string]BoxHandle

	// now is the injectable clock for tests; nil → time.Now().UTC() via clock().
	now func() time.Time
}

// NewSandboxRouter builds a router over a box Backend and the AURA_SANDBOX_* config (37-01)
// specFor maps into each spec.
func NewSandboxRouter(backend Backend, cfg config.SandboxConfig) *SandboxRouter {
	return &SandboxRouter{
		backend:  backend,
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
	// The get-or-create body is shared with EnsureBox's explicit-identity seam
	// (router_provision.go) — resolveAndTrack. Only identity resolution differs: Route
	// derives it from context (with the seeded `local` fallback below); EnsureBox takes it
	// as a parameter and refuses a blank one.
	return r.resolveAndTrack(ctx, r.identityID(ctx))
}

// RouteBrowser is reserved for trusted browser adapters, never arbitrary shell commands.
func (r *SandboxRouter) RouteBrowser(ctx context.Context) (BoxHandle, error) {
	if r == nil || r.backend == nil {
		return BoxHandle{}, errBackendUnavailable
	}
	return r.resolveKindAndTrack(ctx, r.identityID(ctx), Browser)
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
// (CPU→NanoCPUs / memory / pids), and the always-on tenancy Floor. It is the same under every
// profile: every box runs on runc since gVisor was retired (prd.md §12, 2026-10-11).
func (r *SandboxRouter) specFor(id string) SandboxSpec {
	return SandboxSpec{
		IdentityID: id,
		Image:      r.cfg.Image,
		Limits: Resources{
			NanoCPUs:    int64(r.cfg.CPULimit) * nanoCPUsPerCPU,
			MemoryBytes: r.cfg.MemoryLimit,
			PidsLimit:   r.cfg.PidsLimit,
		},
	}
}

func (r *SandboxRouter) specForKind(id string, kind BoxKind) SandboxSpec {
	s := r.specFor(id)
	s.Kind = kind
	if kind == Browser {
		s.Limits = Resources{NanoCPUs: int64(r.cfg.BrowserCPULimit) * nanoCPUsPerCPU,
			MemoryBytes: r.cfg.BrowserMemoryLimit, PidsLimit: r.cfg.BrowserPidsLimit}
	}
	return s
}

func trackingKey(id string, kind BoxKind) string {
	if kind == Browser {
		return "browser/" + id
	}
	return id
}

// Destroy tears down both kinds: their egress sidecars, containers and private
// volumes (Backend.Stop, ShutdownPolicy:Delete). It is the deprovision
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
	defer r.identities.Lock(identityID)()
	var result error
	for _, kind := range []BoxKind{Browser, Shell} {
		key := trackingKey(identityID, kind)
		r.mu.Lock()
		handle, cached := r.handles[key]
		r.mu.Unlock()
		if !cached {
			handle = BoxHandle{ContainerID: workspaceName(identityID, kind), IdentityID: identityID, Kind: kind}
		}
		if err := r.backend.Stop(ctx, handle); err != nil {
			result = errors.Join(result, err)
			continue
		}
		r.mu.Lock()
		delete(r.handles, key)
		delete(r.lastUsed, key)
		r.mu.Unlock()
	}
	return result
}

// clock returns the current time via the injectable now (tests) or time.Now().UTC().
func (r *SandboxRouter) clock() time.Time {
	if r.now != nil {
		return r.now()
	}
	return time.Now().UTC()
}
