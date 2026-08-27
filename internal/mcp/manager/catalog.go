package manager

import (
	"fmt"
	"os"
	"sort"
	"strconv"
	"strings"

	"github.com/chetto1983/aura/internal/mcp"
)

// memoryRecipeURL uses stable Compose DNS in the appliance and the loopback
// host publish for a native Aura process.
//
// The port derives from AURA_ARCADEDB_MCP_PORT, defaulting to 8096. It is
// validated as a TCP port (1-65535) before interpolation: anything else (e.g.
// "8096@evil.example" via the URL userinfo trick) would retarget the
// loopback-by-construction recipe off-host, so garbage falls back to 8096 (WR-01).
func memoryRecipeURL() string {
	if os.Getenv("AURA_IN_CONTAINER") == "1" {
		return "http://aura-arcadedb-mcp:8096/mcp/"
	}
	port := strings.TrimSpace(os.Getenv("AURA_ARCADEDB_MCP_PORT"))
	if n, err := strconv.Atoi(port); err != nil || n < 1 || n > 65535 {
		port = "8096"
	}
	return fmt.Sprintf("http://127.0.0.1:%s/mcp/", port)
}

// whatsappRecipeURL uses stable Compose DNS in the appliance and the loopback
// host publish for a native Aura process.
func whatsappRecipeURL() string {
	if os.Getenv("AURA_IN_CONTAINER") == "1" {
		return "http://whatsapp:8080/mcp/"
	}
	port := strings.TrimSpace(os.Getenv("AURA_WHATSAPP_MCP_PORT"))
	if n, err := strconv.Atoi(port); err != nil || n < 1 || n > 65535 {
		port = "8092"
	}
	return fmt.Sprintf("http://127.0.0.1:%s/mcp/", port)
}

// PIMSidecarBaseURL returns the PIM MCP/admin base. The appliance uses Compose DNS;
// a native Aura process uses the loopback-only host publish. WR-01 keeps an invalid
// host-side port from retargeting the URL through userinfo syntax.
func PIMSidecarBaseURL() string {
	if os.Getenv("AURA_IN_CONTAINER") == "1" {
		return "http://aura-pim-mcp:8080"
	}
	port := strings.TrimSpace(os.Getenv("AURA_PIM_MCP_PORT"))
	if n, err := strconv.Atoi(port); err != nil || n < 1 || n > 65535 {
		port = "8093"
	}
	return fmt.Sprintf("http://127.0.0.1:%s", port)
}

// calendarRecipeURL is the sidecar's MCP-over-HTTP endpoint — the server root "/".
func calendarRecipeURL() string {
	return PIMSidecarBaseURL() + "/"
}

// WhatsAppBridgeBaseURL returns the management REST base. The appliance uses
// Compose DNS; a native Aura process uses the loopback-only host publish.
func WhatsAppBridgeBaseURL() string {
	if os.Getenv("AURA_IN_CONTAINER") == "1" {
		return "http://whatsapp:8081"
	}
	port := strings.TrimSpace(os.Getenv("AURA_WHATSAPP_BRIDGE_PORT"))
	if n, err := strconv.Atoi(port); err != nil || n < 1 || n > 65535 {
		port = "8094"
	}
	return fmt.Sprintf("http://127.0.0.1:%s", port)
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
// be made to hold: the aura service mounts a named volume over /root/.cache/uv for
// the host-direct shell_exec path, and a named volume is seeded ONCE at creation and
// never refreshed, so the warmed cache was invisible to every image built after it.
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
			// MountManagedServer; Aura's trusted-recipe policy distinguishes reads,
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
			// Aura's own ArcadeDB MCP (cmd/arcadedb-mcp). Mounts the memory__* surface
			// Deferred + namespaced through the existing MountManagedServer
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
