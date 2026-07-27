// egress_test.go unit-proves the egress sidecar builder (buildEgressSidecar) without a
// Docker daemon: the always-on filter-table floor DROPs the tenancy boundary and ACCEPTs
// public (TestEgress_FloorRuleset), Runsc + a tightened FQDN allowlist is refused with the
// #934 error (TestEgress_RunscRejectsFQDN), NET_ADMIN lands on the sidecar and NEVER the box
// (TestEgress_CapOnSidecarOnly), and the OpenSandbox rules are built from the CONFIGURED
// allowlist — not an inline default (TestEgress_AllowlistFromPolicy). The live DROP
// enforcement is proven separately on native-Linux dockerd in egress_integration_test.go.

package usersandbox

import (
	"encoding/json"
	"slices"
	"strings"
	"testing"
)

// TestEgress_FloorRuleset asserts the generated filter-table floor DROPs the four internal
// targets (metadata + the three RFC1918 ranges) plus the shared-services bridge, and ACCEPTs
// everything else (policy accept = full public internet, D-04). The floor uses only the
// filter table (no nat) so it is gVisor-safe (issue #934).
func TestEgress_FloorRuleset(t *testing.T) {
	sc, err := buildEgressSidecar(EgressPolicy{Floor: true}, "box-abc", Runc)
	if err != nil {
		t.Fatalf("buildEgressSidecar: %v", err)
	}

	ruleset := sc.FloorRuleset
	for _, target := range []string{metadataIP, rfc1918Ten, rfc1918B172, rfc1918C192, sharedServicesBridgeCIDR} {
		line := "ip daddr " + target
		if !strings.Contains(ruleset, line) || !strings.Contains(ruleset, target+" counter drop") {
			t.Errorf("floor ruleset missing DROP for %q:\n%s", target, ruleset)
		}
	}
	// Public ACCEPT: the base policy is accept (only the internal carve-out is dropped).
	if !strings.Contains(ruleset, "policy accept") {
		t.Errorf("floor ruleset missing public ACCEPT (policy accept):\n%s", ruleset)
	}
	// DNS carve-out, and it must precede the DROPs — nftables takes the FIRST match, so a
	// rule ordered after `ip daddr 10.0.0.0/8 drop` would never be reached. Without it the
	// box cannot resolve names at all: Docker hands a default-bridge box the HOST's upstream
	// resolvers, which are RFC1918 on any LAN, so the drops eat its own DNS and D-04's "full
	// public internet" becomes reachable-by-IP-only (proven live, TestEgress_FloorDropsInternal).
	firstDrop := strings.Index(ruleset, "counter drop")
	for _, accept := range []string{"udp dport 53 accept", "tcp dport 53 accept"} {
		at := strings.Index(ruleset, accept)
		if at < 0 {
			t.Errorf("floor ruleset missing DNS carve-out %q:\n%s", accept, ruleset)
			continue
		}
		if firstDrop >= 0 && at > firstDrop {
			t.Errorf("DNS carve-out %q must precede the DROPs (first match wins):\n%s", accept, ruleset)
		}
	}
	// Filter-table only — the nat table (gVisor-incompatible, #934) must never appear.
	if strings.Contains(ruleset, "nat") {
		t.Errorf("floor ruleset must be filter-table only (no nat, #934):\n%s", ruleset)
	}
	if !strings.Contains(ruleset, "type filter hook output") {
		t.Errorf("floor ruleset must hook the filter output chain:\n%s", ruleset)
	}
	// The ruleset is also carried into the sidecar env for the entrypoint to apply.
	if !slices.Contains(sc.Env, envFloorRuleset+"="+ruleset) {
		t.Errorf("floor ruleset must be carried in the sidecar env under %s", envFloorRuleset)
	}
	// Floor-only (no allowlist) => no OpenSandbox FQDN mode.
	if sc.FQDNMode {
		t.Errorf("floor-only policy must not enable FQDN mode")
	}
	if envValue(sc.Env, envOpenSandboxMode) != "" {
		t.Errorf("floor-only policy must not set %s", envOpenSandboxMode)
	}
}

// TestEgress_RunscRejectsFQDN proves the #934 mutual-exclusion: a Runsc (gVisor) box combined
// with a tightened FQDN allowlist is refused with ErrRunscFQDNMutualExclusion, because the
// OpenSandbox allowlist needs the nat table gVisor does not implement. The floor-only case
// under Runsc is still accepted (only the nat-table FQDN mode is runc-only).
func TestEgress_RunscRejectsFQDN(t *testing.T) {
	_, err := buildEgressSidecar(EgressPolicy{FQDNAllowlist: []string{"pypi.org"}}, "box-runsc", Runsc)
	if err == nil {
		t.Fatalf("Runsc + FQDN allowlist must be refused (#934), got nil error")
	}
	if err != ErrRunscFQDNMutualExclusion {
		t.Fatalf("want ErrRunscFQDNMutualExclusion, got %v", err)
	}

	// Runsc WITHOUT an allowlist is fine — the filter-table floor is gVisor-safe.
	sc, err := buildEgressSidecar(EgressPolicy{Floor: true}, "box-runsc", Runsc)
	if err != nil {
		t.Fatalf("Runsc floor-only must be accepted, got %v", err)
	}
	if sc.FQDNMode {
		t.Errorf("Runsc floor-only must not enable FQDN mode")
	}
}

// TestEgress_CapOnSidecarOnly asserts NET_ADMIN is granted to the sidecar spec and that the
// box HostConfig (toHostConfig) never carries any added capability — the box stays
// net-unprivileged (D-07); only the sidecar can program the netns.
func TestEgress_CapOnSidecarOnly(t *testing.T) {
	sc, err := buildEgressSidecar(EgressPolicy{Floor: true}, "box-cap", Runc)
	if err != nil {
		t.Fatalf("buildEgressSidecar: %v", err)
	}
	if !slices.Contains(sc.CapAdd, capNetAdmin) {
		t.Fatalf("sidecar must carry %s, got CapAdd=%v", capNetAdmin, sc.CapAdd)
	}
	if sc.NetworkMode != "container:box-cap" {
		t.Fatalf("sidecar NetworkMode = %q, want container:box-cap", sc.NetworkMode)
	}

	// The box itself must never gain a capability — assert against the single pinned-safe
	// translator, for an adversarial spec that (fruitlessly) asks for a runsc + allowlist box.
	hc := toHostConfig(SandboxSpec{
		IdentityID:   "box-cap",
		WorkspaceVol: "aura-box-box-cap",
		Egress:       EgressPolicy{Floor: true, FQDNAllowlist: []string{"pypi.org"}},
	})
	if len(hc.CapAdd) != 0 {
		t.Fatalf("box HostConfig must never add capabilities, got CapAdd=%v", hc.CapAdd)
	}
}

// TestEgress_AllowlistFromPolicy proves the sidecar's OpenSandbox rules are built from the
// CONFIGURED EgressPolicy.FQDNAllowlist (the config-sourced list from 37-05 specFor), NOT an
// inline default: a policy carrying exactly {registry.example, *.pypi.org} yields an
// OpenSandbox allow-list of exactly those hosts (T-37-06-CONFIGDROP).
func TestEgress_AllowlistFromPolicy(t *testing.T) {
	want := []string{"registry.example", "*.pypi.org"}
	sc, err := buildEgressSidecar(EgressPolicy{FQDNAllowlist: want}, "box-allow", Runc)
	if err != nil {
		t.Fatalf("buildEgressSidecar: %v", err)
	}
	if !sc.FQDNMode {
		t.Fatalf("a configured allowlist must enable FQDN mode")
	}
	if got := envValue(sc.Env, envOpenSandboxMode); got != openSandboxModeDNSNFT {
		t.Fatalf("%s = %q, want %q", envOpenSandboxMode, got, openSandboxModeDNSNFT)
	}

	rulesJSON := envValue(sc.Env, envOpenSandboxRules)
	if rulesJSON == "" {
		t.Fatalf("%s must be set in FQDN mode", envOpenSandboxRules)
	}
	var rules openSandboxRules
	if err := json.Unmarshal([]byte(rulesJSON), &rules); err != nil {
		t.Fatalf("unmarshal %s: %v (raw=%q)", envOpenSandboxRules, err, rulesJSON)
	}
	if !slices.Equal(rules.Allow, want) {
		t.Fatalf("OpenSandbox allow = %v, want exactly the configured %v", rules.Allow, want)
	}

	// The floor is STILL present under FQDN mode (the tenancy boundary is unconditional).
	if !strings.Contains(sc.FloorRuleset, "ip daddr "+rfc1918Ten) {
		t.Errorf("floor must remain in force under FQDN mode:\n%s", sc.FloorRuleset)
	}
}

// envValue returns the value of key in a "KEY=VALUE" env slice, or "" if absent.
func envValue(env []string, key string) string {
	prefix := key + "="
	for _, e := range env {
		if value, found := strings.CutPrefix(e, prefix); found {
			return value
		}
	}
	return ""
}
