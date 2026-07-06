//go:build docker_integration

// egress_integration_test.go proves SBX-04 egress enforcement against a LIVE dockerd: the
// always-on filter-table floor lets a box reach the public internet but DROPs the tenancy
// boundary (RFC1918 + the 169.254.169.254 metadata IP + the shared-services bridge), the
// sidecar carries CAP_NET_ADMIN while the box never does, and the sidecar is torn down with
// the box (no orphan). The DROP assertions are meaningful ONLY on a native-Linux
// non-masquerading bridge — Docker Desktop/WSL vpnkit NATs around any nftables rule
// (37-RESEARCH Pitfall 3) — so skipUnlessEnforcingBridge makes them informational-only
// locally and MANDATORY (t.Fatal on a non-Linux daemon) under $CI (no-skip-as-green).

package usersandbox

import (
	"context"
	"fmt"
	"os"
	"runtime"
	"strings"
	"testing"

	"github.com/moby/moby/client"
)

// egressTestImage is the aura-egress sidecar image the enforcement tier launches. It defaults
// to the local build tag and is overridable so a live run can point at a digest-pinned ref.
// The native-Linux CI builds `aura-egress:build` (docker/aura-egress/Dockerfile) and sets
// AURA_EGRESS_TEST_IMAGE before running this tier.
func egressTestImage() string {
	if v := strings.TrimSpace(os.Getenv("AURA_EGRESS_TEST_IMAGE")); v != "" {
		return v
	}
	return "aura-egress:latest"
}

// egressFQDNImageOrSkip returns an aura-egress image built WITH the OpenSandbox egress binary
// (the runc-only FQDN mode, #934). The default floor-only image cannot enforce an FQDN
// allowlist, so the FQDN test skips unless AURA_EGRESS_FQDN_IMAGE names such an image.
func egressFQDNImageOrSkip(t *testing.T) string {
	t.Helper()
	v := strings.TrimSpace(os.Getenv("AURA_EGRESS_FQDN_IMAGE"))
	if v == "" {
		t.Skip("FQDN-allowlist enforcement needs an aura-egress image built WITH the OpenSandbox egress binary " +
			"(runc-only, issue #934); set AURA_EGRESS_FQDN_IMAGE to run this tier")
	}
	return v
}

// skipUnlessEnforcingBridge gates the egress DROP assertions on a native-Linux
// non-masquerading bridge. Under $CI it is MANDATORY: the daemon must be native-Linux (a
// non-Linux daemon t.Fatals — a skipped enforcement tier is never a silent pass). Locally it
// is informational-only (Docker Desktop/WSL vpnkit NATs the bridge, Pitfall 3) unless a dev
// opts in with AURA_EGRESS_ENFORCE=1 on a real native-Linux host.
func skipUnlessEnforcingBridge(t *testing.T) {
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

// boxWgetOK reports whether the box can reach url over HTTP within tmo seconds. A DROPped
// destination hangs until the timeout (false); an allowed destination that serves HTTP 200
// returns true. Wrapped in busybox `timeout` so a dropped target can never wedge the test.
func boxWgetOK(t *testing.T, cli *client.Client, containerID, url string, tmo int) bool {
	t.Helper()
	cmd := []string{"/bin/sh", "-c", fmt.Sprintf("timeout %d wget -T %d -q -O /dev/null %s", tmo+2, tmo, url)}
	_, code := rawExec(t, cli, containerID, cmd)
	return code == 0
}

// inspectCapAdd returns the CapAdd list of a container by id-or-name.
func inspectCapAdd(t *testing.T, cli *client.Client, ref string) []string {
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

// TestEgress_FloorDropsInternal is the SBX-04 enforcement proof: with the always-on sidecar,
// the box reaches the public internet (D-04) but is DROPPED from RFC1918 / the metadata IP /
// the shared-services bridge (D-05). It also asserts the cap placement (NET_ADMIN on the
// sidecar, never the box) and that Suspend retains the sidecar (stopped) while Stop removes it
// with no orphan. DROP assertions run native-Linux/CI only (Pitfall 3).
func TestEgress_FloorDropsInternal(t *testing.T) {
	skipUnlessDockerd(t)
	skipUnlessEnforcingBridge(t)
	cli := newTestDockerClient(t)
	backend := NewDockerBackend(cli, testBoxImage(), testLimits(), WithEgress(egressTestImage()))
	ctx := context.Background()

	id := uniqueIdentity(t, "egress-floor")
	spec := SandboxSpec{IdentityID: id, Egress: EgressPolicy{Floor: true}}
	h, err := backend.Resolve(ctx, spec)
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	t.Cleanup(func() { _ = backend.Stop(context.Background(), h) })

	// Cap placement (D-07): NET_ADMIN on the sidecar, NEVER on the box.
	if caps := inspectCapAdd(t, cli, h.ContainerID); len(caps) != 0 {
		t.Fatalf("box must carry no added capabilities, got CapAdd=%v", caps)
	}
	sidecar := egressName(id)
	if caps := inspectCapAdd(t, cli, sidecar); !containsStr(caps, capNetAdmin) {
		t.Fatalf("sidecar must carry %s, got CapAdd=%v", capNetAdmin, caps)
	}
	sins, err := cli.ContainerInspect(ctx, sidecar, client.ContainerInspectOptions{})
	if err != nil {
		t.Fatalf("inspect sidecar: %v", err)
	}
	if sins.Container.HostConfig == nil || string(sins.Container.HostConfig.NetworkMode) != "container:"+h.ContainerID {
		t.Fatalf("sidecar must share the box netns via container:%s, got %v", h.ContainerID, sins.Container.HostConfig)
	}
	if sins.Container.State == nil || !sins.Container.State.Running {
		t.Fatalf("sidecar must be running, state=%+v", sins.Container.State)
	}

	// Full public internet (D-04): a public HTTP host is reachable.
	if !boxWgetOK(t, cli, h.ContainerID, "http://example.com", 6) {
		t.Fatalf("box must reach the public internet (example.com) — floor over-blocked public egress")
	}

	// Tenancy boundary (D-05): every internal target is DROPPED (unreachable).
	for _, target := range []string{
		"http://10.0.0.1",        // RFC1918 10/8
		"http://169.254.169.254", // cloud-metadata IP
		"http://172.18.0.1",      // shared-services bridge
	} {
		if boxWgetOK(t, cli, h.ContainerID, target, 4) {
			t.Fatalf("box reached internal target %q — the tenancy floor did not DROP it (SBX-04 breach)", target)
		}
	}

	// Suspend retains the sidecar (stopped, not removed) so Resume can restart it.
	if err := backend.Suspend(ctx, h); err != nil {
		t.Fatalf("suspend: %v", err)
	}
	ss, err := cli.ContainerInspect(ctx, sidecar, client.ContainerInspectOptions{})
	if err != nil {
		t.Fatalf("inspect sidecar after suspend: %v", err)
	}
	if ss.Container.State == nil || ss.Container.State.Running {
		t.Fatalf("sidecar must be stopped (retained) after suspend, state=%+v", ss.Container.State)
	}

	// Stop removes the sidecar with the box — no orphan.
	if err := backend.Stop(ctx, h); err != nil {
		t.Fatalf("stop: %v", err)
	}
	if containerExists(t, cli, sidecar) {
		t.Fatalf("egress sidecar %q must be gone after Stop (no orphan)", sidecar)
	}
}

// TestEgress_FQDNAllowlist proves the runc-only tightened allowlist: a box resolved with a
// CONFIGURED allowlist reaches an allowed FQDN, and a disallowed FQDN is DROPPED. It needs an
// aura-egress image built with the OpenSandbox egress binary (skips otherwise) and a
// native-Linux enforcing bridge.
func TestEgress_FQDNAllowlist(t *testing.T) {
	skipUnlessDockerd(t)
	skipUnlessEnforcingBridge(t)
	fqdnImage := egressFQDNImageOrSkip(t)
	cli := newTestDockerClient(t)
	backend := NewDockerBackend(cli, testBoxImage(), testLimits(), WithEgress(fqdnImage))
	ctx := context.Background()

	id := uniqueIdentity(t, "egress-fqdn")
	// The allowlist is the CONFIGURED policy (as 37-05 specFor would source it), not an inline
	// default — an allowed host and a disallowed host prove the config drives enforcement.
	spec := SandboxSpec{IdentityID: id, Egress: EgressPolicy{FQDNAllowlist: []string{"example.com"}}}
	h, err := backend.Resolve(ctx, spec)
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	t.Cleanup(func() { _ = backend.Stop(context.Background(), h) })

	if !boxWgetOK(t, cli, h.ContainerID, "http://example.com", 6) {
		t.Fatalf("box must reach the ALLOWED FQDN example.com under the configured allowlist")
	}
	if boxWgetOK(t, cli, h.ContainerID, "http://www.wikipedia.org", 5) {
		t.Fatalf("box reached a DISALLOWED FQDN — the tightened allowlist did not DROP it (SBX-04 breach)")
	}
}

// containsStr reports whether s is in xs.
func containsStr(xs []string, s string) bool {
	for _, x := range xs {
		if x == s {
			return true
		}
	}
	return false
}
