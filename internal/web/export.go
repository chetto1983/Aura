package web

import (
	"context"
	"net"
	"net/netip"
)

// ClassifyIP is the exported form of the package-private classify: it maps a
// resolved IP to an SSRF block reason and a blocked flag, covering the full class
// table (loopback / link-local / private / multicast / unspecified / cgnat /
// this-network / IPv4-mapped unmap). The host-side egress proxy
// (internal/sandbox/network.go) reuses THIS instead of copying the class table, so
// IP classification has a single source of truth (OQ2/A4, no-dupl gate). It
// delegates straight to classify — no behavior is reimplemented here.
func ClassifyIP(ip netip.Addr) (reason string, blocked bool) {
	return classify(ip)
}

// DialGuard is the minimal resolve-then-pin surface the sandbox egress proxy needs
// to validate a CONNECT target with the SAME machinery web_fetch uses: hostname
// blocklist → per-(conv,host) DNS pin reuse → resolve + classify-every-record +
// fail-closed-on-any-blocked → pin. It wraps the package-private guard so the proxy
// inherits the full SSRF control without touching web internals or the HTTP
// transport (OQ2/A4 — export a thin surface, not the internals wholesale).
type DialGuard struct {
	g *guard
}

// NewDialGuard builds a DialGuard with a fresh per-process DNS pin cache (TTL in
// seconds, mirroring AURA_WEB_DNS_PIN_TTL_SEC semantics) over the system resolver.
// The proxy owns one DialGuard; the pin cache is keyed by (conversation,host) so
// concurrent conversations never share a pinned IP (D-25 isolation).
func NewDialGuard(dnsPinTTLSec int) *DialGuard {
	return &DialGuard{g: newGuard(net.DefaultResolver, newDNSPin(dnsPinTTLSec), nil)}
}

// ResolveAndPin validates host for conversation convID and returns the pinned
// public IP on success or a non-empty block reason (the IP is then the zero value).
// It is the proxy's resolve-then-pin gate; the proxy dials ONLY the returned IP so
// no second DNS lookup can rebind the target (TOCTOU close). It delegates to the
// guard's validateAndPin — the same gate web_fetch dials through.
func (d *DialGuard) ResolveAndPin(ctx context.Context, convID, host string) (netip.Addr, string) {
	return d.g.validateAndPin(ctx, convID, host)
}
