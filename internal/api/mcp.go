package api

import (
	"net/http"
	"sort"

	"github.com/aura/aura/internal/mcp"
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
		out := connectorProviderSummaries(deps)
		writeJSON(w, deps.Logger, http.StatusOK, out)
	}
}

func connectorProviderSummaries(deps Deps) []ConnectorProviderSummary {
	out := cloneProviderManifests()
	mailStatus, _ := currentMailSetupStatus(deps)
	databaseStatus, _ := currentDatabaseSetupStatus(deps)
	for i := range out {
		switch out[i].ID {
		case "mail-mcp":
			applyMailToolPolicy(&out[i], mailStatus)
		case "database":
			applyDatabaseStatus(&out[i], databaseStatus)
		}
	}
	return out
}

func cloneProviderManifests() []ConnectorProviderSummary {
	out := make([]ConnectorProviderSummary, len(mcpProviderManifests))
	for i, provider := range mcpProviderManifests {
		out[i] = provider
		out[i].MCPServerNames = append([]string(nil), provider.MCPServerNames...)
		out[i].Capabilities = append([]ConnectorCapability(nil), provider.Capabilities...)
		out[i].RiskBadges = append([]ConnectorRiskBadge(nil), provider.RiskBadges...)
		out[i].RequiredSecrets = append([]string(nil), provider.RequiredSecrets...)
		out[i].ApprovedTools = append([]string(nil), provider.ApprovedTools...)
		out[i].BlockedTools = append([]string(nil), provider.BlockedTools...)
		out[i].SetupHints = append([]string(nil), provider.SetupHints...)
	}
	return out
}

func applyMailToolPolicy(provider *ConnectorProviderSummary, status MailSetupStatus) {
	if provider == nil {
		return
	}
	provider.Status = connectorStatus(status.Configured, status.Connected, status.Error)
	enableMailBaseCapabilities(provider, status.Connected)
	if status.EnableSMTP {
		provider.ApprovedTools = appendUniqueStrings(provider.ApprovedTools, mcp.MailSendTools...)
		provider.BlockedTools = removeStrings(provider.BlockedTools, mcp.MailSendTools...)
		provider.RiskBadges = replaceRiskBadge(provider.RiskBadges, "writes_blocked", ConnectorRiskBadge{ID: "send_enabled", Label: "invio abilitato", Level: "high"})
		enableCapability(provider, "mail.draft_reply", status.Connected)
	}
	if status.EnableIMAPMutations {
		provider.ApprovedTools = appendUniqueStrings(provider.ApprovedTools, mcp.MailIMAPMutationTools...)
		provider.BlockedTools = removeStrings(provider.BlockedTools, mcp.MailIMAPMutationTools...)
		provider.RiskBadges = replaceRiskBadge(provider.RiskBadges, "writes_blocked", ConnectorRiskBadge{ID: "mutations_enabled", Label: "modifica mail abilitata", Level: "high"})
	}
	provider.SetupHints = mailSetupHints(status)
}

func applyDatabaseStatus(provider *ConnectorProviderSummary, status DatabaseSetupStatus) {
	if provider == nil {
		return
	}
	provider.Status = connectorStatus(status.Configured, status.Connected, status.Error)
	for i := range provider.Capabilities {
		provider.Capabilities[i].Enabled = status.Connected
	}
}

func connectorStatus(configured, connected bool, errText string) string {
	if errText != "" {
		return "failed"
	}
	if connected {
		return "enabled"
	}
	if configured {
		return "configured"
	}
	return "not_configured"
}

func enableMailBaseCapabilities(provider *ConnectorProviderSummary, connected bool) {
	for _, id := range []string{"mail.accounts", "mail.search", "mail.read", "mail.extract_tasks"} {
		enableCapability(provider, id, connected)
	}
}

func enableCapability(provider *ConnectorProviderSummary, id string, enabled bool) {
	for i := range provider.Capabilities {
		if provider.Capabilities[i].ID == id {
			provider.Capabilities[i].Enabled = enabled
			return
		}
	}
}

func mailSetupHints(status MailSetupStatus) []string {
	hints := []string{"Configura dalla dashboard: provider, email, IMAP e app password."}
	if status.EnableSMTP {
		hints = append(hints, "Invio, reply e forward sono abilitati per questo account.")
	} else {
		hints = append(hints, "Invio, reply e forward restano disattivati finche non abiliti SMTP.")
	}
	if status.EnableIMAPMutations {
		hints = append(hints, "Spostamento, eliminazione e aggiornamento flag IMAP sono abilitati.")
	} else {
		hints = append(hints, "Spostamento, eliminazione e aggiornamento flag IMAP restano disattivati finche non abiliti il toggle avanzato.")
	}
	return hints
}

func appendUniqueStrings(values []string, extras ...string) []string {
	seen := make(map[string]bool, len(values)+len(extras))
	out := make([]string, 0, len(values)+len(extras))
	for _, value := range values {
		if !seen[value] {
			seen[value] = true
			out = append(out, value)
		}
	}
	for _, value := range extras {
		if !seen[value] {
			seen[value] = true
			out = append(out, value)
		}
	}
	return out
}

func removeStrings(values []string, removals ...string) []string {
	remove := make(map[string]bool, len(removals))
	for _, value := range removals {
		remove[value] = true
	}
	out := make([]string, 0, len(values))
	for _, value := range values {
		if !remove[value] {
			out = append(out, value)
		}
	}
	return out
}

func replaceRiskBadge(values []ConnectorRiskBadge, id string, replacement ConnectorRiskBadge) []ConnectorRiskBadge {
	out := make([]ConnectorRiskBadge, 0, len(values)+1)
	replaced := false
	for _, value := range values {
		if value.ID == id {
			if !replaced {
				out = append(out, replacement)
				replaced = true
			}
			continue
		}
		out = append(out, value)
	}
	if !replaced {
		out = append(out, replacement)
	}
	return out
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
		ApprovedTools:   append([]string(nil), mcp.DatabaseReadTools...),
		BlockedTools:    append([]string(nil), mcp.DatabaseBlockedTools...),
		SetupHints:      []string{"Configura dalla dashboard. Aura usa solo query read-only nella card connettore."},
	},
}
