package mcp

import (
	"errors"
	"path/filepath"
	"strings"
	"testing"

	"github.com/chetto1983/aura/internal/idroot"
)

// writeSharedCatalog stages a shared catalog with a class-(a) stdio server
// (calculator) and the class-(b) shared agent-memory server, returning the catalog
// dir (the per-identity root) after pointing AURA_MCP_CONFIG at it.
func writeSharedCatalog(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "servers.json")
	t.Setenv("AURA_MCP_CONFIG", path)
	doc := ManagedConfig{MCPServers: map[string]ManagedServer{
		"calculator": {
			Command: "uvx",
			Args:    []string{"calculator-mcp-server", "--stdio"},
			Enabled: new(true),
			Source:  "recipe:calculator",
			Trust:   ManagedTrust{Class: TrustTrustedRecipe},
		},
		"memory": {
			Type:    ServerTypeStreamableHTTP,
			URL:     "http://127.0.0.1:8091/mcp/",
			Enabled: new(true),
			Source:  SourceRecipeMemory,
			Trust:   ManagedTrust{Class: TrustTrustedRecipe},
		},
		// weather is a class-(a) REMOTE (streamable_http) server — NOT
		// admin-governed (unlike memory) — proving D-04: a per-identity override
		// must not elevate a remote's trust, only the admin shared catalog can.
		"weather": {
			Type:    ServerTypeStreamableHTTP,
			URL:     "https://weather.example.test/mcp/",
			Enabled: new(true),
			Source:  "recipe:weather",
			Trust:   ManagedTrust{Class: TrustRemoteHTTP},
		},
	}}
	if err := SaveManagedConfig(path, doc); err != nil {
		t.Fatalf("SaveManagedConfig: %v", err)
	}
	return dir
}

// Test 1: an identity with ".." or a slash is rejected by the rooting guard.
func TestIdentityConfigPathRejectsTraversal(t *testing.T) {
	writeSharedCatalog(t)
	for _, id := range []string{"..", "../evil", "a/b", `a\b`, "../../etc"} {
		if _, err := IdentityConfigPath(id); !errors.Is(err, idroot.ErrInvalidIdentity) {
			t.Fatalf("IdentityConfigPath(%q) err = %v, want ErrInvalidIdentity", id, err)
		}
		if _, err := MountForIdentity(id); !errors.Is(err, idroot.ErrInvalidIdentity) {
			t.Fatalf("MountForIdentity(%q) err = %v, want ErrInvalidIdentity", id, err)
		}
		if err := SetEnabledForIdentity(id, "calculator", false); !errors.Is(err, idroot.ErrInvalidIdentity) {
			t.Fatalf("SetEnabledForIdentity(%q) err = %v, want ErrInvalidIdentity", id, err)
		}
	}
}

// Test 2: a valid identity resolves servers.json contained under the mcp root.
func TestIdentityConfigPathContained(t *testing.T) {
	dir := writeSharedCatalog(t)
	got, err := IdentityConfigPath("alice")
	if err != nil {
		t.Fatalf("IdentityConfigPath: %v", err)
	}
	wantSuffix := filepath.Join("alice", "servers.json")
	if !strings.HasSuffix(got, wantSuffix) {
		t.Fatalf("IdentityConfigPath = %q, want suffix %q", got, wantSuffix)
	}
	absRoot, err := filepath.Abs(dir)
	if err != nil {
		t.Fatalf("abs root: %v", err)
	}
	rel, err := filepath.Rel(absRoot, got)
	if err != nil || strings.HasPrefix(rel, "..") || filepath.IsAbs(rel) {
		t.Fatalf("IdentityConfigPath %q escapes root %q (rel=%q, err=%v)", got, absRoot, rel, err)
	}
}

// Test 3: a per-identity enable change alters that identity's class-(a) view without
// touching the shared catalog or another identity's view.
func TestSetEnabledForIdentityOverlaysClassA(t *testing.T) {
	dir := writeSharedCatalog(t)

	if err := SetEnabledForIdentity("alice", "calculator", false); err != nil {
		t.Fatalf("SetEnabledForIdentity: %v", err)
	}

	alice, err := MountForIdentity("alice")
	if err != nil {
		t.Fatalf("MountForIdentity(alice): %v", err)
	}
	if e := alice.MCPServers["calculator"].Enabled; e == nil || *e {
		t.Fatalf("alice calculator enabled = %v, want false", e)
	}

	// The shared catalog on disk is untouched.
	shared, err := LoadManagedConfig(filepath.Join(dir, "servers.json"))
	if err != nil {
		t.Fatalf("LoadManagedConfig: %v", err)
	}
	if e := shared.MCPServers["calculator"].Enabled; e == nil || !*e {
		t.Fatalf("shared calculator enabled = %v, want true (untouched)", e)
	}

	// A different identity with no overlay still sees the shared default (enabled).
	bob, err := MountForIdentity("bob")
	if err != nil {
		t.Fatalf("MountForIdentity(bob): %v", err)
	}
	if e := bob.MCPServers["calculator"].Enabled; e == nil || !*e {
		t.Fatalf("bob calculator enabled = %v, want true", e)
	}
}

// Test 3b: a per-identity trust override lands in the identity's view only.
func TestSetTrustForIdentityOverlaysClassA(t *testing.T) {
	writeSharedCatalog(t)
	if err := SetTrustForIdentity("alice", "calculator", TrustSandboxedLocal); err != nil {
		t.Fatalf("SetTrustForIdentity: %v", err)
	}
	alice, err := MountForIdentity("alice")
	if err != nil {
		t.Fatalf("MountForIdentity(alice): %v", err)
	}
	if got := alice.MCPServers["calculator"].Trust.Class; got != TrustSandboxedLocal {
		t.Fatalf("alice calculator trust = %q, want %q", got, TrustSandboxedLocal)
	}
	bob, err := MountForIdentity("bob")
	if err != nil {
		t.Fatalf("MountForIdentity(bob): %v", err)
	}
	if got := bob.MCPServers["calculator"].Trust.Class; got != TrustTrustedRecipe {
		t.Fatalf("bob calculator trust = %q, want shared %q", got, TrustTrustedRecipe)
	}
}

// Test 3c (D-04): a per-identity trust override on a class-(a) REMOTE
// (streamable_http) server must NOT be applied by MountForIdentity — the
// effective config keeps the shared-catalog trust, so the remote cannot be
// made runnable-via-override.
func TestMountForIdentityRemoteTrustOverrideIgnored(t *testing.T) {
	writeSharedCatalog(t)
	if err := SaveIdentityMCPConfig("alice", IdentityMCPConfig{
		Preferences: map[string]IdentityServerPref{
			"weather": {Trust: ManagedTrust{Class: TrustTrustedLocal}},
		},
	}); err != nil {
		t.Fatalf("SaveIdentityMCPConfig: %v", err)
	}
	alice, err := MountForIdentity("alice")
	if err != nil {
		t.Fatalf("MountForIdentity(alice): %v", err)
	}
	if got := alice.MCPServers["weather"].Trust.Class; got != TrustRemoteHTTP {
		t.Fatalf("alice weather trust = %q, want shared %q (remote trust must not be overridable, D-04)", got, TrustRemoteHTTP)
	}
}

// Test 3d (D-04): an enable toggle on a class-(a) REMOTE server still applies
// via MountForIdentity — the guard only scopes trust elevation, not enable.
func TestMountForIdentityRemoteEnableToggleApplies(t *testing.T) {
	writeSharedCatalog(t)
	if err := SetEnabledForIdentity("alice", "weather", false); err != nil {
		t.Fatalf("SetEnabledForIdentity: %v", err)
	}
	alice, err := MountForIdentity("alice")
	if err != nil {
		t.Fatalf("MountForIdentity(alice): %v", err)
	}
	if e := alice.MCPServers["weather"].Enabled; e == nil || *e {
		t.Fatalf("alice weather enabled = %v, want false (enable toggle unaffected by D-04 trust guard)", e)
	}
}

// Test 3e (D-04): SetTrustForIdentity on a class-(a) REMOTE server fails
// closed with ErrRemoteElevationForbidden and persists no overlay.
func TestSetTrustForIdentityRemoteElevationForbidden(t *testing.T) {
	writeSharedCatalog(t)
	if err := SetTrustForIdentity("alice", "weather", TrustTrustedLocal); !errors.Is(err, ErrRemoteElevationForbidden) {
		t.Fatalf("SetTrustForIdentity(weather) err = %v, want ErrRemoteElevationForbidden", err)
	}
	overlay, err := LoadIdentityMCPConfig("alice")
	if err != nil {
		t.Fatalf("LoadIdentityMCPConfig: %v", err)
	}
	if _, ok := overlay.Preferences["weather"]; ok {
		t.Fatalf("overlay should have no persisted preference for weather, got %#v", overlay.Preferences["weather"])
	}
}

// Test 4: the class-(b) shared agent-memory server cannot be toggled per-identity, and
// mount ignores any overlay pref for it (admin-governed from the shared catalog).
func TestClassBSharedServerNotUserToggleable(t *testing.T) {
	writeSharedCatalog(t)

	if err := SetEnabledForIdentity("alice", "memory", false); !errors.Is(err, ErrSharedAdminGoverned) {
		t.Fatalf("SetEnabledForIdentity(memory) err = %v, want ErrSharedAdminGoverned", err)
	}
	if err := SetTrustForIdentity("alice", "memory", TrustBlocked); !errors.Is(err, ErrSharedAdminGoverned) {
		t.Fatalf("SetTrustForIdentity(memory) err = %v, want ErrSharedAdminGoverned", err)
	}

	// Even a hand-written overlay pref for memory is ignored by mount (defense in depth).
	if err := SaveIdentityMCPConfig("alice", IdentityMCPConfig{
		Preferences: map[string]IdentityServerPref{
			"memory": {Enabled: new(false)},
		},
	}); err != nil {
		t.Fatalf("SaveIdentityMCPConfig: %v", err)
	}
	alice, err := MountForIdentity("alice")
	if err != nil {
		t.Fatalf("MountForIdentity(alice): %v", err)
	}
	if e := alice.MCPServers["memory"].Enabled; e == nil || !*e {
		t.Fatalf("alice memory enabled = %v, want true (admin-governed, overlay ignored)", e)
	}
}

// Test 4b: a per-identity toggle of a server absent from the shared catalog is refused.
func TestSetEnabledForIdentityUnknownServer(t *testing.T) {
	writeSharedCatalog(t)
	if err := SetEnabledForIdentity("alice", "ghost", false); !errors.Is(err, ErrUnknownServer) {
		t.Fatalf("SetEnabledForIdentity(ghost) err = %v, want ErrUnknownServer", err)
	}
}

// IsSharedAdminGoverned keys on the memory recipe Source, not the map name.
func TestIsSharedAdminGoverned(t *testing.T) {
	if !IsSharedAdminGoverned(ManagedServer{Source: SourceRecipeMemory}) {
		t.Fatal("memory recipe source should be shared admin-governed")
	}
	if IsSharedAdminGoverned(ManagedServer{Source: "recipe:calculator"}) {
		t.Fatal("calculator recipe should not be shared admin-governed")
	}
}
