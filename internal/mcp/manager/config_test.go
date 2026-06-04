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
