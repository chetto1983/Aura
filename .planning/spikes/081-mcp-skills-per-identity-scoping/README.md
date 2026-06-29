---
spike: 081
name: mcp-skills-per-identity-scoping
type: standard
validates: "Given the managed MCP config and the skills dir, when both are scoped via identityctx and routed through the identity's sandbox, then identity B's MCP servers, skills, and audit are isolated from identity A — no RBAC, capability_grants-based"
verdict: VALIDATED
related: [001, 032, 064, 003, 005, 078, 080]
tags: [mcp, skills, multi-user, per-identity, identityctx, phase-36, v2.0.0]
---

# Spike 081: MCP + Skills Per-Identity Scoping

## What This Validates

Given the managed MCP config (proven single-user in 001/032/064) and the skills dir + audit (003/005), **when both are scoped per identity via `identityctx` and execution is routed through the identity's sandbox (078)**, then identity B's MCP servers, skills, and audit are isolated from A — identity isolation only, no RBAC, authz stays `capability_grants`-based.

## Current model (cited — from the v2.0.0 quality audit, `docs/audit/quality/`)

- **MCP:** managed config is **process-global** (`~/.aura/mcp/servers.json`); `internal/mcp/manager` builds one runtime registry; `internal/agent/mcptools/mount.go` mounts it for the whole daemon. Trust/audit (`mcp_audit`) are global. (Quality audit QA-C-03 also flags duplicate trust-normalization across two packages → unify in Phase 38.)
- **Skills:** skills dir (`$AURA_SKILLS_DIR`), snippet store (`~/.aura/pyscripts/`), `skill_audit`, and the approval queue are process-global; `cmd/aura/serve_adapters.go:newSkillTool` builds one loader/writer.
- Per-identity `Agent.md` profile already exists at `~/.aura/agents/<identity>/` (spikes 036–039) — the precedent for per-identity filesystem rooting.

## Recommended per-identity design (minimal-industrial-form)

**MCP — shared catalog, per-identity enable + scoped instances routed through the sandbox:**
- Keep ONE catalog/recipe set (shared, read-only). Make **enablement + trust + audit per-identity**: `~/.aura/mcp/<identity>/servers.json` (mirrors the Agent.md per-identity rooting). `mcptools.MountForIdentity(identityctx)` builds the registry from the identity's config.
- **stdio MCP servers (local commands) run INSIDE the identity's sandbox (078)** — not as host daemons — so a per-identity MCP server is automatically isolated and counts against that identity's box, not the host. Streamable-HTTP MCP (e.g. agent-memory 032) stays shared but is **called with the identity as a scope key** (the memory subgraph is already identity/session-keyed per 032/034).
- Avoids N×M host subprocesses (the operator's over-engineering line): shared catalog + per-identity enable + in-sandbox execution.

**Skills — per-identity dir + audit, shared built-ins:**
- Per-identity skills root `$AURA_SKILLS_DIR/<identity>/` + per-identity snippet store `~/.aura/pyscripts/<identity>/` + per-identity `skill_audit` rows (identity-keyed) + per-identity approval queue. Built-in/materialized skills stay shared read-only.
- `newSkillTool` resolves the identity's dir from `identityctx`; **snippets execute inside the identity's sandbox (078)** → per-identity skill execution isolation falls out of the sandbox boundary for free.

## Results

**VALIDATED ✓** (design, grounded in the live code + prior spikes): both subsystems isolate cleanly by (a) per-identity filesystem rooting (same pattern as the validated `Agent.md` store) for config/dir/audit, and (b) routing execution through the per-identity sandbox (078) so running MCP servers + snippets inherit the box's isolation. No RBAC; `capability_grants` gates per-route as today.

## Migration touchpoints (Phase 36)

- New per-identity roots: `~/.aura/mcp/<identity>/`, `$AURA_SKILLS_DIR/<identity>/`, `~/.aura/pyscripts/<identity>/`.
- `mcptools.MountForIdentity` + `newSkillToolForIdentity` (additive, `local` fallback for CLI/no-principal — same shape as the F-028 `*ForIdentity` store methods in Phase 36).
- `mcp_audit` + `skill_audit` rows gain an identity column (migration; or reuse existing identity FK if present).
- Ties to 078 (execution boundary) and 080 (the sandbox's per-identity volume is where in-sandbox MCP/skill state lives).

## Open Questions

- Shared streamable-HTTP MCP (agent-memory) under multi-user: confirm the memory MCP's own scope keys fully isolate per-identity recall (032/034 flagged long-term semantic dedup is NOT provenance-safe → Aura-side identity scope key is mandatory, not upstream dedup). Needs a 2-identity recall test at Phase-36 impl.
- Whether per-identity stdio MCP servers should be lazy-started (on first tool use) vs eager at box create — lean lazy (cost). Defer to impl benchmark.
