package main

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/chetto1983/aura/internal/identity"
)

// share_public_route_test.go is the direct predicate test for isPublicShareRoute
// (serve_webui_share.go) — the ONLY package that can see it, since isPublicShareRoute is
// package main. Its pair, internal/agui/share_public_route_test.go, drives the SAME cases
// through the real agui.RequireAuth chain (the file the coverage gate actually measures,
// since ./cmd/aura contributes ZERO coverage at any tag, WR-01/T-37F-66). Keep the two
// files' cases in lockstep — neither alone proves the allowlist is safe.
func TestPublicShareRouteAllowlist(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name   string
		method string
		path   string
		want   bool
	}{
		{"public token data admitted", http.MethodGet, "/s/abc", true},
		{"public token data subpath admitted", http.MethodGet, "/s/abc/data", true},
		{"public token asset subpath admitted", http.MethodGet, "/s/abc/asset/x", true},
		{"non-GET on the public prefix refused (fail-closed on method)", http.MethodPost, "/s/abc", false},
		{"owner-scoped list route refused", http.MethodGet, "/api/shares", false},
		{"D-10 internal data route stays authenticated, not public", http.MethodGet, "/api/shares/abc/data", false},
		{"D-10 internal asset route stays authenticated, not public", http.MethodGet, "/api/shares/abc/asset/x", false},
		{"internal-tier SPA page /shared/ is not the /s/ prefix", http.MethodGet, "/shared/abc", false},
		{"unrelated api route refused", http.MethodGet, "/api/conversations/1/export", false},
		{"SPA root refused", http.MethodGet, "/", false},
		{"login page refused", http.MethodGet, "/login", false},
		{"naive /s prefix match must not admit /sabotage", http.MethodGet, "/sabotage", false},
		{"bare /s with no trailing slash refused", http.MethodGet, "/s", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := httptest.NewRequest(tt.method, tt.path, nil)
			if got := isPublicShareRoute(r); got != tt.want {
				t.Errorf("isPublicShareRoute(%s %s) = %v, want %v", tt.method, tt.path, got, tt.want)
			}
		})
	}
}

// TestSharePublicCapabilityNameValid settles 37F-RESEARCH.md assumption A3 by execution:
// share.public matches identity's capability grammar, not merely by inspection.
func TestSharePublicCapabilityNameValid(t *testing.T) {
	t.Parallel()
	if err := identity.ValidateCapabilityName(sharePublicCapability); err != nil {
		t.Fatalf("ValidateCapabilityName(%q): %v", sharePublicCapability, err)
	}
}

// Moved here from serve_webui_share.go: no production code in cmd/aura names this
// literal — internal/agui's sharePublicCapabilityName is what the runtime gates on.
// The pair still has to agree, and this test is what checks it.
// sharePublicCapability is the D-02 capability_grants name for the public-share tier:
// per-user grantable, off by default, admin-grantable — exactly identity.create's shape
// (serve_webui.go:271-278's identityCreateCapability comment is the precedent this doc
// mirrors), NOT governance.write's (:110-118). It is deliberately NOT a reuse of
// governance.write: that would mean "to share your own chat publicly you must be a full org
// admin who can install MCP servers and RISKY supply-chain skills" — a privilege-escalation
// smell that contradicts D-02's per-user semantics. share.public is identity.create's
// sibling, not governance.write's.
//
// Verified at plan time (37F-RESEARCH.md A5/R-13): the bootstrap identity is granted the
// literal "*" wildcard (serve_bootstrap.go:176-180), so the operator auto-holds
// share.public — intended, they are the admin — while provisioned identities receive only
// the explicit named capabilities their creator grants (serve_onboarding.go:153-165), never
// the wildcard and never share.public unless a holder explicitly grants it. That contrast is
// what makes the per-user, off-by-default semantics real rather than vacuous.
const sharePublicCapability = "share.public"
