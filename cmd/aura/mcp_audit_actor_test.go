package main

import (
	"errors"
	"os/user"
	"testing"
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
