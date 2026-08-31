package usersandbox

import (
	"context"
	"testing"

	"github.com/chetto1983/aura/internal/config"
	"github.com/chetto1983/aura/internal/identityctx"
)

const destroyIdentity = "645baf03-247a-4671-90b3-659a05b403c4"

func destroyRouter(backend Backend) *SandboxRouter {
	return NewSandboxRouter(backend, config.ProfileDev, unitSandboxConfig())
}

// The warm case: this process routed the identity, so the handle it got back from Resolve is
// the one Stop must receive -- not a reconstruction of it.
func TestDestroy_UsesTheHandleThisProcessResolved(t *testing.T) {
	backend := &fakeBackend{t: t}
	router := destroyRouter(backend)
	if _, err := router.Route(identityctx.WithIdentityID(context.Background(), destroyIdentity)); err != nil {
		t.Fatalf("Route: %v", err)
	}
	if err := router.Destroy(context.Background(), destroyIdentity); err != nil {
		t.Fatalf("Destroy: %v", err)
	}
	if len(backend.stopped) != 1 {
		t.Fatalf("stopped %d boxes, want 1", len(backend.stopped))
	}
	if got := backend.stopped[0].ContainerID; got != "c-"+destroyIdentity {
		t.Fatalf("stopped ContainerID = %q, want the resolved handle", got)
	}
}

// The case that actually happens: an identity is deleted in a LATER daemon lifetime than the
// one that started its box, so nothing is cached. Destroy must still reach the container --
// otherwise every real deprovision leaves the box running, which is precisely the orphan the
// step exists to prevent.
func TestDestroy_ReachesTheBoxOnAColdProcess(t *testing.T) {
	backend := &fakeBackend{t: t}
	router := destroyRouter(backend)
	if err := router.Destroy(context.Background(), destroyIdentity); err != nil {
		t.Fatalf("Destroy: %v", err)
	}
	if len(backend.stopped) != 1 {
		t.Fatalf("stopped %d boxes, want 1", len(backend.stopped))
	}
	stopped := backend.stopped[0]
	if stopped.IdentityID != destroyIdentity {
		t.Fatalf("stopped IdentityID = %q", stopped.IdentityID)
	}
	// boxName is the deterministic container name, which is what makes the cold path sound.
	if stopped.ContainerID != boxName(destroyIdentity) {
		t.Fatalf("stopped ContainerID = %q, want %q", stopped.ContainerID, boxName(destroyIdentity))
	}
}

// A second Destroy must converge rather than fail, because the saga re-runs its steps.
func TestDestroy_IsIdempotent(t *testing.T) {
	backend := &fakeBackend{t: t}
	router := destroyRouter(backend)
	for i := range 2 {
		if err := router.Destroy(context.Background(), destroyIdentity); err != nil {
			t.Fatalf("Destroy #%d: %v", i+1, err)
		}
	}
	if len(backend.stopped) != 2 {
		t.Fatalf("stopped %d times, want 2 (each a no-op at the backend)", len(backend.stopped))
	}
}

// Destroy must forget the identity, so a later Route builds a fresh box instead of handing
// back a handle to a container that no longer exists.
func TestDestroy_ForgetsTheCachedHandle(t *testing.T) {
	backend := &fakeBackend{t: t}
	router := destroyRouter(backend)
	ctx := identityctx.WithIdentityID(context.Background(), destroyIdentity)
	if _, err := router.Route(ctx); err != nil {
		t.Fatalf("Route: %v", err)
	}
	if err := router.Destroy(context.Background(), destroyIdentity); err != nil {
		t.Fatalf("Destroy: %v", err)
	}
	router.mu.Lock()
	_, cached := router.handles[destroyIdentity]
	_, tracked := router.lastUsed[destroyIdentity]
	router.mu.Unlock()
	if cached || tracked {
		t.Fatal("a destroyed box must leave nothing behind in the router's maps")
	}
}

// "We could not check" is not "there is no box". A deprovision that recorded the step as done
// against an unreachable daemon would leave an orphan nothing sweeps, so this denies exactly
// as Route does.
func TestDestroy_FailsClosedWithoutABackend(t *testing.T) {
	for _, test := range []struct {
		name   string
		router *SandboxRouter
	}{
		{"nil router", nil},
		{"backend-less router", destroyRouter(nil)},
	} {
		t.Run(test.name, func(t *testing.T) {
			if err := test.router.Destroy(context.Background(), destroyIdentity); err == nil {
				t.Fatal("Destroy must deny when it cannot reach a backend")
			}
		})
	}
}

func TestDestroy_RequiresAnIdentity(t *testing.T) {
	if err := destroyRouter(&fakeBackend{t: t}).Destroy(context.Background(), "  "); err == nil {
		t.Fatal("Destroy must refuse an empty identity rather than tear down the local fallback")
	}
}
