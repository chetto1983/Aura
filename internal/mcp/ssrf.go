package mcp

import (
	"context"
	"fmt"
	"net/netip"
	"net/url"
	"strings"
)

// ssrf.go is the MCP-local SSRF guard (SEC-08 / CWE-918, T-31-SSRF). It is a
// faithful copy of the already-CodeQL-clean, mutation-tested internal/web SSRF
// classifier, transplanted here (no shared internal/netguard extraction — that is
// QUAL-03 / Phase 32 scope) with ONE policy change: under dev (enforce=false) the
// loopback and private classes are PERMITTED so the compose-DNS sidecars
// (aura-agent-memory-mcp:8080, whatsapp, aura-pim-mcp) and the off-container
// loopback recipes (127.0.0.1:8091/8092/8093) plus every httptest fixture stay
// reachable. The scheme allow-list and the cloud-metadata / link-local block are
// UNCONDITIONAL: that string-path barrier is what breaks the request-forgery taint
// flow regardless of profile (no legitimate MCP server lives at the IMDS endpoint).

// CIDR ranges the netip Is* predicates do NOT cover: CGNAT (RFC 6598) and the
// "this network" 0.0.0.0/8 are public-looking but route internally; the v6 prefix
// covers the cloud-metadata IPv6 region. Parsed once via MustParsePrefix so a typo
// panics at startup, not at request time.
var (
	mcpCgnatPrefix   = netip.MustParsePrefix("100.64.0.0/10")
	mcpThisNetPrefix = netip.MustParsePrefix("0.0.0.0/8")
	mcpMetadataV6Pfx = netip.MustParsePrefix("fd00:ec2::/32")
)

// metadataHostBlocklist is the exact-match set of cloud-metadata / cluster-internal
// hostnames blocked BEFORE resolution. Keys are lowercase; lookups lower-case the
// candidate so the block is case-insensitive. Copied from internal/web.
var metadataHostBlocklist = map[string]struct{}{
	"metadata.google.internal": {},
	"metadata.amazonaws.com":   {},
	"metadata.azure.com":       {},
	"kubernetes.default.svc":   {},
	"host.docker.internal":     {},
}

// allowedSchemes is the unconditional scheme allow-list. Anything else is rejected
// with no resolution — a true no-op for legitimate MCP servers (all http/https).
var allowedSchemes = map[string]struct{}{"http": {}, "https": {}}

// classify maps a resolved IP to a block reason. It is the mutation-gate target:
// every branch is a distinct SSRF class with its own table row in TestClassify.
// Unmap() runs FIRST so an IPv4-mapped IPv6 form (::ffff:169.254.169.254) collapses
// to its v4 shape and hits the v4 predicates — without it the metadata IP slips past
// as an opaque v6 address. A non-empty reason means blocked.
func classify(ip netip.Addr) (reason string, blocked bool) {
	if !ip.IsValid() {
		return "invalid_target", true
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
	case mcpCgnatPrefix.Contains(ip):
		return "cgnat", true
	case mcpThisNetPrefix.Contains(ip):
		return "this_network", true
	case mcpMetadataV6Pfx.Contains(ip):
		return "link_local", true
	}
	return "", false
}

// resolver is the injectable DNS seam (no real network in tests). It mirrors the
// net.Resolver.LookupNetIP signature so the production wiring passes the stdlib
// resolver (net.DefaultResolver) directly.
type resolver interface {
	LookupNetIP(ctx context.Context, network, host string) ([]netip.Addr, error)
}

// metadataReason reports whether a block reason belongs to the cloud-metadata /
// link-local class that is denied in EVERY profile (the unconditional barrier).
func metadataReason(reason string) bool { return reason == "link_local" }

// guardEndpoint validates a raw MCP endpoint URL and returns the parsed *url.URL or
// an error. Five steps, the first three UNCONDITIONAL (they break the CodeQL taint
// flow on every policy branch):
//
//  1. parse + non-empty host;
//  2. scheme ∈ allowedSchemes;
//  3. host ∉ metadataHostBlocklist;
//  4. under enforce, an allow-listed host short-circuits (configured sidecar);
//     otherwise resolve and classify EVERY record — under enforce reject on ANY
//     blocked record (fail closed, never cherry-pick a public IP from a mixed set),
//     under dev reject ONLY the metadata/link-local class (loopback + private pass);
//  5. return the validated URL.
func guardEndpoint(ctx context.Context, raw string, enforce bool, allowHosts map[string]struct{}, res resolver) (*url.URL, error) {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil {
		return nil, fmt.Errorf("ssrf: parse url: %w", err)
	}
	if u.Host == "" {
		return nil, fmt.Errorf("ssrf: empty host in %q", raw)
	}
	if _, ok := allowedSchemes[strings.ToLower(u.Scheme)]; !ok {
		return nil, fmt.Errorf("ssrf: unsupported scheme %q", u.Scheme)
	}
	host := strings.ToLower(u.Hostname())
	if _, bad := metadataHostBlocklist[host]; bad {
		return nil, fmt.Errorf("ssrf: blocked metadata host %q", u.Hostname())
	}
	if enforce {
		if _, ok := allowHosts[host]; ok {
			return u, nil
		}
	}
	ips, err := res.LookupNetIP(ctx, "ip", u.Hostname())
	if err != nil {
		return nil, fmt.Errorf("ssrf: resolve %q: %w", u.Hostname(), err)
	}
	if len(ips) == 0 {
		return nil, fmt.Errorf("ssrf: resolve %q: no addresses", u.Hostname())
	}
	for _, ip := range ips {
		reason, blocked := classify(ip)
		if !blocked {
			continue
		}
		if enforce || metadataReason(reason) {
			return nil, fmt.Errorf("ssrf: blocked target %s (%s)", ip, reason)
		}
	}
	return u, nil
}
