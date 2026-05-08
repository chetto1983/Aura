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
		Name:          "mail-mcp",
		Kind:          "mail",
		Profile:       "personal",
		Description:   "Provider mail generico per IMAP, SMTP, EWS, Microsoft Graph, Gmail e Microsoft 365.",
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
			"Prefer container runtime for production.",
			"Start read-only; enable send/delete in a later reviewed slice.",
		},
	},
	{
		ID:            "google-workspace",
		Name:          "Google Workspace",
		Kind:          "mail",
		Profile:       "personal",
		Description:   "Workspace provider per Gmail, Calendar, Drive e account routing.",
		Status:        "not_configured",
		RuntimeType:   "stdio",
		RepositoryURL: "https://github.com/aaronsb/google-workspace-mcp",
		MCPServerNames: []string{
			"google-workspace",
			"workspace",
			"gmail",
		},
		Capabilities: []ConnectorCapability{
			{ID: "mail.accounts", Label: "Account"},
			{ID: "mail.search", Label: "Cerca Gmail"},
			{ID: "mail.read", Label: "Leggi Gmail"},
			{ID: "mail.draft_reply", Label: "Bozze Gmail", ReviewRequired: true},
		},
		RiskBadges: []ConnectorRiskBadge{
			{ID: "reads_mail", Label: "legge Gmail", Level: "high"},
			{ID: "workspace_scope", Label: "scope Workspace", Level: "medium"},
			{ID: "writes_blocked", Label: "scrittura bloccata", Level: "low"},
		},
		RequiredSecrets: []string{"GOOGLE_CLIENT_ID", "GOOGLE_CLIENT_SECRET"},
		ApprovedTools:   []string{"manage_accounts", "manage_email"},
		BlockedTools:    []string{"manage_drive write operations", "manage_calendar write operations"},
		SetupHints:      []string{"Use only managed email operations in the first slice."},
	},
	{
		ID:            "gmail-dedicated",
		Name:          "Gmail dedicated",
		Kind:          "mail",
		Profile:       "personal",
		Description:   "Fallback Gmail con multi-account, query Gmail, labels, archive e unsubscribe.",
		Status:        "not_configured",
		RuntimeType:   "container",
		RepositoryURL: "https://github.com/navbuildz/gmail-mcp-server",
		MCPServerNames: []string{
			"gmail-dedicated",
			"gmail",
		},
		Capabilities: []ConnectorCapability{
			{ID: "mail.accounts", Label: "Account Gmail"},
			{ID: "mail.search", Label: "Cerca Gmail"},
			{ID: "mail.read", Label: "Leggi Gmail"},
			{ID: "mail.label", Label: "Etichette", ReviewRequired: true},
			{ID: "mail.archive", Label: "Archivia", ReviewRequired: true},
		},
		RiskBadges: []ConnectorRiskBadge{
			{ID: "reads_mail", Label: "legge Gmail", Level: "high"},
			{ID: "mailbox_mutation", Label: "mutazioni bloccate", Level: "medium"},
		},
		RequiredSecrets: []string{"Google OAuth credentials"},
		ApprovedTools:   []string{"list_accounts", "list_emails", "get_email", "batch_process"},
		BlockedTools:    []string{"archive_email", "apply_label", "unsubscribe_email"},
		SetupHints:      []string{"Use as Gmail fallback if the generic provider is too broad."},
	},
	{
		ID:            "outlook-assistant",
		Name:          "Outlook Assistant",
		Kind:          "mail",
		Profile:       "business",
		Description:   "Provider Outlook per email, calendario, contatti e mailbox tramite Microsoft Graph.",
		Status:        "not_configured",
		RuntimeType:   "stdio",
		RepositoryURL: "https://github.com/littlebearapps/outlook-assistant",
		MCPServerNames: []string{
			"outlook-assistant",
			"outlook",
			"mail",
		},
		Capabilities: []ConnectorCapability{
			{ID: "mail.accounts", Label: "Account Microsoft"},
			{ID: "mail.search", Label: "Cerca Outlook"},
			{ID: "mail.read", Label: "Leggi Outlook"},
			{ID: "mail.draft_reply", Label: "Bozze Outlook", ReviewRequired: true},
		},
		RiskBadges: []ConnectorRiskBadge{
			{ID: "reads_mail", Label: "legge Outlook", Level: "high"},
			{ID: "graph_scopes", Label: "scope Graph", Level: "medium"},
			{ID: "writes_blocked", Label: "scrittura bloccata", Level: "low"},
		},
		RequiredSecrets: []string{"OUTLOOK_CLIENT_ID", "OUTLOOK_CLIENT_SECRET"},
		ApprovedTools:   []string{"mail search/read operations"},
		BlockedTools:    []string{"send mail", "calendar writes", "contact writes", "mailbox settings writes"},
		SetupHints:      []string{"Use if Microsoft 365 through the generic provider fails for a tenant."},
	},
	{
		ID:            "executeautomation-database",
		Name:          "ExecuteAutomation Database",
		Kind:          "database",
		Profile:       "business",
		Description:   "Database MCP per SQLite, SQL Server, PostgreSQL e MySQL in profilo Aura read-only.",
		Status:        "not_configured",
		RuntimeType:   "container",
		RepositoryURL: "https://github.com/executeautomation/mcp-database-server",
		MCPServerNames: []string{
			"executeautomation-database",
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
		RequiredSecrets: []string{"database connection string or DB credentials"},
		ApprovedTools:   []string{"list_tables", "describe_table", "read_query", "export_query"},
		BlockedTools:    []string{"write_query", "create_table", "alter_table", "drop_table"},
		SetupHints:      []string{"Enterprise profile starts read-only. Writes require a later review-gated slice."},
	},
}
