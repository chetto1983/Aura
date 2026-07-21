package mcp

import (
	"strings"
	"testing"
)

// TestClassifyManagedServer is the canonical table test for Classify (D-01): the
// single source of truth for transport-type + trust-class resolution. Every row
// here maps directly to a <behavior> bullet in 38-01-PLAN.md. Named distinctly
// from ssrf_test.go's unrelated TestClassify (which tests the lowercase,
// unexported IP-classification function of the same generic name).
func TestClassifyManagedServer(t *testing.T) {
	cases := []struct {
		name      string
		in        ManagedServer
		wantType  string
		wantTrust string
		wantErr   string // substring; empty means "no error expected"
	}{
		// --- inference (no explicit type) ---
		{
			name:    "mixed url and command with no explicit type is rejected",
			in:      ManagedServer{URL: "http://x.test", Command: "cmd"},
			wantErr: "both url and command",
		},
		{
			name:      "command only infers stdio",
			in:        ManagedServer{Command: "uvx"},
			wantType:  ServerTypeStdio,
			wantTrust: TrustBlocked,
		},
		{
			name:      "url only infers streamable_http",
			in:        ManagedServer{URL: "https://x.test"},
			wantType:  ServerTypeStreamableHTTP,
			wantTrust: TrustBlocked,
		},
		{
			name:      "bare server infers stdio",
			in:        ManagedServer{},
			wantType:  ServerTypeStdio,
			wantTrust: TrustBlocked,
		},

		// --- F-013: remote empty/blank/whitespace/unknown trust must block, never auto-promote ---
		{
			name:      "remote empty trust blocks, never remote_http",
			in:        ManagedServer{URL: "http://x.test"},
			wantType:  ServerTypeStreamableHTTP,
			wantTrust: TrustBlocked,
		},
		{
			name:      "remote blank trust blocks",
			in:        ManagedServer{URL: "http://x.test", Trust: ManagedTrust{Class: "   "}},
			wantType:  ServerTypeStreamableHTTP,
			wantTrust: TrustBlocked,
		},
		{
			name:      "remote whitespace-only trust blocks",
			in:        ManagedServer{URL: "http://x.test", Trust: ManagedTrust{Class: "\t\n "}},
			wantType:  ServerTypeStreamableHTTP,
			wantTrust: TrustBlocked,
		},
		{
			name:      "remote unknown trust class blocks",
			in:        ManagedServer{URL: "http://x.test", Trust: ManagedTrust{Class: "super-trusted"}},
			wantType:  ServerTypeStreamableHTTP,
			wantTrust: TrustBlocked,
		},

		// --- recipe source ---
		{
			name:      "recipe source with no explicit class resolves to trusted_recipe",
			in:        ManagedServer{Command: "uvx", Source: "recipe:mail"},
			wantType:  ServerTypeStdio,
			wantTrust: TrustTrustedRecipe,
		},
		{
			name: "memory recipe fixture: streamable_http + trusted_recipe is runnable",
			in: ManagedServer{
				URL:    "http://memory.local/mcp",
				Type:   ServerTypeStreamableHTTP,
				Source: "recipe:memory",
				Trust:  ManagedTrust{Class: TrustTrustedRecipe},
			},
			wantType:  ServerTypeStreamableHTTP,
			wantTrust: TrustTrustedRecipe,
		},

		// --- known class passes through verbatim ---
		{
			name:      "explicit known class passes through verbatim",
			in:        ManagedServer{Command: "uvx", Trust: ManagedTrust{Class: TrustSandboxedLocal}},
			wantType:  ServerTypeStdio,
			wantTrust: TrustSandboxedLocal,
		},

		// --- valid explicit type+trust combos (the matrix) ---
		{
			name:      "stdio + trusted_recipe valid",
			in:        ManagedServer{Type: ServerTypeStdio, Command: "uvx", Trust: ManagedTrust{Class: TrustTrustedRecipe}},
			wantType:  ServerTypeStdio,
			wantTrust: TrustTrustedRecipe,
		},
		{
			name:      "stdio + trusted_local valid",
			in:        ManagedServer{Type: ServerTypeStdio, Command: "uvx", Trust: ManagedTrust{Class: TrustTrustedLocal}},
			wantType:  ServerTypeStdio,
			wantTrust: TrustTrustedLocal,
		},
		{
			name:      "stdio + sandboxed_local valid",
			in:        ManagedServer{Type: ServerTypeStdio, Command: "uvx", Trust: ManagedTrust{Class: TrustSandboxedLocal}},
			wantType:  ServerTypeStdio,
			wantTrust: TrustSandboxedLocal,
		},
		{
			name:      "stdio + blocked valid",
			in:        ManagedServer{Type: ServerTypeStdio, Command: "uvx", Trust: ManagedTrust{Class: TrustBlocked}},
			wantType:  ServerTypeStdio,
			wantTrust: TrustBlocked,
		},
		{
			name:      "streamable_http + trusted_recipe valid",
			in:        ManagedServer{Type: ServerTypeStreamableHTTP, URL: "https://x.test", Trust: ManagedTrust{Class: TrustTrustedRecipe}},
			wantType:  ServerTypeStreamableHTTP,
			wantTrust: TrustTrustedRecipe,
		},
		{
			name:      "streamable_http + remote_http valid",
			in:        ManagedServer{Type: ServerTypeStreamableHTTP, URL: "https://x.test", Trust: ManagedTrust{Class: TrustRemoteHTTP}},
			wantType:  ServerTypeStreamableHTTP,
			wantTrust: TrustRemoteHTTP,
		},
		{
			name:      "streamable_http + blocked valid",
			in:        ManagedServer{Type: ServerTypeStreamableHTTP, URL: "https://x.test", Trust: ManagedTrust{Class: TrustBlocked}},
			wantType:  ServerTypeStreamableHTTP,
			wantTrust: TrustBlocked,
		},

		// --- inconsistent explicit type+trust combos: hard errors ---
		{
			name:    "stdio + remote_http is inconsistent",
			in:      ManagedServer{Type: ServerTypeStdio, Command: "uvx", Trust: ManagedTrust{Class: TrustRemoteHTTP}},
			wantErr: "stdio",
		},
		{
			name:    "streamable_http + trusted_local is inconsistent",
			in:      ManagedServer{Type: ServerTypeStreamableHTTP, URL: "https://x.test", Trust: ManagedTrust{Class: TrustTrustedLocal}},
			wantErr: "streamable_http",
		},
		{
			name:    "streamable_http + sandboxed_local is inconsistent",
			in:      ManagedServer{Type: ServerTypeStreamableHTTP, URL: "https://x.test", Trust: ManagedTrust{Class: TrustSandboxedLocal}},
			wantErr: "streamable_http",
		},
		{
			name:    "unknown explicit type errors",
			in:      ManagedServer{Type: "grpc", Command: "uvx"},
			wantErr: "unknown type",
		},

		// --- explicit type passthrough (no trust set) ---
		{
			name:      "explicit stdio passes through",
			in:        ManagedServer{Type: ServerTypeStdio, Command: "uvx"},
			wantType:  ServerTypeStdio,
			wantTrust: TrustBlocked,
		},
		{
			name:      "explicit streamable_http passes through",
			in:        ManagedServer{Type: ServerTypeStreamableHTTP, URL: "https://x.test"},
			wantType:  ServerTypeStreamableHTTP,
			wantTrust: TrustBlocked,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			gotType, gotTrust, err := Classify(tc.in)
			if tc.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tc.wantErr) {
					t.Fatalf("Classify() err = %v, want contains %q", err, tc.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatalf("Classify() unexpected err = %v", err)
			}
			if gotType != tc.wantType {
				t.Fatalf("Classify() serverType = %q, want %q", gotType, tc.wantType)
			}
			if gotTrust != tc.wantTrust {
				t.Fatalf("Classify() trust = %q, want %q", gotTrust, tc.wantTrust)
			}
		})
	}
}

// TestClassify_RemoteEmptyTrust is the F-013 regression guard named explicitly
// in 38-RESEARCH.md's phase requirements test map: an HTTP-shaped server with
// no known trust class and no recipe:-prefixed source must resolve to
// TrustBlocked, never auto-promoted to the runnable TrustRemoteHTTP class.
func TestClassify_RemoteEmptyTrust(t *testing.T) {
	_, trust, err := Classify(ManagedServer{URL: "https://example.test/mcp"})
	if err != nil {
		t.Fatalf("Classify() unexpected err = %v", err)
	}
	if trust != TrustBlocked {
		t.Fatalf("Classify() trust = %q, want %q (F-013: must not auto-promote to %q)", trust, TrustBlocked, TrustRemoteHTTP)
	}
}

// TestClassify_MixedTransportNeverReachesStdio is the F-027 regression guard:
// a mixed url+command entry with no explicit type must be rejected outright,
// and specifically must never resolve to ServerTypeStdio (the old silent
// fallback this phase closes) even incidentally on the error path.
func TestClassify_MixedTransportNeverReachesStdio(t *testing.T) {
	serverType, _, err := Classify(ManagedServer{URL: "http://x.test", Command: "cmd"})
	if err == nil {
		t.Fatalf("Classify(mixed) = (%q, nil err), want rejection", serverType)
	}
	if serverType == ServerTypeStdio {
		t.Fatalf("Classify(mixed) must never resolve to stdio, even on the error path, got serverType=%q", serverType)
	}
}
