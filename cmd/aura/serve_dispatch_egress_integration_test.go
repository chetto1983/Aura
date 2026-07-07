//go:build docker_integration

// serve_dispatch_egress_integration_test.go is the SBX-04 COMPOSITION-ROOT live proof: it drives
// the production buildSandboxRouter -> Route path (not a hand-built backend) and asserts the box
// created that way carries its always-on egress sidecar — proving the tenancy floor is LAUNCHED by
// the shipped composition root, closing the BLOCKER where launchEgress was a permanent no-op. The
// backend-level DROP semantics are proven by internal/sandbox/usersandbox/egress_integration_test.go
// (TestEgress_FloorDropsInternal); this test proves the PRODUCTION path reaches them. Like that
// test the DROP assertions are meaningful only on a native-Linux non-masquerading bridge (Docker
// Desktop/WSL vpnkit NATs around any nftables rule — 37-RESEARCH Pitfall 3), so both gates skip
// locally but t.Fatal under $CI on a non-Linux daemon (no-skip-as-green). It reuses the untagged
// cmd/aura containsString helper (mcp_test.go).

package main

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net"
	"os"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/moby/moby/api/pkg/stdcopy"
	"github.com/moby/moby/client"

	"github.com/chetto1983/aura/internal/config"
	"github.com/chetto1983/aura/internal/identityctx"
	"github.com/chetto1983/aura/internal/sandbox/usersandbox"
)

// egressITDockerdOrGate resolves the Docker daemon endpoint from DOCKER_HOST and gates the test on
// its reachability: a stdlib socket probe (npipe:// on Windows is not dialable and resolves to
// unreachable). Unreachable => local t.Skip but t.Fatal under $CI (a skipped docker_integration
// tier is never a silent pass — CLAUDE.md no-skip-as-green). Mirrors usersandbox.skipUnlessDockerd.
func egressITDockerdOrGate(t *testing.T) {
	t.Helper()
	var network, addr string
	switch host := strings.TrimSpace(os.Getenv("DOCKER_HOST")); {
	case host == "":
		network, addr = "unix", "/var/run/docker.sock"
	case strings.HasPrefix(host, "unix://"):
		network, addr = "unix", strings.TrimPrefix(host, "unix://")
	case strings.HasPrefix(host, "tcp://"):
		network, addr = "tcp", strings.TrimPrefix(host, "tcp://")
	}
	if network != "" {
		if conn, err := net.DialTimeout(network, addr, 2*time.Second); err == nil {
			_ = conn.Close()
			return
		}
	}
	if strings.TrimSpace(os.Getenv("CI")) != "" {
		t.Fatal("docker_integration requires a reachable Docker daemon under CI — a skipped tier is never a " +
			"silent pass (CLAUDE.md no-skip-as-green); wire dockerd into ci.yml")
	}
	t.Skip("docker_integration requires a reachable Docker daemon; start Docker and re-run " +
		"(e.g. `go test -tags docker_integration ./cmd/aura`)")
}

// egressITEnforcingBridgeOrGate gates the egress DROP assertions on a native-Linux non-masquerading
// bridge. Under $CI it is MANDATORY: a non-Linux daemon t.Fatals (Docker Desktop vpnkit NATs the
// bridge, Pitfall 3 — a skipped enforcement tier is never a silent pass). Locally it skips unless a
// dev opts in with AURA_EGRESS_ENFORCE=1 on a real native-Linux host. Mirrors
// usersandbox.skipUnlessEnforcingBridge so the composition-root proof has the SAME contract as the
// backend-level SBX-04 test.
func egressITEnforcingBridgeOrGate(t *testing.T) {
	t.Helper()
	if strings.TrimSpace(os.Getenv("CI")) != "" {
		if runtime.GOOS != "linux" {
			t.Fatalf("egress enforcement must run on native-Linux dockerd under CI, but GOOS=%s "+
				"(Docker Desktop vpnkit NATs the bridge — Pitfall 3); wire a native-Linux dockerd into ci.yml", runtime.GOOS)
		}
		return
	}
	if runtime.GOOS == "linux" && strings.TrimSpace(os.Getenv("AURA_EGRESS_ENFORCE")) == "1" {
		return
	}
	t.Skip("egress DROP enforcement is native-Linux-only (Docker Desktop/WSL vpnkit NATs the bridge — advisory); " +
		"informational-only outside CI. Set AURA_EGRESS_ENFORCE=1 on a native-Linux non-masquerading host to run (Pitfall 3)")
}

// egressITBoxImage is the box image the composition-root box is created from. It defaults to a
// tiny, universally-pullable image with wget+timeout (busybox) and is overridable so a live run can
// exercise the real fat aura-sandbox image via AURA_SANDBOX_IMAGE / AURA_SANDBOX_TEST_IMAGE.
func egressITBoxImage() string {
	if v := strings.TrimSpace(os.Getenv("AURA_SANDBOX_IMAGE")); v != "" {
		return v
	}
	if v := strings.TrimSpace(os.Getenv("AURA_SANDBOX_TEST_IMAGE")); v != "" {
		return v
	}
	return "busybox:stable"
}

// egressITEgressImage is the aura-egress sidecar image the composition root wires via WithEgress. It
// defaults to the local build tag and is overridable via AURA_SANDBOX_EGRESS_IMAGE (the real config
// knob) so a live run can point at a digest-pinned ref.
func egressITEgressImage() string {
	if v := strings.TrimSpace(os.Getenv("AURA_SANDBOX_EGRESS_IMAGE")); v != "" {
		return v
	}
	return "aura-egress:latest"
}

// egressITIdentity builds a docker-name-safe unique identity id so parallel/repeated runs never
// collide on the aura-box-<id>/aura-egress-<id> container+volume names.
func egressITIdentity(t *testing.T) string {
	t.Helper()
	raw := "cmdaura-egress-" + strings.ReplaceAll(t.Name(), "/", "-") + "-" + time.Now().Format("150405.000000000")
	return strings.NewReplacer(".", "", "_", "-").Replace(raw)
}

// egressITRawExec runs a command in a running container through the moby SDK and returns demuxed
// stdout + the exit code (the primitive the wget reachability probe is built on).
func egressITRawExec(t *testing.T, cli *client.Client, containerID string, cmd []string) (string, int) {
	t.Helper()
	ctx := context.Background()
	ec, err := cli.ExecCreate(ctx, containerID, client.ExecCreateOptions{Cmd: cmd, AttachStdout: true, AttachStderr: true})
	if err != nil {
		t.Fatalf("exec create: %v", err)
	}
	att, err := cli.ExecAttach(ctx, ec.ID, client.ExecAttachOptions{})
	if err != nil {
		t.Fatalf("exec attach: %v", err)
	}
	defer att.Close()
	var outBuf, errBuf bytes.Buffer
	if _, err := stdcopy.StdCopy(&outBuf, &errBuf, att.Reader); err != nil && err != io.EOF {
		t.Fatalf("exec stream: %v", err)
	}
	ins, err := cli.ExecInspect(ctx, ec.ID, client.ExecInspectOptions{})
	if err != nil {
		t.Fatalf("exec inspect: %v", err)
	}
	return outBuf.String(), ins.ExitCode
}

// egressITBoxWgetOK reports whether the box can reach url over HTTP within tmo seconds. A DROPped
// destination hangs until the busybox `timeout` fires (false); an allowed 200 returns true.
func egressITBoxWgetOK(t *testing.T, cli *client.Client, containerID, url string, tmo int) bool {
	t.Helper()
	cmd := []string{"/bin/sh", "-c", fmt.Sprintf("timeout %d wget -T %d -q -O /dev/null %s", tmo+2, tmo, url)}
	_, code := egressITRawExec(t, cli, containerID, cmd)
	return code == 0
}

// egressITCapAdd returns the CapAdd list of a container by id-or-name.
func egressITCapAdd(t *testing.T, cli *client.Client, ref string) []string {
	t.Helper()
	ins, err := cli.ContainerInspect(context.Background(), ref, client.ContainerInspectOptions{})
	if err != nil {
		t.Fatalf("inspect %q: %v", ref, err)
	}
	if ins.Container.HostConfig == nil {
		t.Fatalf("inspect %q: nil HostConfig", ref)
	}
	return ins.Container.HostConfig.CapAdd
}

// TestBuildSandboxRouter_LaunchesEgressFloor proves SBX-04 is closed AT THE COMPOSITION ROOT: a box
// created via the production buildSandboxRouter -> Route path carries its aura-egress sidecar
// (NET_ADMIN on the sidecar, box netns shared), reaches the public internet (D-04) but is DROPPED
// from the cloud-metadata IP + RFC1918 (D-05). It uses single_user_hardened (runc floor — no gVisor
// needed) so specFor keeps Runtime=Runc. This is the LIVE proof carried forward to WSL/CI
// (37-VALIDATION.md Manual-Only); locally it compiles + gates cleanly.
func TestBuildSandboxRouter_LaunchesEgressFloor(t *testing.T) {
	egressITDockerdOrGate(t)
	egressITEnforcingBridgeOrGate(t)

	cli, err := client.NewClientWithOpts(client.FromEnv, client.WithAPIVersionNegotiation())
	if err != nil {
		t.Fatalf("docker client: %v", err)
	}
	t.Cleanup(func() { _ = cli.Close() })

	boxImage, egressImage := egressITBoxImage(), egressITEgressImage()
	id := egressITIdentity(t)
	cfg := &config.Config{
		Profile: config.ProfileSingleUserHardened,
		Sandbox: config.SandboxConfig{
			Image:       boxImage,
			EgressImage: egressImage,
			CPULimit:    1,
			MemoryLimit: 256 << 20,
			PidsLimit:   128,
		},
	}

	// The production composition root builds its OWN docker client from DOCKER_HOST; a strict
	// profile yields a live router (nil only when Docker is unreachable, gated above).
	router := buildSandboxRouter(cfg)
	if router == nil {
		t.Fatal("buildSandboxRouter returned nil under single_user_hardened with a reachable daemon — expected a live router")
	}

	ctx := identityctx.WithIdentityID(context.Background(), id)
	h, routed, err := router.Route(ctx)
	if err != nil {
		t.Fatalf("router.Route: composition-root box creation failed (egress image %q present + built?): %v", egressImage, err)
	}
	if !routed {
		t.Fatal("router.Route: routed=false under a strict profile — the box was not interposed")
	}
	// Tear down via the tested Stop teardown (removes sidecar + box + per-identity volume, no
	// orphan). A second backend on the SAME daemon reproduces Stop's semantics from the handle.
	cleanup := usersandbox.NewDockerBackend(cli, boxImage,
		usersandbox.Resources{NanoCPUs: 1_000_000_000, MemoryBytes: 256 << 20, PidsLimit: 128},
		usersandbox.WithEgress(egressImage))
	t.Cleanup(func() { _ = cleanup.Stop(context.Background(), h) })

	// Composition-root proof: buildSandboxRouter LAUNCHED the aura-egress sidecar. NET_ADMIN on the
	// sidecar ONLY (the box carries none, D-07), sharing the box netns via container:<box>.
	if caps := egressITCapAdd(t, cli, h.ContainerID); len(caps) != 0 {
		t.Fatalf("box must carry no added capabilities, got CapAdd=%v", caps)
	}
	sidecar := "aura-egress-" + id
	if caps := egressITCapAdd(t, cli, sidecar); !containsString(caps, "NET_ADMIN") {
		t.Fatalf("sidecar %q must carry NET_ADMIN, got CapAdd=%v", sidecar, caps)
	}
	sins, err := cli.ContainerInspect(context.Background(), sidecar, client.ContainerInspectOptions{})
	if err != nil {
		t.Fatalf("inspect sidecar %q: %v", sidecar, err)
	}
	if sins.Container.HostConfig == nil || string(sins.Container.HostConfig.NetworkMode) != "container:"+h.ContainerID {
		t.Fatalf("sidecar must share the box netns via container:%s, got %+v", h.ContainerID, sins.Container.HostConfig)
	}
	if sins.Container.State == nil || !sins.Container.State.Running {
		t.Fatalf("sidecar must be running, state=%+v", sins.Container.State)
	}

	// Full public internet (D-04): a public HTTP host is reachable through the floor.
	if !egressITBoxWgetOK(t, cli, h.ContainerID, "http://example.com", 6) {
		t.Fatal("box must reach the public internet (example.com) — the composition-root floor over-blocked public egress")
	}
	// Tenancy boundary (D-05): the cloud-metadata IP + an RFC1918 target are DROPPED — the box the
	// PRODUCTION path created cannot cross into the host/tenancy zone (SBX-04).
	for _, target := range []string{"http://169.254.169.254", "http://10.0.0.1"} {
		if egressITBoxWgetOK(t, cli, h.ContainerID, target, 4) {
			t.Fatalf("box reached internal target %q — the composition-root floor did not DROP it (SBX-04 breach)", target)
		}
	}
}
