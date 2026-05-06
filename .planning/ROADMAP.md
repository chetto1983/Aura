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

## Next Milestone

### v1.3 Memory Consolidation And Quality

Status: validation

Goal: make Aura's durable memory graph useful instead of merely populated. The milestone should remove operational/generated docs from user memory, consolidate orphan pages into hubs, repair broken wiki links, verify embedding/search wiring, and measure answer quality through `search_memory`.

Current validation state:

- Deterministic memory cleanup is implemented and reproducible through `clean_wiki_memory` and nightly maintenance.
- Live wiki dry-run hygiene reports 17 pages, 0 broken links, and 0 orphans.
- Hermetic memory quality scorecard passes 20/20 with wiki, source, and archive evidence.
- Live LLM scorecard still misses the 30s latency budget on `glm-5.1:cloud`, even though tool routing is correct.

First success criteria:

- Wiki graph excludes operational files such as `SCHEMA.md`, `index.md`, and `log.md`.
- Generated planning artifacts stay out of active docs; `.planning/` remains the workflow truth.
- Existing wiki pages either connect to a useful hub or are archived/deleted when they are low-value test debris.
- The agent can run `clean_wiki_memory` to reproduce the graph cleanup loop automatically, with dry-run output before write mode, and nightly wiki maintenance runs the same deterministic cleanup before lint/defer handling.
- Broken links and obsolete aliases are repaired or intentionally replaced by real pages.
- Embedding configuration uses `EMBEDDING_API_KEY`, `EMBEDDING_BASE_URL`, and `EMBEDDING_MODEL`; it must not fall back to `LLM_API_KEY`.
- Embed cache behavior is covered by tests and visible through dashboard health.
- `search_memory` quality is evaluated against real Aura/project questions, with proposals staying review-gated and evidence-backed.

Remaining before closure:

- Choose a faster live model/runtime path or split the live answer path so `search_memory` responses stay under the 30s end-user budget.

Deferred from this milestone:

- New source formats and cloud connectors.
- MarkItDown integration.
- Large UI redesigns beyond memory-quality observability.

## Planned Next After Current Release Closure

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
