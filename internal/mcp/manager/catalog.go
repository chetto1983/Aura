package manager

import (
	"fmt"
	"os"
	"sort"
	"strings"

	"github.com/chetto1983/aura/internal/mcp"
)

// memoryRecipeURL composes the loopback streamable-HTTP URL for the agent-memory
// sidecar. The port derives from AURA_AGENT_MEMORY_MCP_PORT (the name compose.yaml
// already uses, AURA_<DOMAIN>_<UNIT> convention), defaulting to 8091 — no new env var.
func memoryRecipeURL() string {
	port := strings.TrimSpace(os.Getenv("AURA_AGENT_MEMORY_MCP_PORT"))
	if port == "" {
		port = "8091"
	}
	return fmt.Sprintf("http://127.0.0.1:%s/mcp/", port)
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
// (calculator, calendar, mail, memory, whatsapp), sorted by name.
func BuiltInCatalog() []CatalogEntry {
	entries := []CatalogEntry{
		{
			Name:       "calculator",
			Summary:    "calculator-mcp-server over stdio via uvx",
			Source:     "recipe:calculator",
			TrustClass: mcp.TrustTrustedRecipe,
			Runtime:    "local",
			Server: mcp.ManagedServer{
				Command: "uvx",
				Args: []string{
					"--from",
					"calculator-mcp-server@git+https://github.com/chetto1983/calculator-mcp-server.git",
					"--",
					"calculator-mcp-server",
					"--stdio",
				},
				Env:     []string{"PYTHONUNBUFFERED=1"},
				Source:  "recipe:calculator",
				Trust:   mcp.ManagedTrust{Class: mcp.TrustTrustedRecipe},
				Runtime: mcp.ManagedRuntime{Kind: "local"},
			},
		},
		{
			Name:       "calendar",
			Summary:    "Calendar MCP fixture recipe (local deterministic mode by default)",
			Source:     "recipe:calendar",
			TrustClass: mcp.TrustTrustedRecipe,
			Runtime:    "local",
			RequiredEnv: []string{
				"AURA_CALENDAR_MODE=fixture",
				"AURA_CALENDAR_FIXTURE=basic",
			},
			Server: mcp.ManagedServer{
				Command: "aura-calendar-mcp-fixture",
				Env: []string{
					"AURA_CALENDAR_MODE=fixture",
					"AURA_CALENDAR_FIXTURE=basic",
				},
				Source: "recipe:calendar",
				Trust:  mcp.ManagedTrust{Class: mcp.TrustTrustedRecipe},
				Runtime: mcp.ManagedRuntime{
					Kind: "local",
				},
			},
		},
		{
			Name:        "mail",
			Summary:     "martinzarfl/mail-mcp over stdio (SMTP/IMAP env config)",
			Source:      "recipe:mail",
			TrustClass:  mcp.TrustTrustedRecipe,
			Runtime:     "local",
			RequiredEnv: []string{"SMTP_HOST", "SMTP_PORT", "SMTP_USER", "SMTP_PASS", "SMTP_FROM"},
			Server: mcp.ManagedServer{
				Command: "npx",
				Args: []string{
					"-y",
					"github:martinzarfl/mail-mcp",
				},
				Env: []string{
					"SMTP_HOST=smtp.gmail.com",
					"SMTP_PORT=465",
					"SMTP_USER=you@example.com",
					"SMTP_PASS=CHANGE_ME_app_password",
					"SMTP_FROM=you@example.com",
				},
				Source:  "recipe:mail",
				Trust:   mcp.ManagedTrust{Class: mcp.TrustTrustedRecipe},
				Runtime: mcp.ManagedRuntime{Kind: "local"},
			},
		},
		{
			Name:       "whatsapp",
			Summary:    "chetto1983/whatsapp-mcp (whatsmeow bridge in WSL, stdio via wsl.exe)",
			Source:     "recipe:whatsapp",
			TrustClass: mcp.TrustTrustedRecipe,
			Runtime:    "local",
			Server: mcp.ManagedServer{
				Command: "wsl.exe",
				Args: []string{
					"-e", "bash", "-lc",
					"cd ~/whatsapp-mcp/whatsapp-mcp-server && uv run main.py",
				},
				Source:  "recipe:whatsapp",
				Trust:   mcp.ManagedTrust{Class: mcp.TrustTrustedRecipe},
				Runtime: mcp.ManagedRuntime{Kind: "local"},
			},
		},
		{
			// neo4j-labs agent-memory, the first HTTP recipe. Mounts the full 16-tool
			// surface Deferred + memory__*-namespaced through the existing
			// MountManagedServer (D-06/D-07). Trusted (NOT remote_http) so it can mount
			// default-on (D-08); the URL has no launch Command (HTTP recipe).
			Name:       "memory",
			Summary:    "neo4j-labs agent-memory (POLE+O + reasoning traces) over streamable-HTTP",
			Source:     "recipe:memory",
			TrustClass: mcp.TrustTrustedRecipe,
			Runtime:    "local",
			Server: mcp.ManagedServer{
				Type:   mcp.ServerTypeStreamableHTTP,
				URL:    memoryRecipeURL(),
				Source: "recipe:memory",
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
