package mcp

import (
	"context"
	"fmt"
	"net/netip"
	"net/url"
	"strings"

	"github.com/chetto1983/aura/internal/netguard"
)

// ssrf.go is the MCP SSRF guard (SEC-08 / CWE-918, T-31-SSRF). It classifies with
// internal/netguard, the classifier internal/web uses, and changes ONE policy: under
// dev (enforce=false) the loopback and private classes are PERMITTED so the
// compose-DNS sidecars (arcadedb-mcp:8096, whatsapp, aura-pim-mcp) and the
// off-container loopback recipes (127.0.0.1:8096/8092/8093) plus every httptest
// fixture stay reachable. The scheme allow-list and the cloud-metadata / link-local
// block are UNCONDITIONAL: that string-path barrier is what breaks the
// request-forgery taint flow regardless of profile (no legitimate MCP server lives at
// the IMDS endpoint).

// allowedSchemes is the unconditional scheme allow-list. Anything else is rejected
// with no resolution — a true no-op for legitimate MCP servers (all http/https).
var allowedSchemes = map[string]struct{}{"http": {}, "https": {}}

// resolver is the injectable DNS seam (no real network in tests). It mirrors the
// net.Resolver.LookupNetIP signature so the production wiring passes the stdlib
// resolver (net.DefaultResolver) directly.
type resolver interface {
	LookupNetIP(ctx context.Context, network, host string) ([]netip.Addr, error)
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
	if netguard.IsMetadataHostname(u.Hostname()) {
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
		reason, blocked := netguard.Classify(ip)
		if !blocked {
			sawPublic = true
			if !pinned.IsValid() {
				pinned = ip
			}
			continue
		}
		sawBlocked = true
		if netguard.IsMetadata(ip) {
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
