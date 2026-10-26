//go:build !web_integration

package web

import (
	"context"
	"net/netip"
	"strings"
	"testing"
)

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

// TestValidateAndPin_CarriedIPv4 proves the guard judges an IPv4 address carried in
// IPv6 as that address (netguard owns the table): NAT64 and 6to4 forms of the metadata
// IP are refused, and a DNS64 answer for a public site is pinned as answered.
func TestValidateAndPin_CarriedIPv4(t *testing.T) {
	t.Parallel()
	for host, want := range map[string]string{
		"nat64-metadata.test": "",
		"6to4-loopback.test":  "",
		"dns64-public.test":   "64:ff9b::808:808",
	} {
		t.Run(host, func(t *testing.T) {
			t.Parallel()
			g := newGuard(stubResolver{answers: map[string][]netip.Addr{
				"nat64-metadata.test": {netip.MustParseAddr("64:ff9b::a9fe:a9fe")},
				"6to4-loopback.test":  {netip.MustParseAddr("2002:7f00:1::1")},
				"dns64-public.test":   {netip.MustParseAddr("64:ff9b::808:808")},
			}}, newDNSPin(60), nil)
			ip, reason := g.validateAndPin(context.Background(), "conv-1", host)
			if want == "" {
				if reason != ReasonPrivateOrMetadata || ip.IsValid() {
					t.Fatalf("validateAndPin(%s) = (%v, %q), want (invalid, %q)", host, ip, reason, ReasonPrivateOrMetadata)
				}
				return
			}
			if reason != "" || ip.String() != want {
				t.Fatalf("validateAndPin(%s) = (%v, %q), want (%s, \"\")", host, ip, reason, want)
			}
		})
	}
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
// blocked BEFORE resolution, case-insensitively and with or without the root dot
// (T-07-10).
func TestHostnameBlocklist(t *testing.T) {
	t.Parallel()
	hosts := []string{
		"metadata.google.internal",
		"METADATA.AMAZONAWS.COM",
		"metadata.azure.com",
		"kubernetes.default.svc",
		"Host.Docker.Internal",
		"metadata.google.internal.",
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
