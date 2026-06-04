# MCP Sidecar Lifecycle — Study & Aura Recommendation

Date: 2026-06-04
Author: discuss/spike session for Phase 9 (Swarm Minimal) + OpenClaw-compat design review
Scope: how Aura should supervise MCP stdio servers and their companion daemons, and how that relates to the approved OpenClaw plugin-host design (`docs/superpowers/specs/2026-06-02-openclaw-plugin-compatibility-design.md`).

## Why this study

Phase 9's live E2E mounts two real third-party MCP servers (mail-mcp, whatsapp-mcp — spikes 001/002 VALIDATED). The whatsapp server needs an external companion daemon (whatsmeow bridge, REST :8080 + SQLite). Before planning Phase 9 we need a decided, minimal supervision model — and to confirm it does NOT drift into the heavy OpenClaw plugin-host machinery, which targets a different problem (untrusted third-party plugin code).

## Ground truth — Aura today

- `internal/mcp.Open` spawns one stdio subprocess per enabled server; `mcptools.MountServer` mounts its tools; a closer shuts it down at exit.
- `buildRegistryWithMCP` (`cmd/aura/main.go:104`) mounts **every** enabled server from the managed registry (`~/.aura/mcp/servers.json`, `internal/mcp/managed_config.go`), sorted by name.
- **NOT fail-soft (the gap):** on any single mount failure it calls `closeMCPServers(closers)` and `return nil, nil, err` (`main.go:122-124`); `bootChat` then prints `mcp:` + `os.Exit(1)` (`cmd/aura/chat.go:140-143`). One unreachable/misconfigured server aborts `aura chat` entirely.
- No health check, no restart, no lazy activation. `aura mcp doctor <name>` exists but is on-demand (open → list tools → close), one server at a time.
- Companion daemons: the whatsmeow bridge is started manually by the operator today (spike 002).

## Industrial findings (2026)

Full per-system survey with URLs in the session research log. Convergent results:

| System | Spawn timing | Crash handling | Health | Restart | Failure surfacing |
|---|---|---|---|---|---|
| Claude Code | eager at session start (5s timeout) | fail-soft, lazy reconnect on next tool call | none | **none auto** | `✗ Failed to connect` + tool error |
| Claude Desktop | eager | fail-soft, no reconnect | none | none auto | tool error |
| VS Code (GA 2025-07) | lazy + trust-gated; opt-in auto-start | fail-soft | none | update-survival auto-start; `dev.watch` only | UI error + trust dialog |
| Docker MCP Gateway | lazy on first tool call (per container) | container-isolated | not documented | "monitors" (container-level) | gateway routes around |
| HashiCorp go-plugin (OpenClaw's cited precedent) | eager on `Client.Start` | panic isolation (subprocess) | optional gRPC health svc | **none — host owns it** | `Exited()` / RPC error |

**MCP spec (2025-06-18):** mandates only the stdio shutdown order (close-stdin → SIGTERM → SIGKILL), per-request timeouts + cancellation, and an optional `ping` liveness request. Restart, health polling, eager-vs-lazy spawn = **host discretion, unspecified.**

**Two cross-industry constants:**
1. **Nobody auto-restarts** trusted first-party MCP servers — not Claude Code, not Claude Desktop, not VS Code, not even go-plugin (it gives isolation + a health primitive but explicitly leaves restart to the host).
2. The universal model is **fail-soft + lazy reconnect-on-next-use + clear surfacing**, never a supervisor goroutine. Real supervision only appears where it's free (Docker container boundaries).

**Companion daemons:** the MCP ecosystem is actively *collapsing the two-process bridge away* — Sealjay's and `whatsapp-mcp-go` forks are single-binary, daemon-free (serve starts on client connect, exits on disconnect). Where a daemon is unavoidable (browser instances), the MCP server manages it, not the host.

## Recommendation for Aura

### (a) v1 stdio-server supervision — minimal, do NOW in Phase 9
- **Fail-soft boot (DO — highest value).** A failed `Open`/`Mount` logs a WARN and drops that one server from the registry; boot continues. Fixes the `main.go:122-124` + `chat.go:140-143` hard-exit. Matches every client surveyed and satisfies CONTEXT D-21.
- **Lazy reconnect-on-use (DO — cheap, optional within Phase 9).** On a tool call whose backing process has exited, attempt one re-spawn+re-init, else a clean tool error. Exactly Claude Code's behavior; no background loop. Can also be deferred if it complicates the bridge seam — fail-soft boot is the must-have.
- **Periodic `ping` health ticker (SKIP).** No surveyed client does it; reconnect-on-use subsumes it; violates the mini-PC no-busy-poll rule (memory `feedback_minipc_cpu_budget`). Reserve `ping` for the on-demand `aura mcp doctor` probe only.
- **Restart-on-crash background supervisor (DON'T).** Industry — including go-plugin — deliberately omits it. Over-engineering trap.
- **Lazy mount (DEFER).** Eager-mount-all is fine at Aura's server count; add VS-Code/Gateway-style lazy first-use spawn only if boot time or idle subprocess count becomes a measured problem.

### (b) Companion daemons (whatsmeow bridge)
- **Preferred: compose service.** Aura already treats sidecars as compose services (postgres, neo4j, sandbox-agent). The bridge (REST :8080 + SQLite) is structurally identical → a compose service with a `healthcheck` + `depends_on: service_healthy`. Zero new supervision machinery, established pattern. The user's fork (`chetto1983/whatsapp-mcp@6de1dcd`) is the image source.
- **Worth weighing: eliminate the daemon** by adopting a single-binary whatsapp-mcp fork — deletes the bring-up/health problem entirely. Trade-off: our fork already carries the read-back persistence patch; a single-binary fork would need re-patching. Decision deferred to Phase 9 planning.
- **Fallback: operator-manual + `aura mcp doctor`** probe with precise remediation. Do NOT build an in-process supervisor for it.

### (c) Defer to the OpenClaw plugin-host phase
The OpenClaw design's typed RPC + handshake + mTLS + registry snapshots + per-plugin crash isolation + host-owned restart + gRPC health wiring are justified for **untrusted third-party plugin code**, and unjustified for Aura's own trusted boot-mounted MCP servers. Keep that machinery scoped to OpenClaw. Note even there, per go-plugin, **restart policy is host code you still write** — isolation ≠ restart.

## Relationship to the OpenClaw plugin-compat design

The OpenClaw spec (`docs/superpowers/specs/2026-06-02-openclaw-plugin-compatibility-design.md`) and this MCP-sidecar question are **adjacent but distinct**:

- **MCP servers (this study):** trusted, operator-registered, stdio, mounted via the existing managed config. v1 = fail-soft boot, nothing more.
- **OpenClaw plugins (that spec):** untrusted third-party modules in a Node sidecar with default-deny policy, capability allowlists, Postgres-audited mutations, and a control/data-plane split. A much larger, later capability (the spec itself says "larger than Slice 7 skills … new phase or PRD amendment").

The control/data-plane asymmetry (Go owns governance, the sidecar owns module loading) is the right north star for OpenClaw, but Phase 9 must NOT pull any of it forward — the existing `mcptools` seam + managed config + fail-soft boot is the whole v1 requirement. This is the "no atomic bombs" discipline (memory `feedback_no_atomic_bombs_minimal_industrial_shape`): the OpenClaw host is a real future phase, not a Phase-9 dependency.

## Net actions folded into Phase 9 CONTEXT (D-21)
1. Make `buildRegistryWithMCP` fail-soft (per-server WARN-and-drop), with a unit test that a bad server entry does not abort boot.
2. Bridged MCP tools mount `Deferred: true` (spike-001 finding; orthogonal but same boot path).
3. The whatsmeow bridge becomes a compose service (or a single-binary fork is adopted) — decided in planning; not an in-process supervisor.
4. OpenClaw plugin-host machinery stays out of Phase 9 — tracked as its own future capability/PRD amendment.
