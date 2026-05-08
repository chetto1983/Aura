# Aura v4.0 MCP Marketplace And Autonomous Plugin Manager

## Summary

v4.0 turns Aura's MCP support from a static `mcp.json` boot config into a Docker-first plugin marketplace. Aura will sync the official MCP Registry into a local cache, let the operator review and approve installs, run MCP servers as container sidecars when possible, smoke-test them, roll back failures automatically, and expose only approved tools to the agent.

Existing skills remain supported, but the main v4.0 plugin model is MCP marketplace plus container runtime plus dashboard governance.

Planning reference: <https://modelcontextprotocol.io/registry/about>

First approved slice: **v4.0a Mail-First Provider-Agnostic MCP**. Aura starts with mail value, not a generic plugin catalog. It exposes a small canonical Aura mail contract while provider-specific MCP servers remain behind adapters, allowlists, review, and audit. Enterprise database MCP support is planned in the same slice as a separate read-only business profile.

Design: `.planning/phases/v4.0-mcp-plugin-marketplace/DESIGN.md`

## Implementation Progress

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
