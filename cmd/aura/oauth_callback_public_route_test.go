package main

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

// The Google relay callback is the only route a browser reaches without its session cookie on
// purpose; everything next to them must stay behind the gate.
func TestPublicOAuthCallbackRouteAllowlist(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name   string
		method string
		path   string
		want   bool
	}{
		{"retired MCP OAuth callback refused", http.MethodGet, "/api/governance/mcp/authorization/callback", false},
		{"Google PIM callback admitted", http.MethodGet, "/admin/auth/google/callback", true},
		{"non-GET on the Google callback refused", http.MethodPost, "/admin/auth/google/callback", false},
		{"non-GET on the MCP callback refused", http.MethodPost, "/api/governance/mcp/authorization/callback", false},
		{"Google start stays gated", http.MethodGet, "/api/connect/pim/accounts/work/google/start", false},
		{"other sidecar admin paths are not public", http.MethodGet, "/admin/accounts", false},
		{"trailing slash is a different path", http.MethodGet, "/admin/auth/google/callback/", false},
		{"prefix lookalike refused", http.MethodGet, "/admin/auth/google/callbackX", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := httptest.NewRequest(tt.method, tt.path, nil)
			if got := isPublicOAuthCallbackRoute(r); got != tt.want {
				t.Errorf("isPublicOAuthCallbackRoute(%s %s) = %v, want %v", tt.method, tt.path, got, tt.want)
			}
		})
	}
}
