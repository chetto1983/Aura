package api

import (
	"net/http"
	"sort"
)

func handleMCPServers(deps Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		out := make([]MCPServerSummary, 0, len(deps.MCP))
		for _, c := range deps.MCP {
			if c == nil {
				continue
			}
			tools := c.Tools()
			summary := MCPServerSummary{
				Name:      c.Name(),
				Transport: c.Transport(),
				ToolCount: len(tools),
				Tools:     make([]MCPToolInfo, 0, len(tools)),
			}
			for _, tool := range tools {
				summary.Tools = append(summary.Tools, MCPToolInfo{
					Name:        tool.Name,
					Description: tool.Description,
					InputSchema: tool.InputSchema,
				})
			}
			sort.Slice(summary.Tools, func(i, j int) bool {
				return summary.Tools[i].Name < summary.Tools[j].Name
			})
			out = append(out, summary)
		}
		sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
		writeJSON(w, deps.Logger, http.StatusOK, out)
	}
}

func handleMCPProviders(deps Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		out := append([]ConnectorProviderSummary(nil), mcpProviderManifests...)
		sort.Slice(out, func(i, j int) bool {
			if out[i].Kind == out[j].Kind {
				return out[i].Name < out[j].Name
			}
			return out[i].Kind < out[j].Kind
		})
		writeJSON(w, deps.Logger, http.StatusOK, out)
	}
}

var mcpProviderManifests = []ConnectorProviderSummary{
	{
		ID:            "mail-mcp",
		Name:          "mail_mcp",
		Kind:          "mail",
		Profile:       "personal",
		Description:   "Configurazione guidata per Gmail, Outlook e caselle IMAP tramite il runtime MCP mail incluso.",
		Status:        "not_configured",
		RuntimeType:   "container",
		RepositoryURL: "https://github.com/tecnologicachile/mail-mcp",
		MCPServerNames: []string{
			"mail-mcp",
			"mail",
		},
		Capabilities: []ConnectorCapability{
			{ID: "mail.accounts", Label: "Account", Description: "Elenca account e stato provider."},
			{ID: "mail.search", Label: "Cerca mail", Description: "Cerca messaggi con filtri e paginazione."},
			{ID: "mail.read", Label: "Leggi mail", Description: "Legge messaggi e thread."},
			{ID: "mail.draft_reply", Label: "Bozze", Description: "Prepara risposte da revisionare.", ReviewRequired: true},
			{ID: "mail.extract_tasks", Label: "Task", Description: "Estrae scadenze e follow-up."},
		},
		RiskBadges: []ConnectorRiskBadge{
			{ID: "reads_mail", Label: "legge mail", Level: "high"},
			{ID: "drafts_mail", Label: "prepara bozze", Level: "medium"},
			{ID: "writes_blocked", Label: "scrittura bloccata", Level: "low"},
		},
		RequiredSecrets: []string{"MAIL_* account credentials or OAuth refresh token"},
		ApprovedTools: []string{
			"list_all_accounts",
			"imap_verify_account",
			"imap_list_mailboxes",
			"imap_search_messages",
			"imap_get_message",
			"ews_search_messages",
			"ews_get_message",
		},
		BlockedTools: []string{
			"smtp_send_message",
			"smtp_reply_message",
			"smtp_forward_message",
			"graph_send_message",
			"ews_send_message",
			"imap_delete_message",
			"imap_bulk_delete",
			"imap_bulk_move",
			"imap_bulk_update_flags",
		},
		SetupHints: []string{
			"Configura dalla dashboard: provider, email, IMAP e app password.",
			"Il primo profilo resta read-only; invio ed eliminazione sono bloccati.",
		},
	},
	{
		ID:            "database",
		Name:          "database",
		Kind:          "database",
		Profile:       "business",
		Description:   "Configurazione guidata per SQLite, PostgreSQL, MySQL e SQL Server tramite ExecuteAutomation MCP.",
		Status:        "not_configured",
		RuntimeType:   "container",
		RepositoryURL: "https://github.com/executeautomation/mcp-database-server",
		MCPServerNames: []string{
			"database",
			"db",
		},
		Capabilities: []ConnectorCapability{
			{ID: "database.list_tables", Label: "Tabelle", Description: "Elenca tabelle disponibili."},
			{ID: "database.describe_table", Label: "Schema", Description: "Legge schema tabella."},
			{ID: "database.read_query", Label: "Query read-only", Description: "Esegue SELECT/export controllati."},
		},
		RiskBadges: []ConnectorRiskBadge{
			{ID: "reads_database", Label: "legge database", Level: "high"},
			{ID: "writes_blocked", Label: "scrittura bloccata", Level: "low"},
			{ID: "schema_mutation_blocked", Label: "schema bloccato", Level: "low"},
		},
		RequiredSecrets: []string{"database host/path and optional credentials"},
		ApprovedTools:   []string{"list_tables", "describe_table", "read_query", "export_query"},
		BlockedTools:    []string{"write_query", "create_table", "alter_table", "drop_table"},
		SetupHints:      []string{"Configura dalla dashboard. Aura usa solo query read-only nella card connettore."},
	},
}
