# State

Date: 2026-05-06

Active milestone: v1.3 Memory Consolidation And Quality (planned)

Last closed milestone: v1.2 Source Intake Closure

Current branch: `codex/v1-2-closure-polish`

## Current Truth

v1.2 closure is implemented in the worktree and ready for final verification/ship decision. The shipped upload list is PDF, TXT, Markdown, JSON, CSV, XLSX, and DOCX.

The next milestone is memory consolidation and quality: clean the graph, remove generated/operational docs from agent memory, repair wiki links, and verify embedding-backed retrieval.

## Recent Decisions

- Keep v1.2 extraction narrow and auditable.
- Treat Microsoft MarkItDown as a future generalized converter candidate, not a v1.2 dependency.
- Use the local database-backed E2E token path for dashboard E2E.
- Preserve table semantics in the conversations panel and move keyboard activation onto a real button.
- Treat `docs/superpowers/` v1.2 generated plan/spec files as stale workflow artifacts; active planning belongs in `.planning/`.
- Keep `SCHEMA.md`, `index.md`, and `log.md` out of user-facing wiki graph/search memory.

## Resume Notes

- Start from `.planning/phases/03-memory-consolidation-quality/SPEC.md` for the next milestone.
- If adding new intake formats, update source policy, Telegram validation, API acceptance, dashboard copy, extraction tests, and E2E together.
