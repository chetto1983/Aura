package manager

import (
	"reflect"
	"testing"

	"github.com/chetto1983/aura/internal/mcp"
)

// clearMCPEnv zeroes the MCP env knobs so the default-on seam is exercised from a known
// baseline regardless of the host shell.
func clearMCPEnv(t *testing.T) {
	t.Helper()
	t.Setenv("AURA_AGENT_MEMORY_MCP_PORT", "")
}

// runtimePolicies is what every test here asserts on: the policy half of RuntimeSet, which
// is where default-on injection lands.
//
// These tests used to reach it through config.LoadDB(), writing the document to a temp
// servers.json and letting internal/config read it back. The registry is Postgres now
// (migration 0101) and the composition takes the document directly, so the round-trip
// through a file is gone and with it the only reason these lived in internal/config.
func runtimePolicies(t *testing.T, doc mcp.ManagedConfig) map[string]mcp.ManagedServer {
	t.Helper()
	_, policies, err := RuntimeSet(doc)
	if err != nil {
		t.Fatalf("RuntimeSet err = %v, want nil", err)
	}
	return policies
}

// TestMemoryDefaultOn proves a fresh machine (empty/absent AURA_MCP_CONFIG) mounts
// the memory recipe by default — no prior `aura mcp install` (D-08).
func TestMemoryDefaultOn(t *testing.T) {
	clearMCPEnv(t)

	policies := runtimePolicies(t, mcp.ManagedConfig{})
	got, ok := policies["memory"]
	if !ok {
		t.Fatalf("MCPPolicies missing memory on a fresh machine (default-on, D-08): %#v", policies)
	}
	want, ok := LookupCatalog("memory")
	if !ok {
		t.Fatal("LookupCatalog(\"memory\") not found — recipe must exist (Task 1)")
	}
	if !reflect.DeepEqual(got, want.Server) {
		t.Fatalf("memory policy = %#v, want LookupCatalog(\"memory\").Server %#v", got, want.Server)
	}
}

// These four cover the container default-on semantics. They used to run against the
// calculator recipe, which is retired (see BuiltInCatalog); calendar carries
// them now because it is the other in-container default-on whose sidecar ships in the same
// compose file. The semantics under test are the recipe-independent part: default-on
// injects, an explicit disable still wins, an env override still wins, and an
// operator-customized row is never overwritten.

// TestCalendarContainerDefaultOn proves the Aura appliance mounts the calendar recipe by
// default in-container, without requiring a prior `aura mcp install calendar`.
func TestCalendarContainerDefaultOn(t *testing.T) {
	clearMCPEnv(t)
	t.Setenv("AURA_IN_CONTAINER", "1")

	policies := runtimePolicies(t, mcp.ManagedConfig{})
	got, ok := policies["calendar"]
	if !ok {
		t.Fatalf("MCPPolicies missing calendar in Aura container: %#v", policies)
	}
	want, ok := LookupCatalog("calendar")
	if !ok {
		t.Fatal("LookupCatalog(\"calendar\") not found")
	}
	if !reflect.DeepEqual(got, want.Server) {
		t.Fatalf("calendar policy = %#v, want LookupCatalog(\"calendar\").Server %#v", got, want.Server)
	}
}

func TestCalendarDefaultOn_RespectsDisable(t *testing.T) {
	clearMCPEnv(t)
	t.Setenv("AURA_IN_CONTAINER", "1")

	disabled := false
	doc := mcp.ManagedConfig{MCPServers: map[string]mcp.ManagedServer{
		"calendar": {
			Type:    mcp.ServerTypeStreamableHTTP,
			URL:     "http://aura-pim-mcp:8080/",
			Source:  "recipe:calendar",
			Enabled: &disabled,
			Trust:   mcp.ManagedTrust{Class: mcp.TrustTrustedRecipe},
		},
	}}

	policies := runtimePolicies(t, doc)
	if _, ok := policies["calendar"]; ok {
		t.Fatalf("calendar mounted despite explicit disable: %#v", policies)
	}
}

// TestCalendarContainerDefaultOn_RespectsExplicitInstall proves an operator-customized
// calendar entry wins in-container — the default-on inject sees the existing managed/policy
// row and does not overwrite the custom URL.
func TestCalendarContainerDefaultOn_RespectsExplicitInstall(t *testing.T) {
	clearMCPEnv(t)
	t.Setenv("AURA_IN_CONTAINER", "1")

	doc := mcp.ManagedConfig{MCPServers: map[string]mcp.ManagedServer{
		"calendar": {
			Type:   mcp.ServerTypeStreamableHTTP,
			URL:    "http://operator-pim:9999/custom/",
			Source: "recipe:calendar",
			Trust:  mcp.ManagedTrust{Class: mcp.TrustTrustedRecipe},
		},
	}}

	policies := runtimePolicies(t, doc)
	got, ok := policies["calendar"]
	if !ok {
		t.Fatalf("explicit calendar install not mounted: %#v", policies)
	}
	if got.URL != "http://operator-pim:9999/custom/" {
		t.Fatalf("calendar URL = %q, want the operator-customized one (explicit wins)", got.URL)
	}
}

// TestMemoryDefaultOn_RespectsDisable proves `aura mcp disable memory`
// (Enabled=false in servers.json) keeps memory unmounted (D-09 respect disable).
func TestMemoryDefaultOn_RespectsDisable(t *testing.T) {
	clearMCPEnv(t)

	disabled := false
	doc := mcp.ManagedConfig{MCPServers: map[string]mcp.ManagedServer{
		"memory": {
			Type:    mcp.ServerTypeStreamableHTTP,
			URL:     "http://127.0.0.1:8091/mcp/",
			Source:  "recipe:memory",
			Enabled: &disabled,
			Trust:   mcp.ManagedTrust{Class: mcp.TrustTrustedRecipe},
		},
	}}

	policies := runtimePolicies(t, doc)
	if _, ok := policies["memory"]; ok {
		t.Fatalf("memory mounted despite explicit disable: %#v", policies)
	}
}

// TestMemoryDefaultOn_RespectsExplicitInstall proves an operator-customized memory
// server wins — the default-on inject does not override an explicit URL (D-08).
func TestMemoryDefaultOn_RespectsExplicitInstall(t *testing.T) {
	clearMCPEnv(t)

	const customURL = "http://127.0.0.1:18091/mcp/"
	doc := mcp.ManagedConfig{MCPServers: map[string]mcp.ManagedServer{
		"memory": {
			Type:   mcp.ServerTypeStreamableHTTP,
			URL:    customURL,
			Source: "recipe:memory",
			Trust:  mcp.ManagedTrust{Class: mcp.TrustTrustedRecipe},
		},
	}}

	policies := runtimePolicies(t, doc)
	got, ok := policies["memory"]
	if !ok {
		t.Fatalf("explicit memory install not mounted: %#v", policies)
	}
	if got.URL != customURL {
		t.Fatalf("memory URL = %q, want operator-customized %q (explicit wins)", got.URL, customURL)
	}
}

// TestMemoryDefaultOn_RespectsProfileExclusion proves the CR-01 fix: an explicit
// memory entry (custom URL) excluded by the ACTIVE profile must stay unmounted —
// the inject must consult the unfiltered managed doc, never re-add memory at the
// catalog URL behind the operator's profile selection.
func TestMemoryDefaultOn_RespectsProfileExclusion(t *testing.T) {
	clearMCPEnv(t)

	doc := mcp.ManagedConfig{
		ActiveProfile: "work",
		Profiles: map[string]mcp.ManagedProfile{
			"work": {Servers: []string{"other"}},
		},
		MCPServers: map[string]mcp.ManagedServer{
			"memory": {
				Type:   mcp.ServerTypeStreamableHTTP,
				URL:    "http://127.0.0.1:18091/mcp/",
				Source: "recipe:memory",
				Trust:  mcp.ManagedTrust{Class: mcp.TrustTrustedRecipe},
			},
			"other": {
				Type:   mcp.ServerTypeStreamableHTTP,
				URL:    "http://127.0.0.1:19000/mcp/",
				Source: "recipe:other",
				Trust:  mcp.ManagedTrust{Class: mcp.TrustTrustedRecipe},
			},
		},
	}

	policies := runtimePolicies(t, doc)
	if got, ok := policies["memory"]; ok {
		t.Fatalf("memory mounted (URL %q) despite active-profile exclusion (CR-01): %#v", got.URL, policies)
	}
	if _, ok := policies["other"]; !ok {
		t.Fatalf("profile-selected server missing: %#v", policies)
	}
}

// TestSidecarRecipesContainerDefaultOn proves the appliance mounts calendar and whatsapp
// out of the box, like calculator and memory already did.
//
// The regression it guards was invisible from the operator's seat: the cockpit's Connect
// panel pairs the device — scan the QR, WhatsApp reports linked — but pairing is not
// mounting, and nothing in that flow ran `aura mcp install whatsapp`. A live appliance sat
// with `paired: true` on the bridge and ZERO WhatsApp tools in the agent, with no error on
// either side to explain it. Calendar had the identical gap.
func TestSidecarRecipesContainerDefaultOn(t *testing.T) {
	for _, name := range []string{"calendar", "whatsapp"} {
		t.Run(name, func(t *testing.T) {
			clearMCPEnv(t)
			t.Setenv("AURA_IN_CONTAINER", "1")

			policies := runtimePolicies(t, mcp.ManagedConfig{})
			got, ok := policies[name]
			if !ok {
				t.Fatalf("policies missing %s in the appliance container — a paired device with no tools is exactly the trap this closes: %#v", name, policies)
			}
			want, ok := LookupCatalog(name)
			if !ok {
				t.Fatalf("LookupCatalog(%q) not found", name)
			}
			if !reflect.DeepEqual(got, want.Server) {
				t.Fatalf("%s policy = %#v, want LookupCatalog(%q).Server %#v", name, got, name, want.Server)
			}
		})
	}
}

// TestSidecarRecipesNotDefaultOnOutsideContainer keeps a dev host clean: these recipes
// point at compose sidecars that only exist in the appliance stack, so mounting them on a
// laptop would just log a failed dial every boot.
func TestSidecarRecipesNotDefaultOnOutsideContainer(t *testing.T) {
	clearMCPEnv(t)
	t.Setenv("AURA_IN_CONTAINER", "")

	policies := runtimePolicies(t, mcp.ManagedConfig{})
	for _, name := range []string{"calendar", "whatsapp", "calculator"} {
		if _, ok := policies[name]; ok {
			t.Fatalf("%s default-on outside the container: %#v", name, policies)
		}
	}
	// Memory is the deliberate exception: a core capability on every host.
	if _, ok := policies["memory"]; !ok {
		t.Fatalf("memory must stay default-on everywhere: %#v", policies)
	}
}

// TestWhatsAppDefaultOn_RespectsDisable proves default-on never overrides the operator:
// an explicit `aura mcp disable whatsapp` still keeps it unmounted.
func TestWhatsAppDefaultOn_RespectsDisable(t *testing.T) {
	clearMCPEnv(t)
	t.Setenv("AURA_IN_CONTAINER", "1")

	disabled := false
	doc := mcp.ManagedConfig{MCPServers: map[string]mcp.ManagedServer{
		"whatsapp": {
			Type:    mcp.ServerTypeStreamableHTTP,
			URL:     "http://whatsapp:8080/mcp",
			Source:  "recipe:whatsapp",
			Enabled: &disabled,
			Trust:   mcp.ManagedTrust{Class: mcp.TrustTrustedRecipe},
		},
	}}

	policies := runtimePolicies(t, doc)
	if _, ok := policies["whatsapp"]; ok {
		t.Fatalf("whatsapp mounted despite explicit disable: %#v", policies)
	}
}
