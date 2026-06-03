//go:build !web_integration

package web

import (
	"context"
	"net/netip"
	"testing"
)

// TestClassifyIP_NoDrift is the no-drift guard for the exported reuse surface: for
// a representative IP across every block class (plus a public IP), ClassifyIP MUST
// return byte-for-byte the same (reason,blocked) as the package-private classify.
// If the export ever stops delegating (a copy creeps in), this fails — that is the
// dupl/single-source contract (T-08-04-INFO-SSRF) enforced as a test.
func TestClassifyIP_NoDrift(t *testing.T) {
	t.Parallel()
	ips := []string{
		"127.0.0.1",              // loopback v4
		"::1",                    // loopback v6
		"169.254.169.254",        // link-local metadata v4
		"fe80::1",                // link-local v6
		"10.0.0.5",               // private 10/8
		"192.168.1.1",            // private 192.168/16
		"fc00::1",                // ULA v6 (private)
		"100.64.0.1",             // cgnat
		"0.0.0.0",                // unspecified
		"0.1.2.3",                // this-network
		"225.1.2.3",              // global multicast
		"::ffff:169.254.169.254", // IPv4-mapped metadata (Unmap path)
		"::ffff:127.0.0.1",       // IPv4-mapped loopback
		"1.1.1.1",                // public v4
		"2606:4700:4700::1111",   // public v6
	}
	for _, s := range ips {
		t.Run(s, func(t *testing.T) {
			t.Parallel()
			ip := netip.MustParseAddr(s)
			wantReason, wantBlocked := classify(ip)
			gotReason, gotBlocked := ClassifyIP(ip)
			if gotReason != wantReason || gotBlocked != wantBlocked {
				t.Fatalf("ClassifyIP(%s) = (%q,%v); classify = (%q,%v) — export drifted from internal",
					s, gotReason, gotBlocked, wantReason, wantBlocked)
			}
		})
	}
}

// TestClassifyIP_InvalidNoDrift covers the zero-Addr fail-closed default through the
// export — an invalid record must classify invalid_target/blocked identically.
func TestClassifyIP_InvalidNoDrift(t *testing.T) {
	t.Parallel()
	wantReason, wantBlocked := classify(netip.Addr{})
	gotReason, gotBlocked := ClassifyIP(netip.Addr{})
	if gotReason != wantReason || gotBlocked != wantBlocked {
		t.Fatalf("ClassifyIP(zero) = (%q,%v); classify = (%q,%v)", gotReason, gotBlocked, wantReason, wantBlocked)
	}
}

// dialGuardResolver is a deterministic resolver injected into a DialGuard so the
// resolve-then-pin path never touches real DNS in the unit tier.
type dialGuardResolver struct {
	answers map[string][]netip.Addr
}

func (r dialGuardResolver) LookupNetIP(_ context.Context, _, host string) ([]netip.Addr, error) {
	return r.answers[host], nil
}

// newTestDialGuard builds a DialGuard over an injected resolver — same composition
// as NewDialGuard but with a stub resolver, so the proxy's resolve-then-pin gate is
// exercised without live DNS. It reuses the package-private guard/dnsPin, proving
// the exported surface delegates rather than reimplements.
func newTestDialGuard(answers map[string][]netip.Addr, ttlSec int) *DialGuard {
	return &DialGuard{g: newGuard(dialGuardResolver{answers: answers}, newDNSPin(ttlSec), nil)}
}

// TestDialGuard_ResolveAndPin proves the exported DialGuard surface enforces the
// full gate the proxy relies on: an all-public host returns the pinned first public
// IP; a host resolving to ANY blocked record fails closed with no dialable IP; and a
// metadata hostname is blocked by the hostname blocklist BEFORE resolution. This is
// the proxy's reuse contract — the same validateAndPin web_fetch dials through.
func TestDialGuard_ResolveAndPin(t *testing.T) {
	t.Parallel()
	dg := newTestDialGuard(map[string][]netip.Addr{
		"good.test": {netip.MustParseAddr("1.2.3.4"), netip.MustParseAddr("8.8.8.8")},
		"evil.test": {netip.MustParseAddr("1.2.3.4"), netip.MustParseAddr("127.0.0.1")},
	}, 60)

	t.Run("all-public pins first public IP", func(t *testing.T) {
		t.Parallel()
		ip, reason := dg.ResolveAndPin(context.Background(), "conv-1", "good.test")
		if reason != "" {
			t.Fatalf("good.test blocked with reason %q", reason)
		}
		if ip.String() != "1.2.3.4" {
			t.Fatalf("pinned %v, want first public 1.2.3.4", ip)
		}
	})

	t.Run("mixed record set fails closed", func(t *testing.T) {
		t.Parallel()
		ip, reason := dg.ResolveAndPin(context.Background(), "conv-1", "evil.test")
		if reason == "" {
			t.Fatalf("mixed record set returned pinned %v with no block reason", ip)
		}
		if ip.IsValid() {
			t.Fatalf("blocked host returned valid pinned IP %v", ip)
		}
	})

	t.Run("metadata hostname blocked pre-resolution", func(t *testing.T) {
		t.Parallel()
		ip, reason := dg.ResolveAndPin(context.Background(), "conv-1", "metadata.google.internal")
		if reason == "" {
			t.Fatalf("metadata hostname not blocked (pinned %v)", ip)
		}
		if ip.IsValid() {
			t.Fatalf("blocked hostname returned valid pinned IP %v", ip)
		}
	})
}
