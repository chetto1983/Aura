package manager

import (
	"reflect"
	"testing"

	"github.com/chetto1983/aura/internal/mcp"
)

func TestCatalogIncludesTrustedRecipesAndCalendarFixture(t *testing.T) {
	catalog := BuiltInCatalog()
	names := make([]string, 0, len(catalog))
	for _, entry := range catalog {
		names = append(names, entry.Name)
	}
	wantNames := []string{"calculator", "calendar", "mail", "memory", "whatsapp"}
	if !reflect.DeepEqual(names, wantNames) {
		t.Fatalf("catalog names = %#v, want %#v", names, wantNames)
	}

	calendar, ok := LookupCatalog("calendar")
	if !ok {
		t.Fatal("calendar recipe missing")
	}
	if calendar.Server.Trust.Class != mcp.TrustTrustedRecipe {
		t.Fatalf("calendar trust = %q, want %q", calendar.Server.Trust.Class, mcp.TrustTrustedRecipe)
	}
	if calendar.Server.Runtime.Kind != "local" {
		t.Fatalf("calendar runtime = %q, want local", calendar.Server.Runtime.Kind)
	}
	if !containsString(calendar.RequiredEnv, "AURA_CALENDAR_MODE=fixture") {
		t.Fatalf("calendar required env missing fixture mode: %#v", calendar.RequiredEnv)
	}
}

func TestCatalogIncludesMemoryStreamableHTTPRecipe(t *testing.T) {
	t.Setenv("AURA_AGENT_MEMORY_MCP_PORT", "") // literal-8091 assertion below; a sourced .env must not skew it (WR-06)
	memory, ok := LookupCatalog("memory")
	if !ok {
		t.Fatal("memory recipe missing from BuiltInCatalog")
	}
	if memory.TrustClass != mcp.TrustTrustedRecipe {
		t.Fatalf("memory TrustClass = %q, want %q (D-08 trusted)", memory.TrustClass, mcp.TrustTrustedRecipe)
	}
	if memory.Server.Trust.Class != mcp.TrustTrustedRecipe {
		t.Fatalf("memory Server.Trust.Class = %q, want %q", memory.Server.Trust.Class, mcp.TrustTrustedRecipe)
	}
	if memory.Server.Type != mcp.ServerTypeStreamableHTTP {
		t.Fatalf("memory Server.Type = %q, want %q", memory.Server.Type, mcp.ServerTypeStreamableHTTP)
	}
	if memory.Server.URL != "http://127.0.0.1:8091/mcp/" {
		t.Fatalf("memory Server.URL = %q, want loopback /mcp/ URL", memory.Server.URL)
	}
	if memory.Server.Command != "" {
		t.Fatalf("memory Server.Command = %q, want empty (HTTP recipe has no launch command)", memory.Server.Command)
	}
	if memory.Source != "recipe:memory" {
		t.Fatalf("memory Source = %q, want recipe:memory", memory.Source)
	}
}

func TestCatalogMemoryURLHonorsPortEnv(t *testing.T) {
	t.Setenv("AURA_AGENT_MEMORY_MCP_PORT", "9191")
	memory, ok := LookupCatalog("memory")
	if !ok {
		t.Fatal("memory recipe missing from BuiltInCatalog")
	}
	if memory.Server.URL != "http://127.0.0.1:9191/mcp/" {
		t.Fatalf("memory Server.URL = %q, want port from AURA_AGENT_MEMORY_MCP_PORT", memory.Server.URL)
	}
}

// TestCatalogMemoryURLRejectsNonPortEnv proves WR-01: a non-port value (userinfo
// trick, negative, overflow, junk) falls back to 8091 instead of being
// interpolated into the loopback URL.
func TestCatalogMemoryURLRejectsNonPortEnv(t *testing.T) {
	for _, bad := range []string{"8091@evil.example", "0", "65536", "-1", "junk", "80 91"} {
		t.Run(bad, func(t *testing.T) {
			t.Setenv("AURA_AGENT_MEMORY_MCP_PORT", bad)
			memory, ok := LookupCatalog("memory")
			if !ok {
				t.Fatal("memory recipe missing from BuiltInCatalog")
			}
			if memory.Server.URL != "http://127.0.0.1:8091/mcp/" {
				t.Fatalf("port %q produced URL %q, want fallback 8091 loopback", bad, memory.Server.URL)
			}
		})
	}
}

func TestLookupCatalogNotFound(t *testing.T) {
	entry, ok := LookupCatalog("does-not-exist")
	if ok {
		t.Fatalf("LookupCatalog(missing) ok = true, want false")
	}
	if !reflect.DeepEqual(entry, CatalogEntry{}) {
		t.Fatalf("LookupCatalog(missing) entry = %#v, want zero value", entry)
	}
}

func containsString(values []string, want string) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}
