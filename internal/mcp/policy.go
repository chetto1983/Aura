package mcp

import "strings"

const MailIMAPWriteEnabledEnv = "MAIL_IMAP_WRITE_ENABLED"

var MailReadTools = []string{
	"ews_get_message",
	"ews_search_messages",
	"get_setup_guide",
	"imap_get_message",
	"imap_get_message_raw",
	"imap_list_accounts",
	"imap_list_mailboxes",
	"imap_mailbox_status",
	"imap_search_messages",
	"imap_verify_account",
	"list_all_accounts",
	"smtp_verify_account",
}

var MailSendTools = []string{
	"smtp_send_message",
	"smtp_reply_message",
	"smtp_forward_message",
	"graph_send_message",
	"ews_send_message",
}

var MailIMAPMutationTools = []string{
	"imap_delete_message",
	"imap_bulk_delete",
	"imap_bulk_move",
	"imap_bulk_update_flags",
}

var DatabaseReadTools = []string{
	"list_tables",
	"describe_table",
	"read_query",
	"export_query",
}

var DatabaseBlockedTools = []string{
	"write_query",
	"create_table",
	"alter_table",
	"drop_table",
	"append_insight",
	"list_insights",
}

func ToolEnabledForAura(serverName string, env map[string]string, toolName string) bool {
	switch normalizedServerName(serverName) {
	case "mail":
		return mailToolEnabled(env, toolName)
	case "database":
		return toolIn(toolName, DatabaseReadTools)
	default:
		return true
	}
}

func ToolAutonomousForAura(serverName, toolName string) bool {
	switch normalizedServerName(serverName) {
	case "mail":
		return toolIn(toolName, MailReadTools)
	case "database":
		return toolIn(toolName, DatabaseReadTools)
	default:
		return false
	}
}

func NormalizeRuntimeEnv(serverName string, env map[string]string) map[string]string {
	if normalizedServerName(serverName) != "mail" {
		return env
	}
	if !mailEnvFlagEnabled(env, "ENABLE_IMAP_MUTATIONS") || envFlagEnabled(env[MailIMAPWriteEnabledEnv]) {
		return env
	}
	out := make(map[string]string, len(env)+1)
	for key, value := range env {
		out[key] = value
	}
	out[MailIMAPWriteEnabledEnv] = "true"
	return out
}

func mailToolEnabled(env map[string]string, toolName string) bool {
	if toolIn(toolName, MailIMAPMutationTools) {
		return mailIMAPMutationsEnabled(env)
	}
	if toolIn(toolName, MailSendTools) {
		return mailSMTPConfigured(env)
	}
	return true
}

func mailIMAPMutationsEnabled(env map[string]string) bool {
	return mailEnvFlagEnabled(env, "ENABLE_IMAP_MUTATIONS") || envFlagEnabled(env[MailIMAPWriteEnabledEnv])
}

func normalizedServerName(serverName string) string {
	switch strings.TrimSpace(serverName) {
	case "mail", "mail-mcp":
		return "mail"
	case "database", "db":
		return "database"
	default:
		return ""
	}
}

func toolIn(toolName string, tools []string) bool {
	for _, tool := range tools {
		if tool == toolName {
			return true
		}
	}
	return false
}

func mailSMTPConfigured(env map[string]string) bool {
	for key, value := range env {
		if strings.HasPrefix(key, "MAIL_SMTP_") && strings.HasSuffix(key, "_USER") && strings.TrimSpace(value) != "" {
			return true
		}
	}
	return false
}

func mailEnvFlagEnabled(env map[string]string, suffix string) bool {
	for key, value := range env {
		if strings.HasPrefix(key, "AURA_MAIL_") && strings.HasSuffix(key, "_"+suffix) {
			if envFlagEnabled(value) {
				return true
			}
		}
	}
	return false
}

func envFlagEnabled(value string) bool {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "1", "true", "yes", "on", "enabled":
		return true
	default:
		return false
	}
}
