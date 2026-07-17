package agui

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// share_public_route_test.go is the coverage-measured pair of
// cmd/aura/share_public_route_test.go (plan 37F-12). isPublicShareRoute itself lives in
// cmd/aura (package main) and cannot be imported here — and the coverage gate measures
// ./internal/... only, so cmd/aura contributes ZERO coverage at any tag (WR-01/T-37F-66).
// This file drives the SAME allowlist cases through the REAL agui.RequireAuth chain with an
// EQUIVALENT PublicRoute closure (GET-only, "/s/"-prefixed, fail-closed default): the
// property under test is "does RequireAuth actually let this request reach next", not merely
// "does a predicate function return true". Keep this file's table in lockstep with its
// cmd/aura pair — neither alone proves the allowlist is safe.
func isPublicShareRouteEquivalent(r *http.Request) bool {
	if r.Method != http.MethodGet {
		return false
	}
	return strings.HasPrefix(r.URL.Path, "/s/")
}

func TestPublicShareRouteAllowlistThroughRequireAuth(t *testing.T) {
	t.Parallel()
	deps := testDeps("operator-secret")
	deps.PublicRoute = isPublicShareRouteEquivalent

	// /login is deliberately excluded from this table: it is independently public via
	// isPublicPath (unrelated to PublicRoute), so including it here would pass for the wrong
	// reason. /settings stands in as an equivalent authenticated-SPA-route negative case.
	cases := []struct {
		name   string
		method string
		path   string
		public bool // true => reachable WITHOUT a cookie
	}{
		{"public token data admitted", http.MethodGet, "/s/abc", true},
		{"public token data subpath admitted", http.MethodGet, "/s/abc/data", true},
		{"public token asset subpath admitted", http.MethodGet, "/s/abc/asset/x", true},
		{"non-GET on the public prefix refused (fail-closed on method)", http.MethodPost, "/s/abc", false},
		{"owner-scoped list route stays authenticated", http.MethodGet, "/api/shares", false},
		{"D-10 internal data route stays authenticated", http.MethodGet, "/api/shares/abc/data", false},
		{"D-10 internal asset route stays authenticated", http.MethodGet, "/api/shares/abc/asset/x", false},
		{"internal-tier SPA page /shared/ is not the /s/ prefix", http.MethodGet, "/shared/abc", false},
		{"unrelated api route stays authenticated", http.MethodGet, "/api/conversations/1/export", false},
		{"SPA root stays authenticated", http.MethodGet, "/", false},
		{"non-share SPA route stays authenticated", http.MethodGet, "/settings", false},
		{"naive /s prefix match must not admit /sabotage", http.MethodGet, "/sabotage", false},
		{"bare /s with no trailing slash refused", http.MethodGet, "/s", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			next, hit := nextRecorder()
			req := httptest.NewRequest(tc.method, tc.path, nil)
			rec := httptest.NewRecorder()
			RequireAuth(next, deps).ServeHTTP(rec, req)
			if tc.public {
				if !*hit || rec.Code != http.StatusOK {
					t.Fatalf("public path %s %s: hit=%v code=%d, want hit=true code=200", tc.method, tc.path, *hit, rec.Code)
				}
				return
			}
			if *hit {
				t.Fatalf("gated path %s %s reached next without a cookie", tc.method, tc.path)
			}
			if rec.Code != http.StatusUnauthorized && rec.Code != http.StatusFound {
				t.Fatalf("gated path %s %s got status %d, want 401 or 302", tc.method, tc.path, rec.Code)
			}
		})
	}
}
