---
spike: 064
name: calendar-mcp-http-mount
type: standard
validates: "Given the calendar-mcp Linux HTTP sidecar container, when Aura's Go streamable-HTTP MCP client opens + lists + calls + mounts it, then .NET-MCP-over-HTTP interops with Aura's client: initialize, tools/list, real tools/call, and mcptools mount (namespaced calendar__*, Deferred) all succeed"
verdict: VALIDATED
related: [063, 066, 032]
tags: [calendar, mcp, mount, dotnet-interop, http, sidecar, phase-0-gate]
---

# Spike 064: calendar-mcp-http-mount (Phase 0 interop gate)

## What This Validates

The architecture-deciding gate from the `aura-pim-mcp` fork plan: Aura's Go MCP client was
proven against the .NET server over **stdio** (spike 063) and against Python FastMCP over
**streamable-HTTP** (spike 032), but **.NET over HTTP** — the production sidecar shape — was
unproven. This spike runs the **Linux HTTP sidecar container** (the real multi-host deployment
target, not the Windows dev binary) and drives it through Aura's real `internal/mcp` +
`internal/agent/mcptools` path.

## How to Run

```powershell
# build + run the Linux sidecar (upstream Dockerfile; gate needs no fork changes)
docker build -t aura-pim-mcp:gate -f D:\tmp\calendar-mcp\Dockerfile D:\tmp\calendar-mcp
docker run -d --rm --name aura-pim-gate -p 127.0.0.1:8093:8080 -e CALENDAR_MCP_ADMIN_TOKEN=aura-dev-admin-token aura-pim-mcp:gate
```
```bash
# Aura Go harness (reaches Docker Desktop's published port on 127.0.0.1)
go run ./.planning/spikes/064-calendar-mcp-http-mount
```

## What to Expect

Forensic log: `initialize OK` → `ping OK` → `tools/list=29` → scaffold tools present →
`list_accounts -> {"accounts": []}` → mount 29 `calendar__*` Deferred → `SUMMARY VALIDATED`.

## Investigation Trail

- **Runtime correction:** the gate was re-pointed from a Windows self-contained binary to the
  **Linux Docker sidecar** after the operator clarified Aura is multi-host (the appliance is
  Linux). The Windows binary (spike 066) was only a dev convenience for the OAuth E2E.
- Container internal port is **8080** (`Dockerfile` `ASPNETCORE_URLS=http://+:8080`); mapped to
  host `127.0.0.1:8093`. MCP endpoint path is **`/`** (server log: `MCP endpoint: /`); health at
  `/health` returns `{"status":"healthy"}`.
- Ran with an **empty config** (zero accounts) — sufficient for the interop gate; account
  connect is Phase 3–4 (admin REST API).

## Results

**VALIDATED ✓ — GATE GREEN.** `.NET MCP-over-HTTP ↔ Aura Go streamable-HTTP client` interops fully.

| Link | Evidence |
|---|---|
| `initialize` (streamable-HTTP, .NET) | OK via `mcp.OpenServer(ServerTypeStreamableHTTP)` |
| `ping` | OK |
| `tools/list` | 29 tools; all 4 scaffold-expected (`list_accounts`/`get_calendar_events`/`create_event`/`send_email`) present |
| real `tools/call` | `list_accounts` → `{"accounts": []}` |
| tool-error propagation | `get_calendar_events` (zero accounts) → clean MCP tool error surfaced through the HTTP transport (model-self-correctable) |
| `mcptools.MountManagedServer` | 29 tools mounted, **namespaced `calendar__*`**, **all `Deferred`**, count == advertised (no filter) |

**Gate decision:** GREEN → the `aura-pim-mcp` agent mount uses **streamable-HTTP** (no stdio
fallback). DenyRisk=write filtering + the trimmed surface are applied at the fork (server-side
tool trim) + Aura mount layer in later phases.

**Note:** the `mcptools` mount applies **no** DenyRisk filter here (mounted == advertised, by
design for the gate); the destructive-tool gating is validated when the fork's trimmed surface +
the mount policy land in Phases 2–3.
