package manager

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/chetto1983/aura/internal/mcp"
)

func TestStatusSnapshotsShowBlockedDisabledAndProfiles(t *testing.T) {
	disabled := false
	doc := mcp.ManagedConfig{
		Profiles: map[string]mcp.ManagedProfile{"work": {Servers: []string{"manual", "mail"}}},
		MCPServers: map[string]mcp.ManagedServer{
			"manual": {Command: "node", Source: "manual", Trust: mcp.ManagedTrust{Class: mcp.TrustBlocked}},
			"mail":   {Command: "npx", Source: "recipe:mail", Trust: mcp.ManagedTrust{Class: mcp.TrustTrustedRecipe}, RiskLabels: []string{"private_data"}},
			"off":    {Command: "uvx", Enabled: &disabled, Source: "recipe:calculator"},
		},
	}

	got := SnapshotStatus(doc)
	manual := findStatus(t, got, "manual")
	if manual.StartupState != StartupBlocked || manual.Trust != mcp.TrustBlocked {
		t.Fatalf("manual status = %+v", manual)
	}
	if !containsString(manual.Profiles, "work") {
		t.Fatalf("manual profiles = %#v, want work", manual.Profiles)
	}
	off := findStatus(t, got, "off")
	if off.StartupState != StartupDisabled {
		t.Fatalf("disabled startup = %q, want %q", off.StartupState, StartupDisabled)
	}
	mail := findStatus(t, got, "mail")
	if mail.AuthStatus != AuthUnsupported || !strings.Contains(mail.PolicySummary, "private_data") {
		t.Fatalf("mail status = %+v", mail)
	}

	if _, err := json.Marshal(got); err != nil {
		t.Fatalf("marshal status: %v", err)
	}
}

func TestRedactSecrets(t *testing.T) {
	got := RedactSecrets("SMTP_PASS=hunter2 TOKEN=abc Authorization: Bearer secret normal=value")
	for _, bad := range []string{"hunter2", "abc", "Bearer secret"} {
		if strings.Contains(got, bad) {
			t.Fatalf("redacted output still contains %q: %s", bad, got)
		}
	}
	if !strings.Contains(got, "SMTP_PASS=<redacted>") {
		t.Fatalf("redacted output missing key placeholder: %s", got)
	}
}

func TestRuntimeName(t *testing.T) {
	tests := []struct {
		name   string
		server mcp.ManagedServer
		want   string
	}{
		{name: "explicit kind", server: mcp.ManagedServer{Runtime: mcp.ManagedRuntime{Kind: "docker"}}, want: "docker"},
		{name: "kind trimmed", server: mcp.ManagedServer{Runtime: mcp.ManagedRuntime{Kind: "  docker_gateway  "}}, want: "docker_gateway"},
		{name: "url http", server: mcp.ManagedServer{URL: "https://mcp.example.com"}, want: mcp.TrustRemoteHTTP},
		{name: "type http", server: mcp.ManagedServer{Type: mcp.ServerTypeStreamableHTTP}, want: mcp.TrustRemoteHTTP},
		{name: "default local", server: mcp.ManagedServer{Command: "node"}, want: "local"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := runtimeName(tt.server); got != tt.want {
				t.Fatalf("runtimeName = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestAuthStatus(t *testing.T) {
	tests := []struct {
		name   string
		server mcp.ManagedServer
		want   string
	}{
		{name: "no env", server: mcp.ManagedServer{}, want: AuthUnsupported},
		{name: "non-auth env", server: mcp.ManagedServer{Env: []string{"PUBLIC=1"}}, want: AuthUnsupported},
		{name: "oauth", server: mcp.ManagedServer{Env: []string{"GOOGLE_OAUTH_CLIENT=abc"}}, want: AuthOAuth},
		{name: "bearer real", server: mcp.ManagedServer{Env: []string{"BEARER_TOKEN=real-value"}}, want: AuthBearerToken},
		{name: "token placeholder", server: mcp.ManagedServer{Env: []string{"API_TOKEN=${API_TOKEN}"}}, want: AuthNotLoggedIn},
		{name: "token change_me", server: mcp.ManagedServer{Env: []string{"API_TOKEN=CHANGE_ME"}}, want: AuthNotLoggedIn},
		{name: "token empty", server: mcp.ManagedServer{Env: []string{"API_TOKEN="}}, want: AuthNotLoggedIn},
		{name: "authorization header", server: mcp.ManagedServer{Env: []string{"Authorization=Bearer real"}}, want: AuthBearerToken},
		{name: "malformed env skipped", server: mcp.ManagedServer{Env: []string{"BAREWORD", "TOKEN=real"}}, want: AuthBearerToken},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := authStatus(tt.server); got != tt.want {
				t.Fatalf("authStatus = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestPolicySummary(t *testing.T) {
	tests := []struct {
		name   string
		server mcp.ManagedServer
		want   []string
	}{
		{name: "empty", server: mcp.ManagedServer{}, want: nil},
		{
			name: "all parts",
			server: mcp.ManagedServer{
				RiskLabels: []string{"read", "write"},
				ToolPolicy: mcp.ManagedToolPolicy{
					Allow:    []string{"a", "b"},
					Deny:     []string{"c"},
					DenyRisk: []string{"destructive"},
				},
			},
			want: []string{"risk=read,write", "allow=a,b", "deny=c", "denyRisk=destructive"},
		},
		{
			name:   "allow only",
			server: mcp.ManagedServer{ToolPolicy: mcp.ManagedToolPolicy{Allow: []string{"x"}}},
			want:   []string{"allow=x"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := policySummary(tt.server)
			for _, part := range tt.want {
				if !strings.Contains(got, part) {
					t.Fatalf("policySummary = %q, missing %q", got, part)
				}
			}
			if len(tt.want) == 0 && got != "" {
				t.Fatalf("policySummary = %q, want empty", got)
			}
		})
	}
}

func findStatus(t *testing.T, statuses []StatusSnapshot, name string) StatusSnapshot {
	t.Helper()
	for _, status := range statuses {
		if status.Name == name {
			return status
		}
	}
	t.Fatalf("status %q missing: %+v", name, statuses)
	return StatusSnapshot{}
}
