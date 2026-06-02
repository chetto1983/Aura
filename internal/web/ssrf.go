package web

import (
	"context"
	"net/netip"
	"strings"
)

// CIDR constants the netip Is* predicates do NOT cover. cgnat (RFC 6598) and the
// "this network" 0.0.0.0/8 are public-looking but route internally; the v6
// metadata prefix covers the cloud-metadata IPv6 region. Parsed once at init via
// MustParsePrefix so a typo panics at startup, not at request time.
var (
	cgnatPrefix   = netip.MustParsePrefix("100.64.0.0/10")
	thisNetPrefix = netip.MustParsePrefix("0.0.0.0/8")
	metadataV6Pfx = netip.MustParsePrefix("fd00:ec2::/32")
)

// hostnameBlocklist is the exact-match set of cloud-metadata / cluster-internal
// hostnames blocked BEFORE resolution (T-07-10). Keys are lowercase; lookups
// lower-case the candidate so the block is case-insensitive.
var hostnameBlocklist = map[string]struct{}{
	"metadata.google.internal": {},
	"metadata.amazonaws.com":   {},
	"metadata.azure.com":       {},
	"kubernetes.default.svc":   {},
	"host.docker.internal":     {},
}

// classify maps a resolved IP to a block reason. It is the mutation-gate target:
// every branch is a distinct SSRF class with its own table row in
// TestBlocked_Classification. Unmap() runs FIRST so an IPv4-mapped IPv6 form
// (::ffff:169.254.169.254) collapses to its v4 shape and hits the v4 predicates
// (Pitfall 2 / T-07-11) — without it the metadata IP slips past as opaque v6.
func classify(ip netip.Addr) (reason string, blocked bool) {
	if !ip.IsValid() {
		return ReasonInvalidTarget, true
	}
	ip = ip.Unmap()
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
	case cgnatPrefix.Contains(ip):
		return "cgnat", true
	case thisNetPrefix.Contains(ip):
		return "this_network", true
	case metadataV6Pfx.Contains(ip):
		return "link_local", true
	}
	return "", false
}

// resolver is the injectable DNS seam (no real network in tests). It mirrors the
// net.Resolver.LookupNetIP signature so the production wiring passes the stdlib
// resolver directly.
type resolver interface {
	LookupNetIP(ctx context.Context, network, host string) ([]netip.Addr, error)
}

// guard is the SSRF security gate: hostname blocklist → resolve → classify-every
// → pin. It owns the resolver and the pin cache; transport.go composes it.
type guard struct {
	res resolver
	pin *dnsPin
}

func newGuard(res resolver, pin *dnsPin, _ any) *guard {
	return &guard{res: res, pin: pin}
}

// validateAndPin is the per-host gate. Order matters: the hostname blocklist is
// checked BEFORE any resolution (T-07-10), then a live pin short-circuits the
// lookup (anti-rebinding reuse, D-25), then a fresh resolve classifies EVERY
// record and fails closed if ANY is blocked (D-24 — never cherry-pick a public
// IP from a mixed set). On success it pins and returns the first public IP. A
// non-empty reason means blocked; the returned Addr is then the zero value.
func (g *guard) validateAndPin(ctx context.Context, convID, host string) (netip.Addr, string) {
	if _, bad := hostnameBlocklist[strings.ToLower(host)]; bad {
		return netip.Addr{}, ReasonPrivateOrMetadata
	}
	if ip, ok := g.pin.Pinned(convID, host); ok {
		return ip, ""
	}
	ips, err := g.res.LookupNetIP(ctx, "ip", host)
	if err != nil || len(ips) == 0 {
		return netip.Addr{}, ReasonInvalidTarget
	}
	var first netip.Addr
	for _, ip := range ips {
		if _, blocked := classify(ip); blocked {
			return netip.Addr{}, ReasonPrivateOrMetadata // fail closed on ANY blocked record
		}
		if !first.IsValid() {
			first = ip.Unmap()
		}
	}
	g.pin.Pin(convID, host, first)
	return first, ""
}
