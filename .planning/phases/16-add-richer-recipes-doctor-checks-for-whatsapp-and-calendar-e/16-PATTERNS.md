# Phase 16: MCP Sidecar Manager + Third-Party Trust - Patterns

**Mapped:** 2026-06-04
**Purpose:** Existing code patterns and insertion points for Phase 16.

## Existing MCP CLI

### `cmd/aura/mcp.go`

Current commands:

- `install <recipe> [name]`
- `add <name> [--env KEY=VALUE] [--disabled] -- <command> [args...]`
- `list`
- `doctor <name>`
- `tools <name>`
- `enable|disable|remove <name>`

Patterns to preserve:

- `runMCPCommand(ctx,args,out)` is testable and side-effect injectable through env/config.
- `mcpRecipes` is a simple built-in catalog.
- Managed config writes go through `mcp.SaveManagedConfig`.
- Doctor opens the server, lists tools, then runs server-specific checks.
- WhatsApp bridge health uses a small probe seam (`runWhatsAppBridgeWSLProbe`) for testability.

Phase 16 should add commands without turning `mcp.go` into a giant file. Add subcommand-specific helpers and move reusable logic into `internal/mcp/manager` once it stops being CLI glue.

## Managed Config

### `internal/mcp/managed_config.go`

Current shape:

```go
type ManagedConfig struct {
    MCPServers map[string]ManagedServer `json:"mcpServers"`
}

type ManagedServer struct {
    Command string
    Args []string
    Env []string
    Enabled *bool
    Source string
}
```

Phase 16 pattern:

- Keep this JSON backwards-compatible.
- Add `Version`, `Profiles`, and metadata with `omitempty`.
- Preserve `EnabledServers()` for legacy callers or add a profile-aware sibling such as `RuntimeServers(profile string)`.
- Keep validation strict for empty names/commands, but allow HTTP entries with URL instead of command.
- Add unit tests for migration from the current file shape.

## MCP Stdio Client

### `internal/mcp/client.go`

Current behavior:

- launches stdio subprocess with operator-controlled command/args/env
- initializes once
- serializes requests with `mu`
- supports `tools/list` and `tools/call`
- captures stderr tail for failures

Phase 16 pattern:

- Do not mix HTTP transport into this file if it makes it hard to reason about stdio.
- Introduce a small common interface for `ListTools`, `CallTool`, `Ping`, and `Close`.
- Update protocol negotiation intentionally and test old/new server compatibility.
- Add optional ping for doctor/status only.

## Tool Bridge

### `internal/agent/mcptools/bridge.go`

Existing bridge responsibilities:

- list MCP tool definitions
- namespace tool names
- adapt JSON schema into Aura tool `Spec`
- set bridged tools `Deferred:true`
- call MCP tools and return `tools.NewResult`

Phase 16 pattern:

- Keep policy enforcement here or just before here: a blocked tool should not register.
- Risk labels can ride `Spec.Metadata` only if such metadata exists; otherwise keep labels in manager/status output and enforce via allow/deny before mount.
- Preserve namespacing and collision protection.

## Boot Mount

### `cmd/aura/main.go`

Current pattern:

- build base registry first
- validate base registry
- iterate enabled MCP servers
- fail-soft per server on mount failure
- apply known allowlists for mail/WhatsApp

Phase 16 pattern:

- Resolve active profile before boot mount.
- Drop blocked/untrusted servers before launch.
- Mount only policy-approved tools.
- Preserve fail-soft behavior.
- Do not add a background supervisor in this path.

## Status/Control Plane Reference

### `D:/tmp/codex/codex-rs/app-server*`

Reusable ideas:

- status snapshot includes server info, auth status, tools, resources, resource templates
- startup state has `starting`, `ready`, `failed`, `cancelled`
- refresh is explicit and best-effort can be separate from strict
- OAuth login is a control-plane operation, not part of every tool call

Aura v1 can expose this through CLI JSON before it has an app-server protocol.

## Doctor Pattern

Doctor should be layered:

1. Config validation: required fields, env placeholders, trust state.
2. Runtime validation: command/docker/wsl/npm/uv availability.
3. Transport validation: stdio initialize/tools-list or HTTP initialize/tools-list.
4. Recipe validation: WhatsApp bridge, mail env, calendar fixture/auth.
5. Policy validation: blocked risky tools, unknown risks, profile membership.

Each line should be actionable and should not print secrets.

## Testing Patterns

- Use temp `AURA_MCP_CONFIG` for config tests.
- Use fake command hooks for WSL/Docker/runtime probes.
- Use `httptest.Server` for Streamable HTTP.
- Use small fake stdio servers where possible; avoid live `npx` in CI.
- Use grep/doc gates for PRD/requirements amendments.
- Live WhatsApp/mail/calendar checks are operator-only and recorded in quality snapshot, not CI.
