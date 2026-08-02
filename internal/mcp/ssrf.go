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
// (arcadedb-mcp:8096, whatsapp, aura-pim-mcp) and the off-container
// loopback recipes (127.0.0.1:8096/8092/8093) plus every httptest fixture stay
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

// metadataAddress reports cloud-metadata/link-local destinations denied in every
// profile. The explicit IPv6 prefix check runs before the broader private class
// can hide fd00:ec2::/32 behind netip.Addr.IsPrivate.
func metadataAddress(ip netip.Addr) bool {
	if !ip.IsValid() {
		return false
	}
	ip = ip.Unmap()
	return ip.IsLinkLocalUnicast() || ip.IsLinkLocalMulticast() || mcpMetadataV6Pfx.Contains(ip)
}

// guardEndpoint validates a raw MCP endpoint URL and returns the parsed *url.URL or
// an error. Five steps, the first three UNCONDITIONAL (they break the CodeQL taint
// flow on every policy branch):
//
//  1. parse + non-empty host;
//  2. scheme ∈ allowedSchemes;
//  3. host ∉ metadataHostBlocklist;
//  4. resolve and classify EVERY record; strict mode rejects private targets except
//     the exact authority of a trusted built-in recipe, and rejects mixed DNS even
//     for that recipe; dev rejects metadata/link-local while allowing local sidecars;
//  5. return the validated URL.
func guardEndpoint(ctx context.Context, raw string, enforce bool, allowHosts map[string]struct{}, res resolver) (*url.URL, error) {
	policy := EgressPolicy{enforcePrivate: enforce}
	if enforce && len(allowHosts) > 0 {
		if u, err := url.Parse(strings.TrimSpace(raw)); err == nil {
			if _, ok := allowHosts[normalizeEgressHost(u.Hostname())]; ok {
				if authority, authorityErr := canonicalURLAuthority(u); authorityErr == nil {
					policy.allowedPrivateAuthorities = map[string]struct{}{authority: {}}
				}
			}
		}
	}
	return guardEndpointWithPolicy(ctx, raw, policy, res)
}

// guardEndpointWithPolicy validates one initial or redirect URL against the fully
// resolved runtime/source policy. It resolves every candidate even for an allowed
// recipe so metadata and mixed-address DNS cannot hide behind the allow entry.
func guardEndpointWithPolicy(ctx context.Context, raw string, policy EgressPolicy, res resolver) (*url.URL, error) {
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
	authority, err := canonicalURLAuthority(u)
	if err != nil {
		return nil, fmt.Errorf("ssrf: authority: %w", err)
	}
	ips, err := res.LookupNetIP(ctx, "ip", u.Hostname())
	if err != nil {
		return nil, fmt.Errorf("ssrf: resolve %q: %w", u.Hostname(), err)
	}
	if len(ips) == 0 {
		return nil, fmt.Errorf("ssrf: resolve %q: no addresses", u.Hostname())
	}
	if _, err := validateResolvedTarget(policy, authority, ips); err != nil {
		return nil, err
	}
	return u, nil
}

// validateResolvedTarget applies the same classification to initial lookup and
// pinned dial. It rejects mixed public/private answers even for a trusted recipe.
func validateResolvedTarget(policy EgressPolicy, authority string, ips []netip.Addr) (netip.Addr, error) {
	allowPrivate := policy.allowsPrivateAuthority(authority)
	var pinned netip.Addr
	var sawPublic, sawBlocked bool
	for _, ip := range ips {
		ip = ip.Unmap()
		reason, blocked := classify(ip)
		if !blocked {
			sawPublic = true
			if !pinned.IsValid() {
				pinned = ip
			}
			continue
		}
		sawBlocked = true
		if metadataAddress(ip) {
			return netip.Addr{}, fmt.Errorf("ssrf: blocked target %s (%s)", ip, reason)
		}
		if policy.enforcePrivate && (!allowPrivate || !recipePrivateReason(reason)) {
			return netip.Addr{}, fmt.Errorf("ssrf: blocked target %s (%s)", ip, reason)
		}
		if !pinned.IsValid() {
			pinned = ip
		}
	}
	if policy.enforcePrivate && sawPublic && sawBlocked {
		return netip.Addr{}, fmt.Errorf("ssrf: mixed public/private DNS answer for %s", authority)
	}
	if !pinned.IsValid() {
		return netip.Addr{}, fmt.Errorf("ssrf: no usable address for %s", authority)
	}
	return pinned, nil
}

func recipePrivateReason(reason string) bool {
	return reason == "loopback" || reason == "private"
}
