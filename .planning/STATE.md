# State

Date: 2026-05-07

Active milestone: v3.1 Agent Orchestration And System Prompt Versioning (active)

Last closed milestone: v1.3 Memory Consolidation And Quality

Current branch: `codex/v31-closure-gate`

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
- Docker sandbox execution now uses the `pyodide` sidecar via `SANDBOX_RUNTIME_MODE=container` and `SANDBOX_RUNTIME_URL=http://pyodide:8787`; the Aura image no longer installs Node or embeds `/app/runtime/pyodide`.
- `execute_code` uses import-driven Pyodide package loading, so trivial Python calls avoid the old full office/data package load. Live Compose tool smoke returned `5050` with `elapsed_ms=4`; artifact smoke persisted CSV and PNG outputs.
- SQLite remains canonical state. MongoDB was evaluated and deferred until repository metrics justify an optional adapter for high-volume archives/traces/audit logs.
- Focused gate passed: `go test ./internal/conversation ./internal/telegram ./internal/tools ./internal/toolsets ./internal/settings ./internal/api ./internal/orchestration ./cmd/debug_telegram_sandbox ./cmd/debug_files ./cmd/debug_orchestration -count=1`.
- Route probes passed for pipeline review (`swarm_research`) and computed CSV/chart (`sandbox_compute`).
- Codex-style skill orchestration phases 5-7 are implemented:
  - debug route reports include profile, capabilities, exposed tools, skill reads, hidden-tool rejection, loop steps, terminal tool, token/cost, and trace fields;
  - dashboard settings expose orchestration controls as combo boxes/bounded numeric inputs;
  - deterministic route evals cover common prompts and assert hidden tools/stale refs;
  - skill-preflight misses are retryable so the model can read the suggested skill and retry in the same turn;
  - `aura-python-sandbox` satisfies sandbox preflight;
  - terminal `execute_code` and typed file tools return directly from tool output instead of spending another LLM call.
- Auto-low-risk memory capture now blocks obvious address/fuel-card/personal facts from automatic wiki writes and routes them to review.

Open v3.1 orchestration blocker:

- Live swarm route originally selected the right `swarm_research` profile but continued extra parent tool reads after `run_aurabot_swarm`, hit max-loop behavior, and did not report token/cost metrics. The Hermes-style hardening slice now passes its focused live smoke with runtime-launched terminal swarm, one default worker, worker failures `0`, token/cost metrics present, and elapsed time under 30 seconds.
- Live sandbox route now passes under 30s with `list_skills -> read_skill(aura-python-sandbox) -> execute_code`, artifact persistence, terminal tool finalization, token/cost metrics, and no stale skill refs.
- Live document route remains the active closure blocker. It can read `docx`, create, persist, and deliver DOCX files, but the broad "riepilogo documenti e note" prompt still expands evidence too much on the configured live model, exceeding 30s and sometimes producing malformed `create_docx` JSON. v3.1 stays active until this route gets compact bounded evidence or a deterministic document-summary helper.
- Canonical sub-plan: `.planning/phases/04-agent-orchestration-system-prompt-versioning/HERMES_DELEGATION_PLAN.md`
- Current closure sub-plan: `.planning/phases/04-agent-orchestration-system-prompt-versioning/CODEX_SKILL_ORCHESTRATION_PLAN.md`

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
- For the current swarm loop/latency blocker, start from `.planning/phases/04-agent-orchestration-system-prompt-versioning/HERMES_DELEGATION_PLAN.md` before touching code.
- For the next Codex-style route hardening slice, start from `.planning/phases/04-agent-orchestration-system-prompt-versioning/CODEX_SKILL_ORCHESTRATION_PLAN.md`. It records the online research, best skills to use, capability taxonomy, required skill preflight, loop policies, dashboard settings, and E2E closure phases.
- Before closing v3.1, finish the document-route blocker recorded in `.planning/phases/04-agent-orchestration-system-prompt-versioning/VALIDATION.md`, then rerun the full release gate.
- Keep v4.0 MCP/plugin work blocked behind v3.1 tool-profile/prompt-versioning clarity.
- For v4.0 sidecars, copy the Pyodide pattern: keep runtime bloat out of the Aura image, use internal service URLs, health checks, pinned images where possible, and rollback-friendly managed config.
- If adding new intake formats later, update source policy, Telegram validation, API acceptance, dashboard copy, extraction tests, and E2E together.
