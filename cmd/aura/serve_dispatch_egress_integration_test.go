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
	"os"
	"strings"
	"testing"
	"time"

	"github.com/chetto1983/aura/internal/config"
	"github.com/chetto1983/aura/internal/identityctx"
	"github.com/chetto1983/aura/internal/sandbox/usersandbox"
	"github.com/chetto1983/aura/internal/testenv"
	"github.com/moby/moby/api/pkg/stdcopy"
	"github.com/moby/moby/client"
)

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
	const prefix = "cmdaura-egress-"
	stamp := strings.NewReplacer(".", "", "_", "-").Replace(time.Now().Format("150405.000000000"))
	name := strings.NewReplacer("/", "-", ".", "", "_", "-").Replace(t.Name())
	// idroot caps an identity at 64 characters, and a long enough test name walks straight
	// past it — measured in CI run 1789, where a test called
	// TestBoxHoldsOnlyItsOwnAndTheHouseSkills produced an id the layout refused. The stamp is
	// what makes the id unique, so the NAME is the half that gets trimmed: it is only here so
	// a stray container is recognisable in `docker ps`.
	if room := maxIdentityLen - len(prefix) - len(stamp) - 1; len(name) > room {
		name = name[:room]
	}
	id := prefix + name + "-" + stamp
	if len(id) > maxIdentityLen {
		t.Fatalf("egressITIdentity produced %d chars (%q); idroot admits at most %d", len(id), id, maxIdentityLen)
	}
	return id
}

// maxIdentityLen mirrors idroot's identityPattern bound. It is duplicated as a constant
// rather than imported because idroot exports the validator, not the length.
const maxIdentityLen = 64

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

// egressITPublicProbeAttempts bounds the retries of the public reachability probe: example.com is
// a third-party host, so one failed fetch is not evidence that the floor over-blocked. A DROP fails
// every attempt. Mirrors usersandbox.publicProbeAttempts.
const egressITPublicProbeAttempts = 3

// egressITBoxWget reports whether the box can fetch url over HTTP within tmo seconds, and what wget
// printed. A DROPped destination hangs until the busybox `timeout` fires and prints nothing; busybox
// wget prints its errors even with -q, so a fast failure says why.
func egressITBoxWget(t *testing.T, cli *client.Client, containerID, url string, tmo int) (bool, string) {
	t.Helper()
	cmd := []string{"/bin/sh", "-c", fmt.Sprintf("timeout %d wget -T %d -q -O /dev/null %s 2>&1", tmo+2, tmo, url)}
	out, code := egressITRawExec(t, cli, containerID, cmd)
	return code == 0, strings.TrimSpace(out)
}

// egressITRequireBoxReachesPublic fails the test, with every attempt's wget output, when the box
// cannot fetch a public url in egressITPublicProbeAttempts tries.
func egressITRequireBoxReachesPublic(t *testing.T, cli *client.Client, containerID, url, failure string) {
	t.Helper()
	var attempts []string
	for i := 1; i <= egressITPublicProbeAttempts; i++ {
		ok, out := egressITBoxWget(t, cli, containerID, url, 6)
		if ok {
			return
		}
		if out == "" {
			out = "no output (timed out)"
		}
		attempts = append(attempts, fmt.Sprintf("attempt %d: %s", i, out))
		if i < egressITPublicProbeAttempts {
			time.Sleep(2 * time.Second)
		}
	}
	t.Fatalf("%s; %s", failure, strings.Join(attempts, "; "))
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

// hasCap reports whether caps contains want, tolerant of Docker's capability normalization:
// the daemon returns caps in the canonical "CAP_NET_ADMIN" form on inspect even when the
// container was created with the short "NET_ADMIN" name (moby v0.4.1 / Docker 29+). Comparing
// the CAP_-stripped forms keeps the D-07 security assertion (NET_ADMIN on the sidecar) exact
// while robust to the daemon's string form.
func hasCap(caps []string, want string) bool {
	want = strings.TrimPrefix(want, "CAP_")
	for _, c := range caps {
		if strings.TrimPrefix(c, "CAP_") == want {
			return true
		}
	}
	return false
}

// TestBuildSandboxRouter_LaunchesEgressFloor proves SBX-04 is closed AT THE COMPOSITION ROOT: a box
// created via the production buildSandboxRouter -> Route path carries its aura-egress sidecar
// (NET_ADMIN on the sidecar, box netns shared), reaches the public internet (D-04) but is DROPPED
// from the cloud-metadata IP + RFC1918 (D-05). This is the LIVE proof carried forward to WSL/CI
// (37-VALIDATION.md Manual-Only); locally it compiles + gates cleanly.
func TestBuildSandboxRouter_LaunchesEgressFloor(t *testing.T) {
	testenv.Dockerd(t)
	testenv.EnforcingBridge(t)

	cli, err := client.New(client.FromEnv)
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

	// The production composition root builds its OWN docker client from DOCKER_HOST and never
	// returns nil — a router that cannot reach a daemon denies rather than disappearing.
	router := buildSandboxRouter(cfg, nil)
	if router == nil {
		t.Fatal("buildSandboxRouter returned nil — that would be a host-fallback door at the composition root")
	}

	ctx := identityctx.WithIdentityID(context.Background(), id)
	h, err := router.Route(ctx)
	if err != nil {
		t.Fatalf("router.Route: composition-root box creation failed (egress image %q present + built?): %v", egressImage, err)
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
	if caps := egressITCapAdd(t, cli, sidecar); !hasCap(caps, "NET_ADMIN") {
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
	egressITRequireBoxReachesPublic(t, cli, h.ContainerID, "http://example.com",
		"box must reach the public internet (example.com) — the composition-root floor over-blocked public egress")
	// Tenancy boundary (D-05): the cloud-metadata IP + an RFC1918 target are DROPPED — the box the
	// PRODUCTION path created cannot cross into the host/tenancy zone (SBX-04).
	for _, target := range []string{"http://169.254.169.254", "http://10.0.0.1"} {
		if ok, _ := egressITBoxWget(t, cli, h.ContainerID, target, 4); ok {
			t.Fatalf("box reached internal target %q — the composition-root floor did not DROP it (SBX-04 breach)", target)
		}
	}
}
