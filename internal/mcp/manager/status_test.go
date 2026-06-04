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
