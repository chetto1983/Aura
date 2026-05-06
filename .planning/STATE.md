# State

Date: 2026-05-06

Active milestone: v1.3 Memory Consolidation And Quality (validation)

Last closed milestone: v1.2 Source Intake Closure

Current branch: `master`

## Current Truth

v1.2 closure is merged to `master`. The shipped upload list is PDF, TXT, Markdown, JSON, CSV, XLSX, and DOCX.

v1.3 memory consolidation has deterministic cleanup and hermetic quality gates in place: operational wiki files are excluded from memory, source wiki pages stay compact, `clean_wiki_memory` can reproduce graph cleanup automatically, and the live wiki currently reports 17 pages, 0 broken links, and 0 orphan pages.

The remaining release caveat is live LLM latency, not tool routing. With `LLM_MODEL=glm-5.1:cloud`, `debug_memory_quality -live-llm` called `search_memory` for every scenario and kept proposals review-gated, but missed the 30s end-user budget on several scenarios.

## Recent Decisions

- Keep v1.2 extraction narrow and auditable.
- Treat Microsoft MarkItDown as a future generalized converter candidate, not a v1.2 dependency.
- Use the local database-backed E2E token path for dashboard E2E.
- Preserve table semantics in the conversations panel and move keyboard activation onto a real button.
- Treat `docs/superpowers/` v1.2 generated plan/spec files as stale workflow artifacts; active planning belongs in `.planning/`.
- Keep `SCHEMA.md`, `index.md`, and `log.md` out of user-facing wiki graph/search memory.
- Treat `glm-5.1:cloud` live-memory latency as the next quality blocker before calling v1.3 fully closed.

## Resume Notes

- Start from `.planning/phases/03-memory-consolidation-quality/VALIDATION.md` for the next milestone decision.
- If adding new intake formats, update source policy, Telegram validation, API acceptance, dashboard copy, extraction tests, and E2E together.
