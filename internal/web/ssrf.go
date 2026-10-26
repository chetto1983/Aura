package web

import (
	"context"
	"net/netip"

	"github.com/chetto1983/aura/internal/netguard"
)

// resolver is the injectable DNS seam (no real network in tests). It mirrors the
// net.Resolver.LookupNetIP signature so the production wiring passes the stdlib
// resolver directly.
type resolver interface {
	LookupNetIP(ctx context.Context, network, host string) ([]netip.Addr, error)
}

// guard is the SSRF security gate: metadata hostname → resolve → classify every
// record with netguard → pin. It owns the resolver and the pin cache; transport.go
// composes it.
type guard struct {
	res resolver
	pin *dnsPin
}

func newGuard(res resolver, pin *dnsPin, _ any) *guard {
	return &guard{res: res, pin: pin}
}

// validateAndPin is the per-host gate. Order matters: metadata hostnames are
// refused BEFORE any resolution (T-07-10), then a live pin short-circuits the
// lookup (anti-rebinding reuse, D-25), then a fresh resolve classifies EVERY
// record and fails closed if ANY is blocked (D-24 — never cherry-pick a public
// IP from a mixed set). On success it pins and returns the first public IP. A
// non-empty reason means blocked; the returned Addr is then the zero value.
func (g *guard) validateAndPin(ctx context.Context, convID, host string) (netip.Addr, string) {
	if netguard.IsMetadataHostname(host) {
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
		if _, blocked := netguard.Classify(ip); blocked {
			return netip.Addr{}, ReasonPrivateOrMetadata // fail closed on ANY blocked record
		}
		if !first.IsValid() {
			first = ip.Unmap()
		}
	}
	g.pin.Pin(convID, host, first)
	return first, ""
}
