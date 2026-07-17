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
