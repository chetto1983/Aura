package main

func apiBackedTools() []tool {
	return []tool{
		{
			name:        "aura_chat_answer",
			description: "Answer a pending ask_user question for an Aura chat thread.",
			inputSchema: schemaObject(
				prop("question_id", "string", "pending question id from aura_chat"),
				prop("answer", "string", "free-text answer"),
				prop("thread_id", "string", "optional chat thread id"),
				arrayProp("selected_option_ids", "optional selected option ids"),
			),
			handler: handleChatAnswer,
		},
		{
			name:        "aura_wiki_graph",
			description: "Read Aura's materialized wiki graph.",
			inputSchema: schemaObject(),
			handler:     handleWikiGraph,
		},
		{
			name:        "aura_wiki_godnodes",
			description: "List high-degree wiki graph nodes.",
			inputSchema: schemaObject(prop("top_k", "integer", "node count, 1-50")),
			handler:     handleWikiGodNodes,
		},
		{
			name:        "aura_wiki_path",
			description: "Find a bounded wiki graph path between two slugs.",
			inputSchema: schemaObject(
				prop("from", "string", "source wiki slug"),
				prop("to", "string", "target wiki slug"),
				prop("max_hops", "integer", "max hops, 1-20"),
			),
			handler: handleWikiPath,
		},
		{
			name:        "aura_wiki_index_rebuild",
			description: "Rebuild the wiki filesystem graph/index metadata.",
			inputSchema: schemaObject(),
			handler:     handleWikiIndexRebuild,
		},
		{
			name:        "aura_wiki_reindex",
			description: "Admin: rebuild the wiki vector/search projection, optionally dry-run.",
			inputSchema: schemaObject(boolProp("dry_run", "return rebuild report without modifying vectors")),
			handler:     handleWikiReindex,
		},
		{
			name:        "aura_wiki_log_append",
			description: "Append one validated wiki audit log entry.",
			inputSchema: schemaObject(
				prop("action", "string", "log action label"),
				prop("slug", "string", "optional wiki slug"),
			),
			handler: handleWikiLogAppend,
		},
		{
			name:        "aura_source_get",
			description: "Read source metadata by source id.",
			inputSchema: schemaObject(prop("id", "string", "source id")),
			handler:     handleSourceGet,
		},
		{
			name:        "aura_source_ocr",
			description: "Read a source's OCR markdown payload.",
			inputSchema: schemaObject(prop("id", "string", "source id")),
			handler:     handleSourceOCR,
		},
		{
			name:        "aura_source_raw",
			description: "Read a source's original raw bytes as base64 plus response metadata.",
			inputSchema: schemaObject(prop("id", "string", "source id")),
			handler:     handleSourceRaw,
		},
		{
			name:        "aura_source_derived",
			description: "Preview wiki pages and references affected by deleting a source.",
			inputSchema: schemaObject(prop("id", "string", "source id")),
			handler:     handleSourceDerived,
		},
		{
			name:        "aura_source_upload",
			description: "Upload a local file into Aura's source store via multipart form.",
			inputSchema: schemaObject(
				prop("file_path", "string", "local path readable by this MCP server"),
				prop("filename", "string", "optional uploaded filename override"),
			),
			handler: handleSourceUpload,
		},
		{
			name:        "aura_source_ingest",
			description: "Run source ingest for an OCR/extract-ready source.",
			inputSchema: schemaObject(prop("id", "string", "source id")),
			handler:     handleSourceIngest,
		},
		{
			name:        "aura_source_reocr",
			description: "Re-run OCR for a stored PDF source.",
			inputSchema: schemaObject(prop("id", "string", "source id")),
			handler:     handleSourceReOCR,
		},
		{
			name:        "aura_source_delete",
			description: "Destructive: delete a source and cascade indexed memory/wiki artifacts.",
			inputSchema: schemaObject(prop("id", "string", "source id")),
			handler:     handleSourceDelete,
		},
		{
			name:        "aura_file_tree",
			description: "List files under an Aura root: wiki, sources, workspace, or skills.",
			inputSchema: schemaObject(
				prop("root", "string", "one of wiki, sources, workspace, skills"),
				prop("path", "string", "root-relative directory path"),
				boolProp("recursive", "walk recursively"),
			),
			handler: handleFileTree,
		},
		{
			name:        "aura_file_read",
			description: "Read one file from an Aura root, returning utf-8 or base64 content.",
			inputSchema: schemaObject(
				prop("root", "string", "one of wiki, sources, workspace, skills"),
				prop("path", "string", "root-relative file path"),
			),
			handler: handleFileRead,
		},
		{
			name:        "aura_file_write",
			description: "Write one file under an Aura root. Wiki writes trigger reindex hooks.",
			inputSchema: schemaObject(
				prop("root", "string", "one of wiki, sources, workspace, skills"),
				prop("path", "string", "root-relative file path"),
				prop("content", "string", "file content"),
				prop("encoding", "string", "utf-8 or base64"),
			),
			handler: handleFileWrite,
		},
		{
			name:        "aura_file_delete",
			description: "Destructive: delete one file or empty directory from an Aura root.",
			inputSchema: schemaObject(
				prop("root", "string", "one of wiki, sources, workspace, skills"),
				prop("path", "string", "root-relative path"),
			),
			handler: handleFileDelete,
		},
		{
			name:        "aura_file_delete_many",
			description: "Destructive: delete multiple paths from one Aura root.",
			inputSchema: schemaObject(
				prop("root", "string", "one of wiki, sources, workspace, skills"),
				arrayProp("paths", "root-relative paths"),
			),
			handler: handleFileDeleteMany,
		},
		{
			name:        "aura_file_mkdir",
			description: "Create a directory tree under an Aura root.",
			inputSchema: schemaObject(
				prop("root", "string", "one of wiki, sources, workspace, skills"),
				prop("path", "string", "root-relative directory path"),
			),
			handler: handleFileMkdir,
		},
		{
			name:        "aura_file_rename",
			description: "Move or rename a file or directory within one Aura root.",
			inputSchema: schemaObject(
				prop("root", "string", "one of wiki, sources, workspace, skills"),
				prop("from", "string", "source root-relative path"),
				prop("to", "string", "target root-relative path"),
			),
			handler: handleFileRename,
		},
		{
			name:        "aura_task_get",
			description: "Read one scheduled task by name.",
			inputSchema: schemaObject(prop("name", "string", "task name")),
			handler:     handleTaskGet,
		},
		{
			name:        "aura_task_upsert",
			description: "Create or replace a scheduled task/reminder/job.",
			inputSchema: schemaObject(
				prop("name", "string", "task name"),
				prop("kind", "string", "reminder, wiki_maintenance, or agent_job"),
				prop("payload", "string", "task payload"),
				prop("recipient_id", "string", "recipient id for reminders"),
				prop("language", "string", "optional language for agent jobs"),
				prop("at", "string", "RFC3339 UTC one-shot schedule"),
				prop("daily", "string", "HH:MM local recurring schedule"),
				prop("weekdays", "string", "mon,tue,... filter for daily"),
				prop("every_minutes", "integer", "interval recurrence"),
			),
			handler: handleTaskUpsert,
		},
		{
			name:        "aura_task_cancel",
			description: "Cancel a scheduled task while preserving history.",
			inputSchema: schemaObject(prop("name", "string", "task name")),
			handler:     handleTaskCancel,
		},
		{
			name:        "aura_task_delete",
			description: "Destructive: delete a scheduled task row.",
			inputSchema: schemaObject(prop("name", "string", "task name")),
			handler:     handleTaskDelete,
		},
		{
			name:        "aura_skill_get",
			description: "Read one installed skill by name.",
			inputSchema: schemaObject(prop("name", "string", "skill name")),
			handler:     handleSkillGet,
		},
		{
			name:        "aura_skills_catalog",
			description: "Search or list the skills.sh catalog through Aura.",
			inputSchema: schemaObject(
				prop("query", "string", "optional catalog query"),
				prop("limit", "integer", "max catalog results"),
			),
			handler: handleSkillsCatalog,
		},
		{
			name:        "aura_skill_install",
			description: "Admin: install a catalog skill through Aura.",
			inputSchema: schemaObject(
				prop("source", "string", "catalog source"),
				prop("skill_id", "string", "catalog skill id"),
			),
			handler: handleSkillInstall,
		},
		{
			name:        "aura_skill_delete",
			description: "Admin destructive: remove an installed skill by name.",
			inputSchema: schemaObject(prop("name", "string", "skill name")),
			handler:     handleSkillDelete,
		},
		{
			name:        "aura_mcp_status",
			description: "Read Aura MCP server status.",
			inputSchema: schemaObject(),
			handler:     handleMCPStatus,
		},
		{
			name:        "aura_mcp_providers",
			description: "List approved MCP provider profiles and readiness.",
			inputSchema: schemaObject(),
			handler:     handleMCPProviders,
		},
		{
			name:        "aura_mcp_setup_mail_get",
			description: "Read managed mail MCP setup status.",
			inputSchema: schemaObject(),
			handler:     handleMCPSetupMailGet,
		},
		{
			name:        "aura_mcp_setup_mail_save",
			description: "Admin: save managed mail MCP config. Secrets are sent only to Aura.",
			inputSchema: schemaObject(
				prop("provider", "string", "gmail, outlook, or imap"),
				prop("account_id", "string", "mail account id"),
				prop("email", "string", "mail address"),
				prop("app_password", "string", "app password or token"),
				prop("imap_host", "string", "custom IMAP host"),
				prop("imap_port", "integer", "custom IMAP port"),
				boolProp("imap_secure", "use secure IMAP"),
				boolProp("enable_smtp", "configure SMTP too"),
				prop("smtp_host", "string", "custom SMTP host"),
				prop("smtp_port", "integer", "custom SMTP port"),
				boolProp("smtp_secure", "use SMTP TLS/starttls"),
				boolProp("enable_imap_mutations", "allow IMAP writes"),
			),
			handler: handleMCPSetupMailSave,
		},
		{
			name:        "aura_mcp_setup_database_get",
			description: "Read managed database MCP setup status.",
			inputSchema: schemaObject(),
			handler:     handleMCPSetupDatabaseGet,
		},
		{
			name:        "aura_mcp_setup_database_save",
			description: "Admin: save managed database MCP config. Secrets are sent only to Aura.",
			inputSchema: schemaObject(
				prop("provider", "string", "sqlite, postgresql, mysql, or sqlserver"),
				prop("sqlite_path", "string", "SQLite database path"),
				prop("host", "string", "database host"),
				prop("port", "integer", "database port"),
				prop("database", "string", "database name"),
				prop("user", "string", "database user"),
				prop("password", "string", "database password"),
				boolProp("ssl", "enable SSL"),
			),
			handler: handleMCPSetupDatabaseSave,
		},
		{
			name:        "aura_mcp_provider_probe",
			description: "Probe one approved MCP provider against connected server tools.",
			inputSchema: schemaObject(prop("id", "string", "provider id")),
			handler:     handleMCPProviderProbe,
		},
		{
			name:        "aura_mcp_provider_mail_search",
			description: "Search mail through an approved MCP mail provider.",
			inputSchema: schemaObject(
				prop("id", "string", "provider id"),
				prop("query", "string", "mail search query"),
				prop("account", "string", "optional account id"),
				prop("mailbox", "string", "optional mailbox"),
				prop("limit", "integer", "max messages"),
			),
			handler: handleMCPProviderMailSearch,
		},
		{
			name:        "aura_mcp_provider_mail_read",
			description: "Read one mail message through an approved MCP mail provider.",
			inputSchema: schemaObject(
				prop("id", "string", "provider id"),
				prop("message_id", "string", "message id"),
				prop("account", "string", "optional account id"),
			),
			handler: handleMCPProviderMailRead,
		},
		{
			name:        "aura_mcp_invoke",
			description: "Invoke one tool on one Aura-connected MCP server.",
			inputSchema: schemaObject(
				prop("server", "string", "Aura MCP server name"),
				prop("tool", "string", "tool name advertised by that server"),
				objectProp("arguments", "tool arguments object"),
			),
			handler: handleMCPInvoke,
		},
		{
			name:        "aura_tool_compact",
			description: "Run Aura's tool-result/tokenjuice compaction helper. Use mode=context to see the model-visible payload after TokenJuice, fresh-result budgeting, untrusted wrapping, and completed-turn compaction.",
			inputSchema: schemaObject(
				prop("mode", "string", "tokenjuice, tool_result, or context"),
				prop("tool_name", "string", "tool name"),
				objectProp("args", "optional original tool arguments object"),
				arrayProp("argv", "optional argv"),
				prop("command", "string", "optional command text"),
				prop("stdout", "string", "stdout/content"),
				prop("stderr", "string", "stderr"),
				prop("content", "string", "content alias"),
				prop("exit_code", "integer", "optional exit code"),
				prop("max_inline_chars", "integer", "TokenJuice inline cap"),
				prop("tool_result_max_chars", "integer", "completed tool-result context cap"),
				prop("force_rule_id", "string", "optional compaction rule id"),
				boolProp("raw", "return raw compacted text"),
				prop("min_input_bytes", "integer", "minimum input bytes"),
				prop("min_ratio", "number", "minimum savings ratio"),
			),
			handler: handleToolCompact,
		},
		{
			name:        "aura_tool_registry",
			description: "List Aura's live LLM-visible tool registry names.",
			inputSchema: schemaObject(),
			handler:     handleToolRegistry,
		},
		{
			name:        "aura_tool_call",
			description: "Directly execute one live Aura registry tool with JSON args.",
			inputSchema: schemaObject(
				prop("name", "string", "registry tool name, e.g. search"),
				objectProp("args", "tool arguments object"),
				prop("user_id", "string", "optional user id"),
				prop("conversation_id", "string", "optional conversation id"),
			),
			handler: handleToolCall,
		},
		{
			name:        "aura_compact_reindex",
			description: "Admin: rebuild compact memory vector projection.",
			inputSchema: schemaObject(),
			handler:     handleCompactReindex,
		},
		{
			name:        "aura_tool_attempts",
			description: "Read 24h tool-attempt metrics by tool/outcome.",
			inputSchema: schemaObject(),
			handler:     handleToolAttempts,
		},
	}
}

func boolProp(name, desc string) map[string]any {
	return map[string]any{name: map[string]any{"type": "boolean", "description": desc}}
}

func arrayProp(name, desc string) map[string]any {
	return map[string]any{name: map[string]any{"type": "array", "description": desc}}
}

func objectProp(name, desc string) map[string]any {
	return map[string]any{name: map[string]any{"type": "object", "description": desc}}
}
