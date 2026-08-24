package main

import (
	"context"
	"reflect"
	"slices"
	"testing"

	"github.com/chetto1983/aura/internal/mcp"
)

// TestValidateTrustClassReason proves the hoisted single-source-of-truth validator
// (D-13/Pitfall #5): a known trust class AND a non-empty reason are both required, with
// surrounding whitespace trimmed on success.
func TestValidateTrustClassReason(t *testing.T) {
	cases := []struct {
		name       string
		class      string
		reason     string
		wantErr    bool
		wantClass  string
		wantReason string
	}{
		{"valid trusted_local", mcp.TrustTrustedLocal, "operator vetted", false, mcp.TrustTrustedLocal, "operator vetted"},
		{"valid trims surrounding whitespace", "  " + mcp.TrustTrustedLocal + "  ", "  operator vetted  ", false, mcp.TrustTrustedLocal, "operator vetted"},
		{"empty class", "", "operator vetted", true, "", ""},
		{"blank class", "   ", "operator vetted", true, "", ""},
		{"empty reason", mcp.TrustTrustedLocal, "", true, "", ""},
		{"blank reason", mcp.TrustTrustedLocal, "   ", true, "", ""},
		{"unknown class", "super_trusted", "operator vetted", true, "", ""},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			gotClass, gotReason, err := validateTrustClassReason(c.class, c.reason)
			if c.wantErr {
				if err == nil {
					t.Fatalf("validateTrustClassReason(%q, %q) succeeded, want error", c.class, c.reason)
				}
				return
			}
			if err != nil {
				t.Fatalf("validateTrustClassReason(%q, %q) = %v, want success", c.class, c.reason, err)
			}
			if gotClass != c.wantClass || gotReason != c.wantReason {
				t.Fatalf("validateTrustClassReason(%q, %q) = (%q, %q), want (%q, %q)", c.class, c.reason, gotClass, gotReason, c.wantClass, c.wantReason)
			}
		})
	}
}

// TestTrustApproveRejectsInvalidClassOrReason proves the F-038/Pitfall#5 dead-fallback
// removal: TrustApprove itself now enforces validateTrustClassReason (class known +
// reason non-blank), so neither the web caller nor any future CLI caller can reach the old
// `class == "" -> trusted_local` default. Every case supplies a nil *pgxpool.Pool: if
// TrustApprove ever again reached WriteConfigWithAudit for one of these invalid inputs, the
// nil-pool tx.Begin would panic — so a passing (non-panicking) test proves no config/audit
// write was attempted, matching the plan's "no config write, no audit row" requirement.
func TestTrustApproveRejectsInvalidClassOrReason(t *testing.T) {
	withMemoryMCPRegistry(t)
	seed := mcp.ManagedConfig{MCPServers: map[string]mcp.ManagedServer{
		"srv": {Command: "srv-bin"},
	}}
	seedMCPRegistry(t, seed)
	before := readMCPRegistry(t)

	cases := []struct {
		name   string
		class  string
		reason string
	}{
		{"empty class", "", "operator vetted"},
		{"blank class", "   ", "operator vetted"},
		{"empty reason", mcp.TrustTrustedLocal, ""},
		{"blank reason", mcp.TrustTrustedLocal, "   "},
		{"unknown class", "super_trusted", "operator vetted"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			a := mcpWriteAdapter{pool: nil}
			if _, err := a.TrustApprove(context.Background(), "cli:tester", "srv", c.class, c.reason); err == nil {
				t.Fatal("TrustApprove succeeded, want a validation error")
			}
			if after := readMCPRegistry(t); !reflect.DeepEqual(after.MCPServers, before.MCPServers) {
				t.Fatal("TrustApprove wrote the registry despite a validation failure")
			}
		})
	}
}

// TestJoinActiveProfile pins the asymmetry that made a cockpit install look like it worked
// and then do nothing: the server was written to the registry but never joined the profile,
// and ProfileServerNames — not MCPServers — is what decides the runnable set.
func TestJoinActiveProfile(t *testing.T) {
	t.Run("adds the server to the active profile", func(t *testing.T) {
		doc := mcp.ManagedConfig{
			ActiveProfile: "work",
			Profiles:      map[string]mcp.ManagedProfile{"work": {Servers: []string{"memory"}}},
			MCPServers:    map[string]mcp.ManagedServer{"memory": {Command: "node"}, "slack": {URL: "https://mcp.slack.com/mcp"}},
		}
		joinActiveProfile(&doc, "slack")
		if got := doc.ProfileServerNames(""); !slices.Contains(got, "slack") {
			t.Fatalf("installed server missing from the runnable set: %v", got)
		}
	})

	t.Run("is idempotent", func(t *testing.T) {
		doc := mcp.ManagedConfig{
			Profiles:   map[string]mcp.ManagedProfile{mcp.DefaultMCPProfile: {Servers: []string{"slack"}}},
			MCPServers: map[string]mcp.ManagedServer{"slack": {URL: "https://mcp.slack.com/mcp"}},
		}
		joinActiveProfile(&doc, "slack")
		if got := doc.Profiles[mcp.DefaultMCPProfile].Servers; len(got) != 1 {
			t.Fatalf("re-installing duplicated the profile entry: %v", got)
		}
	})

	t.Run("creates the profile map when the config has none", func(t *testing.T) {
		doc := mcp.ManagedConfig{MCPServers: map[string]mcp.ManagedServer{"slack": {URL: "https://mcp.slack.com/mcp"}}}
		joinActiveProfile(&doc, "slack")
		if got := doc.ProfileServerNames(""); !slices.Contains(got, "slack") {
			t.Fatalf("server missing after profile map was created: %v", got)
		}
	})
}
