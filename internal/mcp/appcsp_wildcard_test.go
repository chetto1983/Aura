package mcp

import (
	"strings"
	"testing"
)

// TestConnectWildcard: `*` is honoured for connect-src ONLY as the sole entry.
//
// It is safe now and was not before: an artifact document is served with the CSP
// `sandbox` directive, so it holds an opaque origin and a call back to Aura is
// cross-origin without cookies. The wildcard therefore widens reach to external
// APIs without widening reach to Aura.
//
// Sole-entry is the rule because a curated allowlist must not silently become
// "everything" if a stray `*` lands in it. AllowConnectWildcard is the second
// rule: only the artifact renderer sets it. An MCP view's connectDomains are
// declared by a MOUNTED SERVER, and in the port-split deployment a view shares
// the cockpit's host — so a server that declared `*` would escape WithoutHost.
func TestConnectWildcard(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name    string
		domains []string
		want    string
	}{
		{"sole wildcard opens connect", []string{"*"}, "connect-src *;"},
		{"wildcard with padding still sole", []string{" * "}, "connect-src *;"},
		{"wildcard among others is ignored", []string{"*", "https://api.test"}, "connect-src https://api.test;"},
		{"wildcard never widens resources", []string{"*"}, "img-src data: blob:;"},
		{"no wildcard keeps the floor", nil, "connect-src 'none';"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := ViewPolicy{ConnectDomains: tc.domains, ResourceDomains: tc.domains, AllowConnectWildcard: true}.ContentSecurityPolicy() + ";"
			if !strings.Contains(got, tc.want) {
				t.Fatalf("policy %q does not contain %q", got, tc.want)
			}
		})
	}
}

// TestWildcardRefusedWithoutOptIn: the same `*` an artifact may use is refused for
// an MCP view, whose domains come from a mounted server rather than the operator.
func TestWildcardRefusedWithoutOptIn(t *testing.T) {
	t.Parallel()
	got := ViewPolicy{ConnectDomains: []string{"*"}}.ContentSecurityPolicy()
	if !strings.Contains(got, "connect-src 'none'") {
		t.Fatalf("view policy %q honoured a wildcard it was never opted into", got)
	}
}

// TestWildcardNeverEscapesItsDirective: `*` is the ONE non-origin token allowed, and
// only for connect-src. Anything else that is not an origin stays rejected.
func TestWildcardNeverEscapesItsDirective(t *testing.T) {
	t.Parallel()
	for _, bad := range []string{"* ; script-src *", "https:", "*.test", "http://api.test", "'unsafe-inline'"} {
		got := ViewPolicy{ConnectDomains: []string{bad}, AllowConnectWildcard: true}.ContentSecurityPolicy()
		if !strings.Contains(got, "connect-src 'none'") {
			t.Fatalf("input %q produced %q, want connect-src 'none'", bad, got)
		}
	}
}
