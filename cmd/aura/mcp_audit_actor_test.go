package main

import (
	"errors"
	"os/user"
	"strings"
	"testing"

	"github.com/chetto1983/aura/internal/config"
)

// TestMcpAuditActor table-tests mcpAuditActor's fallback chain (D-13/Pitfall #10) via the
// currentOSUser seam + t.Setenv: os/user.Current() succeeding with a non-blank Username
// wins; on error it falls back to USER then USERNAME; a blank/whitespace username at any
// stage falls through instead of collapsing to a bare "cli:"; every source absent yields
// the literal "cli:unknown". The actor must never be "" and never a bare "cli:".
func TestMcpAuditActor(t *testing.T) {
	orig := currentOSUser
	t.Cleanup(func() { currentOSUser = orig })

	cases := []struct {
		name        string
		currentUser func() (*user.User, error)
		envUSER     string
		envUSERNAME string
		want        string
	}{
		{
			name:        "os/user succeeds",
			currentUser: func() (*user.User, error) { return &user.User{Username: "alice"}, nil },
			want:        "cli:alice",
		},
		{
			name:        "os/user errors, USER env set",
			currentUser: func() (*user.User, error) { return nil, errors.New("boom") },
			envUSER:     "bob",
			want:        "cli:bob",
		},
		{
			name:        "os/user errors, USER blank, USERNAME set",
			currentUser: func() (*user.User, error) { return nil, errors.New("boom") },
			envUSERNAME: "carol",
			want:        "cli:carol",
		},
		{
			name:        "every source absent -> cli:unknown",
			currentUser: func() (*user.User, error) { return nil, errors.New("boom") },
			want:        "cli:unknown",
		},
		{
			name:        "os/user succeeds but blank username falls through to USER",
			currentUser: func() (*user.User, error) { return &user.User{Username: "   "}, nil },
			envUSER:     "dave",
			want:        "cli:dave",
		},
		{
			name:        "every source blank/whitespace -> cli:unknown, never bare cli:",
			currentUser: func() (*user.User, error) { return &user.User{Username: "  "}, nil },
			envUSER:     "   ",
			envUSERNAME: "  ",
			want:        "cli:unknown",
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			currentOSUser = c.currentUser
			t.Setenv("USER", c.envUSER)
			t.Setenv("USERNAME", c.envUSERNAME)
			if got := mcpAuditActor(); got != c.want {
				t.Fatalf("mcpAuditActor() = %q, want %q", got, c.want)
			}
			if got := mcpAuditActor(); got == "" || got == "cli:" {
				t.Fatalf("mcpAuditActor() must never be empty or a bare \"cli:\", got %q", got)
			}
		})
	}
}

// TestMCPCLIProdDisallow is the pure profile-gate test (MCPH-07's literal "audited OR
// disallowed" requirement): mcpPoolRequiredErr takes no I/O, so it is exercised directly
// without a pool or an env var. Under server_production a missing pool is a hard error;
// under every other profile it is nil (mcpWriteManagedConfig degrades to the pre-existing
// plain write instead of ever falling back to an unaudited write under production).
func TestMCPCLIProdDisallow(t *testing.T) {
	cases := []struct {
		name    string
		profile config.RuntimeProfile
		wantErr bool
	}{
		{"dev allows a pool-free write", config.ProfileDev, false},
		{"local_trusted allows a pool-free write", config.ProfileLocalTrusted, false},
		{"single_user_hardened allows a pool-free write", config.ProfileSingleUserHardened, false},
		{"server_production disallows a pool-free write", config.ProfileServerProduction, true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			err := mcpPoolRequiredErr(c.profile, "add")
			if c.wantErr && err == nil {
				t.Fatalf("mcpPoolRequiredErr(%q) = nil, want an error", c.profile)
			}
			if !c.wantErr && err != nil {
				t.Fatalf("mcpPoolRequiredErr(%q) = %v, want nil", c.profile, err)
			}
		})
	}
}

// TestMCPAuditInsertForBuildsCLIActor proves the pure MCPAuditInsert builder every
// mutating verb funnels through always attaches a cli:-prefixed actor and the caller's
// action/name/reason, with no DB required.
func TestMCPAuditInsertForBuildsCLIActor(t *testing.T) {
	insert := mcpAuditInsertFor("add", "srv", "")
	if !strings.HasPrefix(insert.ActorIdentityID, "cli:") {
		t.Fatalf("ActorIdentityID = %q, want a cli: prefix", insert.ActorIdentityID)
	}
	if insert.Action != "add" || insert.ServerName != "srv" || insert.Reason != "" {
		t.Fatalf("mcpAuditInsertFor(\"add\", \"srv\", \"\") = %+v", insert)
	}

	trustInsert := mcpAuditInsertFor("trust", "srv", "operator vetted")
	if !strings.HasPrefix(trustInsert.ActorIdentityID, "cli:") {
		t.Fatalf("ActorIdentityID = %q, want a cli: prefix", trustInsert.ActorIdentityID)
	}
	if trustInsert.Action != "trust" || trustInsert.Reason != "operator vetted" {
		t.Fatalf("mcpAuditInsertFor(\"trust\", \"srv\", \"operator vetted\") = %+v", trustInsert)
	}
}
