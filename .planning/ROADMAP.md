# Roadmap

## Completed Milestones

### v1.2 Source Intake Closure

Status: closed

Closed on 2026-05-06 with truthful upload support for PDF, TXT, Markdown, JSON, CSV, XLSX, and DOCX across Telegram, API, source storage, extraction, wiki ingestion handoff, and dashboard copy.

Key outcomes:

- Shared upload-format policy across source, API, Telegram, and React UI.
- Normalized extraction evidence through `extract.md` and `extract.json`.
- PDF OCR adapted into the same evidence contract.
- XLSX extraction wired through Pyodide with mounted input files.
- DOCX extraction wired through Pyodide with bounded ZIP/XML/text limits.
- Extracted source ingestion closes the non-PDF path into wiki pages.
- Dashboard polish and E2E closure for source inbox, graph, conversations, settings, and adjacent panels.

### v1.3 Memory Consolidation And Quality

Status: closed

Closed on 2026-05-07 with deterministic memory cleanup, compact source anchors, healthy SQLite/Qdrant search, dashboard settings polish, and a strict live memory quality pass under the 30s latency budget.

Goal: make Aura's durable memory graph useful instead of merely populated. The milestone should remove operational/generated docs from user memory, consolidate orphan pages into hubs, repair broken wiki links, verify embedding/search wiring, and measure answer quality through `search_memory`.

Key outcomes:

- Deterministic memory cleanup is implemented and reproducible through `clean_wiki_memory` and nightly maintenance.
- Wiki graph excludes operational files such as `SCHEMA.md`, `index.md`, and `log.md`.
- Generated planning artifacts stay out of active docs; `.planning/` remains the workflow truth.
- Source wiki pages stay compact; raw OCR/extract text remains in source artifacts instead of duplicated embeddings.
- Memory closure audit reports 18 pages, 45 expected index docs, 45 actual index docs, and `issues=0`.
- Embedding configuration uses `EMBEDDING_API_KEY`, `EMBEDDING_BASE_URL`, and `EMBEDDING_MODEL`; it must not fall back to `LLM_API_KEY`.
- Qdrant sidecar search compares cleanly against local search with fallback intact.
- Hermetic scorecard passes 20/20.
- Live scorecard on DB-selected `deepseek/deepseek-v4-flash` passes 20/20 with `search_memory` on every scenario, 4/4 expected proposals, 0 unexpected proposals, and 0 slow scenarios over 30s.
- Settings exposes memory/search/orchestration controls, including `SEARCH_BACKEND` as a combo box and the `agent` settings group.

## Next Milestone

### v3.1 Agent Orchestration And System Prompt Versioning

Status: active

Plan: `.planning/phases/04-agent-orchestration-system-prompt-versioning/PLAN.md`

Goal: make Aura's main Telegram agent use versioned prompt composition, focused tool profiles, and first-class swarm/sandbox routing before the MCP marketplace expands the tool surface.

Success criteria:

- Prompt composition logs version, modules, hash, and active tool profile.
- Telegram exposes only the active profile's tools to the LLM.
- Skills, swarm, and sandbox are first-class route guidance, not incidental tools.
- Swarm research follows the Hermes-style bounded delegation plan: narrow parent tools, capped workers, stale alias rejection, terminal finalization, and worker token/cost aggregation.
- Codex-style skill orchestration is planned as the final v3.1 hardening slice: capability taxonomy, required skill preflight, profile loop policies, and route evals before v4.0 expands MCP/plugin tools.
- Docker sandbox route uses the `pyodide` sidecar and does not require Node or Pyodide inside the Aura image.
- Debug Telegram smoke reports exposed tools, called tools, tokens, estimated context, and cost.
- Document, sandbox-compute, memory, and swarm-research prompts each choose the expected profile in tests.

Current closure evidence:

- Deterministic route evals pass for pipeline audit, memory answer, document summary, CSV/chart compute, dashboard settings, Docker release, MCP plugin proposal, and ordinary answers.
- Live swarm smoke passes with terminal swarm, one bounded worker, no post-swarm parent tool calls, token/cost metrics, no stale skill refs, and elapsed time under 30s.
- Live sandbox smoke passes with `list_skills -> read_skill(aura-python-sandbox) -> execute_code`, artifact persistence, token/cost metrics, no stale skill refs, and elapsed time under 30s.
- Settings Playwright E2E passes and preserves orchestration enum/numeric values.

Open blocker:

- Broad live document-summary generation is functional but not closure-clean yet. It can create and deliver DOCX files after reading `docx`, but the configured live model keeps expanding evidence before `create_docx`, causing >30s latency and occasional malformed tool JSON. v3.1 remains active until the document route is bounded with compact evidence or an equivalent deterministic helper.

### v4.0 MCP Marketplace And Autonomous Plugin Manager

Status: planned

Plan: `.planning/phases/v4.0-mcp-plugin-marketplace/PLAN.md`

Goal: make Aura's plugin system MCP-first, Docker-first, review-gated, and agent-auditable.

Success criteria:

- Aura syncs the official MCP Registry into a local cache without blocking startup.
- Aura can install MCP plugins as managed container sidecars or remote HTTP connections.
- Failed plugin installs smoke-test and roll back automatically without breaking existing tools.
- Dashboard review gates plugin install, update, enable, disable, and rollback operations.
- Enabled approved MCP tools are exposed to the agent with stable `mcp_<server>_<tool>` names.
- Dashboard shows Marketplace, Installed plugins, Health, and Review Queue views.
- Docker smoke validates registry sync, fake MCP install, enable, invoke, and rollback.
