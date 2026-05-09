package telegram

import "testing"

func TestMCPToolEnabledForAuraMailPolicy(t *testing.T) {
	env := map[string]string{
		"MAIL_SMTP_DAVIDE_USER": "me@example.com",
	}
	if !mcpToolEnabledForAura("mail", env, "smtp_send_message") {
		t.Fatal("SMTP send tool should be enabled when SMTP is configured")
	}
	if mcpToolEnabledForAura("mail", env, "imap_delete_message") {
		t.Fatal("IMAP mutation tool should stay disabled without explicit flag")
	}
	env["AURA_MAIL_DAVIDE_ENABLE_IMAP_MUTATIONS"] = "true"
	if !mcpToolEnabledForAura("mail", env, "imap_delete_message") {
		t.Fatal("IMAP mutation tool should be enabled by explicit user flag")
	}
	if !mcpToolEnabledForAura("database", nil, "drop_table") {
		t.Fatal("non-mail MCP servers are not filtered by the mail policy")
	}
}
