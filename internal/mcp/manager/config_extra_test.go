package manager

import (
	"reflect"
	"testing"

	"github.com/chetto1983/aura/internal/mcp"
)

func TestImportProfileOverwriteCredentials(t *testing.T) {
	base := mcp.ManagedConfig{
		MCPServers: map[string]mcp.ManagedServer{
			"mail": {
				Command: "mail-mcp",
				Env:     []string{"MAIL_PASSWORD=old-password"},
			},
		},
	}
	incoming := mcp.ManagedConfig{
		Profiles: map[string]mcp.ManagedProfile{"team": {Servers: []string{"mail"}}},
		MCPServers: map[string]mcp.ManagedServer{
			"mail": {
				Command: "mail-mcp",
				Env:     []string{"MAIL_PASSWORD=new-password"},
				Source:  "recipe:mail",
			},
		},
	}

	if err := ImportProfile(&base, incoming, ImportOptions{OverwriteCredentials: true}); err != nil {
		t.Fatalf("ImportProfile: %v", err)
	}
	got := base.MCPServers["mail"].Env
	want := []string{"MAIL_PASSWORD=new-password"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("overwrite env = %#v, want %#v", got, want)
	}
}

func TestImportProfileInitializesZeroBase(t *testing.T) {
	var base mcp.ManagedConfig
	incoming := mcp.ManagedConfig{
		Profiles: map[string]mcp.ManagedProfile{"team": {Servers: []string{"calc"}}},
		MCPServers: map[string]mcp.ManagedServer{
			"calc": {Command: "uvx", Source: "recipe:calculator"},
		},
	}
	if err := ImportProfile(&base, incoming, ImportOptions{}); err != nil {
		t.Fatalf("ImportProfile: %v", err)
	}
	if base.Version != mcp.ManagedConfigVersion {
		t.Fatalf("base.Version = %d, want %d", base.Version, mcp.ManagedConfigVersion)
	}
	if _, ok := base.MCPServers["calc"]; !ok {
		t.Fatalf("calc not imported: %#v", base.MCPServers)
	}
	if _, ok := base.Profiles["team"]; !ok {
		t.Fatalf("team profile not imported: %#v", base.Profiles)
	}
}

// mergeEnvPreserveCredentials is exercised indirectly via ImportProfile so the
// merge ordering matches the production import path exactly.
func TestMergeEnvPreserveCredentialsBranches(t *testing.T) {
	base := mcp.ManagedConfig{
		MCPServers: map[string]mcp.ManagedServer{
			"srv": {
				Command: "srv",
				Env: []string{
					"MAIL_PASSWORD=real-password", // secret, kept (incoming sends real value)
					"API_TOKEN=stored-token",      // secret, kept (incoming sends placeholder)
					"PUBLIC_HOST=old.example.com", // non-secret, overwritten by incoming
					"LEGACY_SECRET=legacy-keyval", // secret, absent from incoming -> appended
				},
			},
		},
	}
	incoming := mcp.ManagedConfig{
		Profiles: map[string]mcp.ManagedProfile{"team": {Servers: []string{"srv"}}},
		MCPServers: map[string]mcp.ManagedServer{
			"srv": {
				Command: "srv",
				Env: []string{
					"BAREWORD",                    // no '=' -> passed through verbatim
					"MAIL_PASSWORD=incoming-real", // secret with real value -> prior wins
					"API_TOKEN=${API_TOKEN}",      // secret placeholder -> prior wins
					"PUBLIC_HOST=new.example.com", // non-secret -> incoming wins
				},
				Source: "recipe:srv",
			},
		},
	}

	if err := ImportProfile(&base, incoming, ImportOptions{}); err != nil {
		t.Fatalf("ImportProfile: %v", err)
	}
	got := base.MCPServers["srv"].Env
	want := []string{
		"BAREWORD",
		"MAIL_PASSWORD=real-password",
		"API_TOKEN=stored-token",
		"PUBLIC_HOST=new.example.com",
		"LEGACY_SECRET=legacy-keyval",
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("merged env = %#v, want %#v", got, want)
	}
}

func TestMergeEnvPreserveCredentialsSkipsMalformedExisting(t *testing.T) {
	base := mcp.ManagedConfig{
		MCPServers: map[string]mcp.ManagedServer{
			"srv": {
				Command: "srv",
				// "JUST_A_FLAG" has no '=' and is skipped while building the
				// existing index, so it is not re-appended.
				Env: []string{"JUST_A_FLAG", "KEEP=value"},
			},
		},
	}
	incoming := mcp.ManagedConfig{
		Profiles: map[string]mcp.ManagedProfile{"team": {Servers: []string{"srv"}}},
		MCPServers: map[string]mcp.ManagedServer{
			"srv": {Command: "srv", Env: []string{"OTHER=incoming"}, Source: "recipe:srv"},
		},
	}
	if err := ImportProfile(&base, incoming, ImportOptions{}); err != nil {
		t.Fatalf("ImportProfile: %v", err)
	}
	got := base.MCPServers["srv"].Env
	want := []string{"OTHER=incoming", "KEEP=value"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("merged env = %#v, want %#v", got, want)
	}
}

func TestExportProfileSynthesizesMissingProfileEntry(t *testing.T) {
	// The requested profile name is absent from doc.Profiles, but
	// ProfileServerNames falls back to all enabled servers, so ExportProfile
	// must synthesize a ManagedProfile entry from the resolved names
	// (the else branch in ExportProfile).
	doc := mcp.ManagedConfig{
		Profiles: map[string]mcp.ManagedProfile{},
		MCPServers: map[string]mcp.ManagedServer{
			"calc": {Command: "uvx", Source: "recipe:calculator"},
		},
	}
	out, err := ExportProfile(doc, "undefined-profile")
	if err != nil {
		t.Fatalf("ExportProfile: %v", err)
	}
	p, ok := out.Profiles["undefined-profile"]
	if !ok {
		t.Fatalf("synthesized profile missing: %#v", out.Profiles)
	}
	if !reflect.DeepEqual(p.Servers, []string{"calc"}) {
		t.Fatalf("synthesized profile servers = %#v, want [calc]", p.Servers)
	}
	if _, ok := out.MCPServers["calc"]; !ok {
		t.Fatalf("calc server not exported: %#v", out.MCPServers)
	}
}
