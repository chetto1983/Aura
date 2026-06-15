# Phase 16: MCP Sidecar Manager + Third-Party Trust - Context

**Gathered:** 2026-06-04
**Status:** Ready for planning
**Scope decision:** User selected option 2: include third-party/untrusted MCP installs.

<domain>
## Phase Boundary

Deliver an Aura-owned MCP manager that makes MCP usable without hand-editing scattered config. Phase 16 starts from the shipped `aura mcp` CLI, managed config at `~/.aura/mcp/servers.json`, fail-soft boot mounting, mail/WhatsApp recipes, and the WhatsApp doctor bridge probe. It expands that surface into a small local MCP control plane: profiles, recipe/catalog metadata, runtime detection, config prompts, status/doctor/log output, trust approvals, risk labels, and isolated third-party MCP install paths.

Because the user explicitly chose the larger scope, Phase 16 includes third-party/untrusted MCP installs. That changes the shape from "trusted recipe hardening only" to "trusted recipes plus explicit trust policy and sandboxed runtime options." The phase still does not implement the OpenClaw plugin host. OpenClaw remains a separate untrusted plugin-code runtime; Phase 16 governs MCP servers that speak MCP over stdio or Streamable HTTP.

The daily-user outcome: a user can run `aura mcp recipes`, install mail/WhatsApp/calendar, add a third-party MCP server, inspect what it will run, approve trust, assign it to a profile, see tool risk labels, run `aura mcp doctor --all`, and start `aura chat` without a dead or blocked server taking the whole agent down.
</domain>

<decisions>
## Implementation Decisions

### Scope and safety
- **D-01 Expanded scope:** Phase 16 includes third-party/untrusted MCP installs, not only Aura-curated recipes.
- **D-02 Trust classes:** Every configured server has one of: `trusted_recipe`, `trusted_local`, `sandboxed_local`, `remote_http`, or `blocked`. New third-party local commands default to `blocked` until explicitly trusted or converted to `sandboxed_local`.
- **D-03 OpenClaw boundary:** Do not pull in the OpenClaw Node plugin host, typed plugin RPC, mTLS, Postgres-audited plugin mutations, or module loader. MCP servers are process/HTTP endpoints; OpenClaw plugins are arbitrary module code. Keep those phases separate.
- **D-04 Control plane vs data plane:** The MCP manager owns config, profiles, status, doctor, risk labels, and runtime policy. Existing `internal/mcp` and `internal/agent/mcptools` remain the data plane that opens servers and mounts tools.
- **D-05 No silent execution:** `aura chat` must not start a newly added third-party local command until trust has been recorded. Fail-soft boot continues for broken/blocked servers.

### Config, profiles, and catalog
- **D-06 Managed config v2:** Extend `ManagedServer` backwards-compatibly. Existing `command`/`args`/`env` entries still load. New metadata includes type, profile membership, trust state, runtime, risk labels, tool policy, source, and last doctor snapshot.
- **D-07 Profiles:** Add named profiles as first-class config. Profiles are collections of enabled servers and tool-policy overrides. `default` preserves current behavior.
- **D-08 Catalogs:** Built-in catalog entries are curated recipes with richer metadata. Custom third-party entries can be added manually. Remote registry discovery is deferred unless a local catalog file is supplied.
- **D-09 Recipe set:** Keep mail and WhatsApp. Add Calendar as the new first-class recipe candidate. Calendar must have a deterministic fixture/test mode before any live account E2E is considered.

### Runtime and transport
- **D-10 Stdio stays supported:** Existing stdio MCP remains the compatibility baseline.
- **D-11 Streamable HTTP support:** Add a minimal Streamable HTTP MCP client for remote HTTP servers, using the MCP protocol-version header and session handling. SSE-only stays deprecated/out of scope unless needed for backwards compatibility.
- **D-12 Dockerized local runtime:** Third-party local stdio servers should prefer a Dockerized runtime (`docker run -i --rm ...`) with no host mounts by default, explicit mounts/domains, and resource limits. Direct host commands require `trusted_local`.
- **D-13 Docker MCP Gateway compatibility:** Aura may generate/connect to Docker MCP Gateway profiles when Docker MCP Toolkit is installed, but Docker MCP is optional, not a hard dependency.
- **D-14 No restart supervisor:** Preserve the Phase 9 lifecycle decision. No background restart loop or ping ticker for trusted stdio servers. Use on-demand doctor/status pings; container health is delegated to Docker where used.

### Secrets, OAuth, and auth status
- **D-15 Secret handling:** Do not print secret values. Config prompts write placeholders or env references. Shared/team profile export must exclude credentials.
- **D-16 OAuth posture:** For Phase 16, expose auth status and support bearer/header/env credentials. Full dynamic OAuth client registration can be a follow-up unless needed by the first remote HTTP acceptance fixture.

### Tool risk and policy
- **D-17 Risk labels:** Tools are labeled by capability: `read`, `write`, `network`, `filesystem`, `destructive`, `private_data`, `external_send`, and `unknown`. Unknown defaults to risky.
- **D-18 Policy enforcement:** Existing allowlists remain supported. Add deny/risk policy at mount time so blocked/destructive tools never enter the Aura registry for that profile.
- **D-19 Visibility:** `aura mcp tools` and `aura mcp status` show risk labels, deferred status, source, trust class, and whether a tool is mounted or blocked by policy.

### Doctor and observability
- **D-20 Status surface:** Add `aura mcp status [--json]` and `aura mcp doctor --all`. Status should follow the Codex-style shape: server name, startup state, auth status, server info, tools, resources, error.
- **D-21 Runtime checks:** Doctor checks runtime prerequisites: command availability, WSL state, Docker availability, npm/npx/uv version where relevant, HTTP reachability, WhatsApp REST `:8080`, mail env completeness, calendar fixture/auth.
- **D-22 Logs:** Capture MCP stderr tails or log paths per launch/doctor. Logs are redacted for env-looking secrets.

### Validation
- **D-23 Mock-first E2E:** Required CI coverage uses mock stdio and mock Streamable HTTP MCP servers. Live WhatsApp/mail/calendar checks are operator-run only.
- **D-24 No marketplace auto-install:** Natural-language catalog discovery, public marketplace browsing, and automatic third-party install are deferred. This phase supports explicit user commands only.
</decisions>

<canonical_refs>
## Canonical References

### Aura code and docs
- `cmd/aura/mcp.go` - current `aura mcp` command surface, recipes, doctor, WhatsApp REST probe.
- `cmd/aura/mcp_test.go` - current MCP CLI tests.
- `cmd/aura/main.go` - fail-soft boot-level MCP mount and per-server allowlist.
- `internal/mcp/managed_config.go` - durable managed config.
- `internal/mcp/client.go` - stdio MCP client and lifecycle handshake.
- `internal/agent/mcptools/bridge.go` - MCP tool adaptation, namespacing, deferred mount behavior.
- `docs/research/mcp-sidecar-lifecycle-study.md` - locked Phase 9 lifecycle decision: fail-soft, no restart supervisor.
- `docs/superpowers/specs/2026-06-02-openclaw-plugin-compatibility-design.md` - separate OpenClaw plugin-host boundary.
- `.planning/phases/09-swarm-minimal/09-CONTEXT.md` - mail/WhatsApp MCP decisions, Calendar deferred to Phase 16.

### Local external references in `D:/tmp`
- `D:/tmp/codex/codex-rs/app-server-protocol/schema/json/v2/ListMcpServerStatusResponse.json` - status payload model.
- `D:/tmp/codex/codex-rs/app-server-protocol/schema/json/v2/McpServerStatusUpdatedNotification.json` - startup-state notification model.
- `D:/tmp/codex/codex-rs/app-server/src/request_processors/mcp_processor.rs` - status, refresh, OAuth login, and tool-call control-plane processor.
- `D:/tmp/whatsapp-mcp/README.md` - two-process WhatsApp bridge/MCP architecture and operational risks.
- `D:/tmp/mail-mcp/package.json` - mail MCP runtime/dependency expectations.

### Official ecosystem references
- <https://modelcontextprotocol.io/specification/2025-06-18/basic/lifecycle> - lifecycle, shutdown, timeouts.
- <https://modelcontextprotocol.io/specification/2025-06-18/basic/transports> - stdio and Streamable HTTP transport requirements.
- <https://modelcontextprotocol.io/specification/2025-06-18/basic/utilities/ping> - optional ping and stale-connection behavior.
- <https://modelcontextprotocol.io/specification/2025-06-18/basic/authorization> - HTTP authorization/OAuth posture.
- <https://docs.docker.com/ai/mcp-catalog-and-toolkit/> - Docker MCP catalogs, profiles, gateway, and centralized management.
- <https://docs.docker.com/ai/mcp-catalog-and-toolkit/toolkit/> - containerized MCP security defaults.
- <https://docs.docker.com/ai/mcp-catalog-and-toolkit/cli/> - Docker MCP Gateway stdio profile command pattern.
- <https://code.visualstudio.com/docs/agent-customization/mcp-servers> - VS Code trust gate, enable/disable state, logs, sandboxing model.
- <https://code.claude.com/docs/en/mcp> - Claude Code scopes, `/mcp` status, dynamic tool updates, reconnect behavior, plugin-provided MCP.
</canonical_refs>

<specifics>
## Specific Ideas

- The manager should feel like `docker mcp` plus VS Code trust gates, but shaped for Aura CLI users.
- The "sidecar" is not necessarily a long-running daemon in task 1. The first deliverable can be a Go-owned manager/control plane with config and CLI. A persistent local service can be introduced only if the status/log/OAuth surface needs it.
- Third-party local commands should be frictionful by design: show command, args, env placeholders, requested mounts/network, risk labels, and ask the user to trust before chat boot can run it.
- WhatsApp remains the proving ground for multi-process doctor checks: WSL, Python/uv MCP, Go bridge, REST `:8080`, SQLite/session drift.
- Calendar should be introduced with deterministic fixture mode first so CI can validate without a live Google/Microsoft account.
</specifics>

<deferred>
## Deferred Ideas

- Full public MCP marketplace browsing and natural-language install.
- Full OAuth dynamic client registration unless required by the first remote HTTP acceptance fixture.
- OpenClaw plugin host and arbitrary Node module plugin loading.
- Auto-restart supervisor for stdio MCP servers.
- SSE-only legacy transport unless a must-have server still requires it.
- Organization/team sync of MCP profiles beyond export/import without credentials.
</deferred>

---

*Phase: 16-add-richer-recipes-doctor-checks-for-whatsapp-and-calendar-e*
*Context gathered: 2026-06-04*
