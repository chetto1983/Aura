# Aura v4.0 MCP Marketplace And Autonomous Plugin Manager

## Summary

v4.0 turns Aura's MCP support from a static `mcp.json` boot config into a Docker-first plugin marketplace. Aura will sync the official MCP Registry into a local cache, let the operator review and approve installs, run MCP servers as container sidecars when possible, smoke-test them, roll back failures automatically, and expose only approved tools to the agent.

Existing skills remain supported, but the main v4.0 plugin model is MCP marketplace plus container runtime plus dashboard governance.

Planning reference: <https://modelcontextprotocol.io/registry/about>

## User Decisions

- Plugin model: MCP marketplace.
- Runtime: container MCP by default.
- Registry source: official MCP Registry plus local cache.
- Security model: review-gated.
- Agent autonomy: agent may stage, test, and auto-rollback approved proposals, but may not silently enable tools.

## Goal

Make Aura's plugin system MCP-first, Docker-first, review-gated, and agent-auditable.

The milestone is complete when Aura can discover MCP servers, install them as managed container sidecars or remote HTTP connections, smoke-test and roll back failures, show plugin state in the dashboard, and expose only enabled approved MCP tools to the agent.

## Key Changes

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
- Remote HTTP MCP remains supported through URL and secret header references.
- Stdio/uvx MCP remains legacy/manual and should show a warning in the UI.

### Review-Gated Security

- Every install, update, and enable operation requires dashboard review unless it belongs to an already approved proposal.
- Classify risk by network access, mounted paths, secrets, write-capable tools, external API access, and container privilege flags.
- Default containers must run unprivileged.
- Redact secrets from API responses, logs, and UI.
- MCP tools are not registered for the agent until the plugin is enabled.
- Audit every proposal, install, enable, disable, rollback, and invoke failure.

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
  - Marketplace.
  - Installed plugins.
  - Health.
  - Review queue.
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
- Install transaction success and rollback paths.
- MCP manager enable/disable/hot reload.
- Tool registration exposes only enabled approved plugins.

### Integration

- Fake HTTP MCP server install, enable, list tools, and invoke tool.
- Failed MCP server rolls back without breaking existing tools.
- Managed Compose/config generation validates with `docker compose config --quiet`.
- Dashboard E2E covers marketplace search, proposal review, install smoke, enable, disable, and logs.
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
