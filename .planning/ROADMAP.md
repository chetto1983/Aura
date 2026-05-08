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

### v3.1 Agent Runtime Stabilization

Status: closed

Primary blocker evidence: `.planning/phases/04-agent-orchestration-system-prompt-versioning/VALIDATION.md`

Goal: finish stabilizing the simplified Codex/Picobot-style runtime before expanding the tool surface with MCP plugins.

Success criteria:

- Aura uses a compact generic agent loop instead of Telegram-owned god-class orchestration.
- The live tool surface is small and file-first: `default`, `compute`, `document`, and `admin`.
- Bounded workspace file tools are the default filesystem interface, rooted at `/workspace` in Docker.
- Legacy wiki/skill/proposal wrapper tools stay deleted from the LLM surface.
- Skills remain available as file-backed procedures, not ritual preflight blockers.
- Swarm, sandbox, and document tools terminate cleanly instead of spending extra LLM passes.
- Debug smokes report tools, token/cost metrics, loop steps, latency, and runtime root.
- Common live routes stay under the accepted 30s budget.

Closure evidence:

- Phase 05 simplification is implemented and merged to `master`.
- Phase 06 filesystem-first wiki/skills cleanup is implemented.
- Phase 07 runtime workspace, graph cache, Memory Pack, and live benchmark work is implemented.
- Docker runtime cleanup is implemented: `/app` no longer exposes the developer repo; `/workspace` is the runtime root.
- Live swarm/wiki/graph and skill discovery smokes pass under the 30s target after latency repair.

Carry-forward blocker:

- Broad live document-summary generation is functional but not closure-clean yet. It can create and deliver DOCX files, but the configured live model still expands evidence too much before `create_docx`, causing >30s latency and occasional malformed tool JSON. Phase 08 treats this as one symptom of the broader runtime diet and retrieval problem.

### v3.2 Runtime Diet

Status: closed

Closed on 2026-05-08 with the Runtime Diet hot path in place and verified in Docker.

Plan: `.planning/phases/08-runtime-diet-embedding-retrieval/PLAN.md`

Goal: make Aura fast and intelligent by deleting useless runtime complexity, reducing the loop to Picobot-style basics, and using retrieval only when it helps the current answer.

Success criteria:

- Aura no longer emits the hardcoded stopped-before-final-answer fallback after successful tool work.
- Always-on speculative Memory Pack injection is gone from generic turns.
- Retrieval is routing-aware: `minimal`, `retrieve`, and `produce`.
- Embeddings are used through calibrated hybrid retrieval rather than raw top-K trust.
- Qdrant merges or falls back to local search on low-confidence results.
- Source/archive memory uses compact indexed facts, not raw OCR dumps or entire chat archives.
- Document generation consumes one compact evidence capsule before typed file tools.
- Common live routes stay under 30s without repeated broad file/source/archive loops.

Closure evidence:

- The old stopped-before-final-answer fallback is removed.
- Always-on Memory Pack injection is replaced by routing-aware Retrieval Capsule injection.
- Profile/preflight taxonomy and legacy routing aliases are deleted from live code.
- `search_memory` uses compact source/archive/proposal facts in SQLite FTS plus optional Qdrant mirror.
- Qdrant compact memory uses `aura_memory_v1_compact`, separate from wiki vectors.
- Compact mirror sync runs as background maintenance with adaptive batch embeddings.
- Archive append and cleanup mirror compact memory and Qdrant points.
- Document route E2E passed with one LLM call, one tool call, one loop step, and `create_docx` as terminal tool.

## Active Milestone

No active milestone is selected. v4.0 is planned and Runtime Diet no longer blocks it.

## Next Milestone

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
