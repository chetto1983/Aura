//go:build !web_integration

package web

import (
	"context"
	"net/netip"
	"strings"
	"testing"
)

// TestBlocked_Classification is the SC#3 table: every blocklist class (loopback,
// private, link-local/metadata, ULA, CGNAT, this-network/unspecified, multicast)
// MUST classify blocked with an explicit reason, and the public sentinels MUST
// pass. The two IPv4-mapped-IPv6 rows (::ffff:169.254.169.254 / ::ffff:127.0.0.1)
// prove Unmap() runs BEFORE the switch — without it they would slip past the v4
// predicates as opaque v6 addresses (Pitfall 2 / T-07-11).
func TestBlocked_Classification(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name    string
		ip      string
		blocked bool
		reason  string
	}{
		{"loopback v4", "127.0.0.1", true, "loopback"},
		{"loopback v6", "::1", true, "loopback"},
		{"private 10/8", "10.0.0.5", true, "private"},
		{"private 192.168/16", "192.168.1.1", true, "private"},
		{"private 172.16/12", "172.16.0.1", true, "private"},
		{"link-local metadata v4", "169.254.169.254", true, "link_local"},
		{"link-local v6", "fe80::1", true, "link_local"},
		{"ula v6", "fc00::1", true, "private"},
		{"cgnat", "100.64.0.1", true, "cgnat"},
		{"unspecified v4", "0.0.0.0", true, "unspecified"},
		{"this-network 0/8 nonzero", "0.1.2.3", true, "this_network"},
		{"link-local multicast v4", "224.0.0.1", true, "link_local"},
		{"global multicast v4", "225.1.2.3", true, "multicast"},
		// fd00:ec2::/32 sits inside ULA fd00::/8, so IsPrivate() classifies it
		// "private" BEFORE the dedicated metadataV6Pfx case — that case is therefore
		// unreachable (a known dead branch flagged at the Gate-3 mutation review).
		{"v6 ula incl. metadata range", "fd00:ec2::1", true, "private"},
		// IPv4-mapped IPv6 — proves Unmap() before the switch (each row hits a
		// DISTINCT v4-only predicate so dropping Unmap leaves the address unblocked).
		{"mapped metadata", "::ffff:169.254.169.254", true, "link_local"},
		{"mapped loopback", "::ffff:127.0.0.1", true, "loopback"},
		{"mapped private 10/8", "::ffff:10.0.0.1", true, "private"},
		{"mapped cgnat", "::ffff:100.64.0.1", true, "cgnat"},
		{"mapped this-network", "::ffff:0.1.2.3", true, "this_network"},
		// Public — must NOT be blocked.
		{"public v4 cloudflare", "1.1.1.1", false, ""},
		{"public v4 google", "8.8.8.8", false, ""},
		{"public v6", "2606:4700:4700::1111", false, ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			ip := netip.MustParseAddr(tc.ip)
			reason, blocked := classify(ip)
			if blocked != tc.blocked {
				t.Fatalf("classify(%s) blocked=%v, want %v (reason=%q)", tc.ip, blocked, tc.blocked, reason)
			}
			if tc.blocked && reason != tc.reason {
				t.Fatalf("classify(%s) reason=%q, want %q", tc.ip, reason, tc.reason)
			}
		})
	}
}

// TestBlocked_InvalidTarget proves an invalid (zero) Addr is blocked with the
// invalid_target reason — the fail-closed default when a resolver hands back a
// malformed record.
func TestBlocked_InvalidTarget(t *testing.T) {
	t.Parallel()
	reason, blocked := classify(netip.Addr{})
	if !blocked || reason != "invalid_target" {
		t.Fatalf("classify(invalid) = (%q,%v), want (invalid_target,true)", reason, blocked)
	}
}

// stubResolver is a deterministic resolver injected so validateAndPin never
// touches real DNS. answers maps a host to its record set; err overrides.
type stubResolver struct {
	answers map[string][]netip.Addr
	err     error
}

func (s stubResolver) LookupNetIP(_ context.Context, _, host string) ([]netip.Addr, error) {
	if s.err != nil {
		return nil, s.err
	}
	return s.answers[host], nil
}

// TestValidateAndPin_MixedRecords is the D-24 fail-closed contract: a record set
// containing ANY blocked IP blocks the WHOLE host; an all-public set returns a
// pinned public IP. Never cherry-pick the public IP out of a mixed set.
func TestValidateAndPin_MixedRecords(t *testing.T) {
	t.Parallel()
	mustAddr := func(s string) netip.Addr { return netip.MustParseAddr(s) }

	t.Run("mixed public+loopback is blocked", func(t *testing.T) {
		t.Parallel()
		g := newGuard(stubResolver{answers: map[string][]netip.Addr{
			"evil.test": {mustAddr("1.2.3.4"), mustAddr("127.0.0.1")},
		}}, newDNSPin(60), nil)
		ip, reason := g.validateAndPin(context.Background(), "conv-1", "evil.test")
		if reason == "" {
			t.Fatalf("mixed record set returned pinned %v with no block reason — cherry-picked", ip)
		}
		if ip.IsValid() {
			t.Fatalf("blocked host returned a valid pinned IP %v", ip)
		}
	})

	t.Run("all-public returns pinned public IP", func(t *testing.T) {
		t.Parallel()
		g := newGuard(stubResolver{answers: map[string][]netip.Addr{
			"good.test": {mustAddr("1.2.3.4"), mustAddr("8.8.8.8")},
		}}, newDNSPin(60), nil)
		ip, reason := g.validateAndPin(context.Background(), "conv-1", "good.test")
		if reason != "" {
			t.Fatalf("all-public set blocked with reason %q", reason)
		}
		if ip.String() != "1.2.3.4" {
			t.Fatalf("pinned %v, want first public 1.2.3.4", ip)
		}
	})
}

// countingResolver wraps a record set and counts LookupNetIP calls so a test can
// prove the pin short-circuit avoided a second resolve (anti-rebinding reuse).
type countingResolver struct {
	answers map[string][]netip.Addr
	calls   int
}

func (c *countingResolver) LookupNetIP(_ context.Context, _, host string) ([]netip.Addr, error) {
	c.calls++
	return c.answers[host], nil
}

// TestValidateAndPin_PinReuse proves validateAndPin WRITES a pin on the first
// resolve and REUSES it on the second call within TTL — the second call must NOT
// hit the resolver and must return the SAME pinned IP. This is the D-25 anti-
// rebinding contract exercised through the gate (not just the bare dnsPin), and it
// kills the "drop the pin write" / "drop the pin-reuse return" mutants on ssrf.go.
func TestValidateAndPin_PinReuse(t *testing.T) {
	t.Parallel()
	res := &countingResolver{answers: map[string][]netip.Addr{
		"good.test": {netip.MustParseAddr("1.2.3.4")},
	}}
	g := newGuard(res, newDNSPin(60), nil)

	first, reason := g.validateAndPin(context.Background(), "conv-1", "good.test")
	if reason != "" || first.String() != "1.2.3.4" {
		t.Fatalf("first call: got (%v,%q), want (1.2.3.4,\"\")", first, reason)
	}
	if res.calls != 1 {
		t.Fatalf("first call must resolve exactly once, got %d", res.calls)
	}

	second, reason := g.validateAndPin(context.Background(), "conv-1", "good.test")
	if reason != "" {
		t.Fatalf("second call blocked with reason %q", reason)
	}
	if second != first {
		t.Fatalf("pin not reused: first=%v second=%v", first, second)
	}
	if res.calls != 1 {
		t.Fatalf("second call must REUSE the pin (no resolve), but resolver was called %d times total", res.calls)
	}
}

// TestValidateAndPin_ResolveFailsClosed proves a resolver error AND an empty
// record set both fail closed with invalid_target and a zero Addr (a rebind/NXDOMAIN
// must never produce a dialable target). Kills the "len(ips)==0 → false" and the
// "drop the invalid_target return" mutants.
func TestValidateAndPin_ResolveFailsClosed(t *testing.T) {
	t.Parallel()

	t.Run("resolver error", func(t *testing.T) {
		t.Parallel()
		g := newGuard(stubResolver{err: context.DeadlineExceeded}, newDNSPin(60), nil)
		ip, reason := g.validateAndPin(context.Background(), "c", "x.test")
		if reason != ReasonInvalidTarget || ip.IsValid() {
			t.Fatalf("resolver error: got (%v,%q), want (invalid,invalid_target)", ip, reason)
		}
	})

	t.Run("empty record set", func(t *testing.T) {
		t.Parallel()
		g := newGuard(stubResolver{answers: map[string][]netip.Addr{"x.test": {}}}, newDNSPin(60), nil)
		ip, reason := g.validateAndPin(context.Background(), "c", "x.test")
		if reason != ReasonInvalidTarget || ip.IsValid() {
			t.Fatalf("empty records: got (%v,%q), want (invalid,invalid_target)", ip, reason)
		}
	})
}

// TestHostnameBlocklist proves the five cloud-metadata / internal hostnames are
// blocked by the hostname set BEFORE resolution, case-insensitively (T-07-10).
func TestHostnameBlocklist(t *testing.T) {
	t.Parallel()
	hosts := []string{
		"metadata.google.internal",
		"METADATA.AMAZONAWS.COM",
		"metadata.azure.com",
		"kubernetes.default.svc",
		"Host.Docker.Internal",
	}
	// A resolver that would return a public IP — proving the block is by hostname,
	// not by resolution.
	res := stubResolver{answers: map[string][]netip.Addr{}}
	for _, h := range hosts {
		res.answers[strings.ToLower(h)] = []netip.Addr{netip.MustParseAddr("1.2.3.4")}
	}
	g := newGuard(res, newDNSPin(60), nil)
	for _, h := range hosts {
		ip, reason := g.validateAndPin(context.Background(), "conv-1", h)
		if reason == "" {
			t.Fatalf("hostname %q not blocked (pinned %v)", h, ip)
		}
		if ip.IsValid() {
			t.Fatalf("blocked hostname %q returned valid pinned IP %v", h, ip)
		}
	}
}

// TestError_NonLeaky is the D-26/27/28 leak-scan: a sanitized WebError serialized
// to JSON contains only error/reason/message(/status_code) and NEVER a resolved
// IP, CIDR, internal hostname, header, body, or redirect-chain substring — even
// when it was mapped from a rich internal error that DID carry that detail.
func TestError_NonLeaky(t *testing.T) {
	t.Parallel()
	// Internal error carries the sensitive detail (logs/tests only).
	internal := &internalError{
		code:    CodeBlockedURL,
		reason:  ReasonPrivateOrMetadata,
		message: "blocked",
		// Sensitive fields a naive impl might leak:
		resolvedIP:   "169.254.169.254",
		host:         "metadata.google.internal",
		redirectFrom: "http://public.test/ -> http://169.254.169.254/",
	}
	we := sanitize(internal)
	js, err := we.JSON()
	if err != nil {
		t.Fatalf("JSON: %v", err)
	}
	got := string(js)

	for _, leak := range []string{
		"169.254.169.254",
		"metadata.google.internal",
		"redirect",
		"->",
		"/8", "/10", "/12", "/16", // CIDR fragments
	} {
		if strings.Contains(got, leak) {
			t.Fatalf("sanitized WebError leaked %q: %s", leak, got)
		}
	}
	// Positive: the safe keys are present.
	for _, want := range []string{`"error"`, `"reason"`, `"blocked_url"`, `"private_or_metadata_target"`} {
		if !strings.Contains(got, want) {
			t.Fatalf("sanitized WebError missing %q: %s", want, got)
		}
	}
}
