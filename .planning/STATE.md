# State

Date: 2026-05-07

Active milestone: v3.1 Agent Orchestration And System Prompt Versioning (active)

Last closed milestone: v1.3 Memory Consolidation And Quality

Current branch: `codex/v31-orchestration-hooks`

## Current Truth

v1.3 is closed. v3.1 implementation has started on the orchestration bridge. The Docker runtime passes memory closure, embedding/search validation, dashboard settings smoke, full Go/frontend verification, Docker rebuild, and the strict live memory quality gate.

Closure evidence:

- Memory closure audit: 18 wiki pages, 45 expected index docs, 45 actual index docs, `issues=0`.
- Hermetic memory quality: 20/20 with source/archive evidence and review-gated proposals.
- Qdrant comparison: Qdrant reachable at `127.0.0.1:6333`, recommendation `ok`, overlap 1.0 against local search.
- Live memory quality on DB-selected `deepseek/deepseek-v4-flash`: 20/20, `search_memory` 20/20, proposal calls 4/4 where expected, unexpected proposals 0, slow scenarios 0/20, max scenario 20.349 s under the 30 s budget.
- Dashboard settings now renders the `agent` orchestration group and keeps `SEARCH_BACKEND` as an enum combo.
- Release checks passed: `verify-go.ps1`, `go test ./... -count=1`, `npm --prefix web run i18n:check`, `npm --prefix web run build`, `docker compose config --quiet`, Docker rebuild, `/status`, and settings Playwright E2E.

The live quality harness now applies dashboard settings from `aura.db` before selecting the LLM, so future closure runs test the configured app model instead of stale env-only values.

v3.1 implementation evidence:

- `internal/orchestration` now exposes profile decisions with reasons, profile capability contracts, lifecycle hook primitives, hidden-tool policy, and trace redaction.
- Telegram debug smoke now records profile selection reason and hidden-tool rejection.
- `cmd/debug_telegram_sandbox` supports strict expectation flags for profile, no-tools, skill-read, swarm, and sandbox validation.
- `cmd/debug_orchestration` provides a non-live prompt routing harness.
- Focused gate passed: `go test ./internal/conversation ./internal/telegram ./internal/tools ./internal/toolsets ./internal/settings ./internal/api ./internal/orchestration ./cmd/debug_telegram_sandbox ./cmd/debug_files ./cmd/debug_orchestration -count=1`.
- Route probes passed for pipeline review (`swarm_research`) and computed CSV/chart (`sandbox_compute`).

## Next Milestone Handoff

v3.1 is the bridge milestone for "Agent Orchestration And System Prompt Versioning" before v4.0 expands MCP/plugin tools.

Canonical plan: `.planning/phases/04-agent-orchestration-system-prompt-versioning/PLAN.md`

The implementation direction is Claude Code style orchestration without a separate intent model: versioned prompt modules, runtime tool profiles, skill preflight, swarm-first broad synthesis, sandbox-first compute/artifact work, and per-turn tool/token/cost telemetry.

v4.0 is planned as "MCP Marketplace And Autonomous Plugin Manager" after v3.1.

Canonical plan: `.planning/phases/v4.0-mcp-plugin-marketplace/PLAN.md`

The user decisions for v4.0 are MCP marketplace, container MCP runtime, official MCP Registry plus local cache, review-gated security, and agent auto rollback for approved staged proposals. The implementation must keep plugin enablement review-gated and expose only enabled approved MCP tools to the agent.

## Recent Decisions

- Docker is the release runtime.
- Keep source/wiki cleanup automated through `clean_wiki_memory` and closure audits, not hand-edited wiki repair.
- Keep `SCHEMA.md`, `index.md`, and `log.md` out of user-facing wiki graph/search memory.
- Use dedicated `EMBEDDING_*` settings for wiki search; never fall back from embeddings to `LLM_API_KEY`.
- Treat dashboard settings stored in `aura.db` as authoritative for live debug scorecards when validating the running app.
- Proceed to v3.1 orchestration before implementing the v4.0 MCP/plugin marketplace.

## Resume Notes

- Start from `.planning/phases/04-agent-orchestration-system-prompt-versioning/PLAN.md`.
- Keep v4.0 MCP/plugin work blocked behind v3.1 tool-profile/prompt-versioning clarity.
- If adding new intake formats later, update source policy, Telegram validation, API acceptance, dashboard copy, extraction tests, and E2E together.
