package mcp

import (
	"errors"
	"net"
	"net/url"
	"strings"
)

var (
	errEmptyEgressHost         = errors.New("empty host")
	errUnsupportedEgressScheme = errors.New("unsupported scheme")
)

const (
	sourceRecipeCalendar = "recipe:calendar"
	sourceRecipeWhatsApp = "recipe:whatsapp"
)

// EgressPolicy is the resolved network posture for one managed MCP server. The
// private authority set is intentionally opaque: callers construct policy from
// typed managed-server metadata instead of supplying a hostname bypass.
type EgressPolicy struct {
	enforcePrivate            bool
	allowedPrivateAuthorities map[string]struct{}
}

// Enforced reports whether private and loopback destinations are blocked unless
// the exact authority belongs to a trusted built-in HTTP recipe.
func (p EgressPolicy) Enforced() bool {
	return p.enforcePrivate
}

// EgressPolicyForManagedServer resolves an explicit enforcement posture for one
// server. Only Aura's built-in HTTP recipe sources can authorize their configured
// private authority; remote/custom sources never receive an implicit allowance.
func EgressPolicyForManagedServer(enforce bool, server ManagedServer) EgressPolicy {
	policy := EgressPolicy{enforcePrivate: enforce}
	if !enforce || !isTrustedBuiltInHTTPRecipe(server) {
		return policy
	}
	u, err := url.Parse(strings.TrimSpace(server.URL))
	if err != nil {
		return policy
	}
	authority, err := canonicalURLAuthority(u)
	if err != nil {
		return policy
	}
	policy.allowedPrivateAuthorities = map[string]struct{}{authority: {}}
	return policy
}

// RuntimeEgressPolicy binds the strict runtime-profile result to the legacy
// operator hardening knob. A strict profile always wins; the environment can
// tighten a lenient profile but can never loosen a strict one.
func RuntimeEgressPolicy(strictProfile bool, server ManagedServer) EgressPolicy {
	return EgressPolicyForManagedServer(strictProfile || ssrfEnforceFromEnv(), server)
}

func isTrustedBuiltInHTTPRecipe(server ManagedServer) bool {
	serverType, trust, err := Classify(server)
	if err != nil || serverType != ServerTypeStreamableHTTP || trust != TrustTrustedRecipe {
		return false
	}
	switch strings.TrimSpace(server.Source) {
	case SourceRecipeMemory, sourceRecipeCalendar, sourceRecipeWhatsApp:
		return true
	default:
		return false
	}
}

func (p EgressPolicy) allowsPrivateAuthority(authority string) bool {
	_, ok := p.allowedPrivateAuthorities[strings.ToLower(authority)]
	return ok
}

func canonicalURLAuthority(u *url.URL) (string, error) {
	host := normalizeEgressHost(u.Hostname())
	if host == "" {
		return "", &url.Error{Op: "parse", URL: u.String(), Err: errEmptyEgressHost}
	}
	port := u.Port()
	if port == "" {
		switch strings.ToLower(u.Scheme) {
		case "http":
			port = "80"
		case "https":
			port = "443"
		default:
			return "", &url.Error{Op: "parse", URL: u.String(), Err: errUnsupportedEgressScheme}
		}
	}
	return strings.ToLower(net.JoinHostPort(host, port)), nil
}

func canonicalDialAuthority(host, port string) string {
	return strings.ToLower(net.JoinHostPort(normalizeEgressHost(host), port))
}

func normalizeEgressHost(host string) string {
	return strings.TrimSuffix(strings.ToLower(strings.TrimSpace(host)), ".")
}
