package config

import (
	"strings"
	"testing"
)

// TestGuardWebBind locks the WEB-02/D-05 boot policy as a pure-function matrix:
// loopback (v4/v6/named) always boots; wildcard binds (0.0.0.0/::/[::]) are
// non-loopback and gated; a non-loopback bind boots iff Authula auth is configured
// or trust-proxy is enabled; a fail-case error message names the Authula/proxy knobs.
func TestGuardWebBind(t *testing.T) {
	tests := []struct {
		name      string
		bind      string
		auth      bool
		trust     bool
		wantError bool
	}{
		// Loopback always boots with no credential (dev parity).
		{name: "ipv4 loopback", bind: "127.0.0.1:9080"},
		{name: "ipv4 loopback range", bind: "127.0.0.5:9080"},
		{name: "named loopback", bind: "localhost:9080"},
		{name: "ipv6 loopback", bind: "[::1]:9080"},
		{name: "bare ipv4 loopback no port", bind: "127.0.0.1"},

		// Wildcard binds expose all interfaces: non-loopback, therefore gated.
		{name: "wildcard v4 no credential", bind: "0.0.0.0:9080", wantError: true},
		{name: "wildcard v6 bare", bind: "::", wantError: true},
		{name: "wildcard v6 bracketed", bind: "[::]:9080", wantError: true},

		// Non-loopback x {Authula configured, trust-proxy, neither}.
		{name: "non-loopback Authula unlocks", bind: "192.168.1.10:9080", auth: true},
		{name: "non-loopback trust-proxy unlocks", bind: "192.168.1.10:9080", trust: true},
		{name: "non-loopback both unlock", bind: "192.168.1.10:9080", auth: true, trust: true},
		{name: "non-loopback neither", bind: "192.168.1.10:9080", wantError: true},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			err := GuardWebBind(tc.bind, tc.auth, tc.trust)
			if tc.wantError {
				if err == nil {
					t.Fatalf("GuardWebBind(%q, %v, %v) = nil, want error", tc.bind, tc.auth, tc.trust)
				}
				msg := err.Error()
				if !strings.Contains(msg, "AURA_AUTHULA_SECRET") {
					t.Errorf("error message must name AURA_AUTHULA_SECRET, got %q", msg)
				}
				if !strings.Contains(msg, "AURA_WEB_TRUST_PROXY") {
					t.Errorf("error message must name AURA_WEB_TRUST_PROXY, got %q", msg)
				}
				return
			}
			if err != nil {
				t.Fatalf("GuardWebBind(%q, %v, %v) = %v, want nil", tc.bind, tc.auth, tc.trust, err)
			}
		})
	}
}

// TestWebAuthConfigLoad locks that the two knobs load from env via loadBase
// (LoadDB returns them as-is), mirroring the env-coverage style in
// config_channels_test.go / config_serve_test.go.
func TestWebAuthConfigLoad(t *testing.T) {
	clearPostgresEnv(t)

	// Defaults: secret empty, trust-proxy false (neither boot-fatal).
	cfg := LoadDB()
	if cfg.WebAuthSecret != "" {
		t.Errorf("WebAuthSecret default = %q, want empty", cfg.WebAuthSecret)
	}
	if cfg.WebTrustProxy {
		t.Error("WebTrustProxy default = true, want false")
	}

	t.Setenv("AURA_WEB_AUTH_SECRET", "operator-pass")
	t.Setenv("AURA_WEB_TRUST_PROXY", "true")

	cfg = LoadDB()
	if cfg.WebAuthSecret != "operator-pass" {
		t.Errorf("WebAuthSecret override = %q, want operator-pass", cfg.WebAuthSecret)
	}
	if !cfg.WebTrustProxy {
		t.Error("WebTrustProxy override = false, want true")
	}
}
