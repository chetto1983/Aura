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
			"mail":   {Command: "npx", Source: "recipe:mail", Trust: mcp.ManagedTrust{Class: mcp.TrustTrustedRecipe}},
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
	if mail.AuthStatus != AuthUnsupported {
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
		{name: "explicit kind", server: mcp.ManagedServer{Runtime: mcp.ManagedRuntime{Kind: "custom"}}, want: "custom"},
		{name: "kind trimmed", server: mcp.ManagedServer{Runtime: mcp.ManagedRuntime{Kind: "  custom  "}}, want: "custom"},
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
		// The Linear/Atlassian shape: a remote server with NO oauth configuration at all
		// still authorizes by OAuth, because dynamic client registration needs none.
		// Sniffing env key names reported this "unsupported" — the one answer that would
		// stop an operator from even trying to connect it.
		{
			name:   "remote server with no oauth env at all",
			server: mcp.ManagedServer{Type: mcp.ServerTypeStreamableHTTP, URL: "https://mcp.linear.app/mcp"},
			want:   AuthOAuth,
		},
		// A static bearer token is an explicit operator instruction and keeps precedence
		// over a browser flow, so the row must still read as a bearer token.
		{
			name: "remote server with a static bearer token",
			server: mcp.ManagedServer{
				Type: mcp.ServerTypeStreamableHTTP, URL: "https://mcp.notion.com/mcp",
				Env: []string{"MCP_BEARER_TOKEN=real-value"},
			},
			want: AuthBearerToken,
		},
		// MCP_OAUTH_TOKEN_URL contains "TOKEN". Read as a static credential it would
		// report a bearer token the server does not have — on a stdio server, where our
		// own flow cannot apply at all.
		{
			name:   "stdio server carrying only our oauth knobs",
			server: mcp.ManagedServer{Command: "notes-mcp", Env: []string{"MCP_OAUTH_TOKEN_URL=https://x.example/token"}},
			want:   AuthUnsupported,
		},
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
