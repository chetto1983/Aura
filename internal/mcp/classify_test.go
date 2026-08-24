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
			wantTrust: TrustTrustedLocal,
		},
		{
			name:      "url only infers streamable_http",
			in:        ManagedServer{URL: "https://x.test"},
			wantType:  ServerTypeStreamableHTTP,
			wantTrust: TrustRemoteHTTP,
		},
		{
			name:      "bare server infers stdio",
			in:        ManagedServer{},
			wantType:  ServerTypeStdio,
			wantTrust: TrustTrustedLocal,
		},

		// A trust class nobody set is resolved from the transport, not blocked. The F-013
		// cases that used to live here asserted the opposite for every spelling of "unset"
		// (empty, blank, whitespace, unknown); they are gone with the behaviour they pinned.
		// What still matters is that an unreadable class does not become a THIRD state — it
		// resolves the same as unset.
		{
			name:      "unknown trust class resolves from the transport",
			in:        ManagedServer{URL: "http://x.test", Trust: ManagedTrust{Class: "super-trusted"}},
			wantType:  ServerTypeStreamableHTTP,
			wantTrust: TrustRemoteHTTP,
		},
		{
			name:      "whitespace-only trust class resolves from the transport",
			in:        ManagedServer{URL: "http://x.test", Trust: ManagedTrust{Class: "\t\n "}},
			wantType:  ServerTypeStreamableHTTP,
			wantTrust: TrustRemoteHTTP,
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
			wantTrust: TrustTrustedLocal,
		},
		{
			name:      "explicit streamable_http passes through",
			in:        ManagedServer{Type: ServerTypeStreamableHTTP, URL: "https://x.test"},
			wantType:  ServerTypeStreamableHTTP,
			wantTrust: TrustRemoteHTTP,
		},
		{
			// The declared transport decides, not the fields: no URL here, and pairing this
			// with trusted_local would trip checkTypeTrustConsistency and error the classify.
			name:      "explicit streamable_http with no url still resolves remote",
			in:        ManagedServer{Type: ServerTypeStreamableHTTP},
			wantType:  ServerTypeStreamableHTTP,
			wantTrust: TrustRemoteHTTP,
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

// This was the F-013 guard: an HTTP-shaped server with no known trust class had to resolve
// to TrustBlocked rather than the runnable TrustRemoteHTTP. The guard is inverted here on
// purpose. Blocking-by-default could only ever stop the two callers that had already been
// authorized — an operator with governance.write, or one with a shell — while costing a
// second, undiscoverable step that left the server installed, listed and inert.
//
// TrustBlocked is still honoured when it is SET. What is gone is getting it by silence.
func TestClassify_RemoteWithNoTrustIsRunnable(t *testing.T) {
	_, trust, err := Classify(ManagedServer{URL: "https://example.test/mcp"})
	if err != nil {
		t.Fatalf("Classify() unexpected err = %v", err)
	}
	if trust != TrustRemoteHTTP {
		t.Fatalf("Classify() trust = %q, want %q", trust, TrustRemoteHTTP)
	}

	_, blocked, err := Classify(ManagedServer{
		URL:   "https://example.test/mcp",
		Trust: ManagedTrust{Class: TrustBlocked},
	})
	if err != nil {
		t.Fatalf("Classify() unexpected err = %v", err)
	}
	if blocked != TrustBlocked {
		t.Fatalf("an explicitly blocked server resolved to %q, want %q", blocked, TrustBlocked)
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
