package manager

import (
	"reflect"
	"slices"
	"strings"
	"testing"

	"github.com/chetto1983/aura/internal/mcp"
)

func TestCatalogIncludesTrustedRecipesAndCalendarHTTPRecipe(t *testing.T) {
	t.Setenv("AURA_PIM_MCP_PORT", "") // literal-8093 assertion below; a sourced .env must not skew it (WR-06)
	catalog := BuiltInCatalog()
	names := make([]string, 0, len(catalog))
	for _, entry := range catalog {
		names = append(names, entry.Name)
	}
	// The standalone mail recipe was retired — the calendar PIM sidecar subsumes it.
	// The calculator recipe was retired too: it was the only one whose runtime depended on
	// a uv cache warmed at image build time, and compose mounts a named volume over
	// /root/.cache/uv, which is seeded once and never refreshed — so no later image could
	// reach its own warm cache. See BuiltInCatalog.
	wantNames := []string{"calendar", "memory", "whatsapp"}
	if !reflect.DeepEqual(names, wantNames) {
		t.Fatalf("catalog names = %#v, want %#v", names, wantNames)
	}

	calendar, ok := LookupCatalog("calendar")
	if !ok {
		t.Fatal("calendar recipe missing")
	}
	if calendar.TrustClass != mcp.TrustTrustedRecipe {
		t.Fatalf("calendar TrustClass = %q, want %q", calendar.TrustClass, mcp.TrustTrustedRecipe)
	}
	if calendar.Server.Trust.Class != mcp.TrustTrustedRecipe {
		t.Fatalf("calendar trust = %q, want %q", calendar.Server.Trust.Class, mcp.TrustTrustedRecipe)
	}
	if calendar.Runtime != "local" {
		t.Fatalf("calendar runtime = %q, want local", calendar.Runtime)
	}
	if calendar.Server.Type != mcp.ServerTypeStreamableHTTP {
		t.Fatalf("calendar Server.Type = %q, want %q", calendar.Server.Type, mcp.ServerTypeStreamableHTTP)
	}
	if calendar.Server.URL != "http://127.0.0.1:8093/" {
		t.Fatalf("calendar Server.URL = %q, want loopback root / URL", calendar.Server.URL)
	}
	if calendar.Server.Command != "" {
		t.Fatalf("calendar Server.Command = %q, want empty (HTTP recipe has no launch command)", calendar.Server.Command)
	}
	if calendar.Server.Source != "recipe:calendar" {
		t.Fatalf("calendar Source = %q, want recipe:calendar", calendar.Server.Source)
	}
}

func TestCatalogCalendarURLHonorsPortEnv(t *testing.T) {
	t.Setenv("AURA_PIM_MCP_PORT", "9193")
	if got := calendarRecipeURL(); got != "http://127.0.0.1:9193/" {
		t.Fatalf("calendarRecipeURL() = %q, want configured loopback port", got)
	}
}

func TestCatalogCalendarURLRejectsNonPortEnv(t *testing.T) {
	for _, bad := range []string{"8093@evil.example", "0", "65536", "-1", "junk", "80 93"} {
		t.Run(bad, func(t *testing.T) {
			t.Setenv("AURA_PIM_MCP_PORT", bad)
			if got := calendarRecipeURL(); got != "http://127.0.0.1:8093/" {
				t.Fatalf("port %q produced URL %q, want fallback 8093 loopback", bad, got)
			}
		})
	}
}

func TestCatalogIncludesMemoryStreamableHTTPRecipe(t *testing.T) {
	t.Setenv("AURA_ARCADEDB_MCP_PORT", "") // literal-8096 assertion below; a sourced .env must not skew it (WR-06)
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
	if memory.Server.URL != "http://127.0.0.1:8096/mcp/" {
		t.Fatalf("memory Server.URL = %q, want the ArcadeDB loopback /mcp/ URL", memory.Server.URL)
	}
	if memory.Server.Command != "" {
		t.Fatalf("memory Server.Command = %q, want empty (HTTP recipe has no launch command)", memory.Server.Command)
	}
	if memory.Source != "recipe:memory" {
		t.Fatalf("memory Source = %q, want recipe:memory", memory.Source)
	}
}

func TestCatalogMemoryURLHonorsPortEnv(t *testing.T) {
	t.Setenv("AURA_ARCADEDB_MCP_PORT", "9191")
	memory, ok := LookupCatalog("memory")
	if !ok {
		t.Fatal("memory recipe missing from BuiltInCatalog")
	}
	if memory.Server.URL != "http://127.0.0.1:9191/mcp/" {
		t.Fatalf("memory Server.URL = %q, want port from AURA_ARCADEDB_MCP_PORT", memory.Server.URL)
	}
}

func TestCatalogHTTPRecipeURLsRemainLoopbackInContainer(t *testing.T) {
	t.Setenv("AURA_IN_CONTAINER", "1")
	t.Setenv("AURA_ARCADEDB_MCP_PORT", "9191")
	t.Setenv("AURA_WHATSAPP_MCP_PORT", "9192")
	t.Setenv("AURA_PIM_MCP_PORT", "9193")

	memory, ok := LookupCatalog("memory")
	if !ok {
		t.Fatal("memory recipe missing from BuiltInCatalog")
	}
	if memory.Server.URL != "http://127.0.0.1:9191/mcp/" {
		t.Fatalf("memory Server.URL = %q, want shared-namespace loopback URL", memory.Server.URL)
	}

	whatsapp, ok := LookupCatalog("whatsapp")
	if !ok {
		t.Fatal("whatsapp recipe missing from BuiltInCatalog")
	}
	if whatsapp.Server.URL != "http://127.0.0.1:9192/mcp/" {
		t.Fatalf("whatsapp Server.URL = %q, want shared-namespace loopback URL", whatsapp.Server.URL)
	}

	calendar, ok := LookupCatalog("calendar")
	if !ok {
		t.Fatal("calendar recipe missing from BuiltInCatalog")
	}
	if calendar.Server.URL != "http://127.0.0.1:9193/" {
		t.Fatalf("calendar Server.URL = %q, want shared-namespace loopback URL", calendar.Server.URL)
	}
}

// TestCatalogMemoryURLRejectsNonPortEnv proves WR-01: a non-port value (userinfo
// trick, negative, overflow, junk) falls back to 8096 instead of being
// interpolated into the loopback URL.
func TestCatalogMemoryURLRejectsNonPortEnv(t *testing.T) {
	for _, bad := range []string{"8096@evil.example", "0", "65536", "-1", "junk", "80 96"} {
		t.Run(bad, func(t *testing.T) {
			t.Setenv("AURA_ARCADEDB_MCP_PORT", bad)
			memory, ok := LookupCatalog("memory")
			if !ok {
				t.Fatal("memory recipe missing from BuiltInCatalog")
			}
			if memory.Server.URL != "http://127.0.0.1:8096/mcp/" {
				t.Fatalf("port %q produced URL %q, want fallback 8096 loopback", bad, memory.Server.URL)
			}
		})
	}
}

func TestCatalogIncludesWhatsappStreamableHTTPRecipe(t *testing.T) {
	t.Setenv("AURA_WHATSAPP_MCP_PORT", "")

	whatsapp, ok := LookupCatalog("whatsapp")
	if !ok {
		t.Fatal("whatsapp recipe missing from BuiltInCatalog")
	}
	if whatsapp.TrustClass != mcp.TrustTrustedRecipe {
		t.Fatalf("whatsapp TrustClass = %q, want %q", whatsapp.TrustClass, mcp.TrustTrustedRecipe)
	}
	if whatsapp.Server.Trust.Class != mcp.TrustTrustedRecipe {
		t.Fatalf("whatsapp Server.Trust.Class = %q, want %q", whatsapp.Server.Trust.Class, mcp.TrustTrustedRecipe)
	}
	if whatsapp.Server.Type != mcp.ServerTypeStreamableHTTP {
		t.Fatalf("whatsapp Server.Type = %q, want %q", whatsapp.Server.Type, mcp.ServerTypeStreamableHTTP)
	}
	if whatsapp.Server.URL != "http://127.0.0.1:8092/mcp/" {
		t.Fatalf("whatsapp Server.URL = %q, want loopback /mcp/ URL", whatsapp.Server.URL)
	}
	if whatsapp.Server.Command != "" {
		t.Fatalf("whatsapp Server.Command = %q, want empty for HTTP recipe", whatsapp.Server.Command)
	}
	if strings.Contains(strings.ToLower(whatsapp.Summary), "wsl") {
		t.Fatalf("whatsapp Summary still references WSL: %q", whatsapp.Summary)
	}
}

func TestCatalogWhatsappURLHonorsPortEnv(t *testing.T) {
	t.Setenv("AURA_WHATSAPP_MCP_PORT", "9192")

	if got := whatsappRecipeURL(); got != "http://127.0.0.1:9192/mcp/" {
		t.Fatalf("whatsappRecipeURL() = %q, want configured loopback port", got)
	}
}

func TestCatalogWhatsappURLRejectsNonPortEnv(t *testing.T) {
	for _, bad := range []string{"8092@evil.example", "0", "65536", "-1", "junk", "80 92"} {
		t.Run(bad, func(t *testing.T) {
			t.Setenv("AURA_WHATSAPP_MCP_PORT", bad)
			if got := whatsappRecipeURL(); got != "http://127.0.0.1:8092/mcp/" {
				t.Fatalf("port %q produced URL %q, want fallback 8092 loopback", bad, got)
			}
		})
	}
}

func TestWhatsAppBridgeBaseURLRemainsLoopbackInContainer(t *testing.T) {
	t.Setenv("AURA_IN_CONTAINER", "1")
	t.Setenv("AURA_WHATSAPP_BRIDGE_PORT", "9194")

	if got := WhatsAppBridgeBaseURL(); got != "http://127.0.0.1:9194" {
		t.Fatalf("WhatsAppBridgeBaseURL() = %q, want shared-namespace loopback", got)
	}
}

func TestWhatsAppBridgeBaseURLHonorsPortEnv(t *testing.T) {
	t.Setenv("AURA_WHATSAPP_BRIDGE_PORT", "9194")

	if got := WhatsAppBridgeBaseURL(); got != "http://127.0.0.1:9194" {
		t.Fatalf("WhatsAppBridgeBaseURL() = %q, want configured loopback port", got)
	}
}

func TestWhatsAppBridgeBaseURLRejectsNonPortEnv(t *testing.T) {
	for _, bad := range []string{"8094@evil.example", "0", "65536", "-1", "junk", "80 94"} {
		t.Run(bad, func(t *testing.T) {
			t.Setenv("AURA_WHATSAPP_BRIDGE_PORT", bad)
			if got := WhatsAppBridgeBaseURL(); got != "http://127.0.0.1:8094" {
				t.Fatalf("port %q produced URL %q, want fallback 8094 loopback", bad, got)
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
	return slices.Contains(values, want)
}

// No recipe may depend on a uv cache warmed at image build time. That was the calculator's
// design and it could not hold: the aura service mounts a named volume over /root/.cache/uv
// for the host-direct shell_exec path, a named volume is seeded ONCE at creation and never
// refreshed, so every image built after that moment warmed a cache the container could not
// see. Paired with UV_OFFLINE=1 there was no network to fall back to, the stdio server died
// during initialize, and the host surfaced it as "recv: unexpected EOF" — which reads like a
// transport fault and is not one. The recipe is retired; this keeps the shape from coming
// back under another name.
func TestNoRecipeDependsOnABuildTimeUVCache(t *testing.T) {
	for _, entry := range BuiltInCatalog() {
		if slices.Contains(entry.Server.Env, "UV_OFFLINE=1") {
			t.Fatalf("recipe %q sets UV_OFFLINE=1, so it can only resolve from a build-time uv cache — "+
				"which the /root/.cache/uv volume shadows on every image after the first", entry.Name)
		}
	}
}
