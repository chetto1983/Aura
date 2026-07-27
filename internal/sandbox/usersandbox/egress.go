// egress.go builds the per-box egress sidecar (SBX-04, D-03/D-05/D-07): an always-on
// filter-table nftables floor that gives the box full public internet (Claude-Code parity,
// D-04) while DROPping the tenancy boundary — RFC1918, the 169.254.169.254 cloud-metadata
// IP, and the shared-services Docker bridge (D-05). The floor is FILTER-TABLE ONLY so it
// works under BOTH runc and gVisor runsc (gVisor implements filter+mangle but NOT nat —
// issue #934). The opt-in tightened FQDN allowlist (OpenSandbox DNS-redirect, nat-table)
// is therefore runc-ONLY: buildEgressSidecar refuses Runsc + a configured allowlist with a
// clear #934 error. The allowlist it enforces is the CONFIGURED one — sourced by 37-05's
// specFor from AURA_SANDBOX_EGRESS_ALLOWLIST into EgressPolicy.FQDNAllowlist — never an
// inline literal.

package usersandbox

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"
)

// egressSidecarImage is the default aura-egress sidecar image (docker/aura-egress/Dockerfile).
// A production deployment overrides it via WithEgress with a digest-pinned registry ref.
const egressSidecarImage = "aura-egress:latest"

// capNetAdmin is the single kernel capability the sidecar carries so it can program the
// box netns nftables ruleset. It is granted to the SIDECAR ONLY — the box container never
// gets it (D-07: the box stays net-unprivileged).
const capNetAdmin = "NET_ADMIN"

// Filter-table floor DROP targets (D-05). RFC1918 private ranges + the cloud-metadata IP are
// the tenancy boundary: a box can reach any PUBLIC host but never another box, agent-memory,
// Garage, Postgres/Neo4j, or the host LAN at the network layer.
const (
	metadataIP  = "169.254.169.254/32" // cloud-metadata SSRF sink (T-37-06-METADATA)
	rfc1918Ten  = "10.0.0.0/8"
	rfc1918B172 = "172.16.0.0/12"
	rfc1918C192 = "192.168.0.0/16"

	// sharedServicesBridgeCIDR is the Docker shared-services bridge the aura daemon +
	// Postgres/Neo4j/Garage/agent-memory sit on. Docker's default-address-pool hands user
	// networks /16s starting at 172.18.0.0/16, so this range is ALSO covered by the
	// rfc1918B172 DROP above — it is listed explicitly as defense-in-depth (the D-05
	// tenancy boundary must never depend on the broader RFC1918 line alone).
	sharedServicesBridgeCIDR = "172.18.0.0/16"

	// dnsPort is the one destination port the floor accepts ahead of the DROPs, so the box
	// can reach the RFC1918 resolver Docker gave it (see buildFloorRuleset).
	dnsPort = "53"
)

// envFloorRuleset carries the Go-generated filter-table nft floor into the sidecar; the
// image entrypoint applies it with `nft -f -`. Keeping generation in Go (not baked in the
// image) makes the ruleset unit-testable and the image generic.
const envFloorRuleset = "AURA_EGRESS_FLOOR_RULESET"

// OpenSandbox egress env (third-party sidecar — upstream naming kept per CLAUDE.md). Set
// ONLY when a tightened FQDN allowlist is configured (runc-only, D-06).
const (
	envOpenSandboxMode  = "OPENSANDBOX_EGRESS_MODE"
	envOpenSandboxRules = "OPENSANDBOX_EGRESS_RULES"

	openSandboxModeDNSNFT = "dns+nft" // recommended strict mode (DNS proxy + nftables allow-sets)
)

// ErrRunscFQDNMutualExclusion is returned by buildEgressSidecar when a Runsc (gVisor) box is
// combined with a tightened FQDN allowlist. The OpenSandbox allowlist needs an iptables
// REDIRECT in the nat table, which gVisor's netstack does not implement (issue #934) — the
// two are mutually exclusive. The always-on filter-table floor still runs under runsc; only
// the opt-in FQDN mode is refused.
var ErrRunscFQDNMutualExclusion = errors.New(
	"usersandbox: tightened FQDN egress allowlist is runc-only — gVisor runsc does not implement the nat table (issue #934); " +
		"drop the allowlist for a runsc box (the filter-table floor still enforces the tenancy boundary) or use runc")

// EgressSidecar is the resolved spec for a per-box egress sidecar container. It shares the
// box's network namespace (NetworkMode "container:<box>"), carries CAP_NET_ADMIN (the box
// never does), applies the always-on filter-table floor, and — when FQDNMode is on — runs
// the OpenSandbox egress proxy from the configured allowlist. It is a pure value built by
// buildEgressSidecar; DockerBackend turns it into a container.
type EgressSidecar struct {
	Image        string   // the aura-egress image ref (default egressSidecarImage)
	BoxID        string   // the box container id whose netns the sidecar shares
	NetworkMode  string   // "container:<BoxID>"
	CapAdd       []string // ["NET_ADMIN"] — sidecar only (D-07)
	Env          []string // floor ruleset (always) + OpenSandbox env (FQDN mode only)
	FloorRuleset string   // the filter-table nft floor (also carried in Env)
	FQDNMode     bool     // true when a tightened allowlist is configured (runc-only)
}

// buildEgressSidecar builds the sidecar spec for a box from its CONFIGURED egress policy
// (EgressPolicy.FQDNAllowlist is sourced by 37-05 specFor from AURA_SANDBOX_EGRESS_ALLOWLIST
// — never an inline list) and its runtime. The always-on filter-table floor is emitted
// unconditionally (the tenancy boundary is not optional, D-05/D-07). When the policy carries
// a non-empty FQDNAllowlist the sidecar additionally runs OpenSandbox in dns+nft mode built
// from exactly those hosts; that mode is runc-only, so Runsc + a configured allowlist is
// refused with ErrRunscFQDNMutualExclusion (issue #934).
func buildEgressSidecar(policy EgressPolicy, boxID string, runtime RuntimeClass) (EgressSidecar, error) {
	if boxID == "" {
		return EgressSidecar{}, errors.New("usersandbox: buildEgressSidecar requires a non-empty boxID")
	}

	floor := buildFloorRuleset(sharedServicesBridgeCIDR)
	sc := EgressSidecar{
		Image:        egressSidecarImage,
		BoxID:        boxID,
		NetworkMode:  "container:" + boxID,
		CapAdd:       []string{capNetAdmin},
		Env:          []string{envFloorRuleset + "=" + floor},
		FloorRuleset: floor,
	}

	if len(policy.FQDNAllowlist) == 0 {
		return sc, nil
	}

	// A tightened allowlist is configured — OpenSandbox FQDN mode. It is runc-only (#934).
	if runtime == Runsc {
		return EgressSidecar{}, ErrRunscFQDNMutualExclusion
	}
	rules, err := buildOpenSandboxRules(policy.FQDNAllowlist)
	if err != nil {
		return EgressSidecar{}, fmt.Errorf("usersandbox: build OpenSandbox egress rules: %w", err)
	}
	sc.FQDNMode = true
	sc.Env = append(sc.Env,
		envOpenSandboxMode+"="+openSandboxModeDNSNFT,
		envOpenSandboxRules+"="+rules,
	)
	return sc, nil
}

// buildFloorRuleset renders the always-on filter-table nft floor: default policy ACCEPT (full
// public internet, D-04) with an explicit DROP for the metadata IP, each RFC1918 range, and
// the shared-services bridge (D-05). It is filter-table ONLY (no nat) so it is identical under
// runc and gVisor runsc (issue #934 only affects the nat-table FQDN mode).
//
// DNS is accepted BEFORE the drops, and that carve-out is what makes D-04 true rather than
// nominal. A box created by DockerBackend sets no NetworkMode, so it lands on Docker's default
// bridge — which does NOT provide the 127.0.0.11 embedded resolver; the daemon copies the
// HOST's upstream nameservers into the box instead, and on a LAN appliance those are RFC1918.
// Without this the drops eat the box's own DNS: name resolution fails with EPERM while raw IPs
// still connect, so "full public internet" is reachable by address and unusable by name. Proven
// on a native-Linux box (resolver 10.166.10.248): `nslookup example.com` → "Operation not
// permitted", `wget http://example.com` → "bad address", `wget http://<ip>` → HTTP answer.
//
// Scoped to the PORT, not to resolver addresses, on purpose: the sidecar shares the box netns
// but not its mount namespace, so it cannot read the box's /etc/resolv.conf to learn which
// server to allow. The tenancy boundary is unaffected — port 53 reaches a DNS server, and
// Postgres/Neo4j/Garage/agent-memory do not serve DNS — and it opens no new exfiltration
// channel, since D-04 already grants unrestricted PUBLIC egress.
func buildFloorRuleset(bridgeCIDR string) string {
	drops := []string{metadataIP, rfc1918Ten, rfc1918B172, rfc1918C192, bridgeCIDR}
	var b strings.Builder
	b.WriteString("table ip aura_egress {\n")
	b.WriteString("\tchain output {\n")
	b.WriteString("\t\ttype filter hook output priority 0; policy accept;\n")
	// First match wins in an nftables chain, so these must precede the drops.
	fmt.Fprintf(&b, "\t\tudp dport %s accept\n", dnsPort)
	fmt.Fprintf(&b, "\t\ttcp dport %s accept\n", dnsPort)
	for _, cidr := range drops {
		fmt.Fprintf(&b, "\t\tip daddr %s counter drop\n", cidr)
	}
	b.WriteString("\t}\n")
	b.WriteString("}\n")
	return b.String()
}

// openSandboxRules is the OpenSandbox egress policy document (OPENSANDBOX_EGRESS_RULES). Only
// the allow list is populated — the configured FQDN allowlist — so an allowed host resolves
// and everything else is DROPped by the proxy.
type openSandboxRules struct {
	Allow []string `json:"allow"`
}

// buildOpenSandboxRules marshals the CONFIGURED allowlist into the OpenSandbox rules JSON.
// The hosts come straight from policy.FQDNAllowlist (config-sourced) — proving a real
// operator config, not a test fixture, drives enforcement (T-37-06-CONFIGDROP).
func buildOpenSandboxRules(allowlist []string) (string, error) {
	b, err := json.Marshal(openSandboxRules{Allow: allowlist})
	if err != nil {
		return "", err
	}
	return string(b), nil
}
