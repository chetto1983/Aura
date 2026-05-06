# State

Date: 2026-05-06

Active milestone: none

Last closed milestone: v1.2 Source Intake Closure

Current branch: `codex/v1-2-closure-polish`

## Current Truth

v1.2 closure is implemented in the worktree and ready for final verification/ship decision. The shipped upload list is PDF, TXT, Markdown, JSON, CSV, XLSX, and DOCX.

## Recent Decisions

- Keep v1.2 extraction narrow and auditable.
- Treat Microsoft MarkItDown as a future generalized converter candidate, not a v1.2 dependency.
- Use the local database-backed E2E token path for dashboard E2E.
- Preserve table semantics in the conversations panel and move keyboard activation onto a real button.

## Resume Notes

- Start from `.planning/phases/02-v1-2-closure-polish/VALIDATION.md` for the final gate history.
- If adding new intake formats, update source policy, Telegram validation, API acceptance, dashboard copy, extraction tests, and E2E together.
