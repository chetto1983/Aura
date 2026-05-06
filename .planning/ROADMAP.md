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

Status: planned

Goal: make Aura's durable memory graph useful instead of merely populated. The milestone should remove operational/generated docs from user memory, consolidate orphan pages into hubs, repair broken wiki links, verify embedding/search wiring, and measure answer quality through `search_memory`.

First success criteria:

- Wiki graph excludes operational files such as `SCHEMA.md`, `index.md`, and `log.md`.
- Generated planning artifacts stay out of active docs; `.planning/` remains the workflow truth.
- Existing wiki pages either connect to a useful hub or are archived/deleted when they are low-value test debris.
- The agent can run `clean_wiki_memory` to reproduce the graph cleanup loop automatically, with dry-run output before write mode, and nightly wiki maintenance runs the same deterministic cleanup before lint/defer handling.
- Broken links and obsolete aliases are repaired or intentionally replaced by real pages.
- Embedding configuration uses `EMBEDDING_API_KEY`, `EMBEDDING_BASE_URL`, and `EMBEDDING_MODEL`; it must not fall back to `LLM_API_KEY`.
- Embed cache behavior is covered by tests and visible through dashboard health.
- `search_memory` quality is evaluated against real Aura/project questions, with proposals staying review-gated and evidence-backed.

Deferred from this milestone:

- New source formats and cloud connectors.
- MarkItDown integration.
- Large UI redesigns beyond memory-quality observability.
