//go:build docker_integration

package usersandbox

import (
	"context"
	"io"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	cerrdefs "github.com/containerd/errdefs"
	"github.com/google/uuid"
	"github.com/moby/moby/client"

	"github.com/chetto1983/aura/internal/config"
	"github.com/chetto1983/aura/internal/identityctx"
)

func browserSidecarFixture(t *testing.T, maxActive int) (*client.Client, *DockerBackend, *SandboxRouter, context.Context, string) {
	t.Helper()
	skipUnlessDockerd(t)
	cli := newTestDockerClient(t)
	backend := NewDockerBackend(cli, testBoxImage(), testLimits(),
		WithBrowserLimits(testLimits(), maxActive),
		WithBrowserFiles(func(string) ([]BoxFile, error) {
			return []BoxFile{{Path: "/run/aura/agent-browser.key", Content: []byte(strings.Repeat("1", 64)), Mode: 0o600}}, nil
		}))
	cfg := config.SandboxConfig{
		Image: testBoxImage(), IdleTTLSec: 1800, MemoryLimit: 256 << 20, PidsLimit: 128, CPULimit: 1,
		BrowserMemoryLimit: 256 << 20, BrowserPidsLimit: 128, BrowserCPULimit: 1,
		BrowserMaxActive: maxActive,
	}
	router := NewSandboxRouter(backend, cfg)
	id := uuid.NewString()
	ctx := identityctx.WithIdentityID(context.Background(), id)
	t.Cleanup(func() {
		if err := router.Destroy(context.Background(), id); err != nil {
			t.Errorf("cleanup disposable identity: %v", err)
		}
	})
	return cli, backend, router, ctx, id
}

func requireSidecarCommand(t *testing.T, cli *client.Client, h BoxHandle, command string) string {
	t.Helper()
	out, code := rawExec(t, cli, h.ContainerID, []string{"/bin/sh", "-c", command})
	if code != 0 {
		t.Fatalf("fixture command in kind %v exited %d: %s", h.Kind, code, out)
	}
	return out
}

// requireSidecarCommandSoon retries command until it exits 0. ExecStream returns once the exec
// has started, while its /bin/sh wrapper is still writing the PID file before it reaches the
// job's exec, so a probe for the job run right after the call can see only the shell (lost on
// a GitHub runner on 2026-10-21).
func requireSidecarCommandSoon(t *testing.T, cli *client.Client, h BoxHandle, command string) {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for {
		out, code := rawExec(t, cli, h.ContainerID, []string{"/bin/sh", "-c", command})
		if code == 0 {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("fixture command in kind %v still exited %d after 10s: %s", h.Kind, code, out)
		}
		time.Sleep(100 * time.Millisecond)
	}
}

func requireSidecarAbsent(t *testing.T, cli *client.Client, name string) {
	t.Helper()
	if _, err := cli.ContainerInspect(context.Background(), name, client.ContainerInspectOptions{}); !cerrdefs.IsNotFound(err) {
		t.Fatalf("container %s: want absent, got %v", name, err)
	}
}

func TestBrowserSidecarLazyIsolationPersistenceAndPurge(t *testing.T) {
	cli, backend, router, ctx, id := browserSidecarFixture(t, 32)
	shell, err := router.Route(ctx)
	if err != nil {
		t.Fatal(err)
	}
	requireSidecarAbsent(t, cli, "aura-browser-"+id)
	requireSidecarCommand(t, cli, shell, "test ! -e /run/aura/agent-browser.key")
	browser, err := router.RouteBrowser(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if shell.Kind != Shell || browser.Kind != Browser || shell.ContainerID == browser.ContainerID {
		t.Fatalf("shell/browser handles are not isolated: %+v / %+v", shell, browser)
	}
	for _, box := range []struct {
		h          BoxHandle
		volume     string
		mountCount int
	}{{shell, "aura-box-" + id, 2 + len(identityCacheMounts(id))}, {browser, "aura-browser-" + id, 2}} {
		ins, err := cli.ContainerInspect(ctx, box.h.ContainerID, client.ContainerInspectOptions{})
		if err != nil {
			t.Fatal(err)
		}
		if ins.Container.HostConfig.NetworkMode.IsHost() || ins.Container.HostConfig.NetworkMode.IsContainer() {
			t.Fatal("box shares a network namespace")
		}
		if len(ins.Container.HostConfig.Mounts) != box.mountCount {
			t.Fatalf("kind %v mounts: %+v", box.h.Kind, ins.Container.HostConfig.Mounts)
		}
		found := false
		for _, mount := range ins.Container.Mounts {
			if mount.Destination == "/workspace" {
				found = mount.Name == box.volume
			}
			if box.h.Kind == Shell && strings.HasPrefix(mount.Name, "aura-browser-") {
				t.Fatal("shell mounts browser volume")
			}
		}
		if !found {
			t.Fatalf("kind %v missing its own workspace volume", box.h.Kind)
		}
	}
	mode := requireSidecarCommand(t, cli, browser, "stat -c %a /run/aura/agent-browser.key")
	if strings.TrimSpace(mode) != "600" {
		t.Fatalf("browser key mode %q", mode)
	}
	requireSidecarCommand(t, cli, browser, "mkdir -p /workspace/.agent-browser-home/profiles/test/Default; printf synthetic-login > /workspace/.agent-browser-home/profiles/test/Default/Cookies; printf browser-only > /workspace/browser-marker")
	requireSidecarCommand(t, cli, shell, "test ! -e /workspace/browser-marker; test ! -e /run/aura/agent-browser.key; test ! -e /var/run/docker.sock; test -z \"$(find /workspace -name Cookies -o -name 'Login Data')\"; ! grep -q aura-browser- /proc/self/mountinfo")
	process, err := backend.ExecStream(ctx, browser, ExecRequest{Command: "exec sleep 19999"}, nil, io.Discard)
	if err != nil {
		t.Fatal(err)
	}
	stopProcess := sync.OnceFunc(func() { process.Kill(); _, _ = process.Wait() })
	t.Cleanup(stopProcess)
	const processProbe = `found=; for file in /proc/[0-9]*/comm; do read -r name < "$file" || continue; [ "$name" = sleep ] && found=1; done; test "$found" = 1`
	requireSidecarCommandSoon(t, cli, browser, processProbe)
	requireSidecarCommand(t, cli, shell, "! ("+processProbe+")")
	stopProcess()
	if err := backend.Suspend(ctx, browser); err != nil {
		t.Fatal(err)
	}
	if !containerRunning(t, cli, shell.ContainerID) || containerRunning(t, cli, browser.ContainerID) {
		t.Fatal("browser suspension affected shell or failed to stop browser")
	}
	resumed, err := router.RouteBrowser(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if resumed.ContainerID != browser.ContainerID {
		t.Fatal("browser was recreated instead of resumed")
	}
	if body := requireSidecarCommand(t, cli, resumed, "cat /workspace/.agent-browser-home/profiles/test/Default/Cookies"); body != "synthetic-login" {
		t.Fatal("synthetic login did not survive stop/start")
	}
	if err := router.Destroy(ctx, id); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"aura-box-" + id, "aura-browser-" + id, "aura-egress-" + id, "aura-egress-browser-" + id} {
		requireSidecarAbsent(t, cli, name)
	}
	for _, name := range []string{"aura-box-" + id, "aura-browser-" + id} {
		if _, err := cli.VolumeInspect(ctx, name, client.VolumeInspectOptions{}); !cerrdefs.IsNotFound(err) {
			t.Fatalf("purged workspace %s remains: %v", name, err)
		}
	}
}

func TestBrowserSidecarKeepsRunningWhenTheShellIdles(t *testing.T) {
	cli, _, router, ctx, _ := browserSidecarFixture(t, 32)
	now := time.Now().UTC()
	router.now = func() time.Time { return now }
	shell, err := router.Route(ctx)
	if err != nil {
		t.Fatal(err)
	}
	browser, err := router.RouteBrowser(ctx)
	if err != nil {
		t.Fatal(err)
	}
	n, err := router.SuspendIdle(ctx, now.Add(1801*time.Second))
	if err != nil || n != 1 {
		t.Fatalf("idle sweep = %d, %v; want the shell box alone", n, err)
	}
	if containerRunning(t, cli, shell.ContainerID) || !containerRunning(t, cli, browser.ContainerID) {
		t.Fatal("the idle sweep kept the shell box or stopped the browser box")
	}
}

// TestBrowserSidecarInventorySeesAStoppedBox lists through the real daemon. It does not call
// StartBrowserBoxes, which would start every browser box on the host, a live stack's included.
func TestBrowserSidecarInventorySeesAStoppedBox(t *testing.T) {
	_, backend, router, ctx, id := browserSidecarFixture(t, 32)
	browser, err := router.RouteBrowser(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if err := backend.Suspend(ctx, browser); err != nil {
		t.Fatal(err)
	}
	boxes, err := backend.BrowserBoxes(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if !slices.ContainsFunc(boxes, func(h BoxHandle) bool { return h.IdentityID == id && h.ContainerID == browser.ContainerID }) {
		t.Fatalf("BrowserBoxes = %+v, want the stopped box of %s", boxes, id)
	}
}

// TestBrowserSidecarAdmissionCountsOpenChromes: a slot is a Chrome, not a box. A backend that
// starts with no memory of earlier calls, as after an Aura restart, still finds the Chrome open
// in another identity's box and refuses a second browser on a host that admits one. Both boxes
// run meanwhile, and the slot frees when that Chrome ends, with its box still running.
func TestBrowserSidecarAdmissionCountsOpenChromes(t *testing.T) {
	cli, _, router, ctx, _ := browserSidecarFixture(t, 1)
	first, err := router.RouteBrowser(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if err := router.AdmitBrowser(ctx); err != nil {
		t.Fatal(err)
	}
	// Chrome's main process is the one launched with --remote-debugging-port (lab VM, 2026-10-11);
	// the trailing ":" keeps sh from exec-ing sleep in its place, so the flag stays in a cmdline.
	requireSidecarCommand(t, cli, first, `sh -c 'sleep 600; :' --remote-debugging-port=0 >/dev/null 2>&1 & echo $! >/tmp/fake-chrome.pid`)

	secondID := uuid.NewString()
	secondCtx := identityctx.WithIdentityID(context.Background(), secondID)
	secondBackend := NewDockerBackend(cli, testBoxImage(), testLimits(), WithBrowserLimits(testLimits(), 1))
	secondRouter := NewSandboxRouter(secondBackend, router.cfg)
	t.Cleanup(func() {
		if err := secondRouter.Destroy(context.Background(), secondID); err != nil {
			t.Error(err)
		}
	})
	second, err := secondRouter.RouteBrowser(secondCtx)
	if err != nil {
		t.Fatalf("starting a second browser box took a slot: %v", err)
	}
	if err := secondRouter.AdmitBrowser(secondCtx); err == nil || !strings.Contains(err.Error(), "browser capacity") {
		t.Fatalf("a fresh backend admitted a second Chrome beside an open one: %v", err)
	}
	if !containerRunning(t, cli, first.ContainerID) || !containerRunning(t, cli, second.ContainerID) {
		t.Fatal("both browser boxes must keep running")
	}
	requireSidecarCommand(t, cli, first, `kill "$(cat /tmp/fake-chrome.pid)"`)
	if err := secondRouter.AdmitBrowser(secondCtx); err != nil {
		t.Fatalf("the slot did not free when the Chrome ended: %v", err)
	}
	if !containerRunning(t, cli, first.ContainerID) {
		t.Fatal("admission stopped the first identity's box")
	}
}

// TestBrowserSidecarLegacyHomeLeavesTheShellVolume: the logged-in profile a shell volume kept
// from before the browser box is removed and the shell's own work stays (Critical 1, 2026-10-15).
// The per-volume sweep the boot runs reaches a volume whose box is gone, and Resolve sweeps a
// running box for a backend that has not swept it yet.
func TestBrowserSidecarLegacyHomeLeavesTheShellVolume(t *testing.T) {
	cli, backend, router, ctx, id := browserSidecarFixture(t, 32)
	const seed = "mkdir -p /workspace/.agent-browser-home/profiles/old/Default; printf synthetic-login > /workspace/.agent-browser-home/profiles/old/Default/Cookies; printf ordinary-work > /workspace/work.txt"
	const swept = `test ! -e /workspace/.agent-browser-home; test "$(cat /workspace/work.txt)" = ordinary-work`
	shellID, err := backend.createBox(ctx, boxName(id), router.specFor(id))
	if err != nil {
		t.Fatal(err)
	}
	requireSidecarCommand(t, cli, BoxHandle{ContainerID: shellID, IdentityID: id}, seed)
	if _, err := cli.ContainerRemove(ctx, shellID, client.ContainerRemoveOptions{Force: true}); err != nil {
		t.Fatal(err)
	}

	unlock := backend.identities.Lock(id)
	err = backend.sweepLegacyBrowserHome(ctx, id)
	unlock()
	if err != nil {
		t.Fatalf("sweep a volume whose box is gone: %v", err)
	}
	requireSidecarAbsent(t, cli, sweepHelperPrefix+id)
	shell, err := router.Route(ctx)
	if err != nil {
		t.Fatal(err)
	}
	requireSidecarCommand(t, cli, shell, swept)

	requireSidecarCommand(t, cli, shell, seed)
	fresh := NewSandboxRouter(NewDockerBackend(cli, testBoxImage(), testLimits()), router.cfg)
	if shell, err = fresh.Route(ctx); err != nil {
		t.Fatal(err)
	}
	requireSidecarCommand(t, cli, shell, swept)
	requireSidecarAbsent(t, cli, sweepHelperPrefix+id)
}

func TestBrowserSidecarEgressFailureLeavesBoxStopped(t *testing.T) {
	cli, _, router, ctx, id := browserSidecarFixture(t, 32)
	backend := NewDockerBackend(cli, testBoxImage(), testLimits(), WithBrowserLimits(testLimits(), 32), WithEgress(testBoxImage()))
	t.Cleanup(func() {
		if err := backend.Stop(context.Background(), BoxHandle{ContainerID: workspaceName(id, Browser), IdentityID: id, Kind: Browser}); err != nil {
			t.Error(err)
		}
	})
	// Busybox is deliberately not an egress image: ContainerStart succeeds, but
	// no nftables floor is installed. Readiness, not process creation, is the gate.
	if _, err := backend.Resolve(ctx, router.specForKind(id, Browser)); err == nil {
		t.Fatal("browser was returned before its egress floor existed")
	}
	if containerRunning(t, cli, workspaceName(id, Browser)) {
		t.Fatal("failed egress setup left a running browser box")
	}
}

func TestBrowserSidecarResumeRefusesMissingEgress(t *testing.T) {
	cli, original, router, ctx, _ := browserSidecarFixture(t, 32)
	h, err := router.RouteBrowser(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if err := original.Suspend(ctx, h); err != nil {
		t.Fatal(err)
	}
	secured := NewDockerBackend(cli, testBoxImage(), testLimits(), WithBrowserLimits(testLimits(), 32), WithEgress(testBoxImage()))
	if err := secured.Resume(ctx, h); err == nil {
		t.Fatal("Resume accepted a browser whose required egress sidecar is missing")
	}
	if containerRunning(t, cli, h.ContainerID) {
		t.Fatal("failed Resume left an unsecured browser running")
	}
}

func TestBrowserSidecarRetirementPurgesCredentialsNotWork(t *testing.T) {
	cli, backend, router, ctx, id := browserSidecarFixture(t, 32)
	shellID, err := backend.createBox(ctx, boxName(id), router.specFor(id))
	if err != nil {
		t.Fatal(err)
	}
	shell := BoxHandle{ContainerID: shellID, IdentityID: id}
	requireSidecarCommand(t, cli, shell, "printf ordinary-work > /workspace/work.txt")
	browser, err := router.RouteBrowser(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if !containerRunning(t, cli, shell.ContainerID) {
		t.Fatal("starting the browser box stopped the shell box")
	}
	requireSidecarCommand(t, cli, browser, "mkdir -p /workspace/.agent-browser-home; printf synthetic-login > /workspace/.agent-browser-home/Cookies")
	if err := router.PurgeBrowserCredentials(ctx, id); err != nil {
		t.Fatal(err)
	}
	requireSidecarAbsent(t, cli, "aura-browser-"+id)
	if _, err := cli.VolumeInspect(ctx, "aura-browser-"+id, client.VolumeInspectOptions{}); !cerrdefs.IsNotFound(err) {
		t.Fatalf("retired browser volume remains: %v", err)
	}
	shell, err = router.Route(ctx)
	if err != nil {
		t.Fatal(err)
	}
	requireSidecarCommand(t, cli, shell, "test ! -e /workspace/.agent-browser-home; test \"$(cat /workspace/work.txt)\" = ordinary-work")
	requireSidecarAbsent(t, cli, "aura-browser-"+id)
}
