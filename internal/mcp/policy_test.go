package mcp

import "testing"

func TestMailToolPolicy(t *testing.T) {
	env := map[string]string{
		"MAIL_SMTP_DAVIDE_USER": "me@example.com",
	}
	if !ToolEnabledForAura("mail", env, "smtp_send_message") {
		t.Fatal("SMTP send tool should be enabled when SMTP is configured")
	}
	if ToolEnabledForAura("mail", env, "imap_delete_message") {
		t.Fatal("IMAP mutation tool should stay disabled without explicit flag")
	}
	env["AURA_MAIL_DAVIDE_ENABLE_IMAP_MUTATIONS"] = "true"
	if !ToolEnabledForAura("mail", env, "imap_delete_message") {
		t.Fatal("IMAP mutation tool should be enabled by explicit user flag")
	}
	normalized := NormalizeRuntimeEnv("mail", env)
	if normalized[MailIMAPWriteEnabledEnv] != "true" {
		t.Fatalf("%s = %q, want true", MailIMAPWriteEnabledEnv, normalized[MailIMAPWriteEnabledEnv])
	}
	env = map[string]string{MailIMAPWriteEnabledEnv: "true"}
	if !ToolEnabledForAura("mail", env, "imap_delete_message") {
		t.Fatal("IMAP mutation tool should also honor the mail-mcp write flag")
	}
}

func TestDatabaseToolPolicy(t *testing.T) {
	for _, tool := range DatabaseReadTools {
		if !ToolEnabledForAura("database", nil, tool) {
			t.Fatalf("database read tool %q should be enabled", tool)
		}
		if !ToolAutonomousForAura("database", tool) {
			t.Fatalf("database read tool %q should be autonomous", tool)
		}
	}
	for _, tool := range DatabaseBlockedTools {
		if ToolEnabledForAura("database", nil, tool) {
			t.Fatalf("database blocked tool %q should be disabled", tool)
		}
		if ToolAutonomousForAura("database", tool) {
			t.Fatalf("database blocked tool %q should not be autonomous", tool)
		}
	}
	if ToolEnabledForAura("database", nil, "unknown_database_tool") {
		t.Fatal("unknown database tools should stay disabled until reviewed")
	}
	if !ToolEnabledForAura("other", nil, "drop_table") {
		t.Fatal("non-mail/database MCP servers are not filtered by this policy")
	}
}
