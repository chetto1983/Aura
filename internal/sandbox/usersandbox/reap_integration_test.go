//go:build docker_integration

// reap_integration_test.go proves the idle-suspend reaper's LIVE behavior against a real
// dockerd: Route creates + starts the box, forcing its lastUsed old makes SuspendIdle stop it
// (suspend-retain), and the NEXT Route transparently auto-resumes the SAME container — the D-08
// inline resume, never a scheduled one. It closes SBX-03's idle-TTL lifecycle onto the live
// DockerBackend. It skips locally when dockerd is unreachable (t.Fatal under $CI, via the shared
// skipUnlessDockerd gate — no-skip-as-green).

package usersandbox

import (
	"context"
	"testing"
	"time"

	"github.com/moby/moby/client"

	"github.com/chetto1983/aura/internal/config"
	"github.com/chetto1983/aura/internal/identityctx"
)

// TestReap_IdleSuspendAutoResume drives the full idle lifecycle through the router: create via
// Route → force idle → SuspendIdle stops the box → Route auto-resumes the same container.
func TestReap_IdleSuspendAutoResume(t *testing.T) {
	skipUnlessDockerd(t)
	cli := newTestDockerClient(t)

	id := "reap-" + time.Now().UTC().Format("150405.000")
	cfg := config.SandboxConfig{
		IdleTTLSec:  60,
		CPULimit:    1,
		MemoryLimit: 256 << 20,
		PidsLimit:   128,
		Image:       testBoxImage(),
	}
	backend := NewDockerBackend(cli, testBoxImage(), testLimits())
	router := NewSandboxRouter(backend, config.ProfileSingleUserHardened, cfg)
	ctx := identityctx.WithIdentityID(context.Background(), id)

	t.Cleanup(func() {
		h, _ := router.Route(ctx) // ensure a live handle for a clean teardown
		_ = backend.Stop(context.Background(), h)
	})

	// 1. Route creates + starts the box.
	h1, err := router.Route(ctx)
	if err != nil {
		t.Fatalf("Route(create) = (%+v, %v), want a live handle and no error", h1, err)
	}
	if !containerRunning(t, cli, h1.ContainerID) {
		t.Fatalf("box %s not running after create Route", h1.ContainerID)
	}

	// 2. Force lastUsed old, then sweep → the box is suspended (stopped, container + volume
	//    retained by DockerBackend.Suspend).
	router.mu.Lock()
	router.lastUsed[id] = time.Now().Add(-2 * time.Hour)
	router.mu.Unlock()
	n, err := router.SuspendIdle(ctx, time.Now())
	if err != nil {
		t.Fatalf("SuspendIdle err = %v", err)
	}
	if n != 1 {
		t.Fatalf("SuspendIdle suspended %d, want 1", n)
	}
	if containerRunning(t, cli, h1.ContainerID) {
		t.Fatalf("box %s still running after SuspendIdle, want suspended", h1.ContainerID)
	}

	// 3. The next Route transparently auto-resumes the SAME container (inline resume, D-08).
	h2, err := router.Route(ctx)
	if err != nil {
		t.Fatalf("Route(resume) = (%+v, %v), want a live handle and no error", h2, err)
	}
	if h2.ContainerID != h1.ContainerID {
		t.Fatalf("resumed container = %s, want the SAME %s (re-created, not resumed)", h2.ContainerID, h1.ContainerID)
	}
	if !containerRunning(t, cli, h2.ContainerID) {
		t.Fatalf("box %s not running after auto-resume Route", h2.ContainerID)
	}
}

// containerRunning reports whether the named container is in the running state.
func containerRunning(t *testing.T, cli *client.Client, id string) bool {
	t.Helper()
	ins, err := cli.ContainerInspect(context.Background(), id, client.ContainerInspectOptions{})
	if err != nil {
		t.Fatalf("container inspect %s: %v", id, err)
	}
	return ins.Container.State != nil && ins.Container.State.Running
}
