# Phase 16: MCP Sidecar Manager + Third-Party Trust - Research

**Researched:** 2026-06-04
**Domain:** MCP local manager, profiles/catalogs, trust/sandbox policy, Streamable HTTP, doctor/status, third-party MCP onboarding
**Confidence:** MEDIUM-HIGH. The ecosystem direction is clear, but MCP Registry/Docker MCP/VS Code sandbox support are still moving surfaces in 2026.

## Summary

The best 2026 pattern is a small MCP control plane, not a bag of per-client JSON snippets. Docker MCP centralizes catalogs, profiles, clients, credentials, and lifecycle through a gateway. VS Code adds trust prompts, enable/disable state, log/status UI, and sandboxing on supported platforms. Claude Code keeps multiple scopes, shows `/mcp` status, handles dynamic tool updates, and reconnects HTTP/SSE but not stdio. Codex's local app server has the most directly reusable shape for Aura: status snapshots, auth status, refresh, OAuth login, tool/resource lists, and startup-state notifications.

For Aura, this argues for an MCP manager that owns config, profiles, trust, status, doctor, logs, and recipes while keeping tool execution in the existing `internal/mcp` + `mcptools` data plane. Because the user selected third-party/untrusted installs, the manager must include explicit trust and sandbox runtime policy. The main safety rule is simple: a new arbitrary local command is never run by `aura chat` just because it was added. It must be trusted or containerized.

## Ecosystem Findings

### MCP specification

- The 2025-06-18 lifecycle spec defines initialize, operation, shutdown, request timeouts, and cancellation. It does not prescribe host restart policy.
- stdio remains a first-class transport where the client launches the server as a subprocess.
- Streamable HTTP is the current HTTP transport. Local HTTP servers should bind localhost, validate Origin, and authenticate.
- Ping is optional and useful for doctor/status, but should not force Aura into a busy background ticker.
- HTTP authorization is optional but standardized around OAuth 2.1 concepts. Stdio credentials should come from environment/config, not the HTTP OAuth flow.

### Docker MCP Toolkit/Gateway

- Docker's pattern is catalogs + profiles + clients connected through a gateway.
- Profiles let users select different server collections per project/client and share profile definitions without credentials.
- Docker packages MCP servers as containers for dependency isolation and restricts filesystem access by default.
- The CLI exposes `docker mcp gateway run --profile <profile-id>` as a stdio-compatible gateway command, which Aura can interoperate with rather than reimplementing Docker's entire catalog.

### VS Code

- VS Code separates configuration from enable/disable state and asks the user to trust a changed/new server before starting it.
- It exposes start/stop, logs, clear cache, and status actions.
- Sandboxing exists for macOS/Linux stdio MCP servers, but not Windows. Aura should not rely on VS Code's sandbox, but should copy the trust gate UX.

### Claude Code

- Claude Code supports local/project/user scopes, project approval, env expansion, HTTP/SSE/stdio/WebSocket config, and `/mcp` status.
- HTTP/SSE servers get automatic reconnect with backoff; stdio servers are local processes and are not auto-reconnected.
- Plugin-provided MCP servers show the value of bundled distribution: tools and server config installed together. Aura's recipes should use the same idea without importing the plugin system.

### Codex local status surface

Codex's app-server files under `D:/tmp/codex` provide an implementation reference for a status/control API:

- `ListMcpServerStatusResponse.json` includes auth status, server info, tools, resources, and resource templates.
- `McpServerStatusUpdatedNotification.json` models `starting`, `ready`, `failed`, and `cancelled`.
- `mcp_processor.rs` has refresh, status list, OAuth login, resource read, and tool call paths.

This is the closest local analog to what Aura should expose through CLI and possibly a future local service.

## Aura Ground Truth

- `cmd/aura/mcp.go` already has `install`, `add`, `list`, `doctor`, `tools`, `enable`, `disable`, `remove`.
- `internal/mcp/managed_config.go` stores Claude-style `mcpServers` plus Aura metadata (`enabled`, `source`) at `~/.aura/mcp/servers.json`.
- `internal/mcp/client.go` is stdio-only and currently negotiates protocol version `2024-11-05`.
- `cmd/aura/main.go` already mounts enabled MCP servers at boot and has Phase 9 fail-soft behavior.
- `internal/agent/mcptools/bridge.go` adapts MCP tools into Aura tools and already applies namespacing/deferred behavior from Phase 9.
- WhatsApp proved the operational gap: the MCP server can start while its companion bridge/session silently drifts.

## Recommended Architecture

### Control plane

Create an `internal/mcp/manager` or `internal/mcp/managed` layer for:

- config v2 load/save/migration
- profiles and profile membership
- recipe/catalog metadata
- trust approvals
- runtime policy
- status snapshots
- doctor checks
- risk labels and tool policy

The CLI calls this layer. `aura chat` consumes the resulting enabled/profile-filtered server set.

### Data plane

Keep:

- `internal/mcp` stdio client
- new `internal/mcp/httpclient` for Streamable HTTP
- `internal/agent/mcptools` mounting and tool call adaptation
- fail-soft boot behavior

Do not add a background supervisor. Docker/container health can exist as an external runtime check.

### Runtime choices

| Runtime | Use | Default trust |
|---|---|---|
| `local` | Aura-curated recipes and user-approved local commands | approved only after trust is recorded |
| `docker` | Third-party stdio MCP server in a container | preferred for untrusted |
| `docker_gateway` | Docker MCP Gateway profile | trusted gateway command, server trust delegated to Docker profile |
| `remote_http` | Streamable HTTP MCP endpoint | explicit trust + auth status |

### Risk policy

Risk labels should be computed conservatively from recipe metadata plus tool name/description heuristics. Built-in recipe metadata can override heuristics. Unknown labels default to risky and visible.

The mount policy should decide whether a tool enters Aura's registry. This keeps dangerous tools away from the model instead of merely warning in prose.

## Open Questions Resolved For Planning

- **Trusted only vs third-party?** Resolved: include third-party/untrusted installs.
- **Sidecar daemon or CLI manager first?** Plan CLI/control-plane first; introduce a daemon only if OAuth or live status subscriptions truly need it.
- **Docker MCP hard dependency?** No. Integrate if present; provide Aura-managed Docker stdio runtime as the portable path.
- **OpenClaw reuse?** No runtime reuse. Reuse only the conceptual separation: Aura owns governance, sidecars own execution boundaries.

## Validation Architecture

CI must validate with local fixtures:

- fake stdio MCP server
- fake Streamable HTTP MCP server
- fake Docker command runner for generated args
- temp managed config migration/profile tests
- policy tests that prove blocked/destructive tools never mount
- doctor tests with fake WSL/Docker/npm/uv/http probes

Live/operator tiers:

- WhatsApp bridge REST/session check
- mail env/auth check if credentials exist
- calendar live account only after fixture mode is green
- optional Docker MCP Gateway smoke if installed

## Package Legitimacy Audit

Phase 16 should avoid new heavy dependencies unless the Streamable HTTP client or OAuth flow requires one. Favor stdlib HTTP/JSON and existing test helpers. Third-party MCP servers should remain subprocess/container configuration, not Go imports.

Potential external MCP packages:

- `martinzarfl/mail-mcp`: already spike-validated; out-of-process.
- `chetto1983/whatsapp-mcp`: user's fork; already spike-validated; out-of-process.
- Calendar MCP candidate: must be fixture-validated before becoming a trusted recipe.
- Docker MCP Gateway: external CLI integration only; optional dependency.

## Common Pitfalls

1. Running arbitrary `npx`/`uvx` commands at chat boot without trust approval.
2. Treating "doctor green" as "safe"; doctor checks availability, policy checks safety.
3. Hiding blocked tools only in text while still mounting them.
4. Baking secrets into exported profiles.
5. Making Docker Desktop/Docker MCP mandatory for all users.
6. Adding a ping ticker/restart supervisor that violates the existing lifecycle decision.
7. Merging MCP server management with OpenClaw plugin loading.
8. Depending on a live WhatsApp/calendar account for CI.
