# MCP Sidecar Supervision — Study

Date: 2026-06-04
Status: Research complete — feeds Phase 9 planning + the OpenClaw plugin-host capability decision
Companions: `docs/superpowers/specs/2026-06-02-openclaw-plugin-compatibility-design.md`, `.planning/spikes/001-002` (live MCP mounts validated)

## Why now

Spikes 001/002 proved the `internal/mcp` + `mcptools` seam live (mail-mcp, whatsapp-mcp) and exposed the operational gap: MCP servers **are** sidecars, but Aura supervises them with exactly zero policy — and the WhatsApp bridge is a companion daemon the operator must start by hand. Phase 9's live E2E depends on both. Meanwhile the approved OpenClaw design specifies a *supervised* Node sidecar (health, restart, lazy activation) for plugins. This study answers: how much of that machinery do the **trusted first-party MCP sidecars** need, and what stays exclusive to the **untrusted plugin host**?

## Current state (ground truth, 2026-06-04)

| Aspect | Today | Where |
|---|---|---|
| Spawn timing | Eager: every enabled server in `~/.aura/mcp/servers.json` spawns at boot | `buildRegistryWithMCP`, `cmd/aura/main.go:104` |
| Boot failure | **FAIL-HARD**: any server error → `os.Exit(1)` — a typo'd entry or dead bridge kills `aura chat` entirely | `cmd/aura/chat.go:139-144`; `buildRegistryWithMCP` is all-or-nothing (`main.go:121-124`) |
| Mid-session crash | Tool call returns an error; **no reconnect, no respawn** | `mcptools/bridge.go` → `mcp.Client.CallTool` |
| Health | On-demand only: `aura mcp doctor <name>` = spawn + ListTools | `cmd/aura/mcp.go:195` |
| Shutdown | Closers run at exit (reverse order) | `closeMCPServers`, `main.go:131` |