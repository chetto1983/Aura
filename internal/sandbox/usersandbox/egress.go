// egress.go builds the per-box egress sidecar (SBX-04, D-03/D-05/D-07): an always-on
// filter-table nftables floor that gives the box full public internet (Claude-Code parity,
// D-04) while DROPping the tenancy boundary — RFC1918, the 169.254.169.254 cloud-metadata
// IP, and the shared-services Docker bridge (D-05). The floor is FILTER-TABLE ONLY. An
// opt-in FQDN allowlist (OpenSandbox) was removed on 2026-11-25: no shipped image carried
// its binary, so the sidecar exited at boot whenever it was configured.

package usersandbox

import (
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
// the tenancy boundary: a box can reach any PUBLIC host but never another box, Postgres,
// ArcadeDB, Garage, an MCP sidecar, or the host LAN at the network layer.
const (
	metadataIP  = "169.254.169.254/32" // cloud-metadata SSRF sink (T-37-06-METADATA)
	rfc1918Ten  = "10.0.0.0/8"
	rfc1918B172 = "172.16.0.0/12"
	rfc1918C192 = "192.168.0.0/16"

	// sharedServicesBridgeCIDR is the Docker shared-services bridge the aura daemon +
	// Postgres/ArcadeDB/Garage/the MCP sidecars sit on. Docker's default-address-pool hands user
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

// EgressSidecar is the resolved spec for a per-box egress sidecar container. It shares the
// box's network namespace (NetworkMode "container:<box>"), carries CAP_NET_ADMIN (the box
// never does), and applies the always-on filter-table floor. It is a pure value built by
// buildEgressSidecar; DockerBackend turns it into a container.
type EgressSidecar struct {
	Image        string   // the aura-egress image ref (default egressSidecarImage)
	BoxID        string   // the box container id whose netns the sidecar shares
	NetworkMode  string   // "container:<BoxID>"
	CapAdd       []string // ["NET_ADMIN"] — sidecar only (D-07)
	Env          []string // the floor ruleset
	FloorRuleset string   // the filter-table nft floor (also carried in Env)
}

// buildEgressSidecar builds the sidecar spec for a box. The always-on filter-table floor is
// emitted unconditionally (the tenancy boundary is not optional, D-05/D-07).
func buildEgressSidecar(boxID string) (EgressSidecar, error) {
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

	return sc, nil
}

// buildFloorRuleset renders the always-on filter-table nft floor: default policy ACCEPT (full
// public internet, D-04) with an explicit DROP for the metadata IP, each RFC1918 range, and
// the shared-services bridge (D-05). It is filter-table ONLY (no nat).
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
// Postgres/ArcadeDB/Garage/the MCP sidecars do not serve DNS — and it opens no new exfiltration
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
