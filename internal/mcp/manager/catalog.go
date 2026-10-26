package manager

import (
	"os"
	"sort"
	"strconv"
	"strings"

	"github.com/chetto1983/aura/internal/mcp"
)

// sidecarBaseURL is a first-party sidecar's base URL: stable Compose DNS inside the appliance
// (AURA_IN_CONTAINER=1), the loopback host publish for a native Aura process.
//
// The host port comes from portEnv, validated as a TCP port (1-65535) before interpolation:
// anything else (e.g. "8096@evil.example" via the URL userinfo trick) would retarget the
// loopback-by-construction URL off-host, so garbage falls back to defaultPort (WR-01). One
// helper, because the four sidecars below each carried their own copy of this guard.
func sidecarBaseURL(containerURL, portEnv, defaultPort string) string {
	if os.Getenv("AURA_IN_CONTAINER") == "1" {
		return containerURL
	}
	port := strings.TrimSpace(os.Getenv(portEnv))
	if n, err := strconv.Atoi(port); err != nil || n < 1 || n > 65535 {
		port = defaultPort
	}
	return "http://127.0.0.1:" + port
}

// memoryRecipeURL is the ArcadeDB memory MCP endpoint (AURA_ARCADEDB_MCP_PORT, default 8096).
func memoryRecipeURL() string {
	return sidecarBaseURL("http://aura-arcadedb-mcp:8096", "AURA_ARCADEDB_MCP_PORT", "8096") + "/mcp/"
}

// whatsappRecipeURL is the WhatsApp bridge's MCP endpoint (AURA_WHATSAPP_MCP_PORT, default 8092).
func whatsappRecipeURL() string {
	return sidecarBaseURL("http://whatsapp:8080", "AURA_WHATSAPP_MCP_PORT", "8092") + "/mcp/"
}

// PIMSidecarBaseURL returns the PIM MCP/admin base (AURA_PIM_MCP_PORT, default 8093).
func PIMSidecarBaseURL() string {
	return sidecarBaseURL("http://aura-pim-mcp:8080", "AURA_PIM_MCP_PORT", "8093")
}

// calendarRecipeURL is the sidecar's MCP-over-HTTP endpoint — the server root "/".
func calendarRecipeURL() string {
	return PIMSidecarBaseURL() + "/"
}

// WhatsAppBridgeBaseURL returns the WhatsApp bridge's management REST base
// (AURA_WHATSAPP_BRIDGE_PORT, default 8094).
func WhatsAppBridgeBaseURL() string {
	return sidecarBaseURL("http://whatsapp:8081", "AURA_WHATSAPP_BRIDGE_PORT", "8094")
}

// CatalogEntry describes a built-in managed MCP server recipe, pairing
// LLM-facing metadata (summary, trust class, runtime) with the concrete
// mcp.ManagedServer launch spec used to install it.
type CatalogEntry struct {
	Name        string            `json:"name"`
	Summary     string            `json:"summary"`
	Source      string            `json:"source"`
	TrustClass  string            `json:"trustClass"`
	Runtime     string            `json:"runtime"`
	RequiredEnv []string          `json:"requiredEnv,omitempty"`
	Server      mcp.ManagedServer `json:"server"`
}

// BuiltInCatalog returns the curated set of shipped MCP server recipes
// (calendar, memory, whatsapp), sorted by name. The standalone mail recipe was
// retired once the calendar recipe became the unified PIM sidecar (forked
// calendar-mcp) — its send/search email tools subsume mail-mcp.
//
// The calculator recipe was retired too. It was the only recipe whose runtime
// depended on a uv cache warmed at image build time, and that dependency could not
// be made to hold: the aura service mounts a named volume over /root/.cache/uv, and a
// named volume is seeded ONCE at creation and never refreshed, so the warmed cache was
// invisible to every image built after it.
// With UV_OFFLINE=1 there was no network to fall back to, the stdio server died
// during initialize, and the host reported it as "recv: unexpected EOF" — a transport
// fault it was not. What it bought (symbolic math, stats, matrices) the agent already
// reaches through shell_exec and python.
func BuiltInCatalog() []CatalogEntry {
	entries := []CatalogEntry{
		{
			// Aura PIM sidecar (forked calendar-mcp → chetto1983/aura-pim-mcp):
			// unified mail + calendar + contacts over MCP-over-HTTP. The agent mounts
			// the surface Deferred + calendar__*-namespaced through the existing
			// MountManagedServerWithOptions; Aura's trusted-recipe policy distinguishes reads,
			// reversible writes, and externally irreversible sends independently of
			// server-provided hints. The fork also drops bulk destructive tools.
			// Trusted recipe, install-on-demand (NOT default-on like memory):
			// per-deployment OAuth connect is driven by the cockpit via the sidecar's
			// token-gated admin REST API, so Aura boot never depends on this service.
			Name:       "calendar",
			Summary:    "Aura PIM sidecar (forked calendar-mcp): mail+calendar+contacts over streamable-HTTP",
			Source:     "recipe:calendar",
			TrustClass: mcp.TrustTrustedRecipe,
			Runtime:    "local",
			Server: mcp.ManagedServer{
				Type:   mcp.ServerTypeStreamableHTTP,
				URL:    calendarRecipeURL(),
				Source: "recipe:calendar",
				Trust:  mcp.ManagedTrust{Class: mcp.TrustTrustedRecipe},
			},
		},
		{
			Name:       "whatsapp",
			Summary:    "chetto1983/whatsapp-mcp whatsmeow bridge sibling (FastMCP streamable-HTTP)",
			Source:     "recipe:whatsapp",
			TrustClass: mcp.TrustTrustedRecipe,
			Runtime:    "local",
			Server: mcp.ManagedServer{
				Type:   mcp.ServerTypeStreamableHTTP,
				URL:    whatsappRecipeURL(),
				Source: "recipe:whatsapp",
				Trust:  mcp.ManagedTrust{Class: mcp.TrustTrustedRecipe},
			},
		},
		{
			// agent-browser's own MCP server, shipped in docker/aura-sandbox. It runs in
			// each identity's box, one process per identity, so a login made there stays in
			// that identity's box and its tools reach nobody else's (prd.md §12). The box
			// image's entry point supplies the per-identity state key.
			Name:       browserRecipeName,
			Summary:    "agent-browser in the sandbox box: a real Chromium per identity, logins kept in its box",
			Source:     BrowserRecipeSource,
			TrustClass: mcp.TrustTrustedRecipe,
			Runtime:    mcp.RuntimeKindBox,
			Server: mcp.ManagedServer{
				Command: "agent-browser",
				Args:    []string{"mcp"},
				Source:  BrowserRecipeSource,
				Trust:   mcp.ManagedTrust{Class: mcp.TrustTrustedRecipe},
				Runtime: mcp.ManagedRuntime{Kind: mcp.RuntimeKindBox},
			},
		},
		{
			// Aura's own ArcadeDB MCP (cmd/arcadedb-mcp). Mounts the memory__* surface
			// Deferred + namespaced through the existing MountManagedServerWithOptions
			// (D-06/D-07). Trusted (NOT remote_http) so it can mount default-on
			// (D-08); the URL has no launch Command (HTTP recipe).
			//
			// Facts are bitemporal: a fact is never overwritten, its validity window
			// is closed, so both what is true now and what was true then stay
			// answerable.
			Name:       "memory",
			Summary:    "Aura ArcadeDB memory (bitemporal facts + entities, full-text and graph retrieval)",
			Source:     mcp.SourceRecipeMemory,
			TrustClass: mcp.TrustTrustedRecipe,
			Runtime:    "local",
			Server: mcp.ManagedServer{
				Type:   mcp.ServerTypeStreamableHTTP,
				URL:    memoryRecipeURL(),
				Source: mcp.SourceRecipeMemory,
				Trust:  mcp.ManagedTrust{Class: mcp.TrustTrustedRecipe},
			},
		},
	}
	entries = append(entries, craftCatalogEntries()...)
	sort.Slice(entries, func(i, j int) bool { return entries[i].Name < entries[j].Name })
	return entries
}

// LookupCatalog returns the built-in catalog entry with the given name,
// reporting whether a match was found.
func LookupCatalog(name string) (CatalogEntry, bool) {
	for _, entry := range BuiltInCatalog() {
		if entry.Name == name {
			return entry, true
		}
	}
	return CatalogEntry{}, false
}
