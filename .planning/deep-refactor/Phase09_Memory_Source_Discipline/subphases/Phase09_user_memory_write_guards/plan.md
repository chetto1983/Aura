# Phase-Q: User Memory Write Guards — Plan

**Parent phase:** Phase 9 — Memory and Source Discipline  
**Status:** ✅ Closed 2026-05-16  
**Scope reference:** prd.md §5.7 lines 712-725 'Write policy'

## Goal

Close the two write-guard gates left open (deferred) by Phase-O:

1. **Capability check** (`memory.user.write`) at the `WriteApprovedUserFact` boundary
   — so that actors without the capability grant cannot write user_memory rows.
2. **Ambiguity question gate** — when the summarizer yields a `user_memory` candidate
   with `Score < 0.7`, pause and ask the operator before creating a `proposed_updates`
   row. The operator's reply approves, rejects, or reformulates.

Both gates were deferred at the end of Phase-O (US-O04) because they require
infrastructure wired in Phase 1B (identity/Authorize boundary) and Phase 1C
(chat_questions table + answer-callback hub), both now closed.

## Design Decisions

- **Threshold 0.7** is an empirical heuristic — stored as exported const
  `AmbiguityThreshold` for future tuning without recompile.
- **System actor** (nil or empty actor_id) is allowed back-compat: existing approval
  flows that do not carry an actor bypass the capability check. A migration (v16)
  adds `actor_id TEXT NOT NULL DEFAULT ''` to `proposed_updates` so new rows
  can be stamped with the originating actor.
- **No gate on wiki path or operational_memory path** — gate is user_memory only.
  Phase 6 operational_memory path operates under system actor by design.

## Stories

| Story | Description | SHA |
|-------|-------------|-----|
| US-Q01 | `memory.user.write` Authorize gate at `WriteApprovedUserFact` + migration v16 + 4-case tests | c0189ac4 |
| US-Q02 | Ambiguity question gate (`AmbiguityThreshold=0.7`, `ShouldGateUserMemoryWrite`) + answer callbacks + 5-case tests | 1133c03f |
| US-Q03 | Integration test: Q01 + Q02 end-to-end (in-memory SQLite, identity fixtures, slog capture) | 77837d57 |
| US-Q04 | Phase-Q closure docs + prd.md §6.5 / §7.2 / Phase 9 partial-close update | (this commit) |
