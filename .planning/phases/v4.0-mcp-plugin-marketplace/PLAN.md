# Aura v4.0 MCP Marketplace And Autonomous Plugin Manager

## Summary

v4.0 turns Aura's MCP support from a static `mcp.json` boot config into a Docker-first plugin marketplace. Aura will sync the official MCP Registry into a local cache, let the operator review and approve installs, run MCP servers as container sidecars when possible, smoke-test them, roll back failures automatically, and expose only approved tools to the agent.

Existing skills remain supported, but the main v4.0 plugin model is MCP marketplace plus container runtime plus dashboard governance.

Planning reference: <https://modelcontextprotocol.io/registry/about>

First approved slice: **v4.0a Mail-First Provider-Agnostic MCP**. Aura starts with mail value, not a generic plugin catalog. It exposes a small canonical Aura mail contract while provider-specific MCP servers remain behind adapters, allowlists, review, and audit. Enterprise database MCP support is planned in the same slice as a separate read-only business profile.

Design: `.planning/phases/v4.0-mcp-plugin-marketplace/DESIGN.md`

## Implementation Progress

### 2026-05-09 Companion Runtime Plan: Tool Search And Programmatic Execution

Status: planned in `.planning/phases/10-agent-tool-search-programmatic-execution/PLAN.md`.

The MCP marketplace remains the active product milestone, but the next runtime slice needs a separate plan because it changes the agent hot path: small default tool exposure, semantic `tool_search`, programmatic `execute_code` orchestration, context hygiene for tool results, and Telegram `/clear` plus command menu support. v4.0 integration rule: MCP/provider tools become searchable only after Aura's existing review-gated registration enables them; `tool_search` is discovery, not permission escalation.

### 2026-05-09 Database MCP Read-Only Policy And Setup Refactor

Status: implemented.

- Reproduced the database MCP gap with the real ExecuteAutomation server: it advertises 10 tools, including write/schema/insight tools.
- Extracted the reusable managed MCP setup boundary used by mail and database into `managedMCPServer`, covering default command resolution, config lookup, existing-server preservation, and atomic `mcp.json` upsert.
- Moved MCP exposure decisions out of Telegram into `internal/mcppolicy`, shared by runtime registration and dashboard provider manifests.
- Kept mail behavior intact, including the `MAIL_IMAP_WRITE_ENABLED` normalization from the prior slice.
- Added explicit database policy: Aura exposes only `list_tables`, `describe_table`, `read_query`, and `export_query`; `write_query`, `create_table`, `alter_table`, `drop_table`, `append_insight`, and `list_insights` stay hidden until a reviewed future write-capable slice exists.
- Rebuilt the container with a live SQLite test database mounted through `/workspace/tmp/mcp-e2e.db`.

Verification:

- `go test ./cmd/debug_telegram_sandbox ./internal/mcppolicy ./internal/api ./internal/telegram -count=1`
- `go test ./internal/mcp ./internal/config -count=1`
- `go test ./...`
- `go build ./...` (passed; Go printed a Windows stat-cache permission warning)
- `go vet ./...`
- `docker compose up -d --build aura`
- live `/status` returned ok
- container logs show `MCP server registered` for `server=database` with `tools=10` and `aura_tools=4`
- LLM debug E2E passed with `mcp_database_list_tables`, `mcp_database_describe_table`, and `mcp_database_read_query`, returning rows `Ada/Roma` and `Bruno/Milano`

### 2026-05-09 Mail MCP Write Flag Repair

Status: implemented.

- Read conversation logs after Aura told the user that mail delete/write operations were disabled server-side.
- Confirmed the root cause: Aura exposed the reviewed IMAP mutation tools when `AURA_MAIL_*_ENABLE_IMAP_MUTATIONS=true`, but the real `mail-mcp` process also requires `MAIL_IMAP_WRITE_ENABLED=true`.
- Updated the mail setup API so new end-user configurations persist both the Aura review flag and the server-required `MAIL_IMAP_WRITE_ENABLED=true` flag.
- Added startup normalization for legacy runtime configs: when a mail server has Aura IMAP mutations enabled, Aura injects `MAIL_IMAP_WRITE_ENABLED=true` before launching the MCP server.
- Made tool exposure honor either the Aura review flag or the native `MAIL_IMAP_WRITE_ENABLED` flag, so manual configs stay coherent.
- Patched the live ignored `runtime-workspace/mcp.json` and rewrote it as UTF-8 without BOM after Docker exposed that PowerShell's default write path made Linux JSON parsing fail.

Verification:

- `go test ./internal/api ./internal/telegram -run "TestMCPMailSetup|TestMCPToolEnabledForAuraMailPolicy" -count=1`
- `go test ./internal/api ./internal/telegram -count=1`
- `go test ./internal/mcp ./internal/config -count=1`
- `docker compose up -d --build aura`
- live `/status` returned ok
- container logs show `MCP server registered` for `server=mail` with `tools=30` and `aura_tools=30`

### 2026-05-09 Hard Orchestrator Deletion

Status: implemented.

- Deleted the live `internal/orchestration` package instead of disabling it.
- Removed Telegram toolset selection, prompt-compose hooks, before/after tool callbacks, runtime toolset filtering, and document-route tool suppression.
- Simplified the hot path to a Picobot/Hermes-style surface: Aura sends the model the registered tool definitions and lets the model choose.
- Workspace file tools, MCP tools, web tools, source tools, scheduler tools, sandbox tools, and document tools now come from the registry directly; no phrase router decides what the model may see.
- Removed `AURA_TOOLSET_MODE` and `AURA_ORCHESTRATION_LOG_LEVEL` from config, dashboard settings, Compose, and `.env.example`.
- Changed terminal tool policy from legacy `toolset/off` wording to simple `on/off`.
- Kept only runtime loop limits and tool implementation boundaries: tool execution still rejects a tool name that was not included in the actual model-visible definitions for that turn.

Verification:

- `go test ./internal/telegram ./internal/agentruntime ./internal/config ./internal/settings ./internal/api ./cmd/debug_telegram_sandbox -count=1`
- `npm --prefix web run i18n:check`
- `npm --prefix web run build`
- `go test ./...`
- `go build ./...`
- `go vet ./...`
- `docker compose config --quiet`
- `docker compose up -d --build aura`
- live `/status` returned ok

### 2026-05-09 Model-Led Tool Surface

Status: implemented.

- Shifted normal capability exposure away from text-intent orchestration and into registry metadata.
- Added an `autonomous` tool category. Telegram exposes registered autonomous tools in addition to the tiny base surface.
- Marked configured `web_search` and `web_fetch` autonomous, so Aura no longer claims web search is missing when the provider is configured.
- Marked source read/list/lint and persistent tool-registry read/list autonomous.
- Tightened MCP prompt bloat: mail MCP read/search/list/verify tools can be autonomous, while send/delete/move/bulk mutation tools stay registered but are not model-visible by default.
- Removed `search_memory` as a default terminal tool, so the model can follow memory with another exposed tool when useful.
- No regex, phrase router, or hardcoded user-text intent rules were added.

Verification:

- `go test ./internal/tools ./internal/telegram ./internal/orchestration -count=1`
- `go test ./...`
- `go build ./...`
- `go vet ./...`
- `docker compose up -d --build aura`
- live `/status` returned ok
- live `tools_exposed` included `web_search,web_fetch` and read-only mail MCP tools, while mail send/delete/move/bulk tools stayed out of the default prompt

### 2026-05-09 MCP Tool Exposure Repair

Status: implemented.

- Read the conversation logs after the user reported that Aura did not know MCP was available.
- Confirmed the real root cause: the mail MCP server was registered at startup, but the Telegram turn allowlist still exposed only the tiny default toolset.
- Repaired exposure registry-first: MCP tools now declare the `mcp` category, and Telegram appends enabled registered MCP tools by category before sending tool definitions to the model.
- Avoided regex/user-text routing. No phrase matching for "mail", "MCP", or status prompts was added.
- Preserved the end-user enablement boundary: only tools registered by the configured MCP setup can be exposed.
- Docker verification exposed a separate derived-index health issue: `compact_memory_fts` could leave `/status` degraded even though compact memory had just rebuilt.
- Added compact-memory FTS repair from canonical `compact_memory_documents`, and startup repair/recheck only for that derived FTS table.

Verification:

- `go test ./internal/tools ./internal/telegram -count=1`
- `go test ./internal/orchestration -count=1`
- `go test ./internal/memoryindex ./cmd/aura -count=1`
- `go test ./...`
- `go build ./...`
- `go vet ./...`
- `docker compose up -d --build aura`

### 2026-05-08 Real MCP Mail Server Wiring

Status: implemented.

- Extended `mcp.json` stdio server config with `env` so real MCP servers can receive account credentials without new Aura-specific plumbing.
- Passed stdio env through the existing MCP client startup path.
- Added provider-to-server aliases, so `mail-mcp` can probe a real server named `mail` or `mail-mcp`.
- Added `mcp.example.json` mail-mcp read-only template using IMAP env only.
- Kept SMTP/IMAP write flags out of the example and out of Aura enablement.

Verification:

- `go test ./internal/mcp ./internal/api ./internal/telegram -run "TestLoadServersValid|TestMCPProviderProbeMailMCPFindsConfiguredMailAlias|TestMCPProviderMail" -count=1`
- `npm --prefix web run i18n:check`
- `npm --prefix web run build`
- `go test ./...`
- `go build ./...`
- `go vet ./...`

### 2026-05-08 Mail Provider Probe And Read Adapter

Status: implemented.

- Added `POST /mcp/providers/{id}/actions/probe` for connected provider profiles.
- Added read-only canonical mail adapter endpoints for `mail.search` and `mail.read`.
- Mapped `mail-mcp` IMAP/EWS search/read tools behind Aura allowlists.
- Added response redaction for token/password/secret-like mail bodies.
- Kept blocked provider tools visible in probe output without enabling them.
- Wired the dashboard Probe button to the backend and renders ready/missing capabilities.
- Rebuilt embedded dashboard assets under `internal/api/dist`.

Verification:

- `go test ./internal/api -run "TestMCPProviderMail|TestMCPProviders" -count=1`
- `go test ./internal/api -count=1`
- `npm --prefix web run i18n:check`
- `npm --prefix web run build`
- `go test ./...`
- `go build ./...`
- `go vet ./...`

### 2026-05-08 Connector Configuration Surface

Status: implemented.

- Added `GET /mcp/providers` with read-only provider manifests for mail/database candidates.
- Added connector provider DTOs separate from raw MCP server summaries.
- Added API tests proving mail and database profiles exist, and that database read/write allowlists are separated.
- Reworked `/mcp` dashboard into tabs: Connectors, Installed, Health, Review Queue, Raw MCP.
- Kept raw MCP manual tool invocation as diagnostics, not primary configuration.
- Added frontend DTO/API bindings and English/Italian locale strings.
- Built embedded dashboard assets under `internal/api/dist`.

Verification:

- `go test ./internal/api -count=1`
- `npm --prefix web run i18n:check`
- `npm --prefix web run build`
- `go test ./...`
- `go build ./...`

## User Decisions

- Plugin model: MCP marketplace.
- Runtime: container MCP by default.
- Registry source: official MCP Registry plus local cache.
- Security model: review-gated.
- Agent autonomy: agent may stage, test, and auto-rollback approved proposals, but may not silently enable tools.
- First value surface: provider-agnostic mail, then enterprise database read-only profile.
- Mail provider strategy: stable Aura mail contract over approved MCP provider adapters.
- Initial mail candidates: `tecnologicachile/mail-mcp`, `aaronsb/google-workspace-mcp`, `navbuildz/gmail-mcp-server`, and `littlebearapps/outlook-assistant`.
- Initial enterprise database candidate: `executeautomation/mcp-database-server`, read-only allowlist only.

## Goal

Make Aura's plugin system MCP-first, Docker-first, review-gated, and agent-auditable.

The milestone is complete when Aura can discover MCP servers, install them as managed container sidecars or remote HTTP connections, smoke-test and roll back failures, show plugin state in the dashboard, and expose only enabled approved MCP tools to the agent.

## Key Changes

### v4.0a Mail-First Provider-Agnostic Slice

- Add canonical Aura mail capabilities instead of exposing raw provider tool sprawl:
  - `mail.accounts`
  - `mail.search`
  - `mail.read`
  - `mail.thread`
  - `mail.draft_reply`
  - `mail.extract_tasks`
  - optional reviewed `mail.label`
  - optional reviewed `mail.archive`
- Keep mail outside the default toolset. Default remains `search_memory` plus `schedule_task`.
- Add an explicit `mail` toolset or equivalent review-enabled provider surface.
- Map canonical mail capabilities to approved MCP providers through provider manifests/adapters.
- Start with search/read/task extraction/draft preparation. No silent send/delete/bulk move/unsubscribe in the first slice.
- Add Italian workflow skills/procedures on top of mail:
  - triage importante;
  - risposta professionale;
  - follow-up clienti/fornitori;
  - meeting brief;
  - scadenze amministrative.
- Add enterprise database support as a separate business profile using read-only capabilities only:
  - list tables;
  - describe table;
  - read query;
  - export query.
- Block database writes/schema mutation by default.

### MCP Registry Cache

- Add a registry sync service backed by SQLite.
- Sync server metadata from the official MCP Registry API.
- Store server identity, version metadata, runtime hints, tool metadata when available, sync timestamps, and audit status.
- Use a 24h default cache TTL plus manual dashboard refresh.
- Registry sync must never block Aura startup.
- Garage may back up registry cache and audit bundles, but is not primary registry storage.

### Managed Plugin State

- Add Aura-managed MCP plugin records separate from raw registry records.
- Track plugin status: `proposed`, `approved`, `installing`, `installed`, `enabled`, `disabled`, `failed`, and `rolled_back`.
- Track runtime kind: `container`, `remote_http`, or `stdio_legacy`.
- Track required secrets as secret references, not raw values.
- Keep existing raw `mcp.json` support as legacy/manual configuration.
- Store plugin data under `/data/plugins/<plugin_id>/`.
- Do not delete plugin data volumes by default.

### Container-First Runtime

- Prefer MCP servers as Docker/Compose sidecars.
- Follow the established Pyodide sidecar pattern: keep runtime bloat out of the Aura image, communicate through internal Compose service URLs, require service health checks, and make every sidecar restartable without mutating primary Aura state.
- Pin images by digest when practical; when tags are unavoidable, record the resolved digest and validation date in plugin audit metadata.
- Generate managed Compose/config fragments under `/data` instead of rewriting the root Compose file.
- Install transaction flow:
  - Create managed plugin config.
  - Start the sidecar or remote connection.
  - Run MCP initialize and `tools/list`.
  - Run a smoke check if the plugin provides one.
  - Mark installed only after the probe succeeds.
- On failure:
  - Stop the sidecar or connection.
  - Restore the previous managed config.
  - Mark the plugin `rolled_back`.
  - Preserve install logs for dashboard review.
- Sidecar lifecycle must be rollback-friendly: plugin data volumes are preserved by default, generated runtime config is versioned, and the previous enabled tool surface remains available until the new probe passes.
- Remote HTTP MCP remains supported through URL and secret header references.
- Stdio/uvx MCP remains legacy/manual and should show a warning in the UI.

### Review-Gated Security

- Every install, update, and enable operation requires dashboard review unless it belongs to an already approved proposal.
- Classify risk by network access, mounted paths, secrets, write-capable tools, external API access, and container privilege flags.
- Default containers must run unprivileged.
- Redact secrets from API responses, logs, and UI.
- MCP tools are not registered for the agent until the plugin is enabled.
- Audit every proposal, install, enable, disable, rollback, and invoke failure.
- Treat mail and database plugins as high-sensitivity by default.
- Pin package/image/version where practical and flag `npx -y latest` style runtime as development-only.
- Detect tool metadata drift between probes before enabling an already-known plugin.
- Provider adapters must enforce allowlists before calling MCP, even if the raw MCP server exposes wider tools.

### Agent Autonomy

- Add an MCP audit capability that detects:
  - Missing capabilities the agent repeatedly needs.
  - Broken MCP servers.
  - Duplicate native-vs-MCP tools.
  - Unused or bloated MCP tools.
  - Risky plugins that need operator review.
- Agent may create install/fix proposals automatically.
- For approved proposals, agent may stage installation, smoke-test, and roll back on failure.
- Final enablement remains review-gated.
- Nightly maintenance should include MCP health audit and create dashboard review items.

### Dashboard

- Replace the simple MCP panel with:
  - Connectors.
  - Installed.
  - Health.
  - Review queue.
  - Raw MCP diagnostics.
- Keep the existing `/mcp` route initially and render tabs in `web/src/components/MCPPanel.tsx`; do not add sidebar sprawl for v4.0a.
- Add Mail Connectors provider cards for:
  - generic mail (`mail-mcp`);
  - Google Workspace;
  - Gmail fallback;
  - Outlook/Microsoft fallback.
- Add an Enterprise Database card/profile for read-only database MCP.
- Keep existing raw server/tool/schema/manual invoke UI as the Raw MCP tab, not the primary configuration UX.
- Do not put per-account mail/database secrets in the generic runtime `SettingsPanel`. Provider credentials belong to connector-specific flows with secret references and redacted API responses.
- Show runtime type, risk badges, required secrets, install status, smoke result, last error, tool list, logs, enable/disable, and rollback controls.
- Keep manual invoke for enabled tools.
- Add Italian and English locale coverage for all new UI text.

### Docker-Only Release Validation

- v4.0 validation targets the Docker stack first.
- Desktop/tray remains secondary and must not block container release.
- Raspberry-class constraints remain part of acceptance: no heavyweight default sidecars beyond Aura's selected plugin set.

## Public Interfaces

Keep MCP tool names stable as `mcp_<server>_<tool>`.

Extend existing MCP server API responses with:

- `status`
- `enabled`
- `source`
- `last_error`
- `last_seen`
- `risk_level`

Add HTTP APIs under `/api/mcp`:

- `GET /registry`
- `POST /registry/sync`
- `GET /plugins`
- `GET /plugins/{id}`
- `POST /plugins/{id}/propose-install`
- `POST /plugins/{id}/install`
- `POST /plugins/{id}/enable`
- `POST /plugins/{id}/disable`
- `POST /plugins/{id}/rollback`
- `GET /plugins/{id}/logs`

Add mail/provider APIs only after the provider adapter exists:

- `GET /api/mcp/providers`
- `GET /api/mcp/providers/{id}`
- `POST /api/mcp/providers/{id}/configure`
- `POST /api/mcp/providers/{id}/probe`
- `POST /api/mcp/providers/{id}/enable`
- `POST /api/mcp/providers/{id}/disable`
- `GET /api/mcp/providers/{id}/audit`

Frontend DTOs should be explicit and separate from raw MCP server summaries:

- `ConnectorProviderSummary`
- `ConnectorCapability`
- `ConnectorRiskBadge`
- `ConnectorProbeResponse`
- `ConnectorAuditEvent`

Add settings:

- `MCP_REGISTRY_URL`
- `MCP_REGISTRY_SYNC_ENABLED`
- `MCP_PLUGIN_ADMIN`
- `MCP_PLUGIN_AUTO_ROLLBACK`
- `MCP_PLUGIN_DATA_DIR`

## Test Plan

### Unit

- Registry sync against a fake registry server.
- Registry cache TTL and failed-sync behavior.
- Secret redaction in API and log output.
- Risk classifier.
- Mail provider manifest parsing and allowlist enforcement.
- Mail adapter maps fake MCP search/read responses into canonical Aura results.
- Database provider manifest blocks write/schema tools.
- Install transaction success and rollback paths.
- MCP manager enable/disable/hot reload.
- Tool registration exposes only enabled approved plugins.

### Integration

- Fake HTTP MCP server install, enable, list tools, and invoke tool.
- Fake mail MCP server probe, search, read, and blocked send/delete call.
- Fake database MCP server probe, list/read success, and blocked write/drop call.
- Failed MCP server rolls back without breaking existing tools.
- Managed Compose/config generation validates with `docker compose config --quiet`.
- Dashboard E2E covers connector cards, setup status, risk badges, probe, enable, disable, logs, and the preserved Raw MCP diagnostics tab.
- Locale check confirms no missing strings.

### Release Gate

- `go test ./internal/mcp ./internal/api ./internal/telegram ./internal/tools ./internal/toolsets -count=1`
- `npm --prefix web run build`
- `docker compose config --quiet`
- Docker smoke: Aura health, registry sync, fake MCP install, live enabled MCP invoke.

## Assumptions

- v4.0 starts after the current release branch is accepted and `.planning/ROADMAP.md` makes MCP/plugins the active or next milestone.
- The official MCP Registry plus local cache is the only marketplace source for v4.0.
- Container MCP is the default runtime.
- Remote HTTP is supported.
- Stdio remains legacy/manual.
- Agent autonomy means propose, stage, smoke-test, and rollback. It does not silently enable new tools.
- Existing skills remain separate from MCP plugins in v4.0.
- Garage may back up registry cache, logs, and audit bundles, but it is not primary plugin runtime storage.
- v4.0a starts provider-agnostic at Aura's contract boundary, even if the first live provider tested is a single MCP server.
- Mail send/delete/bulk mutation and database writes require a later reviewed slice.
