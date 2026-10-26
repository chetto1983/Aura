// Package netguard decides which addresses an outbound request may reach. It is the one
// classifier behind web_fetch (internal/web), the MCP transport (internal/mcp) and the
// browser URL check (internal/agent/mcptools); before 2026-10-26 the first two carried
// copies of it, and the third a narrower check of its own.
package netguard

import (
	"net/netip"
	"strings"
)

var (
	cgnat       = netip.MustParsePrefix("100.64.0.0/10")
	thisNetwork = netip.MustParsePrefix("0.0.0.0/8")
	metadataV6  = netip.MustParsePrefix("fd00:ec2::/32")
	nat64       = netip.MustParsePrefix("64:ff9b::/96")
	sixToFour   = netip.MustParsePrefix("2002::/16")
	globalV6    = netip.MustParsePrefix("2000::/3")
)

// reserved lists the blocks the IANA special-purpose registries (IPv4 and IPv6, read
// 2026-10-26) mark "Globally Reachable: False" that no netip predicate covers. IPv6 blocks
// outside 2000::/3 need no row: the IPv6 address space registry allocates no global
// unicast there. 192.0.0.0/24 and 2001::/23 are refused whole, so the reachable anycast,
// AS112, ORCHIDv2 and drone-ID blocks inside them are refused too; none serves a page.
var reserved = []netip.Prefix{
	netip.MustParsePrefix("192.0.0.0/24"),    // IETF protocol assignments, NAT64 discovery
	netip.MustParsePrefix("192.0.2.0/24"),    // TEST-NET-1
	netip.MustParsePrefix("192.88.99.2/32"),  // 6a44 relay
	netip.MustParsePrefix("198.18.0.0/15"),   // benchmarking
	netip.MustParsePrefix("198.51.100.0/24"), // TEST-NET-2
	netip.MustParsePrefix("203.0.113.0/24"),  // TEST-NET-3
	netip.MustParsePrefix("240.0.0.0/4"),     // reserved, limited broadcast included
	netip.MustParsePrefix("2001::/23"),       // IETF protocol assignments, Teredo included
	netip.MustParsePrefix("2001:db8::/32"),   // documentation
	netip.MustParsePrefix("3fff::/20"),       // documentation
}

// metadataHostnames are refused before any lookup.
var metadataHostnames = map[string]struct{}{
	"metadata.google.internal": {},
	"metadata.amazonaws.com":   {},
	"metadata.azure.com":       {},
	"kubernetes.default.svc":   {},
	"host.docker.internal":     {},
}

// Classify reports why ip must not be dialed; an empty reason means it may be. An IPv4
// address carried inside IPv6 is judged as that IPv4 address, so a NAT64 or 6to4 form of
// 169.254.169.254 is link_local, while a DNS64 answer for a public site stays dialable.
func Classify(ip netip.Addr) (reason string, blocked bool) {
	if !ip.IsValid() {
		return "invalid_target", true
	}
	ip = carriedIPv4(ip)
	switch {
	case ip.IsLoopback():
		return "loopback", true
	case ip.IsLinkLocalUnicast(), ip.IsLinkLocalMulticast():
		return "link_local", true
	case ip.IsPrivate():
		return "private", true
	case ip.IsMulticast():
		return "multicast", true
	case ip.IsUnspecified():
		return "unspecified", true
	case cgnat.Contains(ip):
		return "cgnat", true
	case thisNetwork.Contains(ip):
		return "this_network", true
	case ip.Is6() && !globalV6.Contains(ip), inReserved(ip):
		return "reserved", true
	}
	return "", false
}

// IsMetadata reports a cloud-metadata or link-local destination, which no profile may
// reach. fd00:ec2::/32 is named on its own because Classify files it as private.
func IsMetadata(ip netip.Addr) bool {
	ip = carriedIPv4(ip)
	return ip.IsLinkLocalUnicast() || ip.IsLinkLocalMulticast() || metadataV6.Contains(ip)
}

// IsMetadataHostname reports a cloud-metadata or cluster-internal name. The match ignores
// case and the root dot, which a resolver accepts as the same name.
func IsMetadataHostname(host string) bool {
	_, blocked := metadataHostnames[strings.TrimSuffix(strings.ToLower(host), ".")]
	return blocked
}

// carriedIPv4 returns the IPv4 address that ip delivers to when it is IPv4-mapped, in the
// NAT64 well-known prefix (RFC 6052 §2.2: the last 32 bits) or 6to4 (RFC 3056 §2: bits
// 16-47), and ip unchanged otherwise. A network-specific NAT64 prefix cannot be told from
// the address alone; the egress floor is the layer for it.
func carriedIPv4(ip netip.Addr) netip.Addr {
	ip = ip.Unmap()
	b := ip.As16()
	switch {
	case nat64.Contains(ip):
		return netip.AddrFrom4([4]byte(b[12:]))
	case sixToFour.Contains(ip):
		return netip.AddrFrom4([4]byte(b[2:]))
	}
	return ip
}

func inReserved(ip netip.Addr) bool {
	for _, p := range reserved {
		if p.Contains(ip) {
			return true
		}
	}
	return false
}
