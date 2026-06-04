package manager

import (
	"sort"

	"github.com/chetto1983/aura/internal/mcp"
)

type CatalogEntry struct {
	Name        string                `json:"name"`
	Summary     string                `json:"summary"`
	Source      string                `json:"source"`
	TrustClass  string                `json:"trustClass"`
	Runtime     string                `json:"runtime"`
	RequiredEnv []string              `json:"requiredEnv,omitempty"`
	RiskLabels  []string              `json:"riskLabels,omitempty"`
	ToolPolicy  mcp.ManagedToolPolicy `json:"toolPolicy,omitempty"`
	Server      mcp.ManagedServer     `json:"server"`
}

func BuiltInCatalog() []CatalogEntry {
	entries := []CatalogEntry{
		{
			Name:       "calculator",
			Summary:    "calculator-mcp-server over stdio via uvx",
			Source:     "recipe:calculator",
			TrustClass: mcp.TrustTrustedRecipe,
			Runtime:    "local",
			RiskLabels: []string{"read"},
			Server: mcp.ManagedServer{
				Command: "uvx",
				Args: []string{
					"--from",
					"calculator-mcp-server@git+https://github.com/chetto1983/calculator-mcp-server.git",
					"--",
					"calculator-mcp-server",
					"--stdio",
				},
				Env:        []string{"PYTHONUNBUFFERED=1"},
				Source:     "recipe:calculator",
				Trust:      mcp.ManagedTrust{Class: mcp.TrustTrustedRecipe},
				Runtime:    mcp.ManagedRuntime{Kind: "local"},
				RiskLabels: []string{"read"},
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
			RiskLabels: []string{"read", "private_data"},
			ToolPolicy: mcp.ManagedToolPolicy{
				Allow: []string{"list_calendars", "list_events", "search_events"},
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
				ToolPolicy: mcp.ManagedToolPolicy{
					Allow: []string{"list_calendars", "list_events", "search_events"},
				},
				RiskLabels: []string{"read", "private_data"},
			},
		},
		{
			Name:        "mail",
			Summary:     "martinzarfl/mail-mcp over stdio (SMTP/IMAP env config)",
			Source:      "recipe:mail",
			TrustClass:  mcp.TrustTrustedRecipe,
			Runtime:     "local",
			RequiredEnv: []string{"SMTP_HOST", "SMTP_PORT", "SMTP_USER", "SMTP_PASS", "SMTP_FROM"},
			RiskLabels:  []string{"read", "private_data", "external_send"},
			ToolPolicy: mcp.ManagedToolPolicy{
				Allow: []string{"send_email", "fetch_emails", "search_emails", "get_thread"},
			},
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
				Source:     "recipe:mail",
				Trust:      mcp.ManagedTrust{Class: mcp.TrustTrustedRecipe},
				Runtime:    mcp.ManagedRuntime{Kind: "local"},
				ToolPolicy: mcp.ManagedToolPolicy{Allow: []string{"send_email", "fetch_emails", "search_emails", "get_thread"}},
				RiskLabels: []string{"read", "private_data", "external_send"},
			},
		},
		{
			Name:       "whatsapp",
			Summary:    "chetto1983/whatsapp-mcp (whatsmeow bridge in WSL, stdio via wsl.exe)",
			Source:     "recipe:whatsapp",
			TrustClass: mcp.TrustTrustedRecipe,
			Runtime:    "local",
			RiskLabels: []string{"read", "private_data", "external_send"},
			ToolPolicy: mcp.ManagedToolPolicy{
				Allow: []string{"send_message", "list_messages", "list_chats", "search_contacts"},
			},
			Server: mcp.ManagedServer{
				Command: "wsl.exe",
				Args: []string{
					"-e", "bash", "-lc",
					"cd ~/whatsapp-mcp/whatsapp-mcp-server && uv run main.py",
				},
				Source:     "recipe:whatsapp",
				Trust:      mcp.ManagedTrust{Class: mcp.TrustTrustedRecipe},
				Runtime:    mcp.ManagedRuntime{Kind: "local"},
				ToolPolicy: mcp.ManagedToolPolicy{Allow: []string{"send_message", "list_messages", "list_chats", "search_contacts"}},
				RiskLabels: []string{"read", "private_data", "external_send"},
			},
		},
	}
	sort.Slice(entries, func(i, j int) bool { return entries[i].Name < entries[j].Name })
	return entries
}

func LookupCatalog(name string) (CatalogEntry, bool) {
	for _, entry := range BuiltInCatalog() {
		if entry.Name == name {
			return entry, true
		}
	}
	return CatalogEntry{}, false
}
