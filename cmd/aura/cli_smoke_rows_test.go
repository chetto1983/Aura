package main

// cliSmokeRow is one invocation of the built binary in TestCLISmoke
// (cli_smoke_integration_test.go, db_integration). The rows are data, untagged, so the unit-tier
// inventory (cli_inventory_test.go) can check every dispatched verb against them.
type cliSmokeRow struct {
	name string
	// argv is the command line; "{name}" is replaced by the value an earlier row captured.
	argv []string
	// exit is the status the row expects; anyExit accepts every status.
	exit int
	// stdout and stderr are substrings the streams must hold; empty means not checked.
	stdout string
	stderr string
	// after is SQL returning one boolean the row's effect must make true.
	after string
	// capture maps a name to SQL returning one text value, read after the row.
	capture map[string]string
}

const anyExit = -1

// cliSmokeRows run in order against one database. A row named after a C-finding of
// docs/superpowers/verification/2026-10-31-cli-audit.md pins a known defect: the slice that
// fixes it flips the row.
var cliSmokeRows = []cliSmokeRow{
	{name: "db_migrate_from_empty", argv: []string{"db", "migrate"}, stdout: "migration(s) applied"},
	{name: "db_migrate_again_is_a_noop", argv: []string{"db", "migrate"}, stdout: "no pending migrations"},
	{name: "db_ping", argv: []string{"db", "ping"}, stdout: "ok: ping"},
	{name: "db_status", argv: []string{"db", "status"}, stdout: "DIRTY"},
	{name: "version", argv: []string{"version"}, stdout: "aura "},
	{name: "tools", argv: []string{"tools"}, stdout: "ask_user"},
	{name: "tools_json", argv: []string{"tools", "--json"}, stdout: `"name": "ask_user"`},
	// The doctor's verdict depends on which services the host runs; the row pins that it runs
	// and that the database check passes.
	{name: "doctor_checks_postgres", argv: []string{"doctor"}, exit: anyExit, stdout: "PASS postgres"},
	{name: "cache_stats", argv: []string{"cache-stats", "--since=1h"}, stdout: "cache metrics since"},
	{name: "agent_dry_run", argv: []string{"agent", "dry-run"}, stdout: `"request_id"`},

	{name: "config_show", argv: []string{"config", "show"}, stdout: "provider:"},
	{name: "config_get", argv: []string{"config", "get", "llm.model"}, stdout: "cli-smoke"},
	{name: "config_set", argv: []string{"config", "set", "llm.temperature", "0.5"}, stdout: "ok: set llm.temperature = 0.5"},
	{name: "config_get_after_set", argv: []string{"config", "get", "llm.temperature"}, stdout: "0.5"},
	{name: "config_validate", argv: []string{"config", "validate"}, stdout: "no configuration violations"},

	{name: "identity_list", argv: []string{"identity", "list"}, stdout: "aura-cli"},
	{name: "identity_get", argv: []string{"identity", "get", "local"}, stdout: "local"},
	{name: "identity_get_by_uuid_is_not_found_C5", argv: []string{"identity", "get", "00000000-0000-0000-0000-000000000001"},
		exit: 1, stderr: "identity not found"},
	{name: "identity_grant", argv: []string{"identity", "grant", "local", "web"}, stdout: `granted "web"`},
	{name: "identity_revoke", argv: []string{"identity", "revoke", "local", "web"}, stdout: `revoked "web"`},
	{name: "identity_recover_mints_a_reset_token", argv: []string{"identity", "recover", "local"}, stdout: "minted break-glass reset token"},

	{name: "gateway_grants_list", argv: []string{"gateway", "grants", "list", "local"}, stdout: "no standing approval grants"},
	{name: "gateway_grants_revoke_nothing", argv: []string{"gateway", "grants", "revoke", "local", "shell_exec"}, stdout: "nothing revoked"},
	{name: "gateway_policy_set", argv: []string{"gateway", "policy", "set", "local", "shell_exec", "deny"}, stdout: "is now deny",
		after: `SELECT EXISTS (SELECT 1 FROM aura.gateway_tool_policies WHERE tool = 'shell_exec' AND policy = 'deny')`},
	{name: "gateway_policy_list", argv: []string{"gateway", "policy", "list", "local"}, stdout: "shell_exec"},
	{name: "gateway_policy_clear", argv: []string{"gateway", "policy", "clear", "local", "shell_exec"}, stdout: "cleared",
		after: `SELECT NOT EXISTS (SELECT 1 FROM aura.gateway_tool_policies WHERE tool = 'shell_exec')`},

	{name: "paused_states_list", argv: []string{"paused-states", "list"}, stdout: "no paused states"},
	{name: "paused_states_purge", argv: []string{"paused-states", "purge", "--before", "2020-01-01T00:00:00Z", "--confirm"}, stdout: "purged"},
	{name: "retention_plan", argv: []string{"retention", "plan"}, stdout: `"token"`},

	{name: "skills_list", argv: []string{"skills", "list"}, stdout: "NAME"},
	{name: "skills_create", argv: []string{"skills", "create", "cli-smoke", "--desc", "smoke probe", "--body", "Say hi."},
		stdout: `create "cli-smoke"`, after: `SELECT EXISTS (SELECT 1 FROM aura.skill_audit WHERE skill_name = 'cli-smoke' AND action = 'create')`},
	{name: "skills_info", argv: []string{"skills", "info", "cli-smoke"}, stdout: "name: cli-smoke"},
	{name: "skills_update", argv: []string{"skills", "update", "cli-smoke", "--desc", "smoke probe 2", "--body", "Say hi again."},
		stdout: `update "cli-smoke"`, after: `SELECT count(*) = 1 FROM aura.skill_audit WHERE skill_name = 'cli-smoke' AND action = 'update'`},
	{name: "skills_always", argv: []string{"skills", "always", "cli-smoke", "on"}, stdout: "always=true",
		after: `SELECT count(*) = 2 FROM aura.skill_audit WHERE skill_name = 'cli-smoke' AND action = 'update'`},
	{name: "skills_shares_refuses_a_skill_the_owner_did_not_write", argv: []string{"skills", "shares", "cli-smoke", "--owner",
		"00000000-0000-0000-0000-000000000001"}, exit: 1, stderr: "owns no skill"},
	{name: "skills_snippet_save", argv: []string{"skills", "snippet", "save", "cli-smoke-snippet", "--lang", "python", "--code", "print(1)"},
		stdout: `snippet "cli-smoke-snippet" saved`},
	{name: "skills_audit", argv: []string{"skills", "audit"}, stdout: "cli-smoke"},
	{name: "skills_delete", argv: []string{"skills", "delete", "cli-smoke"}, stdout: `deleted "cli-smoke"`,
		after: `SELECT EXISTS (SELECT 1 FROM aura.skill_audit WHERE skill_name = 'cli-smoke' AND action = 'delete')`},

	{name: "task_schedule", argv: []string{"task", "schedule", "--kind", "reminder", "--every", "60", "--notify", "none",
		"--args", `{"text":"cli smoke"}`}, stdout: "scheduled",
		capture: map[string]string{"task": `SELECT id::text FROM aura.scheduler_tasks WHERE kind = 'reminder' ORDER BY created_at DESC LIMIT 1`}},
	{name: "task_list", argv: []string{"task", "list"}, stdout: "{task}"},
	{name: "task_list_ignores_an_unknown_flag_C3", argv: []string{"task", "list", "--json"}, stdout: "KIND"},
	{name: "task_pause", argv: []string{"task", "pause", "{task}"}, stdout: "paused",
		after: `SELECT status = 'paused' FROM aura.scheduler_tasks WHERE id = '{task}'`},
	{name: "task_resume", argv: []string{"task", "resume", "{task}"}, stdout: "resumed",
		after: `SELECT status = 'active' FROM aura.scheduler_tasks WHERE id = '{task}'`},
	{name: "task_run_now", argv: []string{"task", "run_now", "{task}"}, stdout: "queued"},
	{name: "task_runs", argv: []string{"task", "runs"}, stdout: "RUN_ID"},
	{name: "task_doctor", argv: []string{"task", "doctor"}, stdout: "active tasks"},
	{name: "task_cancel", argv: []string{"task", "cancel", "{task}"}, stdout: "cancel",
		after: `SELECT status = 'cancelled' FROM aura.scheduler_tasks WHERE id = '{task}'`},

	{name: "chat_list_empty", argv: []string{"chat", "list"}, stdout: "no conversations"},
	{name: "chat_new_with_no_input", argv: []string{"chat", "new"},
		capture: map[string]string{"conversation": `SELECT id::text FROM aura.conversations ORDER BY created_at DESC LIMIT 1`}},
	{name: "chat_list", argv: []string{"chat", "list"}, stdout: "{conversation}"},
	{name: "chat_resume_with_no_input", argv: []string{"chat", "resume", "{conversation}"}},
	{name: "chat_search", argv: []string{"chat", "search", "nothing-said"}, stdout: "no matching turns"},
	{name: "chat_rename", argv: []string{"chat", "rename", "{conversation}", "cli smoke"},
		after: `SELECT title = 'cli smoke' FROM aura.conversations WHERE id = '{conversation}'`},
	{name: "chat_archive", argv: []string{"chat", "archive", "{conversation}"},
		after: `SELECT status = 'archived' FROM aura.conversations WHERE id = '{conversation}'`},
	{name: "chat_unarchive", argv: []string{"chat", "unarchive", "{conversation}"},
		after: `SELECT status = 'active' FROM aura.conversations WHERE id = '{conversation}'`},
	{name: "chat_delete", argv: []string{"chat", "delete", "{conversation}", "--confirm"},
		after: `SELECT NOT EXISTS (SELECT 1 FROM aura.conversations WHERE id = '{conversation}')`},

	// The profile rows come first: C9 shows only while the implicit default profile has never
	// held a server, as on a fresh install.
	{name: "mcp_recipes", argv: []string{"mcp", "recipes"}, stdout: "calendar"},
	{name: "mcp_profile_list_shows_the_implicit_default", argv: []string{"mcp", "profile", "list"}, stdout: "* default"},
	{name: "mcp_profile_create", argv: []string{"mcp", "profile", "create", "cli-smoke"}, stdout: "created profile cli-smoke"},
	{name: "mcp_profile_use", argv: []string{"mcp", "profile", "use", "cli-smoke"}, stdout: "using profile cli-smoke"},
	{name: "mcp_profile_use_default_after_switching_is_not_found_C9", argv: []string{"mcp", "profile", "use", "default"},
		exit: 1, stderr: `MCP profile "default" not found`},
	{name: "mcp_install", argv: []string{"mcp", "install", "calendar"}, stdout: "installed calendar"},
	{name: "mcp_profile_add", argv: []string{"mcp", "profile", "add", "cli-smoke", "calendar"}, stdout: "added calendar"},
	{name: "mcp_list", argv: []string{"mcp", "list"}, stdout: "calendar"},
	{name: "mcp_status", argv: []string{"mcp", "status"}, stdout: "calendar"},
	{name: "mcp_disable", argv: []string{"mcp", "disable", "calendar"}, stdout: "disabled calendar"},
	{name: "mcp_enable", argv: []string{"mcp", "enable", "calendar"}, stdout: "enabled calendar"},
	{name: "mcp_trust_refuses_a_remote_recipe", argv: []string{"mcp", "trust", "calendar", "--reason", "cli smoke"},
		exit: 1, stderr: "cannot have trust class"},
	{name: "mcp_doctor_all", argv: []string{"mcp", "doctor", "--all"}, stdout: "calendar"},
	{name: "mcp_authorizations", argv: []string{"mcp", "authorizations"}, stdout: "authorized no MCP servers"},
	{name: "mcp_logout_with_no_grant", argv: []string{"mcp", "logout", "calendar"}, stdout: "no stored authorization"},
	{name: "mcp_profile_remove", argv: []string{"mcp", "profile", "remove", "cli-smoke", "calendar"}, stdout: "removed calendar"},
	{name: "mcp_remove", argv: []string{"mcp", "remove", "calendar"}, stdout: "removed calendar"},

	{name: "docs_search", argv: []string{"docs", "search", "fattura"}, stdout: `"status"`},
	{name: "objectstore_bootstrap", argv: []string{"objectstore", "bootstrap"}, stdout: "skipped for filesystem-dev"},
	{name: "web_doctor_without_searxng", argv: []string{"web", "doctor"}, exit: exitUsage, stderr: "SEARXNG_URL is not configured"},
}
