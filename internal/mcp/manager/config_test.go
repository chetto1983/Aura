package manager

import (
	"reflect"
	"testing"

	"github.com/chetto1983/aura/internal/mcp"
)

func TestExportProfileRedactsSecretEnv(t *testing.T) {
	doc := mcp.ManagedConfig{
		Profiles: map[string]mcp.ManagedProfile{
			"team": {Servers: []string{"mail"}},
		},
		MCPServers: map[string]mcp.ManagedServer{
			"mail": {
				Command: "mail-mcp",
				Env: []string{
					"MAIL_USER=alice@example.com",
					"MAIL_PASSWORD=s3cr3t",
					"API_TOKEN=${API_TOKEN}",
				},
				Source: "recipe:mail",
				Trust:  mcp.ManagedTrust{Class: mcp.TrustTrustedRecipe},
			},
		},
	}

	exported, err := ExportProfile(doc, "team")
	if err != nil {
		t.Fatalf("ExportProfile: %v", err)
	}
	got := exported.MCPServers["mail"].Env
	want := []string{"MAIL_USER=alice@example.com", "MAIL_PASSWORD=${MAIL_PASSWORD}", "API_TOKEN=${API_TOKEN}"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("exported env = %#v, want %#v", got, want)
	}
}

func TestCutEnvEdgeCases(t *testing.T) {
	tests := []struct {
		name      string
		entry     string
		wantKey   string
		wantValue string
		wantOK    bool
	}{
		{name: "simple", entry: "KEY=value", wantKey: "KEY", wantValue: "value", wantOK: true},
		{name: "empty value", entry: "KEY=", wantKey: "KEY", wantValue: "", wantOK: true},
		{name: "value with equals", entry: "KEY=a=b", wantKey: "KEY", wantValue: "a=b", wantOK: true},
		{name: "no equals", entry: "BAREWORD", wantOK: false},
		{name: "empty string", entry: "", wantOK: false},
		{name: "blank key", entry: "=value", wantOK: false},
		{name: "whitespace key", entry: "   =value", wantOK: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			key, value, ok := cutEnv(tt.entry)
			if ok != tt.wantOK {
				t.Fatalf("cutEnv(%q) ok = %v, want %v", tt.entry, ok, tt.wantOK)
			}
			if key != tt.wantKey || value != tt.wantValue {
				t.Fatalf("cutEnv(%q) = (%q, %q), want (%q, %q)", tt.entry, key, value, tt.wantKey, tt.wantValue)
			}
		})
	}
}

func TestExportProfileNoServers(t *testing.T) {
	doc := mcp.ManagedConfig{
		Profiles:   map[string]mcp.ManagedProfile{"empty": {}},
		MCPServers: map[string]mcp.ManagedServer{},
	}
	if _, err := ExportProfile(doc, "empty"); err == nil {
		t.Fatal("ExportProfile(empty) err = nil, want error")
	}
}

func TestRedactEnvLeavesNonSecretAndPlaceholders(t *testing.T) {
	in := []string{
		"BAREWORD",
		"PUBLIC_HOST=example.com",
		"API_TOKEN=real-secret",
		"OTHER_SECRET=${OTHER_SECRET}",
	}
	got := RedactEnv(in)
	want := []string{
		"BAREWORD",
		"PUBLIC_HOST=example.com",
		"API_TOKEN=${API_TOKEN}",
		"OTHER_SECRET=${OTHER_SECRET}",
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("RedactEnv = %#v, want %#v", got, want)
	}
}

func TestImportProfileDoesNotOverwriteCredentialsByDefault(t *testing.T) {
	base := mcp.ManagedConfig{
		MCPServers: map[string]mcp.ManagedServer{
			"mail": {
				Command: "mail-mcp",
				Env: []string{
					"MAIL_USER=alice@example.com",
					"MAIL_PASSWORD=real-password",
				},
			},
		},
	}
	incoming := mcp.ManagedConfig{
		Profiles: map[string]mcp.ManagedProfile{"team": {Servers: []string{"mail"}}},
		MCPServers: map[string]mcp.ManagedServer{
			"mail": {
				Command: "mail-mcp",
				Env: []string{
					"MAIL_PASSWORD=${MAIL_PASSWORD}",
					"API_TOKEN=${API_TOKEN}",
				},
				Source: "recipe:mail",
			},
		},
	}

	if err := ImportProfile(&base, incoming, ImportOptions{}); err != nil {
		t.Fatalf("ImportProfile: %v", err)
	}
	got := base.MCPServers["mail"].Env
	want := []string{"MAIL_PASSWORD=real-password", "API_TOKEN=${API_TOKEN}", "MAIL_USER=alice@example.com"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("merged env = %#v, want %#v", got, want)
	}
}
